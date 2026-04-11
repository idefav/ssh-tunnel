package tunnel

import (
	"ssh-tunnel/cfg"
	"testing"
)

func TestRouteMatcher_DomainSuffix(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"japan": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "google.com", Type: "domain"},
		}},
	}, "")

	tests := []struct {
		host      string
		wantID    string
		wantMatch bool
	}{
		{"www.google.com", "japan", true},
		{"google.com", "japan", true},
		{"maps.google.com:443", "japan", true},
		{"notgoogle.com", "", false},
		{"example.com", "", false},
	}

	for _, tt := range tests {
		id, ok := rm.Match(tt.host)
		if ok != tt.wantMatch || id != tt.wantID {
			t.Errorf("Match(%q) = (%q, %v), want (%q, %v)", tt.host, id, ok, tt.wantID, tt.wantMatch)
		}
	}
}

func TestRouteMatcher_DomainWildcard(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"us": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "*.internal.corp", Type: "domain"},
		}},
	}, "")

	tests := []struct {
		host      string
		wantID    string
		wantMatch bool
	}{
		{"service.internal.corp", "us", true},
		{"deep.service.internal.corp", "us", true},
		{"internal.corp", "us", true},
		{"external.corp", "", false},
	}

	for _, tt := range tests {
		id, ok := rm.Match(tt.host)
		if ok != tt.wantMatch || id != tt.wantID {
			t.Errorf("Match(%q) = (%q, %v), want (%q, %v)", tt.host, id, ok, tt.wantID, tt.wantMatch)
		}
	}
}

func TestRouteMatcher_IPWildcard(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"hk": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "192.168.*.*", Type: "ip"},
			{Pattern: "10.0.1.5", Type: "ip"},
		}},
	}, "")

	tests := []struct {
		host      string
		wantID    string
		wantMatch bool
	}{
		{"192.168.1.1", "hk", true},
		{"192.168.0.100:8080", "hk", true},
		{"10.0.1.5", "hk", true},
		{"10.0.1.6", "", false},
		{"172.16.0.1", "", false},
	}

	for _, tt := range tests {
		id, ok := rm.Match(tt.host)
		if ok != tt.wantMatch || id != tt.wantID {
			t.Errorf("Match(%q) = (%q, %v), want (%q, %v)", tt.host, id, ok, tt.wantID, tt.wantMatch)
		}
	}
}

func TestRouteMatcher_CIDR(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"sg": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "10.0.0.0/8", Type: "cidr"},
		}},
	}, "")

	tests := []struct {
		host      string
		wantID    string
		wantMatch bool
	}{
		{"10.1.2.3", "sg", true},
		{"10.255.255.255:22", "sg", true},
		{"11.0.0.1", "", false},
		{"example.com", "", false},
	}

	for _, tt := range tests {
		id, ok := rm.Match(tt.host)
		if ok != tt.wantMatch || id != tt.wantID {
			t.Errorf("Match(%q) = (%q, %v), want (%q, %v)", tt.host, id, ok, tt.wantID, tt.wantMatch)
		}
	}
}

func TestRouteMatcher_Specificity(t *testing.T) {
	// More specific rule should win
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"general": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "*.google.com", Type: "domain"},
		}},
		"specific": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "maps.google.com", Type: "domain"},
		}},
	}, "")

	id, ok := rm.Match("maps.google.com")
	if !ok || id != "specific" {
		t.Errorf("Match(maps.google.com) = (%q, %v), want (specific, true)", id, ok)
	}

	id, ok = rm.Match("www.google.com")
	if !ok || id != "general" {
		t.Errorf("Match(www.google.com) = (%q, %v), want (general, true)", id, ok)
	}
}

func TestRouteMatcher_Cache(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"jp": {DomainRoutes: []cfg.DomainRoute{
			{Pattern: "example.jp", Type: "domain"},
		}},
	}, "")

	// First call populates cache
	id1, ok1 := rm.Match("test.example.jp")
	// Second call should hit cache
	id2, ok2 := rm.Match("test.example.jp")

	if id1 != id2 || ok1 != ok2 {
		t.Errorf("Cache inconsistency: first=(%q,%v) second=(%q,%v)", id1, ok1, id2, ok2)
	}

	// ClearCache then match again
	rm.ClearCache()
	id3, ok3 := rm.Match("test.example.jp")
	if id3 != "jp" || !ok3 {
		t.Errorf("After cache clear: Match = (%q, %v), want (jp, true)", id3, ok3)
	}
}

func TestRouteMatcher_GuessType(t *testing.T) {
	tests := []struct {
		pattern  string
		wantType string
	}{
		{"google.com", "domain"},
		{"*.example.com", "domain"},
		{"192.168.1.*", "ip"},
		{"10.0.0.0/8", "cidr"},
		{"172.16.0.1", "ip"},
	}
	for _, tt := range tests {
		got := guessRouteType(tt.pattern)
		if got != tt.wantType {
			t.Errorf("guessRouteType(%q) = %q, want %q", tt.pattern, got, tt.wantType)
		}
	}
}

func TestRouteMatcher_ProfilesWithRoutes(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{
		"jp":     {DomainRoutes: []cfg.DomainRoute{{Pattern: "example.jp", Type: "domain"}}},
		"us":     {DomainRoutes: []cfg.DomainRoute{{Pattern: "example.us", Type: "domain"}}},
		"noroute": {},
	}, "")

	profiles := rm.ProfilesWithRoutes()
	if !profiles["jp"] || !profiles["us"] {
		t.Errorf("Expected jp and us to have routes")
	}
	if profiles["noroute"] {
		t.Errorf("noroute should not have routes")
	}
}

func TestRouteMatcher_EmptyRoutes(t *testing.T) {
	rm := NewRouteMatcher()
	rm.LoadRoutes(map[string]cfg.SSHProfile{}, "")

	id, ok := rm.Match("anything.com")
	if ok || id != "" {
		t.Errorf("Expected no match with empty routes, got (%q, %v)", id, ok)
	}
}
