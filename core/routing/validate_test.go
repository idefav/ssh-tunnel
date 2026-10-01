package routing

import (
	"reflect"
	"testing"
)

func TestValidationDoesNotMutateCallerOnFailure(t *testing.T) {
	store := RouteStore{Version: 2, Groups: []RouteGroup{{ID: "g", Name: "G", Strategy: "fixed", TargetProfileIDs: []string{"a"}, Rules: []RouteRule{
		{ID: "one", Type: "domain", Pattern: "EXAMPLE.COM."},
		{ID: "two", Type: "domain", Pattern: "bad domain"},
	}}}}
	if _, err := ValidateStore(store, map[string]struct{}{"a": {}}); err == nil {
		t.Fatal("expected rejection")
	}
	if store.Groups[0].Rules[0].Pattern != "EXAMPLE.COM." {
		t.Fatal("failed validation mutated existing config")
	}
}

func TestPolicyInheritanceMustBeComplete(t *testing.T) {
	profiles := map[string]struct{}{"a": {}, "b": {}}
	for _, rule := range []RouteRule{{Pattern: "example.com", Strategy: "fixed"}, {Pattern: "example.com", TargetProfileIDs: []string{"a"}}} {
		if _, err := NormalizeRule(rule, profiles); err == nil {
			t.Fatal("partial policy accepted")
		}
	}
	r, err := NormalizeRule(RouteRule{Pattern: "EXAMPLE.COM.", Strategy: " RANDOM ", TargetProfileIDs: []string{" a ", "b"}}, profiles)
	if err != nil || r.Pattern != "example.com" || r.Strategy != "random" || !reflect.DeepEqual(r.TargetProfileIDs, []string{"a", "b"}) {
		t.Fatalf("%+v %v", r, err)
	}
}
