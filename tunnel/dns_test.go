package tunnel

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"ssh-tunnel/cfg"
	"ssh-tunnel/dnsproxy"
	"sync"
	"testing"
	"time"

	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
	"golang.org/x/net/dns/dnsmessage"
)

func dnsQuery(t *testing.T, host string) []byte {
	t.Helper()
	wire, err := (&dnsmessage.Message{Header: dnsmessage.Header{ID: 7, RecursionDesired: true}, Questions: []dnsmessage.Question{{Name: dnsmessage.MustNewName(host + "."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}}).Pack()
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func serveDNSStream(stream io.ReadWriter, octet byte, code dnsmessage.RCode) error {
	var prefix [2]byte
	if _, err := io.ReadFull(stream, prefix[:]); err != nil {
		return err
	}
	wire := make([]byte, binary.BigEndian.Uint16(prefix[:]))
	if _, err := io.ReadFull(stream, wire); err != nil {
		return err
	}
	var m dnsmessage.Message
	if err := m.Unpack(wire); err != nil {
		return err
	}
	m.Response, m.RecursionAvailable, m.RCode = true, true, code
	if code == 0 {
		m.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: m.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{192, 0, 2, octet}}}}
	}
	reply, err := m.Pack()
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint16(prefix[:], uint16(len(reply)))
	_, err = stream.Write(append(prefix[:], reply...))
	return err
}

func dnsPipe(octet byte, code dnsmessage.RCode) net.Conn {
	client, server := net.Pipe()
	go func() { defer server.Close(); _ = serveDNSStream(server, octet, code) }()
	return client
}

func dnsTestPolicy(t *testing.T, routes []cfg.EffectiveRoute) *dnsPolicy {
	t.Helper()
	p, err := newDNSPolicy(cfg.ProfileStore{ActiveProfileID: "default", Profiles: map[string]cfg.SSHProfile{"default": {}, "a": {DNSUpstreams: []string{"10.0.0.53:53", "10.0.0.54:53"}}, "b": {}}}, routes, []string{"1.1.1.1:53"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDNSMatchesQuestionAndInheritsTCPGroups(t *testing.T) {
	groups := cfg.RouteStore{Groups: []cfg.RouteGroup{
		{ID: "g", Name: "Group", Enabled: true, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"a"}, Rules: []cfg.RouteRule{
			{ID: "suffix", Type: cfg.RouteTypeDomain, Pattern: "example.test", Enabled: true},
			{ID: "override", Type: cfg.RouteTypeDomain, Pattern: "api.example.test", Enabled: true, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"b"}},
			{ID: "disabled", Type: cfg.RouteTypeDomain, Pattern: "off.example.test", Enabled: false, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"b"}},
			{ID: "resolver-ip", Type: cfg.RouteTypeIP, Pattern: "10.0.0.53", Enabled: true, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"b"}},
		}},
		{ID: "off", Enabled: false, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"b"}, Rules: []cfg.RouteRule{{ID: "offrule", Type: cfg.RouteTypeDomain, Pattern: "offgroup.test", Enabled: true}}},
	}}
	tun := &Tunnel{dnsPolicy: dnsTestPolicy(t, cfg.ResolveEffectiveRoutes(groups))}
	for _, tc := range []struct{ host, id, resolver string }{{"WWW.Example.Test", "a", "10.0.0.53:53"}, {"api.example.test", "b", "1.1.1.1:53"}, {"off.example.test", "a", "10.0.0.53:53"}, {"other.test", "default", "1.1.1.1:53"}, {"offgroup.test", "default", "1.1.1.1:53"}} {
		t.Run(tc.host, func(t *testing.T) {
			var calls []string
			tun.dnsDialer = func(_ context.Context, id, address string) (net.Conn, error) {
				calls = append(calls, id+"/"+address)
				return dnsPipe(1, 0), nil
			}
			if _, err := tun.queryDNS(context.Background(), dnsQuery(t, tc.host)); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, []string{tc.id + "/" + tc.resolver}) {
				t.Fatalf("wrong DNS route: %v", calls)
			}
		})
	}
}

func TestDNSFailoverAndNoCrossGroupOrOverrideFallback(t *testing.T) {
	rule := testRoute("fixed", "fixed.test", cfg.RouteTypeDomain, true, "a")
	tun := &Tunnel{dnsPolicy: dnsTestPolicy(t, []cfg.EffectiveRoute{rule})}
	var calls []string
	tun.dnsDialer = func(_ context.Context, id, address string) (net.Conn, error) {
		calls = append(calls, id+"/"+address)
		return nil, errors.New("unavailable")
	}
	if _, err := tun.queryDNS(context.Background(), dnsQuery(t, "fixed.test")); err == nil {
		t.Fatal("failed route succeeded")
	}
	if !reflect.DeepEqual(calls, []string{"a/10.0.0.53:53", "a/10.0.0.54:53"}) {
		t.Fatalf("unexpected fallback: %v", calls)
	}
	rule.Strategy, rule.TargetProfileIDs = cfg.RouteStrategyRandom, []string{"a", "b"}
	tun.dnsPolicy = dnsTestPolicy(t, []cfg.EffectiveRoute{rule})
	firstIDs := map[string]bool{}
	for i := 0; i < 40; i++ {
		calls = nil
		tun.dnsDialer = func(_ context.Context, id, address string) (net.Conn, error) {
			calls = append(calls, id)
			if id == "b" {
				return dnsPipe(2, 0), nil
			}
			return nil, errors.New("failed")
		}
		if _, err := tun.queryDNS(context.Background(), dnsQuery(t, "fixed.test")); err != nil {
			t.Fatal(err)
		}
		firstIDs[calls[0]] = true
		for _, id := range calls {
			if id == "default" {
				t.Fatal("random crossed group")
			}
		}
	}
	if len(firstIDs) != 2 {
		t.Fatal("random selection was cached across queries")
	}
	// Valid negative responses stop immediately, even when another resolver exists.
	calls = nil
	tun.dnsPolicy = dnsTestPolicy(t, []cfg.EffectiveRoute{testRoute("fixed", "fixed.test", cfg.RouteTypeDomain, true, "a")})
	tun.dnsDialer = func(_ context.Context, id, address string) (net.Conn, error) {
		calls = append(calls, address)
		return dnsPipe(0, dnsmessage.RCodeNameError), nil
	}
	if _, err := tun.queryDNS(context.Background(), dnsQuery(t, "fixed.test")); err != nil || len(calls) != 1 {
		t.Fatalf("negative fallback: %v %v", err, calls)
	}
}

func TestDNSPolicyUpdateWaitsAndDoesNotCacheAnswers(t *testing.T) {
	tun := &Tunnel{dnsPolicy: dnsTestPolicy(t, nil)}
	entered, release, changed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	var mu sync.Mutex
	var ids []string
	tun.dnsDialer = func(ctx context.Context, id, address string) (net.Conn, error) {
		mu.Lock()
		ids = append(ids, id)
		mu.Unlock()
		once.Do(func() {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
			}
		})
		return dnsPipe(1, 0), nil
	}
	query := dnsQuery(t, "cached.test")
	result := make(chan error, 1)
	go func() { _, err := tun.queryDNS(context.Background(), query); result <- err }()
	<-entered
	next := dnsTestPolicy(t, nil)
	next.defaultProfile = "b"
	go func() { tun.runtimeMu.Lock(); tun.dnsPolicy = next; tun.runtimeMu.Unlock(); close(changed) }()
	select {
	case <-changed:
		t.Fatal("policy published during old query")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	<-changed
	if _, err := tun.queryDNS(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(ids, []string{"default", "b"}) {
		t.Fatalf("stale policy/cache: %v", ids)
	}
}

func TestDNSDeadlineWhilePolicyLocked(t *testing.T) {
	tun := &Tunnel{dnsPolicy: dnsTestPolicy(t, nil)}
	tun.runtimeMu.Lock()
	defer tun.runtimeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := tun.queryDNS(ctx, dnsQuery(t, "example.test")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait ignored deadline: %v", err)
	}
}

// A real SSH server accepts only direct-tcpip channels, records their target,
// and serves DNS itself. No public DNS server or system resolver is contacted.
func dnsSSHFixture(t *testing.T, octet byte) (*ssh.Client, <-chan string, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	targets := make(chan string, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		config := &ssh.ServerConfig{NoClientAuth: true}
		config.AddHostKey(signer)
		server, channels, requests, err := ssh.NewServerConn(conn, config)
		if err != nil {
			conn.Close()
			return
		}
		defer server.Close()
		go ssh.DiscardRequests(requests)
		var workers sync.WaitGroup
		defer workers.Wait()
		for request := range channels {
			var dest struct {
				Host       string
				Port       uint32
				Origin     string
				OriginPort uint32
			}
			if request.ChannelType() != "direct-tcpip" || ssh.Unmarshal(request.ExtraData(), &dest) != nil {
				_ = request.Reject(ssh.UnknownChannelType, "unsupported")
				continue
			}
			targets <- net.JoinHostPort(dest.Host, fmtPort(dest.Port))
			channel, reqs, err := request.Accept()
			if err != nil {
				continue
			}
			go ssh.DiscardRequests(reqs)
			workers.Add(1)
			go func() { defer workers.Done(); defer channel.Close(); _ = serveDNSStream(channel, octet, 0) }()
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("SSH fixture did not stop")
		}
	})
	return client, targets, keyPath
}

func fmtPort(port uint32) string { return fmt.Sprint(port) }

func TestDNSUsesRealSelectedSSHPoolsWithoutBusinessSamples(t *testing.T) {
	clientA, targetA, keyPath := dnsSSHFixture(t, 10)
	clientB, targetB, _ := dnsSSHFixture(t, 20)
	traffic, _ := newTestTrafficStore(t, time.UTC)
	main := &Tunnel{profileID: "default", trafficStore: traffic, enableDNS: true}
	a := &Tunnel{profileID: "a", trafficStore: traffic}
	main.addSSHMember(clientB, "dns-test")
	a.addSSHMember(clientA, "dns-test")
	manager := NewProfileTunnelManager(nil)
	manager.tunnels["a"] = &profileTunnelEntry{tunnel: a}
	main.profileTunnelMgr = manager
	main.dnsPolicy = dnsTestPolicy(t, []cfg.EffectiveRoute{testRoute("a", "private.test", cfg.RouteTypeDomain, true, "a")})
	for _, tc := range []struct {
		host    string
		last    byte
		targets <-chan string
		address string
	}{{"private.test", 10, targetA, "10.0.0.53:53"}, {"public.test", 20, targetB, "1.1.1.1:53"}} {
		reply, err := main.queryDNS(context.Background(), dnsQuery(t, tc.host))
		if err != nil {
			t.Fatal(err)
		}
		var m dnsmessage.Message
		_ = m.Unpack(reply)
		if m.Answers[0].Body.(*dnsmessage.AResource).A[3] != tc.last {
			t.Fatal("wrong SSH outlet")
		}
		select {
		case target := <-tc.targets:
			if target != tc.address {
				t.Fatalf("dialed %s", target)
			}
		case <-time.After(time.Second):
			t.Fatal("SSH channel missing")
		}
	}
	counts, err := traffic.ProfileHealthSummaries(map[string]string{"a": "", "default": ""}, 2)
	if err != nil || counts["a"].SampleCount != 0 || counts["default"].SampleCount != 0 {
		t.Fatalf("DNS polluted business health: %v %v", counts, err)
	}
	// Only DNS enabled still loads authentication; refreshing identity retires
	// old default transports before any new policy can be published.
	config := &cfg.AppConfig{}
	config.ServerIp.Value, config.ServerSshPort.Value, config.LoginUser.Value, config.SshPrivateKeyPath.Value = "127.0.0.1", 22, "test", keyPath
	main.appConfig, main.sshRuntimeIdentity = config, "old-server"
	if err := main.RefreshRuntimeConfigFromAppConfig(); err != nil {
		t.Fatal(err)
	}
	if len(main.auth) == 0 || main.currentPoolSize() != 0 || main.dnsPolicy != nil {
		t.Fatal("DNS-only auth/old pool retirement failed")
	}
}

func TestDNSOnlyServiceLifecycleAndPolicyReload(t *testing.T) {
	// Profile persistence also mirrors to the home directory. Isolate it before
	// any production config read/write, so user profiles never become fixtures.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	client, targets, keyPath := dnsSSHFixture(t, 42)
	// Reserve a port valid for both transports before giving it to production Load.
	probe, err := dnsproxy.Listen("127.0.0.1:0", func(context.Context, []byte) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	address := probe.Addr().String()
	probe.Close()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.properties")
	if err := os.WriteFile(configPath, []byte("dns.enable=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	cfg.SetConfigInstance(v)
	config := cfg.NewAppConfig()
	config.EnableSocks5.Value, config.EnableHttp.Value, config.EnableHttpOverSSH.Value = false, false, false
	config.EnableDNS.Value, config.DNSLocalAddress.Value, config.DNSUpstreams.Value = true, address, "1.1.1.1:53"
	config.ServerIp.Value, config.ServerSshPort.Value, config.SshPrivateKeyPath.Value, config.LoginUser.Value = "127.0.0.1", 22, keyPath, "test"
	config.SSHPoolSize.Value = 1
	initial := cfg.SSHProfile{ServerIp: "127.0.0.1", ServerSshPort: 22, LoginUser: "test", SshPrivateKeyPath: keyPath}
	if _, err := cfg.UpsertProfile(cfg.DEFAULT_PROFILE_ID, initial, config); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.SwitchActiveProfile(cfg.DEFAULT_PROFILE_ID, config); err != nil {
		t.Fatal(err)
	}
	if err := cfg.EnsureAndApplyActiveProfile(config); err != nil {
		t.Fatal(err)
	}
	if config.EnableSocks5.GetValue() || config.EnableHttpOverSSH.GetValue() {
		t.Fatal("fixture must enable only DNS")
	}
	DefaultSshTunnel.sshDialFn = func() (*ssh.Client, error) { return client, nil }
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	if err := loadWithContext(ctx, config, &workers); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); workers.Wait() })
	deadline := time.Now().Add(2 * time.Second)
	for DefaultSshTunnel.currentPoolSize() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if DefaultSshTunnel.currentPoolSize() != 1 {
		t.Fatal("DNS-only startup did not maintain SSH pool")
	}
	udp, err := net.Dial("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_ = udp.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = udp.Write(dnsQuery(t, "default.test"))
	buf := make([]byte, 2048)
	n, err := udp.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var reply dnsmessage.Message
	if reply.Unpack(buf[:n]) != nil || reply.RCode != 0 || len(reply.Answers) != 1 {
		t.Fatal("DNS-only listener failed")
	}
	if target := <-targets; target != "1.1.1.1:53" {
		t.Fatalf("unexpected upstream: %s", target)
	}
	store, err := cfg.ListProfiles(config)
	if err != nil {
		t.Fatal(err)
	}
	profile := store.Profiles[store.ActiveProfileID]
	profile.DNSUpstreams = []string{"10.9.0.53:53"}
	if _, err := cfg.UpsertProfile(store.ActiveProfileID, profile, config); err != nil {
		t.Fatal(err)
	}
	if err := DefaultSshTunnel.ReloadProfileRouting(config); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultSshTunnel.queryDNS(ctx, dnsQuery(t, "default.test")); err != nil {
		t.Fatal(err)
	}
	if target := <-targets; target != "10.9.0.53:53" {
		t.Fatalf("override reload failed: %s", target)
	}
	// Editing startup settings does not replace the running global snapshot.
	config.DNSUpstreams.Value = "9.9.9.9:53"
	profile.DNSUpstreams = nil
	if _, err := cfg.UpsertProfile(store.ActiveProfileID, profile, config); err != nil {
		t.Fatal(err)
	}
	if err := DefaultSshTunnel.ReloadProfileRouting(config); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultSshTunnel.queryDNS(ctx, dnsQuery(t, "default.test")); err != nil {
		t.Fatal(err)
	}
	if target := <-targets; target != "1.1.1.1:53" {
		t.Fatalf("global settings applied without restart: %s", target)
	}
	// Switch the default to a different real SSH transport and resolver. The
	// same question must immediately use the new outlet, without old-pool reuse.
	nextClient, nextTargets, nextKey := dnsSSHFixture(t, 99)
	nextProfile := cfg.SSHProfile{ServerIp: "127.0.0.1", ServerSshPort: 22, LoginUser: "next", SshPrivateKeyPath: nextKey, DNSUpstreams: []string{"10.8.0.53:53"}}
	if _, err := cfg.UpsertProfile("next", nextProfile, config); err != nil {
		t.Fatal(err)
	}
	if _, err := cfg.SwitchActiveProfile("next", config); err != nil {
		t.Fatal(err)
	}
	DefaultSshTunnel.runtimeMu.Lock()
	DefaultSshTunnel.sshDialFn = func() (*ssh.Client, error) { return nextClient, nil }
	DefaultSshTunnel.runtimeMu.Unlock()
	if err := DefaultSshTunnel.RefreshRuntimeConfigFromAppConfig(); err != nil {
		t.Fatal(err)
	}
	if err := DefaultSshTunnel.ReloadProfileRouting(config); err != nil {
		t.Fatal(err)
	}
	DefaultSshTunnel.ReconnectSSHWithSource(ctx, "dns-switch-test")
	wire, err := DefaultSshTunnel.queryDNS(ctx, dnsQuery(t, "default.test"))
	if err != nil {
		t.Fatal(err)
	}
	if reply.Unpack(wire) != nil || reply.Answers[0].Body.(*dnsmessage.AResource).A[3] != 99 {
		t.Fatal("default switch reused old SSH outlet")
	}
	if target := <-nextTargets; target != "10.8.0.53:53" {
		t.Fatalf("default switch used wrong resolver: %s", target)
	}
	DefaultSshTunnel.Shutdown()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not drain")
	}
	again, err := dnsproxy.Listen(address, func(context.Context, []byte) ([]byte, error) { return nil, nil })
	if err != nil {
		t.Fatalf("listeners leaked: %v", err)
	}
	again.Close()
}
