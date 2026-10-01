package routing

import "testing"

func testRoute(id, pattern, routeType string, enabled bool, targets ...string) EffectiveRoute {
	if !enabled {
		pattern = ""
	}
	return EffectiveRoute{ID: id, GroupID: "group-test", GroupName: "Test", Pattern: pattern, Type: routeType, Strategy: RouteStrategyFixed, TargetProfileIDs: targets}
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
	matcher.LoadRoutes([]EffectiveRoute{
		testRoute("suffix", "google.com", RouteTypeDomain, true, "jp"),
		testRoute("wildcard", "*.internal.corp", RouteTypeDomain, true, "us"),
	})
	assertRouteMatch(t, matcher, "www.google.com", "suffix", true)
	assertRouteMatch(t, matcher, "google.com:443", "suffix", true)
	assertRouteMatch(t, matcher, "notgoogle.com", "", false)
	assertRouteMatch(t, matcher, "deep.service.internal.corp", "wildcard", true)
	assertRouteMatch(t, matcher, "internal.corp", "wildcard", true)
}

func TestRouteMatcher_IPWildcardAndCIDR(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]EffectiveRoute{
		testRoute("wildcard", "192.168.*.*", RouteTypeIP, true, "hk"),
		testRoute("exact", "10.0.1.5", RouteTypeIP, true, "hk"),
		testRoute("cidr", "172.16.0.0/12", RouteTypeCIDR, true, "sg"),
		testRoute("ipv6-cidr", "2001:db8::/32", RouteTypeCIDR, true, "sg"),
		testRoute("numeric-domain", "192.168.1.1", RouteTypeDomain, true, "bad"),
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
	matcher.LoadRoutes([]EffectiveRoute{
		testRoute("wildcard", "10.*.*.*", RouteTypeIP, true, "wildcard"),
		testRoute("cidr", "10.20.0.0/16", RouteTypeCIDR, true, "cidr"),
		testRoute("exact-cidr", "10.20.1.7/32", RouteTypeCIDR, true, "cidr"),
		testRoute("exact-ip", "10.20.1.7", RouteTypeIP, true, "exact"),
	})
	assertRouteMatch(t, matcher, "10.20.2.8", "cidr", true)
	assertRouteMatch(t, matcher, "10.20.1.7", "exact-ip", true)
}

func TestRouteMatcher_SpecificityStableOrderAndDisabled(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]EffectiveRoute{
		testRoute("general", "*.google.com", RouteTypeDomain, true, "general"),
		testRoute("specific-first", "maps.google.com", RouteTypeDomain, true, "specific"),
		testRoute("same-specificity-later", "maps.google.com", RouteTypeDomain, true, "later"),
		testRoute("disabled", "disabled.google.com", RouteTypeDomain, false, "disabled"),
	})
	assertRouteMatch(t, matcher, "maps.google.com", "specific-first", true)
	assertRouteMatch(t, matcher, "www.google.com", "general", true)
	assertRouteMatch(t, matcher, "disabled.google.com", "general", true)
}

func TestRouteMatcher_ExactSuffixOutranksEquivalentWildcard(t *testing.T) {
	matcher := NewRouteMatcher()
	matcher.LoadRoutes([]EffectiveRoute{
		testRoute("wildcard", "*.example.com", RouteTypeDomain, true, "wildcard"),
		testRoute("suffix", "example.com", RouteTypeDomain, true, "suffix"),
	})
	assertRouteMatch(t, matcher, "www.example.com", "suffix", true)
}

func TestRouteMatcher_ResolvedGroupsUseGlobalSpecificityAndGroupEnable(t *testing.T) {
	store := RouteStore{Groups: []RouteGroup{
		{ID: "general-group", Name: "General", Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"general"}, Rules: []RouteRule{{ID: "general", Pattern: "*.example.com", Type: RouteTypeDomain, Enabled: true}}},
		{ID: "specific-group", Name: "Specific", Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"specific"}, Rules: []RouteRule{{ID: "specific", Pattern: "api.example.com", Type: RouteTypeDomain, Enabled: true}}},
		{ID: "disabled-group", Name: "Disabled", Enabled: false, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"disabled"}, Rules: []RouteRule{{ID: "disabled", Pattern: "private.example.com", Type: RouteTypeDomain, Enabled: true}}},
	}}
	matcher := NewRouteMatcher()
	matcher.LoadRoutes(ResolveEffectiveRoutes(store))
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
	rule := testRoute("route-a", "example.jp", RouteTypeDomain, true, "jp")
	rule.Strategy = RouteStrategyRandom
	rule.TargetProfileIDs = []string{"jp", "us"}
	matcher.LoadRoutes([]EffectiveRoute{rule})
	first, firstOK := matcher.Match("test.example.jp")
	second, secondOK := matcher.Match("test.example.jp")
	if !firstOK || !secondOK || first.ID != second.ID || first.Strategy != RouteStrategyRandom {
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
	matcher.LoadRoutes([]EffectiveRoute{
		testRoute("fixed", "example.jp", RouteTypeDomain, true, "jp"),
		{ID: "random", GroupID: "group-test", GroupName: "Test", Pattern: "example.us", Type: RouteTypeDomain, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"us", "sg"}},
		testRoute("disabled", "off.test", RouteTypeDomain, false, "off"),
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
		if got := GuessRouteType(pattern); got != want {
			t.Fatalf("GuessRouteType(%q) = %q, want %q", pattern, got, want)
		}
	}
}
