package cfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func testProfiles(ids ...string) map[string]SSHProfile {
	profiles := make(map[string]SSHProfile, len(ids))
	for _, id := range ids {
		profiles[id] = SSHProfile{}
	}
	return profiles
}

func TestNormalizeAndValidateRoute(t *testing.T) {
	profiles := testProfiles("jp", "us")
	fixed, err := NormalizeAndValidateRoute(RouteRule{Pattern: " Example.COM. ", Type: "DOMAIN", Strategy: "fixed", TargetProfileIDs: []string{"jp"}}, profiles)
	if err != nil || fixed.Pattern != "example.com" || fixed.Type != RouteTypeDomain {
		t.Fatalf("unexpected normalized route: %+v err=%v", fixed, err)
	}
	random, err := NormalizeAndValidateRoute(RouteRule{Pattern: "10.1.2.99/24", Type: RouteTypeCIDR, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "us"}}, profiles)
	if err != nil || random.Pattern != "10.1.2.0/24" {
		t.Fatalf("unexpected CIDR normalization: %+v err=%v", random, err)
	}
	ipWildcard, err := NormalizeAndValidateRoute(RouteRule{Pattern: "192.168.*", Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}}, profiles)
	if err != nil || ipWildcard.Type != RouteTypeIP || ipWildcard.Pattern != "192.168.*.*" {
		t.Fatalf("unexpected IP wildcard normalization: %+v err=%v", ipWildcard, err)
	}

	invalid := []RouteRule{
		{Pattern: "x.test", Type: RouteTypeDomain, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp", "us"}},
		{Pattern: "x.test", Type: RouteTypeDomain, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp"}},
		{Pattern: "x.test", Type: RouteTypeDomain, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "jp"}},
		{Pattern: "999.1.1.1", Type: RouteTypeIP, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}},
		{Pattern: "x.test", Type: RouteTypeDomain, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"missing"}},
	}
	for _, rule := range invalid {
		if _, err := NormalizeAndValidateRoute(rule, profiles); err == nil {
			t.Fatalf("expected invalid route to fail: %+v", rule)
		}
	}
}

func TestValidateRouteStoreRejectsNormalizedDuplicate(t *testing.T) {
	store := RouteStore{Routes: []RouteRule{
		{ID: "a", Pattern: "EXAMPLE.COM", Type: RouteTypeDomain, Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}},
		{ID: "b", Pattern: "example.com.", Type: RouteTypeDomain, Enabled: false, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}},
	}}
	if _, err := validateRouteStore(store, testProfiles("jp")); err == nil {
		t.Fatal("expected duplicate normalized pattern error")
	}
}

func TestValidateRouteStoreRejectsEquivalentIPWildcard(t *testing.T) {
	store := RouteStore{Routes: []RouteRule{
		{ID: "a", Pattern: "192.168.*", Type: RouteTypeIP, Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}},
		{ID: "b", Pattern: "192.168.*.*", Type: RouteTypeIP, Enabled: false, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}},
	}}
	if _, err := validateRouteStore(store, testProfiles("jp")); err == nil {
		t.Fatal("expected equivalent IP wildcard duplicate error")
	}
}

func TestLegacyRouteMigrationIsDeterministicAndIdempotent(t *testing.T) {
	profiles := ProfileStore{Profiles: map[string]SSHProfile{
		"jp": {DomainRoutes: []DomainRoute{{Pattern: "Google.COM", Type: "domain"}}},
		"us": {DomainRoutes: []DomainRoute{{Pattern: "example.com.", Type: "domain"}}},
	}}
	store, migratedProfiles, migrated, err := migrateLegacyDomainRoutes(RouteStore{Version: RouteStoreVersion}, profiles)
	if err != nil || !migrated || len(store.Routes) != 2 {
		t.Fatalf("migration failed: %+v migrated=%v err=%v", store, migrated, err)
	}
	rule := store.Routes[0]
	if rule.ID != legacyRouteID("jp", RouteTypeDomain, "google.com") || rule.Strategy != RouteStrategyFixed || len(rule.TargetProfileIDs) != 1 || rule.TargetProfileIDs[0] != "jp" {
		t.Fatalf("unexpected migrated rule: %+v", rule)
	}
	for id, profile := range migratedProfiles.Profiles {
		if len(profile.DomainRoutes) != 0 {
			t.Fatalf("legacy routes not cleared for %s", id)
		}
	}
	again, _, migratedAgain, err := migrateLegacyDomainRoutes(store, migratedProfiles)
	if err != nil || migratedAgain || len(again.Routes) != 2 || again.Routes[0].ID != rule.ID {
		t.Fatalf("migration is not idempotent: %+v migrated=%v err=%v", again, migratedAgain, err)
	}
}

func TestLegacyRouteMigrationRejectsDuplicatePatternAcrossProfiles(t *testing.T) {
	profiles := ProfileStore{Profiles: map[string]SSHProfile{
		"jp": {DomainRoutes: []DomainRoute{{Pattern: "same.test", Type: "domain"}}},
		"us": {DomainRoutes: []DomainRoute{{Pattern: "same.test", Type: "domain"}}},
	}}
	if _, _, _, err := migrateLegacyDomainRoutes(RouteStore{Version: RouteStoreVersion}, profiles); err == nil {
		t.Fatal("expected conflicting legacy patterns to fail instead of changing routing semantics")
	}
}

func TestRouteIDAndAtomicSave(t *testing.T) {
	id, err := newRouteID()
	if err != nil || !regexp.MustCompile(`^route_[0-9a-f]{32}$`).MatchString(id) {
		t.Fatalf("invalid route id %q: %v", id, err)
	}
	path := filepath.Join(t.TempDir(), "routes.json")
	first := RouteStore{Routes: []RouteRule{{ID: "a", Pattern: "a.test", Type: RouteTypeDomain, Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}}}}
	second := RouteStore{Routes: []RouteRule{{ID: "b", Pattern: "b.test", Type: RouteTypeDomain, Enabled: false, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"us"}}}}
	if err := saveRouteStoreAt(path, first); err != nil {
		t.Fatal(err)
	}
	if err := saveRouteStoreAt(path, second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got RouteStore
	if err := json.Unmarshal(data, &got); err != nil || len(got.Routes) != 1 || got.Routes[0].ID != "b" {
		t.Fatalf("atomic replacement produced invalid content: %+v err=%v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("routes file permissions = %v", info.Mode().Perm())
	}
}
