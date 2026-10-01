package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"ssh-tunnel/cfg"
	"ssh-tunnel/dnsproxy"
	"strings"
	"time"
)

type dnsPolicy struct {
	matcher        *RouteMatcher
	defaultProfile string
	upstreams      map[string][]string
}

func newDNSPolicy(store cfg.ProfileStore, routes []cfg.EffectiveRoute, defaults []string) (*dnsPolicy, error) {
	p := &dnsPolicy{matcher: NewRouteMatcher(), defaultProfile: store.ActiveProfileID, upstreams: make(map[string][]string)}
	if p.defaultProfile == "" {
		p.defaultProfile = cfg.DEFAULT_PROFILE_ID
	}
	if _, exists := store.Profiles[p.defaultProfile]; !exists {
		// Legacy/CLI-only installations can have an empty profile store. Their
		// active tunnel is still configured by the ordinary SSH flags.
		p.upstreams[p.defaultProfile] = append([]string(nil), defaults...)
	}
	for id, profile := range store.Profiles {
		servers, err := cfg.NormalizeDNSUpstreams(profile.DNSUpstreams, true)
		if err != nil {
			return nil, fmt.Errorf("Profile %s: %w", id, err)
		}
		if len(servers) == 0 {
			servers = append([]string(nil), defaults...)
		}
		p.upstreams[id] = servers
	}
	// IP/CIDR TCP rules cannot select a DNS answer before it exists.
	var domains []cfg.EffectiveRoute
	for _, r := range routes {
		if r.Type == cfg.RouteTypeDomain {
			domains = append(domains, r)
		}
	}
	p.matcher.LoadRoutes(domains)
	return p, nil
}

func (t *Tunnel) queryDNS(parent context.Context, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, dnsproxy.QueryTimeout)
	defer cancel()
	q, _, err := dnsproxy.ParseQuery(query)
	if err != nil {
		return nil, err
	}
	// Updates wait for bounded in-flight queries. During a credential refresh
	// dnsPolicy is nil until routing publishes a consistent replacement.
	for !t.runtimeMu.TryRLock() {
		if !waitWithContext(ctx, 5*time.Millisecond) {
			return nil, ctx.Err()
		}
	}
	defer t.runtimeMu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := t.dnsPolicy
	if p == nil {
		return nil, errors.New("DNS路由正在更新")
	}
	host := strings.TrimSuffix(strings.ToLower(q.Name.String()), ".")
	rule, matched := p.matcher.Match(host)
	profiles := []string{p.defaultProfile}
	if matched {
		profiles = routeCandidates(rule, nil)
	}
	var last error
	for _, id := range profiles {
		for _, address := range p.upstreams[id] {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			attempt, end := context.WithTimeout(ctx, dnsproxy.AttemptTimeout)
			started := time.Now()
			conn, err := t.dialDNSProfile(attempt, id, address)
			var response []byte
			if err == nil {
				response, err = dnsproxy.Exchange(attempt, conn, query)
			} else if conn != nil {
				conn.Close()
			}
			end()
			log.Printf("DNS domain=%q rule=%q group=%q profile=%q upstream=%q duration=%s success=%t error=%v", host, rule.ID, rule.GroupID, id, address, time.Since(started), err == nil, err)
			if err == nil {
				return response, nil
			}
			last = err
		}
	}
	if last == nil {
		last = errors.New("没有可用DNS出口")
	}
	return nil, fmt.Errorf("DNS解析失败: %w", last)
}

// Caller holds runtimeMu. DNS uses existing pools but never records a business
// access sample or wraps the stream in the business traffic meter.
func (t *Tunnel) dialDNSProfile(ctx context.Context, id, address string) (net.Conn, error) {
	if t.dnsDialer != nil {
		return t.dnsDialer(ctx, id, address)
	}
	outlet := t
	if id != t.profileID {
		if t.profileTunnelMgr == nil {
			return nil, errors.New("DNS Profile不可用")
		}
		outlet = t.profileTunnelMgr.GetTunnel(id)
	}
	if outlet == nil {
		return nil, errors.New("DNS Profile不可用")
	}
	conn, _, _, _, _, err := outlet.dialSSHConnUntracked(ctx, address, outlet.newRequestRetryState())
	return conn, err
}
