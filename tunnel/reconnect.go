package tunnel

import (
	"context"
	"log"
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

	log.Printf("正在尝试重新连接SSH服务器: %s (source=%s)", t.serverAddress, source)
	cl, err2 := t.dialSSH()
	if err2 == nil {
		t.setSSHClient(cl, source)
		t.startKeepAlive(reconnectCtx, cl)
		return
	}
	log.Printf("SSH单次重连失败(source=%s): %v", source, err2)
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
		localAddr := "<nil>"
		remoteAddr := "<nil>"
		if cl.LocalAddr() != nil {
			localAddr = cl.LocalAddr().String()
		}
		if cl.RemoteAddr() != nil {
			remoteAddr = cl.RemoteAddr().String()
		}
		if isFirstConnect {
			log.Printf("SSH首次连接成功(source=%s, reconnectCount=%d, local=%s, remote=%s)", source, currentCount, localAddr, remoteAddr)
		} else {
			log.Printf("SSH重连成功(source=%s, reconnectCount=%d, local=%s, remote=%s)", source, currentCount, localAddr, remoteAddr)
		}
	}

	if oldClient != nil && oldClient != cl {
		_ = oldClient.Close()
		log.Printf("旧SSH连接已释放(source=%s)", source)
	}
}

func (t *Tunnel) startKeepAlive(ctx context.Context, client *ssh.Client) {
	safe.GO(func() {
		var once sync.Once
		t.keepAliveMonitor(ctx, &once, client)

		if !t.invalidateSSHClientIfMatch(client, "keepalive monitor stopped") {
			return
		}

		log.Printf("SSH连接已关闭，准备重新连接")
		safe.GO(func() {
			t.ReconnectSSHWithSource(ctx, "keepalive-monitor")
		})
	})
}

func (t *Tunnel) dialSSH() (*ssh.Client, error) {
	// 尝试建立新的SSH连接
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
