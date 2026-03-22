package tunnel

import (
	"context"
	"log"
	"net"
	"ssh-tunnel/safe"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// reconnectSSH 实现SSH连接的重连逻辑
func (t *Tunnel) ReconnectSSH(ctx context.Context) {
	t.ReconnectSSHWithSource(ctx, "default")
}

func (t *Tunnel) ReconnectSSHWithSource(ctx context.Context, source string) {
	if source == "" {
		source = "unknown"
	}

	reconnectCtx := t.reconnectContext(ctx)
	if reconnectCtx.Err() != nil {
		log.Printf("跳过SSH重连，context已结束(source=%s)", source)
		return
	}

	if !t.beginReconnect(reconnectCtx) {
		log.Printf("跳过SSH重连，已有连接或重连中(source=%s)", source)
		return
	}
	defer t.endReconnect()

	retryInterval := t.retryInterval
	if retryInterval <= 0 {
		retryInterval = defaultReconnectRetry
	}

	maxRetries := t.reconnectMaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultReconnectMaxRetries
	}

	maxInterval := t.reconnectMaxInterval
	if maxInterval <= 0 {
		maxInterval = defaultReconnectMaxInterval
	}

	log.Printf("正在尝试重新连接SSH服务器: %s (source=%s)", t.serverAddress, source)

	backoff := retryInterval
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if reconnectCtx.Err() != nil {
			log.Printf("跳过SSH重连，context已结束(source=%s)", source)
			return
		}

		cl, err := t.dialSSH()
		if err == nil {
			t.setSSHClient(cl, source)
			t.startKeepAlive(reconnectCtx, cl)
			return
		}

		t.recordReconnectFailure(err)
		if attempt == maxRetries {
			log.Printf("SSH重连失败，已达到最大重试次数(source=%s, attempts=%d): %v", source, attempt, err)
			return
		}

		waitInterval := backoff
		if waitInterval > maxInterval {
			waitInterval = maxInterval
		}

		log.Printf("SSH重连失败，准备重试(source=%s, attempt=%d/%d, retryIn=%s): %v", source, attempt, maxRetries, waitInterval, err)
		select {
		case <-reconnectCtx.Done():
			return
		case <-time.After(waitInterval):
		}

		backoff *= 2
		if backoff > maxInterval {
			backoff = maxInterval
		}
	}
}

func (t *Tunnel) beginReconnect(ctx context.Context) bool {
	for {
		t.reconnectMutex.Lock()
		if t.client != nil {
			t.reconnectMutex.Unlock()
			return false
		}

		if !t.reconnecting {
			t.reconnecting = true
			t.reconnectDone = make(chan struct{})
			t.reconnectMutex.Unlock()
			return true
		}

		waitCh := t.reconnectDone
		t.reconnectMutex.Unlock()

		select {
		case <-ctx.Done():
			return false
		case <-waitCh:
		}
	}
}

func (t *Tunnel) endReconnect() {
	t.reconnectMutex.Lock()
	if t.reconnecting {
		t.reconnecting = false
		if t.reconnectDone != nil {
			close(t.reconnectDone)
			t.reconnectDone = nil
		}
	}
	t.reconnectMutex.Unlock()
}

func (t *Tunnel) setSSHClient(cl *ssh.Client, source string) {
	t.reconnectMutex.Lock()
	oldClient := t.client
	t.client = cl
	currentCount := t.reconnectCount
	isFirstConnect := false
	if cl != nil {
		t.consecutiveReconnectFailures = 0
		t.lastReconnectError = ""
		t.lastReconnectAt = time.Now()
		t.lastReconnectFailureAt = time.Time{}
		t.resetExitIPInfo()
		if t.sshConnectedOnce {
			t.reconnectCount++
			currentCount = t.reconnectCount
		} else {
			t.sshConnectedOnce = true
			isFirstConnect = true
			currentCount = t.reconnectCount
		}
	}
	t.reconnectMutex.Unlock()

	if cl != nil {
		localAddr := safeSSHAddrString(func() net.Addr { return cl.LocalAddr() })
		remoteAddr := safeSSHAddrString(func() net.Addr { return cl.RemoteAddr() })
		if isFirstConnect {
			log.Printf("SSH首次连接成功(source=%s, reconnectCount=%d, local=%s, remote=%s)", source, currentCount, localAddr, remoteAddr)
		} else {
			log.Printf("SSH重连成功(source=%s, reconnectCount=%d, local=%s, remote=%s)", source, currentCount, localAddr, remoteAddr)
		}
	}

	if oldClient != nil && oldClient != cl {
		closeSSHClient(oldClient)
		log.Printf("旧SSH连接已释放(source=%s)", source)
	}
}

func (t *Tunnel) startKeepAlive(ctx context.Context, client *ssh.Client) {
	safe.GO(func() {
		var once sync.Once
		if !t.keepAliveMonitor(ctx, &once, client) {
			return
		}

		if !t.invalidateSSHClientIfMatch(client, "keepalive monitor stopped") {
			return
		}

		log.Printf("SSH连接已关闭，准备重新连接")
		safe.GO(func() {
			t.ReconnectSSHWithSource(ctx, "keepalive-monitor")
		})
	})
}

func (t *Tunnel) recordReconnectFailure(err error) {
	t.reconnectMutex.Lock()
	defer t.reconnectMutex.Unlock()

	t.consecutiveReconnectFailures++
	t.lastReconnectFailureAt = time.Now()
	if err != nil {
		t.lastReconnectError = err.Error()
	}
}

func (t *Tunnel) dialSSH() (*ssh.Client, error) {
	// 尝试建立新的SSH连接
	if t.sshDialFn != nil {
		return t.sshDialFn()
	}

	timeout := t.sshDialTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	cl, err := ssh.Dial("tcp", t.serverAddress, &ssh.ClientConfig{
		User:            t.user,
		Auth:            t.auth,
		HostKeyCallback: t.hostKeys,
		Timeout:         timeout,
	})

	if err != nil {
		log.Printf("SSH连接失败: %v", err)
		return nil, err
	}

	// 连接成功
	log.Println("成功重新连接到SSH服务器")
	return cl, nil
}
