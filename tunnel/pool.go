package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHMemberState string

// globalMemberIDCounter provides unique member IDs across all tunnel instances.
var globalMemberIDCounter uint64

func nextGlobalMemberID() uint64 {
	return atomic.AddUint64(&globalMemberIDCounter, 1)
}

const (
	sshMemberHealthy      SSHMemberState = "healthy"
	sshMemberSuspect      SSHMemberState = "suspect"
	sshMemberProbing      SSHMemberState = "probing"
	sshMemberReconnecting SSHMemberState = "reconnecting"
	sshMemberEvicted      SSHMemberState = "evicted"
)

type SSHPoolMember struct {
	ID                uint64         `json:"id"`
	ProfileID         string         `json:"profileId,omitempty"`
	Generation        uint64         `json:"generation"`
	State             SSHMemberState `json:"state"`
	Client            *ssh.Client    `json:"-"`
	ActiveChannels    int64          `json:"activeChannels"`
	CreatedAt         time.Time      `json:"createdAt,omitempty"`
	LastUsedAt        time.Time      `json:"lastUsedAt,omitempty"`
	LastProbeAt       time.Time      `json:"lastProbeAt,omitempty"`
	LastProbeResult   string         `json:"lastProbeResult,omitempty"`
	LastError         string         `json:"lastError,omitempty"`
	LastHardFailureAt time.Time      `json:"lastHardFailureAt,omitempty"`
	TimeoutBurst      uint64         `json:"timeoutBurst"`
	ProbeFailures     uint64         `json:"probeFailures"`
	TimeoutWindowAt   time.Time      `json:"timeoutWindowAt,omitempty"`
	timeoutTargets    map[string]time.Time
}

type SSHPoolSnapshot struct {
	ID                uint64         `json:"id"`
	ProfileID         string         `json:"profileId,omitempty"`
	ServerAddress     string         `json:"serverAddress,omitempty"`
	User              string         `json:"user,omitempty"`
	Generation        uint64         `json:"generation"`
	State             SSHMemberState `json:"state"`
	ActiveChannels    int64          `json:"activeChannels"`
	CreatedAt         time.Time      `json:"createdAt,omitempty"`
	LastUsedAt        time.Time      `json:"lastUsedAt,omitempty"`
	LastProbeAt       time.Time      `json:"lastProbeAt,omitempty"`
	LastProbeResult   string         `json:"lastProbeResult,omitempty"`
	LastError         string         `json:"lastError,omitempty"`
	LastHardFailureAt time.Time      `json:"lastHardFailureAt,omitempty"`
	TimeoutBurst      uint64         `json:"timeoutBurst"`
	ProbeFailures     uint64         `json:"probeFailures"`
}

type pooledConn struct {
	net.Conn
	member *SSHPoolMember
	once   sync.Once
}

const maxEvictedPoolHistory = 20

const (
	sshPoolBalanceLeastActive = "least_active"
	sshPoolBalanceRoundRobin  = "round_robin"
	sshPoolBalanceRandom      = "random"
)

const defaultSSHProbeURLs = "https://ipinfo.io/json,https://api.ipify.org?format=json,https://www.cloudflare.com/cdn-cgi/trace"

type SSHRetryInfo struct {
	RetryCount      int
	RetryReason     string
	RetryMembers    []uint64
	BalanceStrategy string
}

type requestRetryState struct {
	maxExtra            int
	exclude             map[uint64]bool
	members             []uint64
	reason              string
	balanceStrategy     string
	lastErr             error
	lastClass           string
	lastMember          *SSHPoolMember
	reconnectTriggered  bool
	routeID             string
	routeStrategy       string
	attemptedProfiles   []string
	attemptedProfileSet map[string]bool
	profileRetryStates  map[string]*requestRetryState
}

func (t *Tunnel) newRequestRetryState() *requestRetryState {
	return &requestRetryState{
		maxExtra:            t.configuredProxyRetryMaxAttempts(),
		exclude:             make(map[uint64]bool),
		balanceStrategy:     t.configuredBalanceStrategy(),
		attemptedProfileSet: make(map[string]bool),
		profileRetryStates:  make(map[string]*requestRetryState),
	}
}

func (s *requestRetryState) setRoute(ruleID, strategy string) {
	if s == nil {
		return
	}
	if s.routeID != "" && s.routeID != ruleID {
		s.attemptedProfiles = nil
		s.attemptedProfileSet = make(map[string]bool)
		s.profileRetryStates = make(map[string]*requestRetryState)
	}
	s.routeID = ruleID
	s.routeStrategy = strategy
}

func (s *requestRetryState) markProfileAttempt(profileID string) {
	if s == nil || profileID == "" {
		return
	}
	if s.attemptedProfileSet == nil {
		s.attemptedProfileSet = make(map[string]bool)
	}
	if !s.attemptedProfileSet[profileID] {
		s.attemptedProfileSet[profileID] = true
		s.attemptedProfiles = append(s.attemptedProfiles, profileID)
	}
}

func (s *requestRetryState) routeProfiles() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.attemptedProfiles...)
}

func (s *requestRetryState) maxAttempts() int {
	if s == nil || s.maxExtra < 0 {
		return 1
	}
	return 1 + s.maxExtra
}

func (s *requestRetryState) canAttempt() bool {
	if s == nil {
		return false
	}
	return len(s.members) < s.maxAttempts()
}

func (s *requestRetryState) recordAttempt(member *SSHPoolMember) {
	if s == nil || member == nil {
		return
	}
	s.lastMember = member
	s.exclude[member.ID] = true
	s.members = append(s.members, member.ID)
}

func (s *requestRetryState) recordFailure(member *SSHPoolMember, class string, reconnectTriggered bool, err error) {
	if s == nil {
		return
	}
	if member != nil {
		s.lastMember = member
	}
	s.lastClass = class
	s.reconnectTriggered = reconnectTriggered
	s.lastErr = err
	if err != nil && s.reason == "" {
		s.reason = err.Error()
	}
}

func (s *requestRetryState) recordProxyRetryReason(err error) {
	if s == nil || err == nil || s.reason != "" {
		return
	}
	s.reason = err.Error()
}

func (s *requestRetryState) info() SSHRetryInfo {
	if s == nil {
		return SSHRetryInfo{}
	}
	retryCount := len(s.members) - 1
	if retryCount < 0 {
		retryCount = 0
	}
	return SSHRetryInfo{
		RetryCount:      retryCount,
		RetryReason:     s.reason,
		RetryMembers:    append([]uint64(nil), s.members...),
		BalanceStrategy: s.balanceStrategy,
	}
}

func memberID(member *SSHPoolMember) uint64 {
	if member == nil {
		return 0
	}
	return member.ID
}

func (p *pooledConn) Close() error {
	var err error
	p.once.Do(func() {
		if p.member != nil {
			p.member.releaseChannel()
		}
		if p.Conn != nil {
			err = p.Conn.Close()
		}
	})
	return err
}

func (p *pooledConn) CloseWrite() error {
	if p == nil || p.Conn == nil {
		return nil
	}
	if cw, ok := p.Conn.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return p.Close()
}

func (t *Tunnel) acquireMemberChannel(member *SSHPoolMember) {
	if member == nil {
		return
	}
	atomic.AddInt64(&member.ActiveChannels, 1)
	t.sshPoolMu.Lock()
	if member.State != sshMemberEvicted {
		member.LastUsedAt = time.Now()
	}
	t.sshPoolMu.Unlock()
}

func (m *SSHPoolMember) releaseChannel() {
	if m == nil {
		return
	}
	for {
		current := atomic.LoadInt64(&m.ActiveChannels)
		if current <= 0 {
			return
		}
		if atomic.CompareAndSwapInt64(&m.ActiveChannels, current, current-1) {
			return
		}
	}
}

func (t *Tunnel) configuredPoolSize() int {
	if t.sshPoolSize <= 0 {
		return 5
	}
	return t.sshPoolSize
}

func (t *Tunnel) configuredProbeFailureThreshold() uint64 {
	if t.sshProbeFailureThreshold <= 0 {
		return 2
	}
	return t.sshProbeFailureThreshold
}

func (t *Tunnel) configuredProbeTimeout() time.Duration {
	if t.sshProbeTimeout <= 0 {
		return 3 * time.Second
	}
	return t.sshProbeTimeout
}

func (t *Tunnel) configuredSuspectCooldown() time.Duration {
	if t.sshSuspectCooldown <= 0 {
		return 10 * time.Second
	}
	return t.sshSuspectCooldown
}

func (t *Tunnel) configuredBalanceStrategy() string {
	switch strings.TrimSpace(strings.ToLower(t.sshPoolBalanceStrategy)) {
	case sshPoolBalanceRoundRobin:
		return sshPoolBalanceRoundRobin
	case sshPoolBalanceRandom:
		return sshPoolBalanceRandom
	default:
		return sshPoolBalanceLeastActive
	}
}

func (t *Tunnel) configuredProxyRetryMaxAttempts() int {
	if t.proxyRetryMaxAttempts < 0 {
		return 0
	}
	return t.proxyRetryMaxAttempts
}

func (t *Tunnel) configuredProxyRetryInitialBufferBytes() int {
	if t.proxyRetryInitialBufferBytes <= 0 {
		return 32768
	}
	return t.proxyRetryInitialBufferBytes
}

func (t *Tunnel) configuredReplenishInterval() time.Duration {
	if t.sshPoolReplenishInterval <= 0 {
		return time.Second
	}
	return t.sshPoolReplenishInterval
}

func (t *Tunnel) snapshotPoolMembers() []SSHPoolSnapshot {
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	out := make([]SSHPoolSnapshot, 0, len(t.sshPool)+len(t.sshPoolEvicted))
	appendSnapshot := func(member *SSHPoolMember) {
		if member == nil {
			return
		}
		out = append(out, SSHPoolSnapshot{
			ID:                member.ID,
			ProfileID:         member.ProfileID,
			ServerAddress:     t.serverAddress,
			User:              t.user,
			Generation:        member.Generation,
			State:             member.State,
			ActiveChannels:    atomic.LoadInt64(&member.ActiveChannels),
			CreatedAt:         member.CreatedAt,
			LastUsedAt:        member.LastUsedAt,
			LastProbeAt:       member.LastProbeAt,
			LastProbeResult:   member.LastProbeResult,
			LastError:         member.LastError,
			LastHardFailureAt: member.LastHardFailureAt,
			TimeoutBurst:      member.TimeoutBurst,
			ProbeFailures:     member.ProbeFailures,
		})
	}
	for _, member := range t.sshPool {
		appendSnapshot(member)
	}
	for _, member := range t.sshPoolEvicted {
		appendSnapshot(member)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (t *Tunnel) countPoolStates() (healthy, suspect, probing, reconnecting, evicted int) {
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	for _, member := range t.sshPool {
		if member == nil {
			continue
		}
		switch member.State {
		case sshMemberHealthy:
			healthy++
		case sshMemberSuspect:
			suspect++
		case sshMemberProbing:
			probing++
		case sshMemberReconnecting:
			reconnecting++
		case sshMemberEvicted:
			evicted++
		}
	}
	evicted = len(t.sshPoolEvicted)
	return
}

func (t *Tunnel) healthyPoolCount() int {
	healthy, _, _, _, _ := t.countPoolStates()
	return healthy
}

func (t *Tunnel) currentPoolSize() int {
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	count := 0
	for _, member := range t.sshPool {
		if member != nil && member.State != sshMemberEvicted {
			count++
		}
	}
	return count
}

// shrinkPoolTo evicts idle (0 active channels) members until pool size <= target.
// Prefers evicting members with fewest active channels first.
func (t *Tunnel) shrinkPoolTo(target int) {
	for {
		current := t.currentPoolSize()
		if current <= target {
			return
		}
		// Find an idle member to evict (activeChannels == 0)
		t.sshPoolMu.Lock()
		var victim *SSHPoolMember
		for i := len(t.sshPool) - 1; i >= 0; i-- {
			m := t.sshPool[i]
			if m != nil && m.State != sshMemberEvicted && atomic.LoadInt64(&m.ActiveChannels) == 0 {
				victim = m
				break
			}
		}
		t.sshPoolMu.Unlock()
		if victim == nil {
			// No idle member to evict, stop
			return
		}
		t.evictMember(victim, fmt.Sprintf("pool shrink to %d", target))
	}
}

func (t *Tunnel) pickSSHMember() *SSHPoolMember {
	return t.pickSSHMemberExcluding(nil)
}

func (t *Tunnel) pickSSHMemberExcluding(exclude map[uint64]bool) *SSHPoolMember {
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()

	healthyCandidates := make([]*SSHPoolMember, 0, len(t.sshPool))
	suspectCandidates := make([]*SSHPoolMember, 0, len(t.sshPool))
	now := time.Now()
	for _, member := range t.sshPool {
		if member == nil || member.Client == nil {
			continue
		}
		if member.State == sshMemberSuspect && !member.TimeoutWindowAt.IsZero() && now.Sub(member.TimeoutWindowAt) > t.configuredSuspectCooldown() {
			member.State = sshMemberHealthy
			member.TimeoutBurst = 0
			member.ProbeFailures = 0
			member.TimeoutWindowAt = time.Time{}
			member.timeoutTargets = nil
			member.LastError = ""
		}
		if exclude != nil && exclude[member.ID] {
			continue
		}
		switch member.State {
		case sshMemberHealthy:
			healthyCandidates = append(healthyCandidates, member)
		case sshMemberSuspect:
			suspectCandidates = append(suspectCandidates, member)
		}
	}
	candidates := healthyCandidates
	if len(candidates) == 0 {
		candidates = suspectCandidates
	}
	if len(candidates) == 0 {
		return nil
	}
	switch t.configuredBalanceStrategy() {
	case sshPoolBalanceRoundRobin:
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].LastUsedAt.Equal(candidates[j].LastUsedAt) {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].LastUsedAt.Before(candidates[j].LastUsedAt)
		})
		return candidates[0]
	case sshPoolBalanceRandom:
		return candidates[rand.Intn(len(candidates))]
	default:
		sort.Slice(candidates, func(i, j int) bool {
			leftActive := atomic.LoadInt64(&candidates[i].ActiveChannels)
			rightActive := atomic.LoadInt64(&candidates[j].ActiveChannels)
			if leftActive != rightActive {
				return leftActive < rightActive
			}
			if candidates[i].LastUsedAt.Equal(candidates[j].LastUsedAt) {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].LastUsedAt.Before(candidates[j].LastUsedAt)
		})
		return candidates[0]
	}
}

func (t *Tunnel) findMemberByClient(expected *ssh.Client) *SSHPoolMember {
	if expected == nil {
		return nil
	}
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	for _, member := range t.sshPool {
		if member != nil && member.Client == expected && member.State != sshMemberEvicted {
			return member
		}
	}
	return nil
}

func (t *Tunnel) evictMember(member *SSHPoolMember, reason string) bool {
	if member == nil {
		return false
	}
	t.sshPoolMu.Lock()
	if member.State == sshMemberEvicted {
		t.sshPoolMu.Unlock()
		return false
	}
	client := member.Client
	member.Client = nil
	member.State = sshMemberEvicted
	member.LastError = reason
	member.LastHardFailureAt = time.Now()
	for i, current := range t.sshPool {
		if current == member {
			t.sshPool = append(t.sshPool[:i], t.sshPool[i+1:]...)
			break
		}
	}
	t.appendEvictedMemberLocked(member)
	t.sshPoolMu.Unlock()
	if client != nil {
		closeSSHClient(client)
	}
	t.markReconnectRecoveryNeeded()
	t.resetExitIPInfo()
	if reason != "" {
		log.Printf("SSH pool member evicted(id=%d,generation=%d): %s", member.ID, member.Generation, reason)
	}
	return true
}

func (t *Tunnel) appendEvictedMemberLocked(member *SSHPoolMember) {
	if member == nil {
		return
	}
	for _, current := range t.sshPoolEvicted {
		if current == member {
			return
		}
	}
	t.sshPoolEvicted = append(t.sshPoolEvicted, member)
	if len(t.sshPoolEvicted) > maxEvictedPoolHistory {
		t.sshPoolEvicted = t.sshPoolEvicted[len(t.sshPoolEvicted)-maxEvictedPoolHistory:]
	}
}

func (t *Tunnel) DisconnectSSHPool(reason string) {
	t.sshPoolMu.Lock()
	members := append([]*SSHPoolMember(nil), t.sshPool...)
	t.sshPool = nil
	now := time.Now()
	clients := make([]*ssh.Client, 0, len(members))
	for _, member := range members {
		if member == nil || member.State == sshMemberEvicted {
			continue
		}
		if member.Client != nil {
			clients = append(clients, member.Client)
			member.Client = nil
		}
		member.State = sshMemberEvicted
		member.LastError = reason
		member.LastHardFailureAt = now
		t.appendEvictedMemberLocked(member)
	}
	t.sshPoolMu.Unlock()

	for _, client := range clients {
		closeSSHClient(client)
	}
	if len(members) > 0 {
		t.markReconnectRecoveryNeeded()
	}
	t.resetExitIPInfo()
	if reason != "" {
		log.Printf("SSH pool disconnected(count=%d): %s", len(members), reason)
	}
}

func (t *Tunnel) markReconnectRecoveryNeeded() {
	t.reconnectMutex.Lock()
	if t.sshConnectedOnce {
		t.reconnectRecoveryPending = true
	}
	t.reconnectMutex.Unlock()
}

func (t *Tunnel) invalidateSSHClientIfMatch(expected *ssh.Client, reason string) bool {
	member := t.findMemberByClient(expected)
	if member == nil {
		return false
	}
	return t.evictMember(member, reason)
}

func (t *Tunnel) invalidateSSHClient(reason string) {
	member := t.pickSSHMember()
	if member != nil {
		t.evictMember(member, reason)
	}
}

func (t *Tunnel) HasHealthySSHMemberAfter(since time.Time) bool {
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	for _, member := range t.sshPool {
		if member == nil || member.Client == nil || member.State != sshMemberHealthy {
			continue
		}
		if since.IsZero() || member.CreatedAt.After(since) || member.CreatedAt.Equal(since) {
			return true
		}
	}
	return false
}

func (t *Tunnel) currentSSHClient() *ssh.Client {
	member := t.pickSSHMember()
	if member == nil {
		return nil
	}
	return member.Client
}

func (t *Tunnel) currentSSHGeneration() uint64 {
	member := t.pickSSHMember()
	if member == nil {
		return 0
	}
	return member.Generation
}

func (t *Tunnel) GetSSHClient() *ssh.Client {
	member := t.pickSSHMember()
	if member == nil {
		return nil
	}
	return member.Client
}

func (t *Tunnel) PeekSSHClient() *ssh.Client {
	return t.GetSSHClient()
}

func (t *Tunnel) ensurePool(ctx context.Context, source string) {
	if !(t.enableSocks5 || t.enableHttpOverSSH) {
		return
	}
	target := t.configuredPoolSize()
	for t.currentPoolSize() < target {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		t.ReconnectSSHWithSource(ctx, source)
		if t.currentPoolSize() >= target {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(t.configuredReplenishInterval()):
		}
	}
}

func (t *Tunnel) configuredProbeURL() string {
	if strings.TrimSpace(t.sshProbeURL) == "" {
		return ipInfoEndpoint
	}
	return strings.TrimSpace(t.sshProbeURL)
}

func (t *Tunnel) configuredProbeURLs() []string {
	raw := strings.TrimSpace(t.sshProbeURLs)
	if raw == "" {
		raw = strings.TrimSpace(t.sshProbeURL)
	}
	if raw == "" {
		raw = defaultSSHProbeURLs
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if url := strings.TrimSpace(part); url != "" {
			out = append(out, url)
		}
	}
	if len(out) == 0 {
		out = append(out, ipInfoEndpoint)
	}
	return out
}

func (t *Tunnel) probeSSHMemberURL(ctx context.Context, httpClient *http.Client, probeURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("User-Agent", "ssh-tunnel/pool-probe")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return errors.New(resp.Status)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

func (t *Tunnel) probeSSHMember(ctx context.Context, member *SSHPoolMember) error {
	if member == nil || member.Client == nil {
		return errors.New("ssh member is not connected")
	}
	httpClient, err := t.sshHTTPClientForClient(member.Client, t.configuredProbeTimeout())
	if err != nil {
		return err
	}
	if transport, ok := httpClient.Transport.(*http.Transport); ok {
		defer transport.CloseIdleConnections()
	}
	probeURLs := t.configuredProbeURLs()
	type probeResult struct {
		url string
		err error
	}
	resultCh := make(chan probeResult, len(probeURLs))
	probeCtx, cancel := context.WithTimeout(ctx, t.configuredProbeTimeout())
	defer cancel()
	for _, probeURL := range probeURLs {
		url := probeURL
		go func() {
			resultCh <- probeResult{url: url, err: t.probeSSHMemberURL(probeCtx, httpClient, url)}
		}()
	}
	var failures []string
	for range probeURLs {
		result := <-resultCh
		if result.err == nil {
			return nil
		}
		failures = append(failures, result.url+": "+result.err.Error())
	}
	return errors.New("all probe urls failed: " + strings.Join(failures, "; "))
}

func (t *Tunnel) markMemberProbe(member *SSHPoolMember, result string, err error) {
	if member == nil {
		return
	}
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	member.LastProbeAt = time.Now()
	member.LastProbeResult = result
	if err != nil {
		member.LastError = err.Error()
	}
}

func (t *Tunnel) maybeProbeMember(ctx context.Context, member *SSHPoolMember) bool {
	if member == nil || member.Client == nil {
		return false
	}
	t.sshPoolMu.Lock()
	if member.State == sshMemberProbing || member.State == sshMemberReconnecting || member.State == sshMemberEvicted {
		t.sshPoolMu.Unlock()
		return false
	}
	member.State = sshMemberProbing
	t.sshPoolMu.Unlock()

	err := t.probeSSHMember(ctx, member)
	if err == nil {
		t.sshPoolMu.Lock()
		if member.State != sshMemberEvicted {
			member.State = sshMemberHealthy
			member.TimeoutBurst = 0
			member.ProbeFailures = 0
			member.TimeoutWindowAt = time.Time{}
			member.timeoutTargets = nil
			member.LastError = ""
		}
		t.sshPoolMu.Unlock()
		t.markMemberProbe(member, "ok", nil)
		return true
	}

	t.markMemberProbe(member, "failed", err)
	if isSSHReconnectError(err) {
		t.evictMember(member, "probe failed: "+err.Error())
		return false
	}

	t.sshPoolMu.Lock()
	member.ProbeFailures++
	shouldEvict := member.ProbeFailures >= t.configuredProbeFailureThreshold()
	if member.State != sshMemberEvicted {
		member.State = sshMemberSuspect
	}
	t.sshPoolMu.Unlock()
	if shouldEvict {
		t.evictMember(member, "probe threshold exceeded: "+err.Error())
		return false
	}
	return false
}

// ClearEvictedMembers removes all entries from the evicted member history.
// Returns the number of entries cleared.
func (t *Tunnel) ClearEvictedMembers() int {
	t.sshPoolMu.Lock()
	defer t.sshPoolMu.Unlock()
	count := len(t.sshPoolEvicted)
	t.sshPoolEvicted = nil
	return count
}

// EvictMemberByID finds an active pool member by ID and evicts it.
// Returns true if the member was found and evicted.
func (t *Tunnel) EvictMemberByID(memberID uint64) bool {
	t.sshPoolMu.Lock()
	var target *SSHPoolMember
	for _, m := range t.sshPool {
		if m != nil && m.ID == memberID && m.State != sshMemberEvicted {
			target = m
			break
		}
	}
	t.sshPoolMu.Unlock()
	if target == nil {
		return false
	}
	return t.evictMember(target, "manual evict by admin")
}

// ProbeMemberByID finds a pool member by ID and triggers a probe.
// Returns (found, probeErr).
func (t *Tunnel) ProbeMemberByID(ctx context.Context, memberID uint64) (bool, error) {
	t.sshPoolMu.Lock()
	var target *SSHPoolMember
	for _, m := range t.sshPool {
		if m != nil && m.ID == memberID {
			target = m
			break
		}
	}
	t.sshPoolMu.Unlock()
	if target == nil {
		return false, nil
	}
	ok := t.maybeProbeMember(ctx, target)
	if !ok {
		// Check if probing failed
		t.sshPoolMu.Lock()
		lastErr := target.LastError
		t.sshPoolMu.Unlock()
		if lastErr != "" {
			return true, fmt.Errorf("%s", lastErr)
		}
	}
	return true, nil
}

func (t *Tunnel) recordMemberDialSuccess(member *SSHPoolMember) {
	t.recordSSHDialSuccess()
	if member == nil {
		return
	}
	t.sshPoolMu.Lock()
	if member.State != sshMemberEvicted {
		member.State = sshMemberHealthy
		member.TimeoutBurst = 0
		member.ProbeFailures = 0
		member.TimeoutWindowAt = time.Time{}
		member.timeoutTargets = nil
		member.LastError = ""
	}
	t.sshPoolMu.Unlock()
}

func (t *Tunnel) distinctTimeoutTargetCount(member *SSHPoolMember, now time.Time) int {
	if member == nil {
		return 0
	}
	pruned := make(map[string]time.Time, len(member.timeoutTargets))
	for host, ts := range member.timeoutTargets {
		if now.Sub(ts) <= sshDialTimeoutWindow {
			pruned[host] = ts
		}
	}
	member.timeoutTargets = pruned
	return len(member.timeoutTargets)
}

func (t *Tunnel) recordMemberDialFailure(ctx context.Context, member *SSHPoolMember, target string, err error) (string, bool) {
	class := t.classifyDialError(err, true)
	t.recordSSHDialFailure(err)
	if member == nil {
		return class, class == failureClassSSHTransportDead
	}
	if class == failureClassSSHTransportDead {
		t.evictMember(member, err.Error())
		return class, true
	}
	if class != failureClassSSHChannelTimeout {
		t.sshPoolMu.Lock()
		member.LastError = err.Error()
		t.sshPoolMu.Unlock()
		return class, false
	}

	t.sshPoolMu.Lock()
	now := time.Now()
	if member.TimeoutWindowAt.IsZero() || now.Sub(member.TimeoutWindowAt) > sshDialTimeoutWindow {
		member.TimeoutWindowAt = now
		member.TimeoutBurst = 1
		member.timeoutTargets = map[string]time.Time{}
	} else {
		member.TimeoutBurst++
	}
	if member.timeoutTargets == nil {
		member.timeoutTargets = make(map[string]time.Time)
	}
	member.timeoutTargets[target] = now
	member.LastError = err.Error()
	member.State = sshMemberSuspect
	distinctTargets := t.distinctTimeoutTargetCount(member, now)
	timeoutBurst := member.TimeoutBurst
	t.sshPoolMu.Unlock()

	if timeoutBurst >= sshDialTimeoutLimit && distinctTargets >= 2 {
		if t.maybeProbeMember(ctx, member) {
			return class, false
		}
		t.sshPoolMu.Lock()
		evicted := member.State == sshMemberEvicted
		t.sshPoolMu.Unlock()
		return class, evicted
	}
	return class, false
}

func (t *Tunnel) dialSSHConn(ctx context.Context, target string, retryState *requestRetryState) (net.Conn, *SSHPoolMember, string, bool, SSHRetryInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if retryState == nil {
		retryState = t.newRequestRetryState()
	}

	timeout := t.sshDestTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	for retryState.canAttempt() {
		member := t.pickSSHMemberExcluding(retryState.exclude)
		if member == nil || member.Client == nil {
			if retryState.lastErr != nil {
				return nil, retryState.lastMember, retryState.lastClass, retryState.reconnectTriggered, retryState.info(), retryState.lastErr
			}
			retryState.recordFailure(nil, failureClassSSHTransportDead, true, SSHReconnectRequired)
			return nil, nil, failureClassSSHTransportDead, true, retryState.info(), SSHReconnectRequired
		}
		retryState.recordAttempt(member)

		timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
		conn, err := member.Client.DialContext(timeoutCtx, "tcp", target)
		cancel()
		if err == nil {
			t.acquireMemberChannel(member)
			t.recordMemberDialSuccess(member)
			return &pooledConn{Conn: conn, member: member}, member, "", false, retryState.info(), nil
		}

		lastClass, reconnectTriggered := t.recordMemberDialFailure(ctx, member, target, err)
		retryState.recordFailure(member, lastClass, reconnectTriggered, err)
		if reconnectTriggered {
			continue
		}
		// Dial has not proxied user payload yet, so it is safe to try another member.
		if lastClass == failureClassClientClosed {
			break
		}
	}
	return nil, retryState.lastMember, retryState.lastClass, retryState.reconnectTriggered, retryState.info(), retryState.lastErr
}
