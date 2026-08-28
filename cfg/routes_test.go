package cfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/spf13/viper"
)

func testProfiles(ids ...string) map[string]SSHProfile {
	profiles := make(map[string]SSHProfile, len(ids))
	for _, id := range ids {
		profiles[id] = SSHProfile{}
	}
	return profiles
}

func testGroup(id, name, strategy string, targets []string, rules ...RouteRule) RouteGroup {
	return RouteGroup{ID: id, Name: name, Enabled: true, Strategy: strategy, TargetProfileIDs: targets, Rules: rules}
}

func TestNormalizeAndValidateRouteInheritanceAndOverride(t *testing.T) {
	profiles := testProfiles("jp", "us")
	inherited, err := NormalizeAndValidateRoute(RouteRule{Pattern: " Example.COM. ", Type: "DOMAIN", Enabled: true}, profiles)
	if err != nil || inherited.Pattern != "example.com" || inherited.Type != RouteTypeDomain || inherited.Strategy != "" || inherited.TargetProfileIDs != nil {
		t.Fatalf("unexpected inherited route: %+v err=%v", inherited, err)
	}
	random, err := NormalizeAndValidateRoute(RouteRule{Pattern: "10.1.2.99/24", Type: RouteTypeCIDR, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "us"}}, profiles)
	if err != nil || random.Pattern != "10.1.2.0/24" {
		t.Fatalf("unexpected override route: %+v err=%v", random, err)
	}
	ipWildcard, err := NormalizeAndValidateRoute(RouteRule{Pattern: "192.168.*", Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}}, profiles)
	if err != nil || ipWildcard.Type != RouteTypeIP || ipWildcard.Pattern != "192.168.*.*" {
		t.Fatalf("unexpected IP wildcard normalization: %+v err=%v", ipWildcard, err)
	}
	invalid := []RouteRule{
		{Pattern: "x.test", Strategy: RouteStrategyFixed},
		{Pattern: "x.test", TargetProfileIDs: []string{"jp"}},
		{Pattern: "x.test", Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp", "us"}},
		{Pattern: "x.test", Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp"}},
		{Pattern: "x.test", Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "jp"}},
		{Pattern: "999.1.1.1", Type: RouteTypeIP},
		{Pattern: "x.test", Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"missing"}},
	}
	for _, rule := range invalid {
		if _, err := NormalizeAndValidateRoute(rule, profiles); err == nil {
			t.Fatalf("expected invalid route to fail: %+v", rule)
		}
	}
}

func TestValidateRouteStoreGroupsAndGlobalDuplicates(t *testing.T) {
	profiles := testProfiles("jp", "us")
	valid := RouteStore{Groups: []RouteGroup{
		testGroup("g1", "OpenAI", RouteStrategyFixed, []string{"jp"}, RouteRule{ID: "r1", Pattern: "openai.com", Type: RouteTypeDomain, Enabled: true}),
		testGroup("g2", "Google", RouteStrategyRandom, []string{"jp", "us"}, RouteRule{ID: "r2", Pattern: "google.com", Type: RouteTypeDomain, Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"us"}}),
	}}
	if _, err := validateRouteStore(valid, profiles); err != nil {
		t.Fatal(err)
	}
	duplicateName := valid
	duplicateName.Groups = append([]RouteGroup(nil), valid.Groups...)
	duplicateName.Groups[1].Name = " openai "
	if _, err := validateRouteStore(duplicateName, profiles); err == nil {
		t.Fatal("expected case-insensitive duplicate group name error")
	}
	duplicatePattern := valid
	duplicatePattern.Groups = append([]RouteGroup(nil), valid.Groups...)
	duplicatePattern.Groups[1].Rules = []RouteRule{{ID: "r2", Pattern: "OPENAI.COM.", Type: RouteTypeDomain, Enabled: false}}
	if _, err := validateRouteStore(duplicatePattern, profiles); err == nil {
		t.Fatal("expected duplicate pattern across groups")
	}
	invalidPolicy := valid
	invalidPolicy.Groups = append([]RouteGroup(nil), valid.Groups...)
	invalidPolicy.Groups[0].TargetProfileIDs = []string{"jp", "us"}
	if _, err := validateRouteStore(invalidPolicy, profiles); err == nil {
		t.Fatal("expected invalid fixed group policy")
	}
}

func TestResolveEffectiveRoutesInheritanceOverrideAndDoubleEnable(t *testing.T) {
	store := RouteStore{Groups: []RouteGroup{
		{ID: "g1", Name: "OpenAI", Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}, Rules: []RouteRule{
			{ID: "inherit", Pattern: "openai.com", Type: RouteTypeDomain, Enabled: true},
			{ID: "override", Pattern: "api.openai.com", Type: RouteTypeDomain, Enabled: true, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "us"}},
			{ID: "off-rule", Pattern: "off.openai.com", Type: RouteTypeDomain, Enabled: false},
		}},
		{ID: "g2", Name: "Disabled", Enabled: false, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"us"}, Rules: []RouteRule{{ID: "hidden", Pattern: "hidden.test", Type: RouteTypeDomain, Enabled: true}}},
	}}
	routes := ResolveEffectiveRoutes(store)
	if len(routes) != 2 || routes[0].ID != "inherit" || routes[0].GroupID != "g1" || routes[0].GroupName != "OpenAI" || routes[0].Strategy != RouteStrategyFixed || routes[0].TargetProfileIDs[0] != "jp" {
		t.Fatalf("unexpected inherited effective routes: %+v", routes)
	}
	if routes[1].ID != "override" || routes[1].Strategy != RouteStrategyRandom || len(routes[1].TargetProfileIDs) != 2 {
		t.Fatalf("unexpected override: %+v", routes[1])
	}
}

func TestVersion1AndProfileRouteMigrationIsDeterministicAndIdempotent(t *testing.T) {
	profiles := ProfileStore{Profiles: map[string]SSHProfile{
		"jp": {DomainRoutes: []DomainRoute{{Pattern: "Google.COM", Type: "domain"}}},
		"us": {},
	}}
	oldRule := RouteRule{ID: "route_old", Pattern: "openai.com", Type: RouteTypeDomain, Enabled: true, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "us"}}
	store, migratedProfiles, routesChanged, profilesChanged, err := migrateRouteData(routeStoreFile{Version: 1, Routes: []RouteRule{oldRule}}, profiles)
	if err != nil || !routesChanged || !profilesChanged || len(store.Groups) != 1 || len(store.Groups[0].Rules) != 2 {
		t.Fatalf("migration failed: %+v routeChanged=%v profileChanged=%v err=%v", store, routesChanged, profilesChanged, err)
	}
	group := store.Groups[0]
	if group.ID != migratedRouteGroupID() || group.Name != migratedRouteGroupName || group.Strategy != RouteStrategyRandom || len(group.TargetProfileIDs) != 2 {
		t.Fatalf("unexpected migrated group: %+v", group)
	}
	if group.Rules[0].Strategy != oldRule.Strategy || len(group.Rules[0].TargetProfileIDs) != 2 {
		t.Fatalf("old flat rule did not retain explicit override: %+v", group.Rules[0])
	}
	legacy := group.Rules[1]
	if legacy.ID != legacyRouteID("jp", RouteTypeDomain, "google.com") || legacy.Strategy != RouteStrategyFixed || legacy.TargetProfileIDs[0] != "jp" {
		t.Fatalf("unexpected profile migration: %+v", legacy)
	}
	if len(migratedProfiles.Profiles["jp"].DomainRoutes) != 0 {
		t.Fatal("legacy profile routes were not cleared")
	}
	again, _, changedAgain, profilesAgain, err := migrateRouteData(routeStoreFile{Version: 2, Groups: store.Groups}, migratedProfiles)
	if err != nil || changedAgain || profilesAgain || len(again.Groups) != 1 || len(again.Groups[0].Rules) != 2 {
		t.Fatalf("migration is not idempotent: %+v changed=%v/%v err=%v", again, changedAgain, profilesAgain, err)
	}
}

func TestLegacyProfileMigrationRejectsDuplicatePattern(t *testing.T) {
	profiles := ProfileStore{Profiles: map[string]SSHProfile{
		"jp": {DomainRoutes: []DomainRoute{{Pattern: "same.test", Type: "domain"}}},
		"us": {DomainRoutes: []DomainRoute{{Pattern: "same.test", Type: "domain"}}},
	}}
	if _, _, _, _, err := migrateRouteData(routeStoreFile{Version: 2}, profiles); err == nil {
		t.Fatal("expected conflicting legacy patterns to fail")
	}
}

func TestRouteIDsAndAtomicVersion2Save(t *testing.T) {
	groupID, err := newRouteGroupID()
	if err != nil || !regexp.MustCompile(`^group_[0-9a-f]{32}$`).MatchString(groupID) {
		t.Fatalf("invalid group id %q: %v", groupID, err)
	}
	routeID, err := newRouteID()
	if err != nil || !regexp.MustCompile(`^route_[0-9a-f]{32}$`).MatchString(routeID) {
		t.Fatalf("invalid route id %q: %v", routeID, err)
	}
	path := filepath.Join(t.TempDir(), "routes.json")
	first := RouteStore{Groups: []RouteGroup{testGroup("g1", "One", RouteStrategyFixed, []string{"jp"})}}
	second := RouteStore{Groups: []RouteGroup{testGroup("g2", "Two", RouteStrategyFixed, []string{"us"})}}
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
	if err := json.Unmarshal(data, &got); err != nil || got.Version != 2 || len(got.Groups) != 1 || got.Groups[0].ID != "g2" {
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

func TestRouteGroupAndRuleCRUDMoveToggleCascadeAndReferences(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.properties")
	if err := os.WriteFile(configPath, []byte("active.profile.id=jp\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	previousConfig := configInstance
	configInstance = v
	t.Cleanup(func() { configInstance = previousConfig })
	profileData, _ := json.Marshal(ProfileStore{ActiveProfileID: "jp", Profiles: testProfiles("jp", "us")})
	if err := os.WriteFile(filepath.Join(dir, "profiles.json"), profileData, 0600); err != nil {
		t.Fatal(err)
	}

	store, err := UpsertRouteGroup(RouteGroup{Name: "OpenAI", Description: "AI service", Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}}, nil)
	if err != nil || len(store.Groups) != 1 {
		t.Fatalf("create group failed: %+v err=%v", store, err)
	}
	firstGroupID := store.Groups[0].ID
	store, err = UpsertRouteGroup(RouteGroup{Name: "Backup", Enabled: true, Strategy: RouteStrategyRandom, TargetProfileIDs: []string{"jp", "us"}}, nil)
	if err != nil || len(store.Groups) != 2 {
		t.Fatalf("create second group failed: %+v err=%v", store, err)
	}
	secondGroupID := store.Groups[1].ID
	if _, err := UpsertRouteGroup(RouteGroup{Name: " openai ", Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{"jp"}}, nil); err == nil {
		t.Fatal("expected duplicate group name rejection")
	}

	store, err = UpsertRoute(firstGroupID, RouteRule{Pattern: "api.openai.com", Type: RouteTypeDomain, Enabled: true}, nil)
	if err != nil || len(store.Groups[0].Rules) != 1 || store.Groups[0].Rules[0].Strategy != "" {
		t.Fatalf("create inherited rule failed: %+v err=%v", store, err)
	}
	route := store.Groups[0].Rules[0]
	route.Strategy = RouteStrategyFixed
	route.TargetProfileIDs = []string{"us"}
	store, err = UpsertRoute(secondGroupID, route, nil)
	if err != nil || len(store.Groups[0].Rules) != 0 || len(store.Groups[1].Rules) != 1 || store.Groups[1].Rules[0].ID != route.ID {
		t.Fatalf("move rule failed: %+v err=%v", store, err)
	}
	store, err = ToggleRouteGroup(secondGroupID, false, nil)
	if err != nil || store.Groups[1].Enabled || len(ResolveEffectiveRoutes(store)) != 0 {
		t.Fatalf("group toggle did not disable effective rule: %+v err=%v", store, err)
	}
	refs, err := FindRouteReferences("us", nil)
	if err != nil || len(refs.GroupIDs) != 1 || refs.GroupIDs[0] != secondGroupID || len(refs.RouteIDs) != 1 || refs.RouteIDs[0] != route.ID {
		t.Fatalf("disabled references were not protected: %+v err=%v", refs, err)
	}
	if _, err := DeleteRouteGroup(secondGroupID, false, nil); err == nil {
		t.Fatal("expected non-empty group delete conflict")
	} else if conflict, ok := err.(*RouteGroupNotEmptyError); !ok || conflict.RuleCount != 1 {
		t.Fatalf("unexpected group delete error: %T %v", err, err)
	}
	store, err = DeleteRouteGroup(secondGroupID, true, nil)
	if err != nil || len(store.Groups) != 1 {
		t.Fatalf("cascade delete failed: %+v err=%v", store, err)
	}
	if _, err := DeleteRouteGroup(firstGroupID, false, nil); err != nil {
		t.Fatalf("empty group delete failed: %v", err)
	}
}
