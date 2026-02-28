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
	reconnectCtx := t.reconnectContext(ctx)
	if reconnectCtx.Err() != nil {
		return
	}

	if !t.beginReconnect(reconnectCtx) {
		return
	}
	defer t.endReconnect()

	log.Printf("正在尝试重新连接SSH服务器: %s", t.serverAddress)
	cl, err2 := t.dialSSH()
	if err2 == nil {
		t.setSSHClient(cl)
		t.startKeepAlive(reconnectCtx)
		return
	}
	log.Println(err2)

	maxRetries := t.reconnectMaxRetries
	if maxRetries <= 0 {
		maxRetries = 20
	}
	// 初始重试间隔（秒）
	baseRetryInterval := t.retryInterval
	if baseRetryInterval == 0 {
		baseRetryInterval = 1 * time.Second
	}
	maxRetryInterval := t.reconnectMaxInterval
	if maxRetryInterval <= 0 {
		maxRetryInterval = 5 * time.Second
	}

	// 指数退避策略的重试
	for i := 0; i < maxRetries; i++ {
		// 检查上下文是否已取消
		if reconnectCtx.Err() != nil {
			log.Println("重连操作被取消")
			return
		}

		// 计算当前重试的等待时间（使用指数退避策略）
		retryInterval := baseRetryInterval * time.Duration(1<<uint(i))
		if retryInterval > maxRetryInterval {
			retryInterval = maxRetryInterval
		}

		log.Printf("等待 %v 秒后尝试重连 (尝试 %d/%d)", retryInterval.Seconds(), i+1, maxRetries)
		select {
		case <-reconnectCtx.Done():
			return
		case <-time.After(retryInterval):
			// 继续尝试重连
		}

		cl, err := t.dialSSH()
		if err != nil {
			log.Printf("SSH连接尝试失败: %v，继续重试", err)
			continue
		}

		t.setSSHClient(cl)
		t.startKeepAlive(reconnectCtx)
		return
	}

	log.Printf("达到最大重试次数 (%d)，将在服务需要时重新尝试", maxRetries)
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

func (t *Tunnel) setSSHClient(cl *ssh.Client) {
	t.reconnectMutex.Lock()
	t.client = cl
	t.reconnectMutex.Unlock()
}

func (t *Tunnel) startKeepAlive(ctx context.Context) {
	safe.GO(func() {
		var once sync.Once
		var wg sync.WaitGroup
		wg.Add(1)
		t.keepAliveMonitor(ctx, &once, &wg)

		t.invalidateSSHClient("keepalive monitor stopped")

		log.Printf("SSH连接已关闭，准备重新连接")
		safe.GO(func() {
			t.ReconnectSSH(ctx)
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
		log.Printf("SSH重连失败: %v，将继续重试", err)
		return nil, err
	}

	// 连接成功
	log.Println("成功重新连接到SSH服务器")
	return cl, nil
}
