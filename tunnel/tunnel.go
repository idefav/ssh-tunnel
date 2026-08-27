package tunnel

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

var (
	NetworkError         = errors.New("network error")
	SSHReconnectRequired = errors.New("ssh reconnect required")
	SSHDialError         = errors.New("ssh dial error")
)

type KeepAliveConfig struct {
	// Interval is the amount of time in seconds to wait before the
	// tunnel client will send a keep-alive message to ensure some minimum
	// traffic on the SSH connection.
	Interval uint

	// CountMax is the maximum number of consecutive failed responses to
	// keep-alive messages the client is willing to tolerate before considering
	// the SSH connection as dead.
	CountMax uint
}

type Tunnel struct {
	enableSocks5           bool
	enableHttp             bool
	enableHttpBasic        bool
	enableHttpOverSSH      bool
	enableHttpDomainFilter bool
	httpLocalAddress       string
	httpBasicUserName      string
	httpBasicPassword      string
	serverAddress          string
	localAddress           string
	user                   string
	auth                   []ssh.AuthMethod
	hostKeys               ssh.HostKeyCallback
	domains                map[string]bool
	domainMatchCache       map[string]bool
	domainMutex            sync.RWMutex
	appConfig              *cfg.AppConfig

	retryInterval                time.Duration
	keepAlive                    KeepAliveConfig
	sshDialTimeout               time.Duration
	sshDestTimeout               time.Duration
	reconnectMaxRetries          int
	reconnectMaxInterval         time.Duration
	needReBind                   bool
	client                       *ssh.Client
	sshConnectedOnce             bool
	reconnectCount               uint64
	consecutiveReconnectFailures uint64
	lastReconnectError           string
	lastReconnectAt              time.Time
	lastReconnectFailureAt       time.Time
	tunnelCtx                    context.Context
	reconnectMutex               sync.Mutex // 添加重连锁，确保同一时间只有一个重连过程
	reconnecting                 bool
	reconnectDone                chan struct{}
	sshDialFn                    func() (*ssh.Client, error)

	proxyUploadBytes   uint64
	proxyDownloadBytes uint64
	activeProxyConns   int64
	acceptErrors       uint64
	listenerRestarts   uint64
	proxyStatsMutex    sync.Mutex
	proxyLastAt        time.Time
	proxyLastUpload    uint64
	proxyLastDownload  uint64
	proxyUploadBps     float64
	proxyDownloadBps   float64

	speedTestMu     sync.Mutex
	activeSpeedTest *speedTest

	requestTracker     *ProxyRequestTracker
	requestTrackerOnce sync.Once

	exitInfoMu        sync.RWMutex
	exitInfoRefreshMu sync.Mutex
	lastExitIPInfo    ExitIPInfo

	sshHealthMutex         sync.Mutex
	sshClientGeneration    uint64
	sshDialTimeoutWindowAt time.Time
	sshDialTimeoutStreak   uint64
	lastSSHFailureClass    string

	sshPoolMu                    sync.Mutex
	sshPool                      []*SSHPoolMember
	sshPoolEvicted               []*SSHPoolMember
	sshPoolSize                  int
	sshPoolReplenishInterval     time.Duration
	sshPoolBalanceStrategy       string
	sshProbeURL                  string
	sshProbeURLs                 string
	sshProbeTimeout              time.Duration
	sshProbeFailureThreshold     uint64
	sshSuspectCooldown           time.Duration
	proxyRetryMaxAttempts        int
	proxyRetryInitialBufferBytes int
	reconnectRecoveryPending     bool

	// Profile-based domain routing
	routeMatcher     *RouteMatcher
	profileTunnelMgr *ProfileTunnelManager
	profileID        string // profileID for this tunnel instance (empty for main tunnel)
	trafficStore     *TrafficStore
	routeDialer      func(profileID, host string, retryState *requestRetryState) (destinationConn, error)
}

type ProxyMetrics struct {
	UploadBytesTotal   uint64  `json:"uploadBytesTotal"`
	DownloadBytesTotal uint64  `json:"downloadBytesTotal"`
	UploadBps          float64 `json:"uploadBps"`
	DownloadBps        float64 `json:"downloadBps"`
	ActiveProxyConns   int64   `json:"activeProxyConns"`
}

type SSHConnectionStats struct {
	ConnectionCount              int               `json:"connectionCount"`
	ReconnectCount               uint64            `json:"reconnectCount"`
	ConsecutiveReconnectFailures uint64            `json:"consecutiveReconnectFailures"`
	LastReconnectError           string            `json:"lastReconnectError,omitempty"`
	LastReconnectAt              time.Time         `json:"lastReconnectAt,omitempty"`
	LastReconnectFailureAt       time.Time         `json:"lastReconnectFailureAt,omitempty"`
	SSHHealthy                   bool              `json:"sshHealthy"`
	SSHClientGeneration          uint64            `json:"sshClientGeneration"`
	SSHDialTimeoutStreak         uint64            `json:"sshDialTimeoutStreak"`
	LastSSHFailureClass          string            `json:"lastSSHFailureClass,omitempty"`
	PoolSize                     int               `json:"poolSize"`
	HealthyCount                 int               `json:"healthyCount"`
	SuspectCount                 int               `json:"suspectCount"`
	ProbingCount                 int               `json:"probingCount"`
	ReconnectingCount            int               `json:"reconnectingCount"`
	EvictedCount                 int               `json:"evictedCount"`
	PoolMembers                  []SSHPoolSnapshot `json:"poolMembers,omitempty"`
}

type ListenerStats struct {
	AcceptErrors     uint64 `json:"acceptErrors"`
	ListenerRestarts uint64 `json:"listenerRestarts"`
}

type ExitIPInfo struct {
	Available    bool      `json:"available"`
	IP           string    `json:"ip,omitempty"`
	City         string    `json:"city,omitempty"`
	Region       string    `json:"region,omitempty"`
	Country      string    `json:"country,omitempty"`
	Location     string    `json:"location,omitempty"`
	Organization string    `json:"organization,omitempty"`
	Timezone     string    `json:"timezone,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
	Error        string    `json:"error,omitempty"`
}

type destinationConn struct {
	conn                net.Conn
	sshClient           *ssh.Client
	sshMemberID         uint64
	profileID           string
	viaSSH              bool
	generation          uint64
	retryInfo           SSHRetryInfo
	routeID             string
	routeStrategy       string
	attemptedProfileIDs []string
}

const (
	requestPhaseDial         = "dial"
	requestPhaseProxying     = "proxying"
	requestPhaseInitialWrite = "initial_write"

	failureClassSSHTransportDead  = "ssh_transport_dead"
	failureClassSSHChannelTimeout = "ssh_channel_timeout"
	failureClassDestTimeout       = "dest_timeout"
	failureClassDestReset         = "dest_reset"
	failureClassClientClosed      = "client_closed"
	failureClassUnknown           = "unknown"

	sshDialTimeoutWindow = 5 * time.Second
	sshDialTimeoutLimit  = 3
)

func (t *Tunnel) nextSSHGeneration() uint64 {
	t.sshHealthMutex.Lock()
	defer t.sshHealthMutex.Unlock()
	t.sshClientGeneration++
	return t.sshClientGeneration
}

func (t *Tunnel) resetSSHFailureState() {
	t.sshHealthMutex.Lock()
	t.sshDialTimeoutWindowAt = time.Time{}
	t.sshDialTimeoutStreak = 0
	t.lastSSHFailureClass = ""
	t.sshHealthMutex.Unlock()
}

func (t *Tunnel) classifyDialError(err error, viaSSH bool) string {
	if err == nil {
		return ""
	}
	if isSSHReconnectError(err) {
		return failureClassSSHTransportDead
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if viaSSH {
			return failureClassSSHChannelTimeout
		}
		return failureClassDestTimeout
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		if viaSSH {
			return failureClassSSHChannelTimeout
		}
		return failureClassDestTimeout
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "reset by peer"), strings.Contains(msg, "connection reset"):
		return failureClassDestReset
	case strings.Contains(msg, "client disconnected"), strings.Contains(msg, "use of closed network connection"):
		return failureClassClientClosed
	default:
		return failureClassUnknown
	}
}

func (t *Tunnel) recordSSHDialSuccess() {
	t.resetSSHFailureState()
}

func (t *Tunnel) recordSSHDialFailure(err error) (string, bool) {
	class := t.classifyDialError(err, true)
	t.sshHealthMutex.Lock()
	defer t.sshHealthMutex.Unlock()

	t.lastSSHFailureClass = class
	if class != failureClassSSHChannelTimeout {
		t.sshDialTimeoutWindowAt = time.Time{}
		t.sshDialTimeoutStreak = 0
		return class, false
	}

	now := time.Now()
	if t.sshDialTimeoutWindowAt.IsZero() || now.Sub(t.sshDialTimeoutWindowAt) > sshDialTimeoutWindow {
		t.sshDialTimeoutWindowAt = now
		t.sshDialTimeoutStreak = 1
	} else {
		t.sshDialTimeoutStreak++
	}
	return class, t.sshDialTimeoutStreak >= sshDialTimeoutLimit
}

func (t *Tunnel) snapshotSSHHealth() (generation uint64, timeoutStreak uint64, lastFailureClass string) {
	t.sshHealthMutex.Lock()
	defer t.sshHealthMutex.Unlock()
	return t.sshClientGeneration, t.sshDialTimeoutStreak, t.lastSSHFailureClass
}

func (t *Tunnel) SetTunnelContext(ctx context.Context) {
	t.reconnectMutex.Lock()
	t.tunnelCtx = ctx
	t.reconnectMutex.Unlock()
}

func (t *Tunnel) reconnectContext(ctx context.Context) context.Context {
	t.reconnectMutex.Lock()
	tunnelCtx := t.tunnelCtx
	t.reconnectMutex.Unlock()

	if tunnelCtx != nil && tunnelCtx.Err() == nil {
		return tunnelCtx
	}
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func (t *Tunnel) DisconnectSSHClient() {
	t.DisconnectSSHPool("manual disconnect")
}

func (t *Tunnel) AppConfig() *cfg.AppConfig {
	return t.appConfig
}

func (t *Tunnel) SetAppConfig(appConfig *cfg.AppConfig) {
	t.appConfig = appConfig
}

func (t *Tunnel) Domains() map[string]bool {
	t.domainMutex.RLock()
	defer t.domainMutex.RUnlock()
	return cloneStringBoolMap(t.domains)
}

func (t *Tunnel) SetDomains(domains map[string]bool) {
	t.domainMutex.Lock()
	t.domains = cloneStringBoolMap(domains)
	t.domainMutex.Unlock()
}

func (t *Tunnel) DomainMatchCache() map[string]bool {
	t.domainMutex.RLock()
	defer t.domainMutex.RUnlock()
	return cloneStringBoolMap(t.domainMatchCache)
}

func (t *Tunnel) SetDomainMatchCache(domainMatchCache map[string]bool) {
	t.domainMutex.Lock()
	t.domainMatchCache = cloneStringBoolMap(domainMatchCache)
	t.domainMutex.Unlock()
}

func (t *Tunnel) GetRouteMatcher() *RouteMatcher {
	return t.routeMatcher
}

func (t *Tunnel) GetProfileTunnelMgr() *ProfileTunnelManager {
	return t.profileTunnelMgr
}

func (t *Tunnel) GetTrafficStore() *TrafficStore {
	return t.trafficStore
}

func (t *Tunnel) GetRequestTracker() *ProxyRequestTracker {
	t.requestTrackerOnce.Do(func() {
		t.requestTracker = NewProxyRequestTracker(50)
	})
	return t.requestTracker
}

func splitHostPort(address string) (string, string) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address, ""
	}
	return host, port
}

func (t *Tunnel) addProxyUploadBytes(n int64) {
	if n <= 0 {
		return
	}
	atomic.AddUint64(&t.proxyUploadBytes, uint64(n))
}

func (t *Tunnel) addProxyDownloadBytes(n int64) {
	if n <= 0 {
		return
	}
	atomic.AddUint64(&t.proxyDownloadBytes, uint64(n))
}

func (t *Tunnel) copyProxyData(destination io.WriteCloser, source io.ReadCloser, upload bool) {
	n, err := io.Copy(destination, source)
	if t.trafficStore == nil {
		if upload {
			t.addProxyUploadBytes(n)
		} else {
			t.addProxyDownloadBytes(n)
		}
	}

	closeWrite(destination)
	if err != nil && !isIgnorableProxyErr(err) {
		log.Printf("proxy copy failed: %v", err)
	}
}

type closeWriter interface {
	CloseWrite() error
}

func closeWrite(c io.Closer) {
	if c == nil {
		return
	}
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

func closeConn(c io.Closer) {
	if c == nil {
		return
	}
	_ = c.Close()
}

func (t *Tunnel) proxyBidirectional(left io.ReadWriteCloser, right io.ReadWriteCloser, req *ProxyRequest) {
	done := make(chan struct{}, 2)
	safe.GO(func() {
		t.copyProxyData(left, right, true)
		done <- struct{}{}
	})
	safe.GO(func() {
		t.copyProxyData(right, left, false)
		done <- struct{}{}
	})
	<-done
	<-done
	closeConn(left)
	closeConn(right)
	if req != nil {
		t.GetRequestTracker().MarkCompleted(req)
	}
}

type prefixReadConn struct {
	net.Conn
	prefix []byte
}

func (c *prefixReadConn) Read(p []byte) (int, error) {
	if len(c.prefix) == 0 {
		return c.Conn.Read(p)
	}
	n := copy(p, c.prefix)
	c.prefix = c.prefix[n:]
	return n, nil
}

func writeFull(conn net.Conn, data []byte) (int, error) {
	total := 0
	for total < len(data) {
		n, err := conn.Write(data[total:])
		total += n
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func isTargetTLSPort(target string) bool {
	_, port, err := net.SplitHostPort(target)
	return err == nil && port == "443"
}

func shouldRetryEarlyProxyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "eof")
}

func (t *Tunnel) proxyBidirectionalWithEarlyRetry(ctx context.Context, client net.Conn, dest destinationConn, target string, req *ProxyRequest, retryState *requestRetryState) {
	if !dest.viaSSH || retryState == nil || !isTargetTLSPort(target) || t.configuredProxyRetryMaxAttempts() <= 0 {
		t.proxyBidirectional(dest.conn, client, req)
		return
	}

	bufferLimit := t.configuredProxyRetryInitialBufferBytes()
	if bufferLimit <= 0 {
		t.proxyBidirectional(dest.conn, client, req)
		return
	}

	initial := make([]byte, bufferLimit)
	_ = client.SetReadDeadline(time.Now().Add(t.proxyHandshakeTimeout()))
	n, readErr := client.Read(initial)
	_ = client.SetReadDeadline(time.Time{})
	if n == 0 {
		if netErr, ok := readErr.(net.Error); ok && netErr.Timeout() {
			t.proxyBidirectional(dest.conn, client, req)
			return
		}
		if readErr != nil && !isIgnorableProxyErr(readErr) {
			t.GetRequestTracker().MarkFailedDetailed(req, requestPhaseInitialWrite, failureClassClientClosed, readErr.Error(), false)
			closeConn(dest.conn)
			closeConn(client)
			return
		}
		t.proxyBidirectional(dest.conn, client, req)
		return
	}
	initial = initial[:n]

	tracker := t.GetRequestTracker()
	for {
		written, writeErr := writeFull(dest.conn, initial)
		if writeErr != nil {
			retryState.recordProxyRetryReason(writeErr)
			if written == 0 && retryState.canAttempt() {
				closeConn(dest.conn)
				retryDest, retryErr := t.getDestConn(target, retryState)
				if retryErr == nil && retryDest.conn != nil && retryDest.viaSSH {
					dest = retryDest
					retryInfo := retryState.info()
					tracker.UpdateMetadata(req, requestPhaseInitialWrite, dest.viaSSH, 0, "", false, dest.generation)
					tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
					tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
					tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
					continue
				}
				if retryErr != nil {
					writeErr = retryErr
					retryState.recordProxyRetryReason(retryErr)
				}
			}
			failureClass := t.classifyDialError(writeErr, dest.viaSSH)
			tracker.MarkFailedDetailed(req, requestPhaseInitialWrite, failureClass, writeErr.Error(), false)
			closeConn(dest.conn)
			closeConn(client)
			return
		}

		firstServerByte := make([]byte, 1)
		_ = dest.conn.SetReadDeadline(time.Now().Add(t.proxyHandshakeTimeout()))
		serverN, serverErr := dest.conn.Read(firstServerByte)
		_ = dest.conn.SetReadDeadline(time.Time{})
		if serverN > 0 {
			tracker.UpdateMetadata(req, requestPhaseProxying, dest.viaSSH, 0, "", false, dest.generation)
			tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
			tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
			retryInfo := retryState.info()
			tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
			t.proxyBidirectional(&prefixReadConn{Conn: dest.conn, prefix: firstServerByte[:serverN]}, client, req)
			return
		}
		if serverErr != nil && shouldRetryEarlyProxyError(serverErr) && retryState.canAttempt() {
			retryState.recordProxyRetryReason(serverErr)
			closeConn(dest.conn)
			retryDest, retryErr := t.getDestConn(target, retryState)
			if retryErr == nil && retryDest.conn != nil && retryDest.viaSSH {
				dest = retryDest
				retryInfo := retryState.info()
				tracker.UpdateMetadata(req, requestPhaseInitialWrite, dest.viaSSH, 0, "", false, dest.generation)
				tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
				tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
				tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
				continue
			}
			if retryErr != nil {
				serverErr = retryErr
				retryState.recordProxyRetryReason(retryErr)
			}
		}
		if serverErr != nil && shouldRetryEarlyProxyError(serverErr) {
			failureClass := t.classifyDialError(serverErr, dest.viaSSH)
			tracker.MarkFailedDetailed(req, requestPhaseInitialWrite, failureClass, serverErr.Error(), false)
			closeConn(dest.conn)
			closeConn(client)
			return
		}
		t.proxyBidirectional(dest.conn, client, req)
		return
	}
}

func (t *Tunnel) SnapshotProxyMetrics() ProxyMetrics {
	if t.trafficStore != nil {
		metrics, _ := t.SnapshotTraffic()
		return metrics
	}
	uploadTotal := atomic.LoadUint64(&t.proxyUploadBytes)
	downloadTotal := atomic.LoadUint64(&t.proxyDownloadBytes)
	activeProxyConns := atomic.LoadInt64(&t.activeProxyConns)

	now := time.Now()
	t.proxyStatsMutex.Lock()
	defer t.proxyStatsMutex.Unlock()

	if t.proxyLastAt.IsZero() {
		t.proxyLastAt = now
		t.proxyLastUpload = uploadTotal
		t.proxyLastDownload = downloadTotal
		return ProxyMetrics{
			UploadBytesTotal:   uploadTotal,
			DownloadBytesTotal: downloadTotal,
			UploadBps:          0,
			DownloadBps:        0,
			ActiveProxyConns:   activeProxyConns,
		}
	}

	elapsed := now.Sub(t.proxyLastAt).Seconds()
	if elapsed > 0 {
		uploadDelta := uploadTotal - t.proxyLastUpload
		downloadDelta := downloadTotal - t.proxyLastDownload
		t.proxyUploadBps = float64(uploadDelta) / elapsed
		t.proxyDownloadBps = float64(downloadDelta) / elapsed
		t.proxyLastAt = now
		t.proxyLastUpload = uploadTotal
		t.proxyLastDownload = downloadTotal
	}

	return ProxyMetrics{
		UploadBytesTotal:   uploadTotal,
		DownloadBytesTotal: downloadTotal,
		UploadBps:          t.proxyUploadBps,
		DownloadBps:        t.proxyDownloadBps,
		ActiveProxyConns:   activeProxyConns,
	}
}

// SnapshotTraffic returns a consistent persistent traffic snapshot. Callers that
// need both the compatibility metrics and per-scope details should use this
// method so speed samples are advanced only once.
func (t *Tunnel) SnapshotTraffic() (ProxyMetrics, TrafficSummary) {
	if t.trafficStore == nil {
		return t.SnapshotProxyMetrics(), TrafficSummary{}
	}
	summary := t.trafficStore.Summary()
	return ProxyMetrics{
		UploadBytesTotal:   summary.Overall.UploadBytesTotal,
		DownloadBytesTotal: summary.Overall.DownloadBytesTotal,
		UploadBps:          summary.Overall.UploadBps,
		DownloadBps:        summary.Overall.DownloadBps,
		ActiveProxyConns:   atomic.LoadInt64(&t.activeProxyConns),
	}, summary
}

func (t *Tunnel) SnapshotSSHConnectionStats() SSHConnectionStats {
	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()

	count := 0
	count = t.healthyPoolCount()

	generation, timeoutStreak, lastFailureClass := t.snapshotSSHHealth()
	healthyCount, suspectCount, probingCount, reconnectingCount, evictedCount := t.countPoolStates()
	poolMembers := t.snapshotPoolMembers()

	return SSHConnectionStats{
		ConnectionCount:              count,
		ReconnectCount:               t.reconnectCount,
		ConsecutiveReconnectFailures: t.consecutiveReconnectFailures,
		LastReconnectError:           t.lastReconnectError,
		LastReconnectAt:              t.lastReconnectAt,
		LastReconnectFailureAt:       t.lastReconnectFailureAt,
		SSHHealthy:                   healthyCount > 0,
		SSHClientGeneration:          generation,
		SSHDialTimeoutStreak:         timeoutStreak,
		LastSSHFailureClass:          lastFailureClass,
		PoolSize:                     healthyCount + suspectCount + probingCount + reconnectingCount,
		HealthyCount:                 healthyCount,
		SuspectCount:                 suspectCount,
		ProbingCount:                 probingCount,
		ReconnectingCount:            reconnectingCount,
		EvictedCount:                 evictedCount,
		PoolMembers:                  poolMembers,
	}
}

func (t *Tunnel) SnapshotListenerStats() ListenerStats {
	return ListenerStats{
		AcceptErrors:     atomic.LoadUint64(&t.acceptErrors),
		ListenerRestarts: atomic.LoadUint64(&t.listenerRestarts),
	}
}

func (t *Tunnel) ResetReconnectCount() {
	t.reconnectMutex.Lock()
	t.reconnectCount = 0
	t.reconnectMutex.Unlock()
}

func (t *Tunnel) MeasureSSHLatency() (int64, error) {
	client := t.GetSSHClient()
	if client == nil {
		return 0, errors.New("SSH client is not connected")
	}

	start := time.Now()
	_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
	if err != nil {
		return 0, err
	}

	return time.Since(start).Milliseconds(), nil
}

func (t *Tunnel) bindHttpTunnel(ctx context.Context, wg *sync.WaitGroup) {
	// Accept all incoming connections.
	t.httpProxyStartEx(ctx, wg)

}

func (t *Tunnel) bindSocks5Tunnel(ctx context.Context, wg *sync.WaitGroup) {
	// Accept all incoming connections.
	t.socks5ProxyStart(ctx)

}
func (t *Tunnel) socks5ProxyStart(ctx context.Context) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("socks5ProxyStart panic recovered: %v", err)
		}
	}()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	t.serveTCPProxy(ctx, t.localAddress, "SOCKS5", func(conn net.Conn) {
		resolveErr := t.socks5Proxy(ctx, conn)
		if resolveErr != nil && errors.Is(resolveErr, SSHReconnectRequired) {
			t.needReBind = true
			safe.GO(func() {
				t.ReconnectSSHWithSource(ctx, "socks5-proxy")
			})
		}
	})
}

func (t *Tunnel) handleHTTP(ctx context.Context, w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)

}

func (t *Tunnel) getDestConn(host string, retryState *requestRetryState) (destinationConn, error) {
	// Check standalone routing first. A matched rule never falls back to the default route.
	if dest, matched, err := t.tryRouteMatch(host, retryState); matched {
		return t.meterDestination(dest), err
	}

	if !t.enableHttpOverSSH {
		conn, err := net.DialTimeout("tcp", host, 3*time.Second)
		return t.meterDestination(destinationConn{conn: conn}), err
	}

	if !t.enableHttpDomainFilter {
		conn, client, generation, memberID, retryInfo, err := t.createSSHConn(host, retryState)
		return t.meterDestination(destinationConn{conn: conn, sshClient: client, sshMemberID: memberID, profileID: t.profileID, viaSSH: true, generation: generation, retryInfo: retryInfo}), err
	}

	if t.shouldUseSSHForHost(host) {
		conn, client, generation, memberID, retryInfo, err := t.createSSHConn(host, retryState)
		return t.meterDestination(destinationConn{conn: conn, sshClient: client, sshMemberID: memberID, profileID: t.profileID, viaSSH: true, generation: generation, retryInfo: retryInfo}), err
	}

	conn, err := net.DialTimeout("tcp", host, 10*time.Second)
	return t.meterDestination(destinationConn{conn: conn}), err

}

func (t *Tunnel) meterDestination(dest destinationConn) destinationConn {
	if dest.conn == nil || t.trafficStore == nil {
		return dest
	}
	profileID := dest.profileID
	if !dest.viaSSH {
		profileID = ""
	} else if profileID == "" {
		profileID = t.profileID
		dest.profileID = profileID
	}
	dest.conn = &meteredConn{Conn: dest.conn, store: t.trafficStore, profileID: profileID}
	return dest
}

// tryRouteMatch resolves a matched standalone rule. Matched rules fail closed.
func (t *Tunnel) tryRouteMatch(host string, retryState *requestRetryState) (destinationConn, bool, error) {
	if t.routeMatcher == nil || t.profileTunnelMgr == nil {
		return destinationConn{}, false, nil
	}
	rule, matched := t.routeMatcher.Match(host)
	if !matched {
		return destinationConn{}, false, nil
	}
	if retryState == nil {
		retryState = t.newRequestRetryState()
	}
	retryState.setRoute(rule.ID, rule.Strategy)
	targets := append([]string(nil), rule.TargetProfileIDs...)
	if rule.Strategy == cfg.RouteStrategyRandom {
		remaining := targets[:0]
		for _, target := range targets {
			if !retryState.attemptedProfileSet[target] {
				remaining = append(remaining, target)
			}
		}
		targets = remaining
		rand.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })
	}
	if len(targets) == 0 {
		dest := destinationConn{viaSSH: true, routeID: rule.ID, routeStrategy: rule.Strategy, attemptedProfileIDs: retryState.routeProfiles()}
		return dest, true, fmt.Errorf("路由%s的目标Profile均不可用", rule.ID)
	}

	var failures []string
	var last destinationConn
	for _, profileID := range targets {
		retryState.markProfileAttempt(profileID)
		if retryState.profileRetryStates == nil {
			retryState.profileRetryStates = make(map[string]*requestRetryState)
		}
		profileState := retryState.profileRetryStates[profileID]
		if profileState == nil {
			stateTunnel := t
			if t.routeDialer == nil && profileID != t.profileID {
				stateTunnel = t.profileTunnelMgr.GetTunnel(profileID)
				if stateTunnel == nil {
					failures = append(failures, profileID+": tunnel unavailable")
					continue
				}
			}
			profileState = stateTunnel.newRequestRetryState()
			retryState.profileRetryStates[profileID] = profileState
		}

		dest, err := t.dialRouteProfile(profileID, host, profileState)
		dest.routeID = rule.ID
		dest.routeStrategy = rule.Strategy
		dest.attemptedProfileIDs = retryState.routeProfiles()
		last = dest
		if err == nil {
			return dest, true, nil
		}
		failures = append(failures, profileID+": "+err.Error())
		if rule.Strategy == cfg.RouteStrategyFixed {
			break
		}
	}
	last.viaSSH = true
	last.routeID = rule.ID
	last.routeStrategy = rule.Strategy
	last.attemptedProfileIDs = retryState.routeProfiles()
	log.Printf("独立路由连接失败(route=%s, host=%s, targets=%v): %s", rule.ID, host, rule.TargetProfileIDs, strings.Join(failures, "; "))
	return last, true, fmt.Errorf("路由%s目标连接失败: %s", rule.ID, strings.Join(failures, "; "))
}

func (t *Tunnel) dialRouteProfile(profileID, host string, retryState *requestRetryState) (destinationConn, error) {
	if t.routeDialer != nil {
		return t.routeDialer(profileID, host, retryState)
	}
	if profileID == t.profileID {
		conn, client, generation, memberID, retryInfo, err := t.createSSHConn(host, retryState)
		return destinationConn{conn: conn, sshClient: client, sshMemberID: memberID, profileID: profileID, viaSSH: true, generation: generation, retryInfo: retryInfo}, err
	}
	return t.profileTunnelMgr.CreateSSHConnForProfile(profileID, host, retryState)
}

func (t *Tunnel) createSSHConn(host string, retryState *requestRetryState) (net.Conn, *ssh.Client, uint64, uint64, SSHRetryInfo, error) {
	conn, member, _, reconnectTriggered, retryInfo, err := t.dialSSHConn(context.Background(), host, retryState)
	if err != nil {
		if reconnectTriggered {
			return nil, nil, 0, memberID(member), retryInfo, fmt.Errorf("%w: %v", SSHDialError, err)
		}
		return nil, nil, 0, memberID(member), retryInfo, err
	}
	return conn, member.Client, member.Generation, member.ID, retryInfo, nil
}

func (t *Tunnel) handleHTTPS(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	tracker := t.GetRequestTracker()
	host, port := splitHostPort(r.Host)
	if port == "" {
		port = "443"
	}
	req := tracker.StartRequest(host, port, "HTTPS", t.enableHttpOverSSH)
	tracker.UpdateMetadata(req, requestPhaseDial, t.enableHttpOverSSH, 0, "", false, 0)

	retryState := t.newRequestRetryState()
	dialStartedAt := time.Now()
	dest, err := t.getDestConn(r.Host, retryState)
	if err != nil {
		failureClass := t.classifyDialError(err, dest.viaSSH)
		reconnectTriggered := dest.viaSSH && shouldReconnect(err)
		tracker.UpdateMetadata(req, requestPhaseDial, dest.viaSSH, time.Since(dialStartedAt), failureClass, reconnectTriggered, dest.generation)
		tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
		tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
		tracker.UpdateRetryInfo(req, dest.retryInfo.RetryCount, dest.retryInfo.RetryReason, dest.retryInfo.RetryMembers, dest.retryInfo.BalanceStrategy)
		tracker.MarkFailedDetailed(req, requestPhaseDial, failureClass, err.Error(), reconnectTriggered)
		http.Error(w, err.Error(), proxyHTTPStatus(err, dest.viaSSH))
		return
	}
	tracker.UpdateMetadata(req, requestPhaseDial, dest.viaSSH, time.Since(dialStartedAt), "", false, dest.generation)
	tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
	tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
	tracker.UpdateRetryInfo(req, dest.retryInfo.RetryCount, dest.retryInfo.RetryReason, dest.retryInfo.RetryMembers, dest.retryInfo.BalanceStrategy)
	tracker.MarkActive(req)
	tracker.UpdateMetadata(req, requestPhaseProxying, dest.viaSSH, 0, "", false, dest.generation)
	finishProxyConn := t.beginActiveProxyConn()
	defer finishProxyConn()
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		tracker.MarkFailed(req, "Hijacking not supported")
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		tracker.MarkFailed(req, err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return // fixed: was missing return
	}
	t.proxyBidirectionalWithEarlyRetry(ctx, clientConn, dest, r.Host, req, retryState)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func (t *Tunnel) basicAuth(w http.ResponseWriter, r *http.Request) bool {
	if !t.enableHttpBasic {
		return true
	}
	var auth = r.Header.Get("Proxy-Authorization")
	if ms := strings.Split(auth, " "); len(ms) == 2 && ms[0] == "Basic" {
		// check user:password
		up, err := base64.StdEncoding.DecodeString(ms[1])
		if err == nil {
			if ms := strings.Split(string(up), ":"); len(ms) == 2 {
				var user, password = ms[0], ms[1]
				var ok = false
				if user == t.httpBasicUserName && password == t.httpBasicPassword {
					ok = true
				}
				if ok {
					return true
				}
			}
		}
		w.WriteHeader(http.StatusProxyAuthRequired)
	} else {

		w.WriteHeader(http.StatusProxyAuthRequired)
		w.Header().Set("Proxy-Authenticate", `Basic realm="Http Proxy"`)
	}
	return false
}

func (t *Tunnel) httpProxyStartEx(ctx context.Context, wg *sync.WaitGroup) {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("httpProxyStartEx panic recovered: %v", err)
		}
	}()

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	t.serveTCPProxy(ctx, t.httpLocalAddress, "HTTP", func(client net.Conn) {
		t.handleClientRequest(ctx, client)
	})
}

func (t *Tunnel) handleClientRequest(ctx context.Context, client net.Conn) {
	defer client.Close()
	defer func() {
		if err := recover(); err != nil {
			log.Println("panic occurred:", err)
		}
	}()
	var b [1024]byte
	_ = client.SetReadDeadline(time.Now().Add(t.proxyHandshakeTimeout()))
	n, err := client.Read(b[:])
	if err != nil {
		log.Println(err)
		fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
		return
	}
	_ = client.SetReadDeadline(time.Time{})
	var method, host, address string
	firstLineEnd := bytes.IndexByte(b[:n], '\n')
	if firstLineEnd < 0 {
		log.Println("invalid http proxy request: missing request line")
		fmt.Fprint(client, "HTTP/1.1 400 invalid request\r\n\r\n")
		return
	}
	fmt.Sscanf(string(b[:firstLineEnd]), "%s%s", &method, &host)

	if method == http.MethodConnect {
		address = host
	} else {
		hostPortURL, err := url.Parse(host)
		if err != nil {
			log.Println(err)
			fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
			return
		}
		if hostPortURL.Opaque == "443" { //https访问
			address = hostPortURL.Scheme + ":443"
		} else { //http访问
			if strings.Index(hostPortURL.Host, ":") == -1 { //host不带端口， 默认80
				address = hostPortURL.Host + ":80"
			} else {
				address = hostPortURL.Host
			}
		}
	}

	protocol := "HTTP"
	if method == http.MethodConnect {
		protocol = "HTTPS"
	}
	tracker := t.GetRequestTracker()
	rHost, rPort := splitHostPort(address)
	req := tracker.StartRequest(rHost, rPort, protocol, t.enableHttpOverSSH)
	tracker.UpdateMetadata(req, requestPhaseDial, t.enableHttpOverSSH, 0, "", false, 0)

	retryState := t.newRequestRetryState()
	dialStartedAt := time.Now()
	dest, err := t.getConn(ctx, client, address, retryState)
	if err != nil {
		failureClass := t.classifyDialError(err, dest.viaSSH)
		reconnectTriggered := dest.viaSSH && shouldReconnect(err)
		tracker.UpdateMetadata(req, requestPhaseDial, dest.viaSSH, time.Since(dialStartedAt), failureClass, reconnectTriggered, dest.generation)
		tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
		tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
		tracker.UpdateRetryInfo(req, dest.retryInfo.RetryCount, dest.retryInfo.RetryReason, dest.retryInfo.RetryMembers, dest.retryInfo.BalanceStrategy)
		tracker.MarkFailedDetailed(req, requestPhaseDial, failureClass, err.Error(), reconnectTriggered)
		return
	}
	if dest.conn == nil {
		tracker.UpdateMetadata(req, requestPhaseDial, dest.viaSSH, time.Since(dialStartedAt), failureClassUnknown, false, dest.generation)
		tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
		tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
		tracker.UpdateRetryInfo(req, dest.retryInfo.RetryCount, dest.retryInfo.RetryReason, dest.retryInfo.RetryMembers, dest.retryInfo.BalanceStrategy)
		tracker.MarkFailedDetailed(req, requestPhaseDial, failureClassUnknown, "destination connection is nil", false)
		log.Println("Get Dest Connection Failed: destination connection is nil")
		fmt.Fprint(client, "HTTP/1.1 500 destination connection is nil\r\n\r\n")
		return
	}
	destConn := dest.conn
	tracker.UpdateMetadata(req, requestPhaseDial, dest.viaSSH, time.Since(dialStartedAt), "", false, dest.generation)
	tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
	tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
	tracker.UpdateRetryInfo(req, dest.retryInfo.RetryCount, dest.retryInfo.RetryReason, dest.retryInfo.RetryMembers, dest.retryInfo.BalanceStrategy)
	tracker.MarkActive(req)
	tracker.UpdateMetadata(req, requestPhaseProxying, dest.viaSSH, 0, "", false, dest.generation)
	finishProxyConn := t.beginActiveProxyConn()
	defer finishProxyConn()

	if method == "CONNECT" {
		fmt.Fprint(client, "HTTP/1.1 200 Connection established\r\n\r\n")
	} else {
		written, writeErr := destConn.Write(b[:n])
		if writeErr != nil && written == 0 && n <= t.configuredProxyRetryInitialBufferBytes() && dest.viaSSH && t.configuredProxyRetryMaxAttempts() > 0 && retryState.canAttempt() {
			retryState.recordProxyRetryReason(writeErr)
			closeConn(destConn)
			retryDest, retryErr := t.getDestConn(address, retryState)
			if retryErr == nil && retryDest.conn != nil && retryDest.viaSSH {
				retryWritten, retryWriteErr := retryDest.conn.Write(b[:n])
				if retryWriteErr == nil {
					dest = retryDest
					destConn = retryDest.conn
					retryInfo := retryState.info()
					tracker.UpdateMetadata(req, requestPhaseInitialWrite, dest.viaSSH, 0, "", false, dest.generation)
					tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
					tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
					tracker.UpdateRetryInfo(req, retryInfo.RetryCount, writeErr.Error(), retryInfo.RetryMembers, retryInfo.BalanceStrategy)
					writeErr = nil
					written = retryWritten
				} else {
					closeConn(retryDest.conn)
					writeErr = retryWriteErr
					written = retryWritten
					retryInfo := retryState.info()
					if retryInfo.RetryReason == "" {
						retryInfo.RetryReason = writeErr.Error()
					}
					tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
				}
			} else if retryErr != nil {
				writeErr = retryErr
				retryInfo := retryState.info()
				tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryErr.Error(), retryInfo.RetryMembers, retryInfo.BalanceStrategy)
			}
		}
		if writeErr != nil {
			log.Printf("write initial request to destination failed: %v", writeErr)
			failureClass := t.classifyDialError(writeErr, dest.viaSSH)
			tracker.UpdateMetadata(req, requestPhaseInitialWrite, dest.viaSSH, 0, failureClass, false, dest.generation)
			if dest.viaSSH && (dest.routeID == "" || dest.profileID == t.profileID) && dest.sshClient != nil && isSSHReconnectError(writeErr) {
				t.invalidateSSHClientIfMatch(dest.sshClient, "http initial write failed: "+writeErr.Error())
				safe.GO(func() {
					t.ReconnectSSHWithSource(ctx, "http-proxy-write")
				})
				tracker.UpdateMetadata(req, requestPhaseInitialWrite, dest.viaSSH, 0, failureClass, true, dest.generation)
			}
			status := proxyHTTPStatus(writeErr, dest.viaSSH)
			fmt.Fprintf(client, "HTTP/1.1 %d %s\r\n\r\n", status, http.StatusText(status))
			tracker.MarkFailedDetailed(req, requestPhaseInitialWrite, failureClass, writeErr.Error(), dest.viaSSH && isSSHReconnectError(writeErr))
			return
		}
	}
	if method == "CONNECT" {
		t.proxyBidirectionalWithEarlyRetry(ctx, client, dest, address, req, retryState)
		return
	}
	t.proxyBidirectional(destConn, client, req)
}

func (t *Tunnel) getConn(ctx context.Context, client net.Conn, address string, retryState *requestRetryState) (destinationConn, error) {
	dest, err := t.getDestConn(address, retryState)
	if err == nil && dest.conn != nil {
		return dest, nil
	}

	if dest.viaSSH && (dest.routeID == "" || dest.profileID == t.profileID) && shouldReconnect(err) {
		if err != nil && dest.sshClient != nil {
			t.invalidateSSHClientIfMatch(dest.sshClient, err.Error())
		}
		safe.GO(func() {
			t.ReconnectSSHWithSource(t.reconnectContext(ctx), "http-proxy-request")
		})
		log.Printf("Get Dest Connection Failed(%s): ssh pool unavailable, replenish scheduled: %v", address, err)
		fmt.Fprint(client, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return dest, err
	}

	if err != nil {
		log.Printf("Get Dest Connection Failed(%s): %v", address, err)
		fmt.Fprintf(client, "HTTP/1.1 %d %s\r\n\r\n", proxyHTTPStatus(err, dest.viaSSH), http.StatusText(proxyHTTPStatus(err, dest.viaSSH)))
	} else {
		log.Printf("Get Dest Connection Failed(%s): destination connection is nil", address)
		fmt.Fprint(client, "HTTP/1.1 500 destination connection is nil\r\n\r\n")
		err = errors.New("destination connection is nil")
	}
	return dest, err
}

func shouldReconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, SSHReconnectRequired) || errors.Is(err, SSHDialError) {
		return true
	}
	return isSSHReconnectError(err)
}

func proxyHTTPStatus(err error, viaSSH bool) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return http.StatusGatewayTimeout
	}
	if viaSSH && shouldReconnect(err) {
		return http.StatusBadGateway
	}
	return http.StatusBadGateway
}

func isSSHReconnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unexpected packet in response") ||
		strings.Contains(msg, "max packet length exceeded") ||
		strings.Contains(msg, "ssh: handshake failed") ||
		strings.Contains(msg, "ssh: disconnect") ||
		strings.Contains(msg, "ssh: connection closed") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection")
}

func isIgnorableProxyErr(err error) bool {
	if err == nil {
		return true
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "use of closed network connection") ||
		strings.Contains(errText, "wsarecv") ||
		strings.Contains(errText, "forcibly closed by the remote host") ||
		strings.Contains(errText, "broken pipe") ||
		strings.Contains(errText, "connection reset by peer") ||
		err == io.EOF
}

func (t *Tunnel) httpProxyStart(ctx context.Context, wg *sync.WaitGroup) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	server := &http.Server{
		Addr: t.httpLocalAddress,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := t.basicAuth(w, r)
			if !result {
				return
			}
			if r.Method == http.MethodConnect {
				t.handleHTTPS(ctx, w, r)
			} else {
				t.handleHTTP(ctx, w, r)
			}
		}),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	safe.GO(func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Printf("Http server error: %v", err)
		}
	})
	log.Printf("Http Server Started at %s", t.httpLocalAddress)
	<-connCtx.Done()
	ctx, timeOutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		// extra handling here
		timeOutCancel()
	}()
	log.Println("Server Stopped!")
	err := server.Shutdown(ctx)

	if err != nil {
		log.Printf("Server Shutdown Failed: %+v", err)
	} else {
		log.Print("Server Exited Properly")
	}

}

func (t *Tunnel) socks5Proxy(ctx context.Context, conn net.Conn) error {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	safe.GO(func() {
		<-connCtx.Done()
		conn.Close()
	})

	_ = conn.SetReadDeadline(time.Now().Add(t.proxyHandshakeTimeout()))
	verNMethods := make([]byte, 2)
	if _, err := io.ReadFull(conn, verNMethods); err != nil {
		log.Println(err)
		return err
	}

	if verNMethods[0] != 0x05 {
		return fmt.Errorf("unsupported socks version: %d", verNMethods[0])
	}

	nMethods := int(verNMethods[1])
	if nMethods <= 0 {
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return errors.New("no auth method provided")
	}

	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(conn, methods); err != nil {
		log.Println(err)
		return err
	}

	methodOK := false
	for _, method := range methods {
		if method == 0x00 {
			methodOK = true
			break
		}
	}

	if !methodOK {
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return errors.New("socks5 no-auth method not supported by client")
	}

	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		log.Println(err)
		return err
	}

	requestHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, requestHeader); err != nil {
		log.Println(err)
		return err
	}

	if requestHeader[0] != 0x05 {
		_ = writeSocks5Reply(conn, 0x01, nil)
		return fmt.Errorf("invalid socks request version: %d", requestHeader[0])
	}

	if requestHeader[1] != 0x01 {
		_ = writeSocks5Reply(conn, 0x07, nil)
		return fmt.Errorf("unsupported socks command: %d", requestHeader[1])
	}

	addr, err := readSocks5TargetAddress(conn, requestHeader[3])
	if err != nil {
		_ = writeSocks5Reply(conn, 0x08, nil)
		log.Println(err)
		return err
	}

	tracker := t.GetRequestTracker()
	sHost, sPort := splitHostPort(addr)
	req := tracker.StartRequest(sHost, sPort, "SOCKS5", true)
	tracker.UpdateMetadata(req, requestPhaseDial, true, 0, "", false, 0)

	retryState := t.newRequestRetryState()

	// Check profile-based domain routing first
	if dest, matched, routeErr := t.tryRouteMatch(addr, retryState); matched {
		if routeErr != nil {
			log.Println(routeErr)
			_ = writeSocks5Reply(conn, mapSocks5ReplyCode(routeErr), nil)
			tracker.UpdateRouteInfo(req, retryState.routeID, retryState.routeStrategy, retryState.routeProfiles())
			tracker.MarkFailed(req, routeErr.Error())
			return routeErr
		}
		dest = t.meterDestination(dest)
		if err := writeSocks5Reply(conn, 0x00, dest.conn.LocalAddr()); err != nil {
			_ = dest.conn.Close()
			log.Println(err)
			tracker.MarkFailed(req, err.Error())
			return err
		}
		tracker.UpdateMetadata(req, requestPhaseDial, true, 0, "", false, dest.generation)
		tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
		tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
		tracker.MarkActive(req)
		tracker.UpdateMetadata(req, requestPhaseProxying, true, 0, "", false, dest.generation)
		_ = conn.SetReadDeadline(time.Time{})
		finishProxyConn := t.beginActiveProxyConn()
		defer finishProxyConn()
		t.proxyBidirectionalWithEarlyRetry(ctx, conn, dest, addr, req, retryState)
		return nil
	}

	dialStartedAt := time.Now()
	server, member, failureClass, reconnectTriggered, retryInfo, err := t.dialSSHConn(context.Background(), addr, retryState)
	if err != nil {
		log.Println(err)
		_ = writeSocks5Reply(conn, mapSocks5ReplyCode(err), nil)
		tracker.UpdateMetadata(req, requestPhaseDial, true, time.Since(dialStartedAt), failureClass, reconnectTriggered, 0)
		tracker.UpdateSSHMember(req, memberID(member), t.profileID)
		tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
		tracker.MarkFailedDetailed(req, requestPhaseDial, failureClass, err.Error(), reconnectTriggered)
		if reconnectTriggered {
			return SSHReconnectRequired
		}
		return err
	}
	generation := member.Generation
	tracker.UpdateMetadata(req, requestPhaseDial, true, time.Since(dialStartedAt), "", false, generation)
	tracker.UpdateSSHMember(req, member.ID, t.profileID)
	tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)

	if err := writeSocks5Reply(conn, 0x00, server.LocalAddr()); err != nil {
		_ = server.Close()
		log.Println(err)
		tracker.MarkFailed(req, err.Error())
		return err
	}

	tracker.MarkActive(req)
	tracker.UpdateMetadata(req, requestPhaseProxying, true, 0, "", false, generation)
	_ = conn.SetReadDeadline(time.Time{})
	finishProxyConn := t.beginActiveProxyConn()
	defer finishProxyConn()
	dest := t.meterDestination(destinationConn{
		conn:        server,
		sshClient:   member.Client,
		sshMemberID: member.ID,
		profileID:   t.profileID,
		viaSSH:      true,
		generation:  generation,
		retryInfo:   retryInfo,
	})
	t.proxyBidirectionalWithEarlyRetry(ctx, conn, dest, addr, req, retryState)
	return nil
}

func readSocks5TargetAddress(conn net.Conn, atyp byte) (string, error) {
	switch atyp {
	case 0x01:
		buf := make([]byte, 6)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host := net.IP(buf[:4]).String()
		port := binary.BigEndian.Uint16(buf[4:])
		return fmt.Sprintf("%s:%d", host, port), nil
	case 0x03:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", err
		}
		hostLen := int(lenBuf[0])
		if hostLen <= 0 {
			return "", errors.New("invalid domain length")
		}
		hostBuf := make([]byte, hostLen+2)
		if _, err := io.ReadFull(conn, hostBuf); err != nil {
			return "", err
		}
		host := string(hostBuf[:hostLen])
		port := binary.BigEndian.Uint16(hostBuf[hostLen:])
		return fmt.Sprintf("%s:%d", host, port), nil
	case 0x04:
		buf := make([]byte, 18)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", err
		}
		host := net.IP(buf[:16]).String()
		port := binary.BigEndian.Uint16(buf[16:])
		return fmt.Sprintf("[%s]:%d", host, port), nil
	default:
		return "", fmt.Errorf("unsupported socks atyp: %d", atyp)
	}
}

func writeSocks5Reply(conn net.Conn, rep byte, bindAddr net.Addr) error {
	response := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}

	if tcpAddr, ok := bindAddr.(*net.TCPAddr); ok && tcpAddr != nil {
		if ip4 := tcpAddr.IP.To4(); ip4 != nil {
			response = []byte{0x05, rep, 0x00, 0x01, ip4[0], ip4[1], ip4[2], ip4[3], 0, 0}
			binary.BigEndian.PutUint16(response[8:], uint16(tcpAddr.Port))
		} else if ip16 := tcpAddr.IP.To16(); ip16 != nil {
			response = make([]byte, 22)
			response[0] = 0x05
			response[1] = rep
			response[2] = 0x00
			response[3] = 0x04
			copy(response[4:20], ip16)
			binary.BigEndian.PutUint16(response[20:], uint16(tcpAddr.Port))
		}
	}

	_, err := conn.Write(response)
	return err
}

func mapSocks5ReplyCode(err error) byte {
	if err == nil {
		return 0x00
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 0x04
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return 0x04
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "refused") {
		return 0x05
	}
	if strings.Contains(msg, "network is unreachable") {
		return 0x03
	}
	if strings.Contains(msg, "host is unreachable") {
		return 0x04
	}
	return 0x01
}

func (t *Tunnel) httpProxy(ctx context.Context, conn net.Conn) error {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	safe.GO(func() {
		<-connCtx.Done()
		conn.Close()
	})

	var b [1024]byte

	n, err := conn.Read(b[:])
	if err != nil {
		log.Println(err)
		return err
	}

	conn.Write([]byte{0x05, 0x00})

	n, err = conn.Read(b[:])
	if err != nil {
		log.Println(err)
		return err
	}

	var addr string
	switch b[3] {
	case 0x01:
		sip := sockIP{}
		if err := binary.Read(bytes.NewReader(b[4:n]), binary.BigEndian, &sip); err != nil {
			log.Println("Request parsing error")
			return err
		}
		addr = sip.toAddr()
	case 0x03:
		host := string(b[5 : n-2])
		var port uint16
		err = binary.Read(bytes.NewReader(b[n-2:n]), binary.BigEndian, &port)
		if err != nil {
			log.Println(err)
			return err
		}
		addr = fmt.Sprintf("%s:%d", host, port)
	}

	tracker := t.GetRequestTracker()
	hpHost, hpPort := splitHostPort(addr)
	req := tracker.StartRequest(hpHost, hpPort, "SOCKS5", true)

	retryState := t.newRequestRetryState()

	// Check profile-based domain routing first
	if dest, matched, routeErr := t.tryRouteMatch(addr, retryState); matched {
		if routeErr != nil {
			log.Println(routeErr)
			tracker.UpdateRouteInfo(req, retryState.routeID, retryState.routeStrategy, retryState.routeProfiles())
			tracker.MarkFailed(req, routeErr.Error())
			return NetworkError
		}
		dest = t.meterDestination(dest)
		tracker.UpdateMetadata(req, requestPhaseDial, true, 0, "", false, dest.generation)
		tracker.UpdateSSHMember(req, dest.sshMemberID, dest.profileID)
		tracker.UpdateRouteInfo(req, dest.routeID, dest.routeStrategy, dest.attemptedProfileIDs)
		tracker.MarkActive(req)
		tracker.UpdateMetadata(req, requestPhaseProxying, true, 0, "", false, dest.generation)
		conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		t.proxyBidirectionalWithEarlyRetry(ctx, conn, dest, addr, req, retryState)
		return nil
	}

	dialStartedAt := time.Now()
	server, member, failureClass, reconnectTriggered, retryInfo, err := t.dialSSHConn(context.Background(), addr, retryState)
	if err != nil {
		log.Println(err)
		tracker.UpdateMetadata(req, requestPhaseDial, true, time.Since(dialStartedAt), failureClass, reconnectTriggered, 0)
		tracker.UpdateSSHMember(req, memberID(member), t.profileID)
		tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
		tracker.MarkFailedDetailed(req, requestPhaseDial, failureClass, err.Error(), reconnectTriggered)
		return NetworkError
	}
	generation := member.Generation
	tracker.UpdateMetadata(req, requestPhaseDial, true, time.Since(dialStartedAt), "", false, generation)
	tracker.UpdateSSHMember(req, member.ID, t.profileID)
	tracker.UpdateRetryInfo(req, retryInfo.RetryCount, retryInfo.RetryReason, retryInfo.RetryMembers, retryInfo.BalanceStrategy)
	tracker.MarkActive(req)
	tracker.UpdateMetadata(req, requestPhaseProxying, true, 0, "", false, generation)
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	dest := t.meterDestination(destinationConn{
		conn:        server,
		sshClient:   member.Client,
		sshMemberID: member.ID,
		profileID:   t.profileID,
		viaSSH:      true,
		generation:  generation,
		retryInfo:   retryInfo,
	})
	t.proxyBidirectionalWithEarlyRetry(ctx, conn, dest, addr, req, retryState)
	return nil
}

type sockIP struct {
	A, B, C, D byte
	PORT       uint16
}

func (ip sockIP) toAddr() string {
	return fmt.Sprintf("%d.%d.%d.%d:%d", ip.A, ip.B, ip.C, ip.D, ip.PORT)
}

func (t *Tunnel) dialTunnel(ctx context.Context, wg *sync.WaitGroup, client *ssh.Client, cn1 net.Conn) {
	defer wg.Done()

	// The inbound connection is established. Make sure we close it eventually.
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	safe.GO(func() {
		<-connCtx.Done()
		cn1.Close()
	})

	// Establish the outbound connection.
	var cn2 net.Conn
	var err error

	cn2, err = client.Dial("tcp", t.serverAddress)
	if err != nil {
		log.Printf("dial error: %v", err)
		return
	}

	safe.GO(func() {
		<-connCtx.Done()
		cn2.Close()
	})

	log.Printf("connection established")
	defer log.Printf("connection closed")

	// Copy bytes from one connection to the other until one side closes.
	var once sync.Once
	var wg2 sync.WaitGroup
	wg2.Add(2)
	safe.GO(func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn1, cn2); err != nil {
			once.Do(func() { log.Printf("connection error: %v", err) })
		}
		once.Do(func() {}) // Suppress future errors
	})
	safe.GO(func() {
		defer wg2.Done()
		defer cancel()
		if _, err := io.Copy(cn2, cn1); err != nil {
			once.Do(func() { log.Printf("connection error: %v", err) })
		}
		once.Do(func() {}) // Suppress future errors
	})
	wg2.Wait()
}

func (t *Tunnel) keepAliveMonitor(ctx context.Context, once *sync.Once, member *SSHPoolMember) bool {
	if t.keepAlive.Interval == 0 || t.keepAlive.CountMax == 0 {
		return false
	}
	if member == nil {
		return false
	}
	client := member.Client
	if client == nil {
		return false
	}
	wait := make(chan error, 1)
	safe.GO(func() {
		wait <- client.Wait()
	})
	var aliveCount int32
	ticker := time.NewTicker(time.Duration(t.keepAlive.Interval) * time.Second)
	defer ticker.Stop()
	probeTimeout := t.keepAliveProbeTimeout()
	for {
		select {
		case <-ctx.Done():
			return false
		case err := <-wait:
			if t.findMemberByClient(client) == nil {
				return false
			}
			if err != nil && err != io.EOF {
				once.Do(func() {
					log.Printf("SSH member wait error(memberId=%d,generation=%d): %v", member.ID, member.Generation, err)
				})
			}
			return true
		case <-ticker.C:
			if t.findMemberByClient(client) == nil {
				return false
			}

			probeDone := make(chan error, 1)
			safe.GO(func() {
				_, _, probeErr := client.SendRequest("keepalive@openssh.com", true, nil)
				probeDone <- probeErr
			})

			probeFailed := false
			select {
			case probeErr := <-probeDone:
				if probeErr == nil {
					atomic.StoreInt32(&aliveCount, 0)
					continue
				}
				probeFailed = true
			case <-time.After(probeTimeout):
				probeFailed = true
			}

			if !probeFailed {
				continue
			}

			if n := atomic.AddInt32(&aliveCount, 1); n > int32(t.keepAlive.CountMax) {
				once.Do(func() {
					log.Printf("SSH keep-alive termination(server=%s, memberId=%d, generation=%d, consecutiveFailures=%d)", t.serverAddress, member.ID, member.Generation, n)
				})
				return true
			}
		}
	}
}
