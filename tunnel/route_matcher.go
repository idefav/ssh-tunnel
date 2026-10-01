package tunnel

import "github.com/idefav/ssh-tunnel/core/routing"

// RouteMatcher is shared with mobile without desktop configuration dependencies.
type RouteMatcher = routing.RouteMatcher

func NewRouteMatcher() *RouteMatcher       { return routing.NewRouteMatcher() }
func guessRouteType(pattern string) string { return routing.GuessRouteType(pattern) }
