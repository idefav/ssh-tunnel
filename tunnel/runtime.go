package tunnel

import (
	"context"
	"errors"
	"log"
	"net"
	"ssh-tunnel/safe"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	defaultProxyHandshakeTimeout = 10 * time.Second
	defaultListenerRetryMin      = 200 * time.Millisecond
	defaultListenerRetryMax      = 3 * time.Second
	defaultReconnectRetry        = time.Second
	defaultReconnectMaxInterval  = 5 * time.Second
	defaultReconnectMaxRetries   = 20
	defaultKeepAliveProbeTimeout = 5 * time.Second
)

func cloneStringBoolMap(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return make(map[string]bool)
	}

	dst := make(map[string]bool, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func closeSSHClient(client *ssh.Client) {
	if client == nil {
		return
	}

	defer func() {
		if err := recover(); err != nil {
			log.Printf("SSH client close panic recovered: %v", err)
		}
	}()

	_ = client.Close()
}

func safeSSHAddrString(getter func() net.Addr) (result string) {
	result = "<nil>"
	defer func() {
		if err := recover(); err != nil {
			result = "<unavailable>"
		}
	}()

	addr := getter()
	if addr != nil {
		result = addr.String()
	}
	return result
}

func (t *Tunnel) proxyHandshakeTimeout() time.Duration {
	return defaultProxyHandshakeTimeout
}

func (t *Tunnel) keepAliveProbeTimeout() time.Duration {
	if t.sshDestTimeout > defaultKeepAliveProbeTimeout {
		return t.sshDestTimeout
	}
	return defaultKeepAliveProbeTimeout
}

func (t *Tunnel) recordAcceptError() {
	atomic.AddUint64(&t.acceptErrors, 1)
}

func (t *Tunnel) recordListenerRestart() {
	atomic.AddUint64(&t.listenerRestarts, 1)
}

func (t *Tunnel) beginActiveProxyConn() func() {
	atomic.AddInt64(&t.activeProxyConns, 1)
	var once sync.Once
	return func() {
		once.Do(func() {
			atomic.AddInt64(&t.activeProxyConns, -1)
		})
	}
}

func (t *Tunnel) shouldUseSSHForHost(host string) bool {
	if matched, ok := t.cachedDomainMatch(host); ok {
		return matched
	}

	hostOnly := host
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = splitHost
	} else {
		hostOnly = strings.Split(host, ":")[0]
	}
	hostOnly = strings.ToLower(strings.Trim(hostOnly, "."))

	t.domainMutex.RLock()
	domains := cloneStringBoolMap(t.domains)
	t.domainMutex.RUnlock()

	for domain := range domains {
		if domain == "" {
			continue
		}
		if strings.HasSuffix(hostOnly, strings.ToLower(domain)) {
			t.rememberDomainMatch(host, true)
			return true
		}
	}

	t.rememberDomainMatch(host, false)
	return false
}

func (t *Tunnel) cachedDomainMatch(host string) (bool, bool) {
	t.domainMutex.RLock()
	defer t.domainMutex.RUnlock()

	value, ok := t.domainMatchCache[host]
	return value, ok
}

func (t *Tunnel) rememberDomainMatch(host string, matched bool) {
	t.domainMutex.Lock()
	defer t.domainMutex.Unlock()

	if t.domainMatchCache == nil {
		t.domainMatchCache = make(map[string]bool)
	}
	t.domainMatchCache[host] = matched
}

func (t *Tunnel) serveTCPProxy(ctx context.Context, address string, name string, handler func(net.Conn)) {
	backoff := defaultListenerRetryMin

	for {
		if ctx.Err() != nil {
			return
		}

		listener, err := net.Listen("tcp", address)
		if err != nil {
			log.Printf("Failed to start %s proxy server: %v", name, err)
			if !waitWithContext(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		log.Printf("%s proxy server listening on %s", name, address)
		backoff = defaultListenerRetryMin

		err = t.acceptLoop(ctx, listener, name, handler)
		_ = listener.Close()
		if ctx.Err() != nil {
			return
		}

		t.recordListenerRestart()
		if err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("%s listener restarting after accept error: %v", name, err)
		}

		if !waitWithContext(ctx, defaultListenerRetryMin) {
			return
		}
	}
}

func (t *Tunnel) acceptLoop(ctx context.Context, listener net.Listener, name string, handler func(net.Conn)) error {
	safeClose := make(chan struct{})
	safe.GO(func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-safeClose:
		}
	})
	defer close(safeClose)

	backoff := defaultListenerRetryMin
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}

			t.recordAcceptError()
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				log.Printf("%s accept temporary error: %v", name, err)
				if !waitWithContext(ctx, backoff) {
					return nil
				}
				backoff = nextBackoff(backoff)
				continue
			}

			return err
		}

		backoff = defaultListenerRetryMin
		safe.GO(func() {
			handler(conn)
		})
	}
}

func waitWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	if current <= 0 {
		return defaultListenerRetryMin
	}
	current *= 2
	if current > defaultListenerRetryMax {
		return defaultListenerRetryMax
	}
	return current
}
