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

	t.runtimeMu.RLock()
	retryInterval, maxRetries, maxInterval, address := t.retryInterval, t.reconnectMaxRetries, t.reconnectMaxInterval, t.serverAddress
	t.runtimeMu.RUnlock()
	if retryInterval <= 0 {
		retryInterval = defaultReconnectRetry
	}

	if maxRetries <= 0 {
		maxRetries = defaultReconnectMaxRetries
	}

	if maxInterval <= 0 {
		maxInterval = defaultReconnectMaxInterval
	}

	log.Printf("正在尝试重新连接SSH服务器: %s (source=%s)", address, source)

	backoff := retryInterval
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if reconnectCtx.Err() != nil {
			log.Printf("跳过SSH重连，context已结束(source=%s)", source)
			return
		}

		// A runtime refresh must not publish a new identity while an old
		// handshake can still add a channel-capable member to this pool.
		t.runtimeMu.RLock()
		cl, err := t.dialSSH()
		if reconnectCtx.Err() != nil {
			closeSSHClient(cl)
			t.runtimeMu.RUnlock()
			return
		}
		if err == nil {
			member := t.addSSHMember(cl, source)
			t.runtimeMu.RUnlock()
			t.startKeepAlive(reconnectCtx, member)
			return
		}
		t.runtimeMu.RUnlock()

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
		t.runtimeMu.RLock()
		targetSize := t.configuredPoolSize()
		t.runtimeMu.RUnlock()
		t.reconnectMutex.Lock()
		if t.currentPoolSize() >= targetSize {
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

func (t *Tunnel) addSSHMember(cl *ssh.Client, source string) *SSHPoolMember {
	t.reconnectMutex.Lock()
	currentCount := t.reconnectCount
	isFirstConnect := false
	if cl != nil {
		t.consecutiveReconnectFailures = 0
		t.lastReconnectError = ""
		t.lastReconnectAt = time.Now()
		t.lastReconnectFailureAt = time.Time{}
		t.resetExitIPInfo()
		if t.reconnectRecoveryPending {
			t.reconnectCount++
			currentCount = t.reconnectCount
			t.reconnectRecoveryPending = false
		} else if t.sshConnectedOnce {
			currentCount = t.reconnectCount
		} else {
			t.sshConnectedOnce = true
			isFirstConnect = true
			currentCount = t.reconnectCount
		}
	}
	t.reconnectMutex.Unlock()

	if cl == nil {
		return nil
	}

	member := &SSHPoolMember{
		Client:     cl,
		ProfileID:  t.profileID,
		State:      sshMemberHealthy,
		Generation: t.nextSSHGeneration(),
		CreatedAt:  time.Now(),
	}
	t.sshPoolMu.Lock()
	member.ID = nextGlobalMemberID()
	t.sshPool = append(t.sshPool, member)
	t.sshPoolMu.Unlock()

	if cl != nil {
		t.resetSSHFailureState()
		localAddr := safeSSHAddrString(func() net.Addr { return cl.LocalAddr() })
		remoteAddr := safeSSHAddrString(func() net.Addr { return cl.RemoteAddr() })
		if isFirstConnect {
			log.Printf("SSH首次连接成功(source=%s, reconnectCount=%d, generation=%d, memberId=%d, local=%s, remote=%s)", source, currentCount, member.Generation, member.ID, localAddr, remoteAddr)
		} else {
			log.Printf("SSH重连成功(source=%s, reconnectCount=%d, generation=%d, memberId=%d, local=%s, remote=%s)", source, currentCount, member.Generation, member.ID, localAddr, remoteAddr)
		}
	}
	return member
}

func (t *Tunnel) startKeepAlive(ctx context.Context, member *SSHPoolMember) {
	safe.GO(func() {
		var once sync.Once
		if !t.keepAliveMonitor(ctx, &once, member) {
			return
		}

		if member == nil || !t.evictMember(member, "keepalive monitor stopped") {
			return
		}

		log.Printf("SSH连接已关闭，准备补充连接池成员(memberId=%d,generation=%d)", member.ID, member.Generation)
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
