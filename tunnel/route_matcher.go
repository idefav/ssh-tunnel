package tunnel

import (
	"log"
	"net"
	"sort"
	"ssh-tunnel/cfg"
	"strings"
	"sync"
)

// compiledRoute holds a pre-processed routing rule for efficient matching.
type compiledRoute struct {
	ProfileID string
	Pattern   string
	Type      string // "domain", "ip", "cidr"
	// For domain matching
	lowerPattern string
	isWildcard   bool   // starts with "*."
	domainSuffix string // suffix after "*."
	// For CIDR matching
	cidrNet *net.IPNet
	// Specificity score (higher = more specific, matched first)
	specificity int
}

// RouteMatcher matches hostnames/IPs to profile IDs based on domain routing rules.
type RouteMatcher struct {
	mu     sync.RWMutex
	routes []compiledRoute
	cache  map[string]string // host -> profileID (empty string = no match)
}

func NewRouteMatcher() *RouteMatcher {
	return &RouteMatcher{
		cache: make(map[string]string),
	}
}

// LoadRoutes compiles routing rules from all profiles. Call on startup and whenever profiles change.
// Routes belonging to activeProfileID are skipped since the main tunnel already handles that profile.
func (rm *RouteMatcher) LoadRoutes(profiles map[string]cfg.SSHProfile, activeProfileID string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.routes = nil
	rm.cache = make(map[string]string)

	for profileID, profile := range profiles {
		if profileID == activeProfileID {
			continue
		}
		for _, route := range profile.DomainRoutes {
			cr := compileRoute(profileID, route)
			if cr != nil {
				rm.routes = append(rm.routes, *cr)
			}
		}
	}

	// Sort by specificity descending (most specific first)
	sort.Slice(rm.routes, func(i, j int) bool {
		return rm.routes[i].specificity > rm.routes[j].specificity
	})
}

// Match finds the profileID that should handle the given host.
// Returns (profileID, true) if a match is found, ("", false) otherwise.
func (rm *RouteMatcher) Match(host string) (string, bool) {
	rm.mu.RLock()
	if len(rm.routes) == 0 {
		rm.mu.RUnlock()
		return "", false
	}

	// Check cache
	if cached, ok := rm.cache[host]; ok {
		rm.mu.RUnlock()
		if cached == "" {
			return "", false
		}
		return cached, true
	}
	// Copy routes for matching outside lock
	routes := rm.routes
	rm.mu.RUnlock()

	// Extract hostname (strip port if present)
	hostOnly := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	} else {
		hostOnly = strings.Split(host, ":")[0]
	}
	hostOnly = strings.ToLower(strings.TrimRight(hostOnly, "."))

	// Try matching each route in specificity order
	for _, route := range routes {
		if matchRoute(route, hostOnly) {
			rm.mu.Lock()
			rm.cache[host] = route.ProfileID
			rm.mu.Unlock()
			return route.ProfileID, true
		}
	}

	// Cache negative result
	rm.mu.Lock()
	rm.cache[host] = ""
	rm.mu.Unlock()
	return "", false
}

// ClearCache clears the match cache. Call when routes change.
func (rm *RouteMatcher) ClearCache() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.cache = make(map[string]string)
}

// HasRoutesForProfile returns true if any routes exist for the given profileID.
func (rm *RouteMatcher) HasRoutesForProfile(profileID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for _, r := range rm.routes {
		if r.ProfileID == profileID {
			return true
		}
	}
	return false
}

// ProfilesWithRoutes returns the set of profileIDs that have at least one routing rule.
func (rm *RouteMatcher) ProfilesWithRoutes() map[string]bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	result := make(map[string]bool)
	for _, r := range rm.routes {
		result[r.ProfileID] = true
	}
	return result
}

func compileRoute(profileID string, route cfg.DomainRoute) *compiledRoute {
	pattern := strings.TrimSpace(route.Pattern)
	if pattern == "" {
		return nil
	}
	routeType := strings.ToLower(strings.TrimSpace(route.Type))
	if routeType == "" {
		routeType = guessRouteType(pattern)
	}

	cr := &compiledRoute{
		ProfileID:    profileID,
		Pattern:      pattern,
		Type:         routeType,
		lowerPattern: strings.ToLower(pattern),
	}

	switch routeType {
	case "domain":
		if strings.HasPrefix(cr.lowerPattern, "*.") {
			cr.isWildcard = true
			cr.domainSuffix = cr.lowerPattern[1:] // keep the dot: ".example.com"
			cr.specificity = len(cr.domainSuffix) * 10
		} else {
			cr.specificity = len(cr.lowerPattern)*10 + 5 // exact domain > wildcard
		}
	case "ip":
		cr.specificity = specificityForIPPattern(cr.lowerPattern)
	case "cidr":
		_, cidrNet, err := net.ParseCIDR(pattern)
		if err != nil {
			log.Printf("Invalid CIDR pattern '%s' for profile '%s': %v", pattern, profileID, err)
			return nil
		}
		cr.cidrNet = cidrNet
		ones, _ := cidrNet.Mask.Size()
		cr.specificity = ones // /32 > /24 > /16 > /8
	default:
		log.Printf("Unknown route type '%s' for profile '%s', pattern '%s'", routeType, profileID, pattern)
		return nil
	}

	return cr
}

func guessRouteType(pattern string) string {
	if strings.Contains(pattern, "/") {
		return "cidr"
	}
	// Wildcard domain patterns like *.example.com
	if strings.HasPrefix(pattern, "*.") {
		return "domain"
	}
	// Check if it looks like an IP or IP pattern
	normalized := strings.Replace(pattern, "*", "0", -1)
	if ip := net.ParseIP(normalized); ip != nil {
		return "ip"
	}
	return "domain"
}

func specificityForIPPattern(pattern string) int {
	// Count non-wildcard octets
	parts := strings.Split(pattern, ".")
	score := 0
	for _, p := range parts {
		if p != "*" {
			score += 32
		}
	}
	return score
}

func matchRoute(route compiledRoute, hostOnly string) bool {
	switch route.Type {
	case "domain":
		return matchDomain(route, hostOnly)
	case "ip":
		return matchIP(route, hostOnly)
	case "cidr":
		return matchCIDR(route, hostOnly)
	}
	return false
}

func matchDomain(route compiledRoute, hostOnly string) bool {
	if route.isWildcard {
		// *.example.com matches sub.example.com and example.com
		return strings.HasSuffix(hostOnly, route.domainSuffix) || hostOnly == route.lowerPattern[2:]
	}
	// Exact suffix match: "google.com" matches "www.google.com" and "google.com"
	if hostOnly == route.lowerPattern {
		return true
	}
	return strings.HasSuffix(hostOnly, "."+route.lowerPattern)
}

func matchIP(route compiledRoute, hostOnly string) bool {
	// Wildcard IP matching: "192.168.*" matches "192.168.1.1"
	pattern := route.lowerPattern
	if !strings.Contains(pattern, "*") {
		// Exact IP match
		return hostOnly == pattern
	}

	// Split into octets and compare
	patternParts := strings.Split(pattern, ".")
	hostParts := strings.Split(hostOnly, ".")
	if len(patternParts) != len(hostParts) {
		return false
	}
	for i, pp := range patternParts {
		if pp == "*" {
			continue
		}
		if pp != hostParts[i] {
			return false
		}
	}
	return true
}

func matchCIDR(route compiledRoute, hostOnly string) bool {
	if route.cidrNet == nil {
		return false
	}
	ip := net.ParseIP(hostOnly)
	if ip == nil {
		return false
	}
	return route.cidrNet.Contains(ip)
}
