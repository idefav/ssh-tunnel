package tunnel

import (
	"math/rand"
	"ssh-tunnel/cfg"
)

// routeCandidates selects outlets independently of the address to dial. TCP
// dials the destination; DNS matches the question and dials a resolver instead.
func routeCandidates(rule cfg.EffectiveRoute, attempted map[string]bool) []string {
	targets := append([]string(nil), rule.TargetProfileIDs...)
	if rule.Strategy == cfg.RouteStrategyRandom {
		remaining := targets[:0]
		for _, id := range targets {
			if !attempted[id] {
				remaining = append(remaining, id)
			}
		}
		targets = remaining
		rand.Shuffle(len(targets), func(i, j int) { targets[i], targets[j] = targets[j], targets[i] })
	}
	return targets
}
