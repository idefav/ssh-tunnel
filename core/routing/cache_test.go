package routing

import (
	"fmt"
	"strings"
	"testing"
)

func TestMatchCacheIsBoundedWithoutChangingResults(t *testing.T) {
	m := NewRouteMatcher()
	m.LoadRoutes([]EffectiveRoute{{ID: "r", Pattern: "example.org", Type: "domain", Strategy: "fixed", TargetProfileIDs: []string{"a"}}})
	for i := 0; i < 10000; i++ {
		rule, ok := m.Match(fmt.Sprintf("%d.example.org", i))
		if !ok || rule.ID != "r" {
			t.Fatal("cache changed matching")
		}
	}
	if len(m.cache) > 4096 {
		t.Fatal("unbounded cache")
	}
	n := len(m.cache)
	m.Match(strings.Repeat("x", 1024) + ".example.org")
	if len(m.cache) != n {
		t.Fatal("oversized key retained")
	}
}
