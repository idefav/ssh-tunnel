package tunnel

import (
	"log"
	"net"
	"sort"
	"ssh-tunnel/cfg"
	"strconv"
	"strings"
	"sync"
)

// compiledRoute holds a pre-processed routing rule for efficient matching.
type compiledRoute struct {
	Rule    cfg.RouteRule
	Pattern string
	Type    string // "domain", "ip", "cidr"
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
	mu         sync.RWMutex
	routes     []compiledRoute
	routesByID map[string]cfg.RouteRule
	cache      map[string]string // host -> routeID (empty string = no match)
	generation uint64
}

func NewRouteMatcher() *RouteMatcher {
	return &RouteMatcher{
		cache:      make(map[string]string),
		routesByID: make(map[string]cfg.RouteRule),
	}
}

// LoadRoutes compiles enabled standalone routing rules.
func (rm *RouteMatcher) LoadRoutes(rules []cfg.RouteRule) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.routes = nil
	rm.cache = make(map[string]string)
	rm.routesByID = make(map[string]cfg.RouteRule)
	rm.generation++

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		cr := compileRoute(rule)
		if cr != nil {
			rm.routes = append(rm.routes, *cr)
			rm.routesByID[rule.ID] = cloneRouteRule(rule)
		}
	}

	// Sort by specificity descending (most specific first)
	sort.SliceStable(rm.routes, func(i, j int) bool {
		return rm.routes[i].specificity > rm.routes[j].specificity
	})
}

// Match finds the standalone route rule that should handle the given host.
func (rm *RouteMatcher) Match(host string) (cfg.RouteRule, bool) {
	rm.mu.RLock()
	if len(rm.routes) == 0 {
		rm.mu.RUnlock()
		return cfg.RouteRule{}, false
	}

	// Check cache
	if cached, ok := rm.cache[host]; ok {
		rule, found := rm.routesByID[cached]
		rm.mu.RUnlock()
		if cached == "" || !found {
			return cfg.RouteRule{}, false
		}
		return cloneRouteRule(rule), true
	}
	// Copy routes for matching outside lock
	routes := rm.routes
	generation := rm.generation
	rm.mu.RUnlock()

	// Extract hostname (strip port if present)
	hostOnly := strings.Trim(host, "[]")
	if net.ParseIP(hostOnly) != nil {
		// Raw IPv4/IPv6 address without a port.
	} else if h, _, err := net.SplitHostPort(host); err == nil {
		hostOnly = h
	} else {
		hostOnly = strings.Split(host, ":")[0]
	}
	hostOnly = strings.ToLower(strings.TrimRight(hostOnly, "."))

	// Try matching each route in specificity order
	for _, route := range routes {
		if matchRoute(route, hostOnly) {
			rm.mu.Lock()
			if rm.generation == generation {
				rm.cache[host] = route.Rule.ID
			}
			rm.mu.Unlock()
			return cloneRouteRule(route.Rule), true
		}
	}

	// Cache negative result
	rm.mu.Lock()
	if rm.generation == generation {
		rm.cache[host] = ""
	}
	rm.mu.Unlock()
	return cfg.RouteRule{}, false
}

// ClearCache clears the match cache. Call when routes change.
func (rm *RouteMatcher) ClearCache() {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.cache = make(map[string]string)
	rm.generation++
}

// HasRoutesForProfile returns true if an enabled rule targets profileID.
func (rm *RouteMatcher) HasRoutesForProfile(profileID string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	for _, r := range rm.routes {
		for _, target := range r.Rule.TargetProfileIDs {
			if target == profileID {
				return true
			}
		}
	}
	return false
}

// ProfilesWithRoutes returns the set of profileIDs targeted by enabled rules.
func (rm *RouteMatcher) ProfilesWithRoutes() map[string]bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	result := make(map[string]bool)
	for _, r := range rm.routes {
		for _, target := range r.Rule.TargetProfileIDs {
			result[target] = true
		}
	}
	return result
}

func cloneRouteRule(rule cfg.RouteRule) cfg.RouteRule {
	rule.TargetProfileIDs = append([]string(nil), rule.TargetProfileIDs...)
	return rule
}

func compileRoute(rule cfg.RouteRule) *compiledRoute {
	pattern := strings.TrimSpace(rule.Pattern)
	if pattern == "" {
		return nil
	}
	routeType := strings.ToLower(strings.TrimSpace(rule.Type))
	if routeType == "" {
		routeType = guessRouteType(pattern)
	}

	cr := &compiledRoute{
		Rule:         cloneRouteRule(rule),
		Pattern:      pattern,
		Type:         routeType,
		lowerPattern: strings.ToLower(pattern),
	}

	switch routeType {
	case "domain":
		if strings.HasPrefix(cr.lowerPattern, "*.") {
			cr.isWildcard = true
			cr.domainSuffix = cr.lowerPattern[1:] // keep the dot: ".example.com"
			cr.specificity = len(cr.lowerPattern[2:]) * 10
		} else {
			cr.specificity = len(cr.lowerPattern)*10 + 5 // exact domain > wildcard
		}
	case "ip":
		cr.specificity = specificityForIPPattern(cr.lowerPattern)
	case "cidr":
		_, cidrNet, err := net.ParseCIDR(pattern)
		if err != nil {
			log.Printf("Invalid CIDR pattern '%s' for route '%s': %v", pattern, rule.ID, err)
			return nil
		}
		cr.cidrNet = cidrNet
		ones, _ := cidrNet.Mask.Size()
		cr.specificity = ones // /32 > /24 > /16 > /8
	default:
		log.Printf("Unknown route type '%s' for route '%s', pattern '%s'", routeType, rule.ID, pattern)
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
	// Check if it looks like an IP or IP wildcard prefix.
	parts := strings.Split(pattern, ".")
	looksLikeIP := len(parts) >= 1 && len(parts) <= 4
	for _, part := range parts {
		if part == "*" {
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			looksLikeIP = false
			break
		}
	}
	if looksLikeIP && (len(parts) == 4 || parts[len(parts)-1] == "*") {
		return "ip"
	}
	return "domain"
}

func specificityForIPPattern(pattern string) int {
	// Count non-wildcard octets
	parts := strings.Split(pattern, ".")
	score := 0
	hasWildcard := false
	for _, p := range parts {
		if p != "*" {
			score += 8
		} else {
			hasWildcard = true
		}
	}
	if !hasWildcard {
		score++
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
	if net.ParseIP(hostOnly) != nil {
		return false
	}
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
