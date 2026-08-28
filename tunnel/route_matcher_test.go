package tunnel

import (
	"context"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"ssh-tunnel/cfg"
	"testing"
	"time"
)

func testRoute(id, pattern, routeType string, enabled bool, targets ...string) cfg.EffectiveRoute {
	if !enabled {
		pattern = ""
	}
	return cfg.EffectiveRoute{ID: id, GroupID: "group-test", GroupName: "Test", Pattern: pattern, Type: routeType, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: targets}
}

func assertRouteMatch(t *testing.T, matcher *RouteMatcher, host, wantID string, wantMatch bool) {
	t.Helper()
	rule, ok := matcher.Match(host)
	if ok != wantMatch || rule.ID != wantID {
		t.Fatalf("Match(%q) = (%q, %v), want (%q, %v)", host, rule.ID, ok, wantID, wantMatch)
	}
}

func TestRouteMatcher_DomainSuffixAndWildcard(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{
		testRoute("suffix", "google.com", cfg.RouteTypeDomain, true, "jp"),
		testRoute("wildcard", "*.internal.corp", cfg.RouteTypeDomain, true, "us"),
	})
	assertRouteMatch(t, matcher, "www.google.com", "suffix", true)
	assertRouteMatch(t, matcher, "google.com:443", "suffix", true)
	assertRouteMatch(t, matcher, "notgoogle.com", "", false)
	assertRouteMatch(t, matcher, "deep.service.internal.corp", "wildcard", true)
	assertRouteMatch(t, matcher, "internal.corp", "wildcard", true)
}

func TestRouteMatcher_IPWildcardAndCIDR(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{
		testRoute("wildcard", "192.168.*.*", cfg.RouteTypeIP, true, "hk"),
		testRoute("exact", "10.0.1.5", cfg.RouteTypeIP, true, "hk"),
		testRoute("cidr", "172.16.0.0/12", cfg.RouteTypeCIDR, true, "sg"),
		testRoute("ipv6-cidr", "2001:db8::/32", cfg.RouteTypeCIDR, true, "sg"),
		testRoute("numeric-domain", "192.168.1.1", cfg.RouteTypeDomain, true, "bad"),
	})
	assertRouteMatch(t, matcher, "192.168.1.1:8080", "wildcard", true)
	assertRouteMatch(t, matcher, "10.0.1.5", "exact", true)
	assertRouteMatch(t, matcher, "10.0.1.6", "", false)
	assertRouteMatch(t, matcher, "172.31.255.255", "cidr", true)
	assertRouteMatch(t, matcher, "172.32.0.1", "", false)
	assertRouteMatch(t, matcher, "[2001:db8::7]:443", "ipv6-cidr", true)
	assertRouteMatch(t, matcher, "2001:db8::8", "ipv6-cidr", true)
}

func TestRouteMatcher_IPAndCIDRSpecificity(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{
		testRoute("wildcard", "10.*.*.*", cfg.RouteTypeIP, true, "wildcard"),
		testRoute("cidr", "10.20.0.0/16", cfg.RouteTypeCIDR, true, "cidr"),
		testRoute("exact-cidr", "10.20.1.7/32", cfg.RouteTypeCIDR, true, "cidr"),
		testRoute("exact-ip", "10.20.1.7", cfg.RouteTypeIP, true, "exact"),
	})
	assertRouteMatch(t, matcher, "10.20.2.8", "cidr", true)
	assertRouteMatch(t, matcher, "10.20.1.7", "exact-ip", true)
}

func TestRouteMatcher_SpecificityStableOrderAndDisabled(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{
		testRoute("general", "*.google.com", cfg.RouteTypeDomain, true, "general"),
		testRoute("specific-first", "maps.google.com", cfg.RouteTypeDomain, true, "specific"),
		testRoute("same-specificity-later", "maps.google.com", cfg.RouteTypeDomain, true, "later"),
		testRoute("disabled", "disabled.google.com", cfg.RouteTypeDomain, false, "disabled"),
	})
	assertRouteMatch(t, matcher, "maps.google.com", "specific-first", true)
	assertRouteMatch(t, matcher, "www.google.com", "general", true)
	assertRouteMatch(t, matcher, "disabled.google.com", "general", true)
}

func TestRouteMatcher_ExactSuffixOutranksEquivalentWildcard(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{
		testRoute("wildcard", "*.example.com", cfg.RouteTypeDomain, true, "wildcard"),
		testRoute("suffix", "example.com", cfg.RouteTypeDomain, true, "suffix"),
	})
	assertRouteMatch(t, matcher, "www.example.com", "suffix", true)
}

func TestRouteMatcher_ResolvedGroupsUseGlobalSpecificityAndGroupEnable(t *testing.T) {
	store := cfg.RouteStore{Groups: []cfg.RouteGroup{
		{ID: "general-group", Name: "General", Enabled: true, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"general"}, Rules: []cfg.RouteRule{{ID: "general", Pattern: "*.example.com", Type: cfg.RouteTypeDomain, Enabled: true}}},
		{ID: "specific-group", Name: "Specific", Enabled: true, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"specific"}, Rules: []cfg.RouteRule{{ID: "specific", Pattern: "api.example.com", Type: cfg.RouteTypeDomain, Enabled: true}}},
		{ID: "disabled-group", Name: "Disabled", Enabled: false, Strategy: cfg.RouteStrategyFixed, TargetProfileIDs: []string{"disabled"}, Rules: []cfg.RouteRule{{ID: "disabled", Pattern: "private.example.com", Type: cfg.RouteTypeDomain, Enabled: true}}},
	}}
	matcher := NewRouteMatcher()
	matcher.LoadRoutes(cfg.ResolveEffectiveRoutes(store))
	route, matched := matcher.Match("api.example.com")
	if !matched || route.ID != "specific" || route.GroupID != "specific-group" || route.TargetProfileIDs[0] != "specific" {
		t.Fatalf("specific cross-group route mismatch: %+v matched=%v", route, matched)
	}
	route, matched = matcher.Match("private.example.com")
	if !matched || route.ID != "general" {
		t.Fatalf("disabled group should be excluded, got %+v matched=%v", route, matched)
	}
}

func TestRouteMatcher_CacheStoresRuleIDAndReloadClears(t *testing.T) {
	matcher := NewRouteMatcher()
	rule := testRoute("route-a", "example.jp", cfg.RouteTypeDomain, true, "jp")
	rule.Strategy = cfg.RouteStrategyRandom
	rule.TargetProfileIDs = []string{"jp", "us"}
	matcher.LoadRoutes([]cfg.EffectiveRoute{rule})
	first, firstOK := matcher.Match("test.example.jp")
	second, secondOK := matcher.Match("test.example.jp")
	if !firstOK || !secondOK || first.ID != second.ID || first.Strategy != cfg.RouteStrategyRandom {
		t.Fatalf("cache mismatch: first=%+v second=%+v", first, second)
	}
	first.TargetProfileIDs[0] = "mutated"
	third, _ := matcher.Match("test.example.jp")
	if third.TargetProfileIDs[0] != "jp" {
		t.Fatal("returned rule must be a clone")
	}
	matcher.LoadRoutes(nil)
	assertRouteMatch(t, matcher, "test.example.jp", "", false)
}

func TestRouteMatcher_ProfilesWithRoutes(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{
		testRoute("fixed", "example.jp", cfg.RouteTypeDomain, true, "jp"),
		{ID: "random", GroupID: "group-test", GroupName: "Test", Pattern: "example.us", Type: cfg.RouteTypeDomain, Strategy: cfg.RouteStrategyRandom, TargetProfileIDs: []string{"us", "sg"}},
		testRoute("disabled", "off.test", cfg.RouteTypeDomain, false, "off"),
	})
	profiles := matcher.ProfilesWithRoutes()
	for _, profileID := range []string{"jp", "us", "sg"} {
		if !profiles[profileID] {
			t.Fatalf("expected %s in route targets", profileID)
		}
	}
	if profiles["off"] {
		t.Fatal("disabled rule target must not be prewarmed")
	}
}

func TestRouteMatcher_GuessType(t *testing.T) {
	for pattern, want := range map[string]string{
		"google.com": "domain", "*.example.com": "domain", "192.168.*": "ip", "192.168.1.*": "ip", "10.0.0.0/8": "cidr", "172.16.0.1": "ip",
	} {
		if got := guessRouteType(pattern); got != want {
			t.Fatalf("guessRouteType(%q) = %q, want %q", pattern, got, want)
		}
	}
}

func TestTryRouteMatch_FixedAndActiveProfile(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{testRoute("fixed", "active.test", cfg.RouteTypeDomain, true, "active")})
	tunnel := &Tunnel{routeMatcher: matcher, profileTunnelMgr: NewProfileTunnelManager(matcher), profileID: "active"}
	var attempted []string
	tunnel.routeDialer = func(profileID, host string, _ *requestRetryState) (destinationConn, error) {
		attempted = append(attempted, profileID)
		client, server := net.Pipe()
		_ = server.Close()
		return destinationConn{conn: client, profileID: profileID, viaSSH: true}, nil
	}
	dest, matched, err := tunnel.tryRouteMatch("active.test:443", tunnel.newRequestRetryState())
	if err != nil || !matched || dest.profileID != "active" || dest.routeID != "fixed" || dest.routeGroupID != "group-test" || dest.routeGroupName != "Test" {
		t.Fatalf("unexpected fixed result: dest=%+v matched=%v err=%v", dest, matched, err)
	}
	_ = dest.conn.Close()
	if len(attempted) != 1 || attempted[0] != "active" {
		t.Fatalf("active profile was not dialed exactly once: %v", attempted)
	}
}

func TestTryRouteMatch_RandomFailoverAndNoDefaultFallback(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{{ID: "random", GroupID: "group-random", GroupName: "Random", Pattern: "random.test", Type: cfg.RouteTypeDomain, Strategy: cfg.RouteStrategyRandom, TargetProfileIDs: []string{"a", "b", "c"}}})
	tunnel := &Tunnel{routeMatcher: matcher, profileTunnelMgr: NewProfileTunnelManager(matcher), profileID: "default"}
	var attempted []string
	tunnel.routeDialer = func(profileID, host string, _ *requestRetryState) (destinationConn, error) {
		attempted = append(attempted, profileID)
		if len(attempted) < 3 {
			return destinationConn{profileID: profileID, viaSSH: true}, errors.New("dial failed")
		}
		client, server := net.Pipe()
		_ = server.Close()
		return destinationConn{conn: client, profileID: profileID, viaSSH: true}, nil
	}
	dest, matched, err := tunnel.tryRouteMatch("random.test", tunnel.newRequestRetryState())
	if err != nil || !matched || len(attempted) != 3 || len(dest.attemptedProfileIDs) != 3 {
		t.Fatalf("random failover result: attempted=%v dest=%+v matched=%v err=%v", attempted, dest, matched, err)
	}
	_ = dest.conn.Close()

	attempted = nil
	tunnel.routeDialer = func(profileID, host string, _ *requestRetryState) (destinationConn, error) {
		attempted = append(attempted, profileID)
		return destinationConn{profileID: profileID, viaSSH: true}, errors.New("unavailable")
	}
	dest, matched, err = tunnel.tryRouteMatch("random.test", tunnel.newRequestRetryState())
	if !matched || err == nil || len(attempted) != 3 || dest.profileID == "default" {
		t.Fatalf("all failures must fail closed: attempted=%v dest=%+v matched=%v err=%v", attempted, dest, matched, err)
	}
}

func TestTryRouteMatchRecordsEachProfileCandidateExactlyOnce(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{{ID: "health-random", GroupID: "group-health", Pattern: "health.test", Type: cfg.RouteTypeDomain, Strategy: cfg.RouteStrategyRandom, TargetProfileIDs: []string{"a", "b", "c"}}})
	store, _ := newTestTrafficStore(t, time.UTC)
	manager := NewProfileTunnelManager(matcher, store)
	manager.identities = map[string]string{"a": "id-a", "b": "id-b", "c": "id-c", "default": "id-default"}
	tunnel := &Tunnel{routeMatcher: matcher, profileTunnelMgr: manager, profileID: "default", profileIdentity: "id-default", trafficStore: store}
	var attempted []string
	tunnel.routeDialer = func(profileID, host string, _ *requestRetryState) (destinationConn, error) {
		attempted = append(attempted, profileID)
		if len(attempted) < 3 {
			return destinationConn{profileID: profileID, viaSSH: true}, errors.New("dial failed")
		}
		client, server := net.Pipe()
		_ = server.Close()
		return destinationConn{conn: client, profileID: profileID, viaSSH: true}, nil
	}
	dest, matched, err := tunnel.tryRouteMatch("health.test:443", tunnel.newRequestRetryState())
	if err != nil || !matched || len(attempted) != 3 {
		t.Fatalf("unexpected failover: attempted=%v matched=%v err=%v", attempted, matched, err)
	}
	_ = dest.conn.Close()

	summaries, err := store.ProfileHealthSummaries(manager.identities, 2)
	if err != nil {
		t.Fatal(err)
	}
	for index, profileID := range attempted {
		summary := summaries[profileID]
		if summary.SampleCount != 1 {
			t.Fatalf("profile %s recorded %d attempts, want exactly one", profileID, summary.SampleCount)
		}
		if index < 2 && (summary.FailureCount != 1 || summary.Status != ProfileHealthStatusDegraded) {
			t.Fatalf("failed candidate %s has unexpected health: %+v", profileID, summary)
		}
		if index == 2 && (summary.SuccessCount != 1 || summary.Status != ProfileHealthStatusReachable) {
			t.Fatalf("successful candidate %s has unexpected health: %+v", profileID, summary)
		}
	}
	if summaries["default"].SampleCount != 0 {
		t.Fatalf("default/direct profile was incorrectly counted: %+v", summaries["default"])
	}
}

func TestTryRouteMatch_RandomSelectionRunsPerConnection(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]cfg.EffectiveRoute{{ID: "random", GroupID: "group-random", GroupName: "Random", Pattern: "cached.test", Type: cfg.RouteTypeDomain, Strategy: cfg.RouteStrategyRandom, TargetProfileIDs: []string{"a", "b"}}})
	tunnel := &Tunnel{routeMatcher: matcher, profileTunnelMgr: NewProfileTunnelManager(matcher)}
	dials := 0
	tunnel.routeDialer = func(profileID, host string, _ *requestRetryState) (destinationConn, error) {
		dials++
		client, server := net.Pipe()
		_ = server.Close()
		return destinationConn{conn: client, profileID: profileID, viaSSH: true}, nil
	}
	for i := 0; i < 2; i++ {
		dest, matched, err := tunnel.tryRouteMatch("cached.test", tunnel.newRequestRetryState())
		if err != nil || !matched {
			t.Fatalf("connection %d failed: %v", i, err)
		}
		_ = dest.conn.Close()
	}
	if dials != 2 {
		t.Fatalf("route cache must not cache selected profile: got %d dials", dials)
	}
}

func TestSameProfileConnectionDetectsPoolAndSSHParameterChanges(t *testing.T) {
	base := cfg.SSHProfile{ServerIp: "127.0.0.1", ServerSshPort: 22, LoginUser: "test", SshPrivateKeyPath: "/key", RetryIntervalSec: 3, SSHPoolSize: 2}
	if !sameProfileConnection(base, base) {
		t.Fatal("identical profiles should reuse the background tunnel")
	}
	changes := []func(*cfg.SSHProfile){
		func(profile *cfg.SSHProfile) { profile.ServerIp = "127.0.0.2" },
		func(profile *cfg.SSHProfile) { profile.ServerSshPort = 2222 },
		func(profile *cfg.SSHProfile) { profile.LoginUser = "other" },
		func(profile *cfg.SSHProfile) { profile.SshPrivateKeyPath = "/other-key" },
		func(profile *cfg.SSHProfile) { profile.RetryIntervalSec = 4 },
		func(profile *cfg.SSHProfile) { profile.SSHPoolSize = 3 },
	}
	for index, change := range changes {
		modified := base
		change(&modified)
		if sameProfileConnection(base, modified) {
			t.Fatalf("connection change %d was not detected", index)
		}
	}
}

func TestProfileTunnelManagerHotAddsRebuildsAndStopsReferencedPools(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	profiles := map[string]cfg.SSHProfile{
		"active": {ServerIp: "127.0.0.1", ServerSshPort: 1, LoginUser: "test", SshPrivateKeyPath: keyPath, SSHPoolSize: 1},
		"backup": {ServerIp: "127.0.0.1", ServerSshPort: 1, LoginUser: "test", SshPrivateKeyPath: keyPath, SSHPoolSize: 1},
		"unused": {ServerIp: "127.0.0.1", ServerSshPort: 1, LoginUser: "test", SshPrivateKeyPath: keyPath, SSHPoolSize: 1},
	}
	matcher := NewRouteMatcher()
	manager := NewProfileTunnelManager(matcher)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer manager.StopAll()
	rules := []cfg.EffectiveRoute{
		testRoute("backup-rule", "backup.test", cfg.RouteTypeDomain, true, "backup"),
		testRoute("active-rule", "active.test", cfg.RouteTypeDomain, true, "active"),
	}
	manager.ReloadProfiles(ctx, profiles, "active", rules)
	ids := manager.ActiveProfileIDs()
	if len(ids) != 1 || ids[0] != "backup" {
		t.Fatalf("only referenced non-active profile should be prewarmed: %v", ids)
	}
	first := manager.GetTunnel("backup")
	changedProfiles := make(map[string]cfg.SSHProfile, len(profiles))
	for id, profile := range profiles {
		changedProfiles[id] = profile
	}
	changed := changedProfiles["backup"]
	changed.ServerSshPort = 2
	changedProfiles["backup"] = changed
	manager.ReloadProfiles(ctx, changedProfiles, "active", rules)
	if rebuilt := manager.GetTunnel("backup"); rebuilt == nil || rebuilt == first {
		t.Fatal("changed SSH parameters should rebuild the background tunnel")
	}
	manager.ReloadProfiles(ctx, changedProfiles, "active", nil)
	if ids := manager.ActiveProfileIDs(); len(ids) != 0 {
		t.Fatalf("unreferenced background tunnels should stop immediately: %v", ids)
	}
}
