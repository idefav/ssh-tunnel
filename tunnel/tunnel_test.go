package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type mockHalfCloseConn struct {
	readData         []byte
	readOffset       int
	written          []byte
	closeCalled      bool
	closeWriteCalled bool
}

func (m *mockHalfCloseConn) Read(p []byte) (int, error) {
	if m.readOffset >= len(m.readData) {
		return 0, io.EOF
	}
	n := copy(p, m.readData[m.readOffset:])
	m.readOffset += n
	return n, nil
}

func (m *mockHalfCloseConn) Write(p []byte) (int, error) {
	m.written = append(m.written, p...)
	return len(p), nil
}

func (m *mockHalfCloseConn) Close() error {
	m.closeCalled = true
	return nil
}

func (m *mockHalfCloseConn) CloseWrite() error {
	m.closeWriteCalled = true
	return nil
}

func (m *mockHalfCloseConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (m *mockHalfCloseConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (m *mockHalfCloseConn) SetDeadline(time.Time) error      { return nil }
func (m *mockHalfCloseConn) SetReadDeadline(time.Time) error  { return nil }
func (m *mockHalfCloseConn) SetWriteDeadline(time.Time) error { return nil }

func TestRecordSSHDialFailureTriggersReconnectAfterTimeoutBurst(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.client = &ssh.Client{}

	for i := 0; i < sshDialTimeoutLimit-1; i++ {
		class, reconnect := tunnel.recordSSHDialFailure(context.DeadlineExceeded)
		if class != failureClassSSHChannelTimeout {
			t.Fatalf("expected timeout failure class, got %q", class)
		}
		if reconnect {
			t.Fatalf("unexpected reconnect on attempt %d", i+1)
		}
	}

	class, reconnect := tunnel.recordSSHDialFailure(context.DeadlineExceeded)
	if class != failureClassSSHChannelTimeout {
		t.Fatalf("expected timeout failure class, got %q", class)
	}
	if !reconnect {
		t.Fatal("expected reconnect after timeout burst")
	}

	stats := tunnel.SnapshotSSHConnectionStats()
	if stats.SSHDialTimeoutStreak != sshDialTimeoutLimit {
		t.Fatalf("expected timeout streak %d, got %d", sshDialTimeoutLimit, stats.SSHDialTimeoutStreak)
	}
	if stats.LastSSHFailureClass != failureClassSSHChannelTimeout {
		t.Fatalf("expected last failure class %q, got %q", failureClassSSHChannelTimeout, stats.LastSSHFailureClass)
	}
}

func TestRecordSSHDialSuccessResetsTimeoutBurstState(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.recordSSHDialFailure(context.DeadlineExceeded)
	tunnel.recordSSHDialFailure(context.DeadlineExceeded)

	tunnel.recordSSHDialSuccess()

	stats := tunnel.SnapshotSSHConnectionStats()
	if stats.SSHDialTimeoutStreak != 0 {
		t.Fatalf("expected timeout streak reset, got %d", stats.SSHDialTimeoutStreak)
	}
	if stats.LastSSHFailureClass != "" {
		t.Fatalf("expected last failure class reset, got %q", stats.LastSSHFailureClass)
	}
}

func TestCopyProxyDataUsesHalfCloseWhenAvailable(t *testing.T) {
	tunnel := newTestTunnel()
	dst := &mockHalfCloseConn{}
	src := &mockHalfCloseConn{readData: []byte("hello")}

	tunnel.copyProxyData(dst, src, true)

	if string(dst.written) != "hello" {
		t.Fatalf("expected copied payload, got %q", string(dst.written))
	}
	if !dst.closeWriteCalled {
		t.Fatal("expected CloseWrite to be used")
	}
	if dst.closeCalled {
		t.Fatal("did not expect full Close during copy")
	}
}

func TestPooledConnCloseWriteDoesNotReleaseChannel(t *testing.T) {
	member := &SSHPoolMember{ID: 1, State: sshMemberHealthy, ActiveChannels: 1}
	raw := &mockHalfCloseConn{}
	conn := &pooledConn{Conn: raw, member: member}

	if err := conn.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite failed: %v", err)
	}
	if !raw.closeWriteCalled {
		t.Fatal("expected underlying CloseWrite to be called")
	}
	if raw.closeCalled {
		t.Fatal("did not expect underlying Close during half-close")
	}
	if got := atomic.LoadInt64(&member.ActiveChannels); got != 1 {
		t.Fatalf("expected active channel to stay at 1 after CloseWrite, got %d", got)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if got := atomic.LoadInt64(&member.ActiveChannels); got != 0 {
		t.Fatalf("expected active channel to be released on final Close, got %d", got)
	}
}

func TestClassifyDialErrorPrefersSSHTransportFailures(t *testing.T) {
	tunnel := newTestTunnel()
	err := errors.New("ssh: unexpected packet in response to channel open: <nil>")
	if got := tunnel.classifyDialError(err, true); got != failureClassSSHTransportDead {
		t.Fatalf("expected ssh transport failure class, got %q", got)
	}
}

func TestPickSSHMemberPrefersHealthyLeastLoaded(t *testing.T) {
	tunnel := newTestTunnel()
	suspect := &SSHPoolMember{ID: 1, State: sshMemberSuspect, Client: &ssh.Client{}}
	busy := &SSHPoolMember{ID: 2, State: sshMemberHealthy, Client: &ssh.Client{}, ActiveChannels: 3}
	idle := &SSHPoolMember{ID: 3, State: sshMemberHealthy, Client: &ssh.Client{}, ActiveChannels: 1}
	tunnel.sshPool = []*SSHPoolMember{suspect, busy, idle}

	got := tunnel.pickSSHMember()
	if got != idle {
		t.Fatalf("expected least-loaded healthy member, got %#v", got)
	}
}

func TestPickSSHMemberRoundRobinUsesOldestHealthy(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshPoolBalanceStrategy = sshPoolBalanceRoundRobin
	newer := &SSHPoolMember{ID: 2, State: sshMemberHealthy, Client: &ssh.Client{}, LastUsedAt: time.Now()}
	older := &SSHPoolMember{ID: 3, State: sshMemberHealthy, Client: &ssh.Client{}, LastUsedAt: time.Now().Add(-time.Minute)}
	suspect := &SSHPoolMember{ID: 1, State: sshMemberSuspect, Client: &ssh.Client{}, LastUsedAt: time.Now().Add(-time.Hour)}
	tunnel.sshPool = []*SSHPoolMember{newer, older, suspect}

	got := tunnel.pickSSHMember()
	if got != older {
		t.Fatalf("expected round-robin to choose oldest healthy member, got %#v", got)
	}
}

func TestConfiguredProbeURLsPrefersListOverLegacyURL(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshProbeURL = "https://legacy.example/json"
	tunnel.sshProbeURLs = "https://one.example/json, https://two.example/trace"

	got := tunnel.configuredProbeURLs()
	want := []string{"https://one.example/json", "https://two.example/trace"}
	if len(got) != len(want) {
		t.Fatalf("expected %d probe URLs, got %#v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("probe URL %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestConfiguredProbeURLsFallsBackToLegacyURL(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshProbeURL = "https://legacy.example/json"
	tunnel.sshProbeURLs = ""

	got := tunnel.configuredProbeURLs()
	if len(got) != 1 || got[0] != tunnel.sshProbeURL {
		t.Fatalf("expected legacy probe URL, got %#v", got)
	}
}

func TestConfiguredProbeURLsUsesDefaultListWhenUnset(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshProbeURL = ""
	tunnel.sshProbeURLs = ""

	got := tunnel.configuredProbeURLs()
	if len(got) < 2 {
		t.Fatalf("expected built-in multi-url probe list, got %#v", got)
	}
}

func TestRecordMemberDialFailureSameTargetDoesNotEvict(t *testing.T) {
	tunnel := newTestTunnel()
	member := &SSHPoolMember{ID: 1, State: sshMemberHealthy}

	for i := 0; i < sshDialTimeoutLimit; i++ {
		class, reconnect := tunnel.recordMemberDialFailure(context.Background(), member, "example.com:443", context.DeadlineExceeded)
		if class != failureClassSSHChannelTimeout {
			t.Fatalf("expected timeout failure class, got %q", class)
		}
		if reconnect {
			t.Fatalf("same-target timeout should not evict member on attempt %d", i+1)
		}
	}
	if member.State == sshMemberEvicted {
		t.Fatal("same-target timeouts should not evict ssh member")
	}
}

func TestDisconnectSSHClientClearsEntirePool(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshConnectedOnce = true
	tunnel.sshPool = []*SSHPoolMember{
		{ID: 1, State: sshMemberHealthy, Client: &ssh.Client{}},
		{ID: 2, State: sshMemberSuspect, Client: &ssh.Client{}},
	}

	tunnel.DisconnectSSHClient()

	if got := tunnel.currentPoolSize(); got != 0 {
		t.Fatalf("expected active pool to be empty, got %d", got)
	}
	if got := len(tunnel.sshPoolEvicted); got != 2 {
		t.Fatalf("expected two evicted history entries, got %d", got)
	}
	if !tunnel.reconnectRecoveryPending {
		t.Fatal("expected reconnect recovery to be pending after disconnecting an established pool")
	}
}

func TestEvictedPoolHistoryIsBounded(t *testing.T) {
	tunnel := newTestTunnel()
	tunnel.sshConnectedOnce = true

	for i := 0; i < maxEvictedPoolHistory+5; i++ {
		member := &SSHPoolMember{ID: uint64(i + 1), State: sshMemberHealthy, Client: &ssh.Client{}}
		tunnel.sshPool = append(tunnel.sshPool, member)
		tunnel.evictMember(member, "test")
	}

	if got := tunnel.currentPoolSize(); got != 0 {
		t.Fatalf("expected no active members, got %d", got)
	}
	if got := len(tunnel.sshPoolEvicted); got != maxEvictedPoolHistory {
		t.Fatalf("expected evicted history capped at %d, got %d", maxEvictedPoolHistory, got)
	}
	if first := tunnel.sshPoolEvicted[0].ID; first != 6 {
		t.Fatalf("expected oldest retained evicted member id 6, got %d", first)
	}
}
