package routing

import "strings"

const (
	RouteStoreVersion   = 2
	RouteTypeDomain     = "domain"
	RouteTypeIP         = "ip"
	RouteTypeCIDR       = "cidr"
	RouteStrategyFixed  = "fixed"
	RouteStrategyRandom = "random"
)

type RouteRule struct {
	ID               string   `json:"id"`
	Pattern          string   `json:"pattern"`
	Type             string   `json:"type"`
	Enabled          bool     `json:"enabled"`
	Strategy         string   `json:"strategy,omitempty"`
	TargetProfileIDs []string `json:"targetProfileIds,omitempty"`
}

type RouteGroup struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Description      string      `json:"description,omitempty"`
	Enabled          bool        `json:"enabled"`
	Strategy         string      `json:"strategy"`
	TargetProfileIDs []string    `json:"targetProfileIds"`
	Rules            []RouteRule `json:"rules"`
}

type RouteStore struct {
	Version int          `json:"version"`
	Groups  []RouteGroup `json:"groups"`
}

// EffectiveRoute is the fully resolved runtime representation of a rule.
type EffectiveRoute struct {
	ID               string
	GroupID          string
	GroupName        string
	Pattern          string
	Type             string
	Strategy         string
	TargetProfileIDs []string
}

func explicitRoute(rule RouteRule) bool {
	return strings.TrimSpace(rule.Strategy) != "" && len(rule.TargetProfileIDs) > 0
}

func ResolveEffectiveRoutes(store RouteStore) []EffectiveRoute {
	var routes []EffectiveRoute
	for _, group := range store.Groups {
		if !group.Enabled {
			continue
		}
		for _, rule := range group.Rules {
			if !rule.Enabled {
				continue
			}
			strategy := group.Strategy
			targets := group.TargetProfileIDs
			if explicitRoute(rule) {
				strategy = rule.Strategy
				targets = rule.TargetProfileIDs
			}
			routes = append(routes, EffectiveRoute{ID: rule.ID, GroupID: group.ID, GroupName: group.Name, Pattern: rule.Pattern, Type: rule.Type, Strategy: strategy, TargetProfileIDs: append([]string(nil), targets...)})
		}
	}
	return routes
}
