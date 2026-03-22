package tunnel

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func newTestTunnel() *Tunnel {
	return &Tunnel{
		retryInterval:        time.Millisecond,
		reconnectMaxRetries:  3,
		reconnectMaxInterval: 2 * time.Millisecond,
	}
}

func TestReconnectSSHWithSourceHonorsRetryLimits(t *testing.T) {
	tunnel := newTestTunnel()
	var attempts atomic.Int32
	tunnel.sshDialFn = func() (*ssh.Client, error) {
		attempts.Add(1)
		return nil, errors.New("dial failed")
	}

	tunnel.ReconnectSSHWithSource(context.Background(), "test")

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 dial attempts, got %d", got)
	}

	stats := tunnel.SnapshotSSHConnectionStats()
	if stats.ConnectionCount != 0 {
		t.Fatalf("expected no active ssh client, got %d", stats.ConnectionCount)
	}
	if stats.ConsecutiveReconnectFailures != 3 {
		t.Fatalf("expected 3 consecutive failures, got %d", stats.ConsecutiveReconnectFailures)
	}
	if stats.LastReconnectError == "" {
		t.Fatal("expected last reconnect error to be recorded")
	}
	if stats.LastReconnectFailureAt.IsZero() {
		t.Fatal("expected last reconnect failure time to be recorded")
	}
}

func TestReconnectSSHWithSourceSuccessResetsFailureState(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.keepAlive = KeepAliveConfig{}
	tunnel.consecutiveReconnectFailures = 5
	tunnel.lastReconnectError = "old error"
	tunnel.lastReconnectFailureAt = time.Now()

	var attempts atomic.Int32
	tunnel.sshDialFn = func() (*ssh.Client, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("dial failed")
		}
		return &ssh.Client{}, nil
	}

	tunnel.ReconnectSSHWithSource(context.Background(), "test")

	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts before success, got %d", got)
	}

	stats := tunnel.SnapshotSSHConnectionStats()
	if stats.ConnectionCount != 1 {
		t.Fatalf("expected active ssh client after success, got %d", stats.ConnectionCount)
	}
	if stats.ConsecutiveReconnectFailures != 0 {
		t.Fatalf("expected failure counter reset, got %d", stats.ConsecutiveReconnectFailures)
	}
	if stats.LastReconnectError != "" {
		t.Fatalf("expected last reconnect error to be cleared, got %q", stats.LastReconnectError)
	}
	if stats.LastReconnectAt.IsZero() {
		t.Fatal("expected last reconnect success time to be set")
	}
	if !stats.LastReconnectFailureAt.IsZero() {
		t.Fatal("expected last reconnect failure time to be cleared after success")
	}
}

func TestReconnectSSHWithSourceSingleFlight(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.keepAlive = KeepAliveConfig{}

	var attempts atomic.Int32
	tunnel.sshDialFn = func() (*ssh.Client, error) {
		attempts.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &ssh.Client{}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tunnel.ReconnectSSHWithSource(context.Background(), "parallel")
		}()
	}
	wg.Wait()

	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected a single dial attempt, got %d", got)
	}
	if tunnel.PeekSSHClient() == nil {
		t.Fatal("expected ssh client to be established")
	}
}

func TestInvalidateSSHClientIfMatchKeepsNewClient(t *testing.T) {
	tunnel := newTestTunnel()
	oldClient := &ssh.Client{}
	newClient := &ssh.Client{}
	tunnel.client = newClient

	if tunnel.invalidateSSHClientIfMatch(oldClient, "old client failure") {
		t.Fatal("expected invalidate to fail for non-current client")
	}
	if tunnel.PeekSSHClient() != newClient {
		t.Fatal("expected current ssh client to remain unchanged")
	}
}

func TestKeepAliveProbeTimeout(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshDestTimeout = 2 * time.Second
	if got := tunnel.keepAliveProbeTimeout(); got != defaultKeepAliveProbeTimeout {
		t.Fatalf("expected default keepalive timeout %s, got %s", defaultKeepAliveProbeTimeout, got)
	}

	tunnel.sshDestTimeout = 9 * time.Second
	if got := tunnel.keepAliveProbeTimeout(); got != 9*time.Second {
		t.Fatalf("expected keepalive timeout to use ssh destination timeout, got %s", got)
	}
}
