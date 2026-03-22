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
}

type ProxyMetrics struct {
	UploadBytesTotal   uint64  `json:"uploadBytesTotal"`
	DownloadBytesTotal uint64  `json:"downloadBytesTotal"`
	UploadBps          float64 `json:"uploadBps"`
	DownloadBps        float64 `json:"downloadBps"`
	ActiveProxyConns   int64   `json:"activeProxyConns"`
}

type SSHConnectionStats struct {
	ConnectionCount              int       `json:"connectionCount"`
	ReconnectCount               uint64    `json:"reconnectCount"`
	ConsecutiveReconnectFailures uint64    `json:"consecutiveReconnectFailures"`
	LastReconnectError           string    `json:"lastReconnectError,omitempty"`
	LastReconnectAt              time.Time `json:"lastReconnectAt,omitempty"`
	LastReconnectFailureAt       time.Time `json:"lastReconnectFailureAt,omitempty"`
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
	conn      net.Conn
	sshClient *ssh.Client
	viaSSH    bool
}

func (t *Tunnel) currentSSHClient() *ssh.Client {
	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()
	return t.client
}

func (t *Tunnel) invalidateSSHClient(reason string) {
	t.reconnectMutex.Lock()
	client := t.client
	t.client = nil
	t.reconnectMutex.Unlock()

	if client != nil {
		closeSSHClient(client)
		t.resetExitIPInfo()
		if reason != "" {
			log.Printf("SSH client invalidated: %s", reason)
		}
	}
}

func (t *Tunnel) invalidateSSHClientIfMatch(expected *ssh.Client, reason string) bool {
	if expected == nil {
		return false
	}

	t.reconnectMutex.Lock()
	if t.client != expected {
		t.reconnectMutex.Unlock()
		return false
	}
	t.client = nil
	t.reconnectMutex.Unlock()

	closeSSHClient(expected)
	t.resetExitIPInfo()
	if reason != "" {
		log.Printf("SSH client invalidated: %s", reason)
	}
	return true
}

func (t *Tunnel) GetSSHClient() *ssh.Client {
	t.reconnectMutex.Lock()
	client := t.client
	t.reconnectMutex.Unlock()

	if client != nil {
		return client
	}

	t.ReconnectSSHWithSource(t.reconnectContext(nil), "get-ssh-client")

	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()
	return t.client
}

func (t *Tunnel) PeekSSHClient() *ssh.Client {
	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()
	return t.client
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
	t.invalidateSSHClient("manual disconnect")
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
	defer destination.Close()
	defer source.Close()

	n, err := io.Copy(destination, source)
	if upload {
		t.addProxyUploadBytes(n)
	} else {
		t.addProxyDownloadBytes(n)
	}

	if err != nil && !isIgnorableProxyErr(err) {
		log.Printf("proxy copy failed: %v", err)
	}
}

func (t *Tunnel) SnapshotProxyMetrics() ProxyMetrics {
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

func (t *Tunnel) SnapshotSSHConnectionStats() SSHConnectionStats {
	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()

	count := 0
	if t.client != nil {
		count = 1
	}

	return SSHConnectionStats{
		ConnectionCount:              count,
		ReconnectCount:               t.reconnectCount,
		ConsecutiveReconnectFailures: t.consecutiveReconnectFailures,
		LastReconnectError:           t.lastReconnectError,
		LastReconnectAt:              t.lastReconnectAt,
		LastReconnectFailureAt:       t.lastReconnectFailureAt,
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

func (t *Tunnel) getDestConn(host string) (destinationConn, error) {
	if !t.enableHttpOverSSH {
		conn, err := net.DialTimeout("tcp", host, 3*time.Second)
		return destinationConn{conn: conn}, err
	}

	if !t.enableHttpDomainFilter {
		conn, client, err := t.createSSHConn(host)
		return destinationConn{conn: conn, sshClient: client, viaSSH: true}, err
	}

	if t.shouldUseSSHForHost(host) {
		conn, client, err := t.createSSHConn(host)
		return destinationConn{conn: conn, sshClient: client, viaSSH: true}, err
	}

	conn, err := net.DialTimeout("tcp", host, 10*time.Second)
	return destinationConn{conn: conn}, err

}

func (t *Tunnel) createSSHConn(host string) (net.Conn, *ssh.Client, error) {
	client := t.GetSSHClient()
	if client == nil {
		return nil, nil, SSHReconnectRequired
	}
	timeout := t.sshDestTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	background := context.Background()
	timeoutCtx, cancel := context.WithTimeout(background, timeout)
	defer cancel()

	conn, err := client.DialContext(timeoutCtx, "tcp", host)
	if err != nil {
		if isSSHReconnectError(err) {
			return nil, client, fmt.Errorf("%w: %v", SSHDialError, err)
		}
		return nil, client, err
	}

	return conn, client, nil
}

func (t *Tunnel) handleHTTPS(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	tracker := t.GetRequestTracker()
	host, port := splitHostPort(r.Host)
	if port == "" {
		port = "443"
	}
	req := tracker.StartRequest(host, port, "HTTPS", t.enableHttpOverSSH)

	dest, err := t.getDestConn(r.Host)
	if err != nil {
		tracker.MarkFailed(req, err.Error())
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	destConn := dest.conn
	tracker.MarkActive(req)
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

	done := make(chan struct{}, 2)
	safe.GO(func() {
		t.copyProxyData(destConn, clientConn, true)
		done <- struct{}{}
	})
	safe.GO(func() {
		t.copyProxyData(clientConn, destConn, false)
		done <- struct{}{}
	})
	// 等待任一方向结束即标记完成
	safe.GO(func() {
		<-done
		tracker.MarkCompleted(req)
	})
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

	dest, done := t.getConn(ctx, client, address)
	if done {
		tracker.MarkFailed(req, "connection failed")
		return
	}
	if dest.conn == nil {
		tracker.MarkFailed(req, "destination connection is nil")
		log.Println("Get Dest Connection Failed: destination connection is nil")
		fmt.Fprint(client, "HTTP/1.1 500 destination connection is nil\r\n\r\n")
		return
	}
	destConn := dest.conn
	tracker.MarkActive(req)
	finishProxyConn := t.beginActiveProxyConn()
	defer finishProxyConn()

	if method == "CONNECT" {
		fmt.Fprint(client, "HTTP/1.1 200 Connection established\r\n\r\n")
	} else {
		if _, writeErr := destConn.Write(b[:n]); writeErr != nil {
			log.Printf("write initial request to destination failed: %v", writeErr)
			if dest.viaSSH && dest.sshClient != nil && isSSHReconnectError(writeErr) {
				t.invalidateSSHClientIfMatch(dest.sshClient, "http initial write failed: "+writeErr.Error())
				safe.GO(func() {
					t.ReconnectSSHWithSource(ctx, "http-proxy-write")
				})
			}
			fmt.Fprint(client, "HTTP/1.1 500 "+writeErr.Error()+"\r\n\r\n")
			tracker.MarkFailed(req, writeErr.Error())
			return
		}
	}
	//进行转发
	safe.GO(func() {
		t.copyProxyData(destConn, client, true)
	})
	t.copyProxyData(client, destConn, false)
	tracker.MarkCompleted(req)
}

func (t *Tunnel) getConn(ctx context.Context, client net.Conn, address string) (destinationConn, bool) {
	dest, err := t.getDestConn(address)
	if err == nil && dest.conn != nil {
		return dest, false
	}

	if dest.viaSSH && shouldReconnect(err) {
		if err != nil && dest.sshClient != nil {
			t.invalidateSSHClientIfMatch(dest.sshClient, err.Error())
		}

		t.ReconnectSSHWithSource(t.reconnectContext(ctx), "http-proxy-request")
		if t.PeekSSHClient() == nil {
			log.Printf("Get Dest Connection Failed(%s): ssh reconnect failed", address)
			fmt.Fprint(client, "HTTP/1.1 500 ssh reconnect failed\r\n\r\n")
			return destinationConn{}, true
		}

		dest, err = t.getDestConn(address)
		if err == nil && dest.conn != nil {
			return dest, false
		}

		if err != nil {
			log.Printf("Get Dest Connection Failed(%s) after reconnect: %v", address, err)
			fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
		} else {
			log.Printf("Get Dest Connection Failed(%s) after reconnect: destination connection is nil", address)
			fmt.Fprint(client, "HTTP/1.1 500 destination connection is nil\r\n\r\n")
		}
		return destinationConn{}, true
	}

	if err != nil {
		log.Printf("Get Dest Connection Failed(%s): %v", address, err)
		fmt.Fprint(client, "HTTP/1.1 500 "+err.Error()+"\r\n\r\n")
	} else {
		log.Printf("Get Dest Connection Failed(%s): destination connection is nil", address)
		fmt.Fprint(client, "HTTP/1.1 500 destination connection is nil\r\n\r\n")
	}
	return destinationConn{}, true
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
		strings.Contains(errText, "forcibly closed by the remote host")
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

	sshClient := t.GetSSHClient()
	if sshClient == nil {
		_ = writeSocks5Reply(conn, 0x01, nil)
		tracker.MarkFailed(req, "SSH client not connected")
		return SSHReconnectRequired
	}

	timeout := t.sshDestTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), timeout)
	defer timeoutCancel()

	server, err := sshClient.DialContext(timeoutCtx, "tcp", addr)
	if err != nil {
		log.Println(err)
		_ = writeSocks5Reply(conn, mapSocks5ReplyCode(err), nil)
		tracker.MarkFailed(req, err.Error())
		if isSSHReconnectError(err) {
			t.invalidateSSHClientIfMatch(sshClient, "socks5 dial failed: "+err.Error())
			return SSHReconnectRequired
		}
		return err
	}

	if err := writeSocks5Reply(conn, 0x00, server.LocalAddr()); err != nil {
		_ = server.Close()
		log.Println(err)
		tracker.MarkFailed(req, err.Error())
		return err
	}

	tracker.MarkActive(req)
	_ = conn.SetReadDeadline(time.Time{})
	finishProxyConn := t.beginActiveProxyConn()
	defer finishProxyConn()
	safe.GO(func() {
		t.copyProxyData(server, conn, true)
	})
	t.copyProxyData(conn, server, false)
	tracker.MarkCompleted(req)
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

	sshClient := t.GetSSHClient()
	if sshClient == nil {
		tracker.MarkFailed(req, "SSH client not connected")
		return NetworkError
	}

	timeout := t.sshDestTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), timeout)
	defer timeoutCancel()

	server, err := sshClient.DialContext(timeoutCtx, "tcp", addr)
	if err != nil {
		log.Println(err)
		tracker.MarkFailed(req, err.Error())
		return NetworkError
	}
	tracker.MarkActive(req)
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	safe.GO(func() {
		t.copyProxyData(server, conn, true)
	})
	t.copyProxyData(conn, server, false)
	tracker.MarkCompleted(req)
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

func (t *Tunnel) keepAliveMonitor(ctx context.Context, once *sync.Once, client *ssh.Client) bool {
	if t.keepAlive.Interval == 0 || t.keepAlive.CountMax == 0 {
		return false
	}
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
			if t.currentSSHClient() != client {
				return false
			}
			if err != nil && err != io.EOF {
				once.Do(func() { log.Printf("(%v) SSH error: %v", t, err) })
			}
			return true
		case <-ticker.C:
			if t.currentSSHClient() != client {
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
				once.Do(func() { log.Printf("(%v) SSH keep-alive termination", t) })
				return true
			}
		}
	}
}
