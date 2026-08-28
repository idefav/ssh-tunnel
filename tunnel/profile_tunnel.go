package tunnel

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ProfileTunnelManager manages per-profile Tunnel instances for standalone routing rules.
type ProfileTunnelManager struct {
	mu         sync.RWMutex
	tunnels    map[string]*profileTunnelEntry
	identities map[string]string
	matcher    *RouteMatcher
	store      *TrafficStore
}

type profileTunnelEntry struct {
	tunnel  *Tunnel
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	profile cfg.SSHProfile
}

func NewProfileTunnelManager(matcher *RouteMatcher, stores ...*TrafficStore) *ProfileTunnelManager {
	manager := &ProfileTunnelManager{
		tunnels:    make(map[string]*profileTunnelEntry),
		identities: make(map[string]string),
		matcher:    matcher,
	}
	if len(stores) > 0 {
		manager.store = stores[0]
	}
	return manager
}

// StartProfileTunnel creates and starts a Tunnel for the given profile.
// The tunnel only maintains an SSH pool (no SOCKS5/HTTP listener).
func (ptm *ProfileTunnelManager) StartProfileTunnel(parentCtx context.Context, profileID string, profile cfg.SSHProfile) error {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	// Stop existing tunnel for this profile if any
	if entry, ok := ptm.tunnels[profileID]; ok {
		ptm.stopEntryLocked(profileID, entry)
	}

	t, err := newProfileTunnel(profileID, profile)
	if err != nil {
		return fmt.Errorf("failed to create tunnel for profile %s: %w", profileID, err)
	}
	t.trafficStore = ptm.store

	ctx, cancel := context.WithCancel(parentCtx)
	t.SetTunnelContext(ctx)

	entry := &profileTunnelEntry{
		tunnel:  t,
		cancel:  cancel,
		profile: profile,
	}

	// Start pool maintenance loop
	entry.wg.Add(1)
	safe.GO(func() {
		defer entry.wg.Done()
		for {
			if ctx.Err() != nil {
				return
			}
			t.ensurePool(ctx, fmt.Sprintf("profile-%s", profileID))
			select {
			case <-ctx.Done():
				return
			case <-time.After(t.configuredReplenishInterval()):
			}
		}
	})

	ptm.tunnels[profileID] = entry
	log.Printf("已启动Profile隧道(profileId=%s, server=%s, poolSize=%d)", profileID, t.serverAddress, t.sshPoolSize)
	return nil
}

// StopProfileTunnel stops the tunnel for the given profile and cleans up.
func (ptm *ProfileTunnelManager) StopProfileTunnel(profileID string) {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()

	if entry, ok := ptm.tunnels[profileID]; ok {
		ptm.stopEntryLocked(profileID, entry)
	}
}

func (ptm *ProfileTunnelManager) stopEntryLocked(profileID string, entry *profileTunnelEntry) {
	entry.cancel()
	entry.tunnel.DisconnectSSHPool(fmt.Sprintf("profile tunnel stopped: %s", profileID))
	entry.wg.Wait()
	delete(ptm.tunnels, profileID)
	log.Printf("已停止Profile隧道(profileId=%s)", profileID)
}

// GetTunnel returns the Tunnel for the given profile, or nil.
func (ptm *ProfileTunnelManager) GetTunnel(profileID string) *Tunnel {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	if entry, ok := ptm.tunnels[profileID]; ok {
		return entry.tunnel
	}
	return nil
}

// StopAll stops all profile tunnels.
func (ptm *ProfileTunnelManager) StopAll() {
	ptm.mu.Lock()
	defer ptm.mu.Unlock()
	for profileID, entry := range ptm.tunnels {
		ptm.stopEntryLocked(profileID, entry)
	}
}

// ReloadProfiles diffs current running tunnels against the new profile set,
// starting/stopping tunnels as needed.
func (ptm *ProfileTunnelManager) ReloadProfiles(parentCtx context.Context, profiles map[string]cfg.SSHProfile, activeProfileID string, rules []cfg.EffectiveRoute) {
	ptm.mu.Lock()
	ptm.identities = make(map[string]string, len(profiles))
	for id, profile := range profiles {
		ptm.identities[id] = cfg.SSHProfileConnectionFingerprint(profile)
	}

	targeted := make(map[string]bool)
	for _, rule := range rules {
		for _, target := range rule.TargetProfileIDs {
			targeted[target] = true
		}
	}
	wanted := make(map[string]cfg.SSHProfile)
	for id, profile := range profiles {
		if targeted[id] && id != activeProfileID {
			wanted[id] = profile
		}
	}

	for id, entry := range ptm.tunnels {
		profile, ok := wanted[id]
		if !ok || !sameProfileConnection(entry.profile, profile) {
			ptm.stopEntryLocked(id, entry)
		}
	}
	ptm.mu.Unlock()

	for id, profile := range wanted {
		ptm.mu.RLock()
		_, exists := ptm.tunnels[id]
		ptm.mu.RUnlock()
		if !exists {
			if err := ptm.StartProfileTunnel(parentCtx, id, profile); err != nil {
				log.Printf("启动Profile隧道失败(profileId=%s): %v", id, err)
			}
		}
	}

	if ptm.matcher != nil {
		ptm.matcher.LoadRoutes(rules)
	}
}

func (ptm *ProfileTunnelManager) ProfileIdentity(profileID string) string {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	return ptm.identities[profileID]
}

func sameProfileConnection(left, right cfg.SSHProfile) bool {
	return left.ServerIp == right.ServerIp &&
		left.ServerSshPort == right.ServerSshPort &&
		left.LoginUser == right.LoginUser &&
		left.SshPrivateKeyPath == right.SshPrivateKeyPath &&
		left.RetryIntervalSec == right.RetryIntervalSec &&
		normalizedProfilePoolSize(left.SSHPoolSize) == normalizedProfilePoolSize(right.SSHPoolSize)
}

func normalizedProfilePoolSize(size int) int {
	if size <= 0 {
		return 2
	}
	return size
}

// ActiveProfileIDs returns the list of currently running profile tunnel IDs.
func (ptm *ProfileTunnelManager) ActiveProfileIDs() []string {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	ids := make([]string, 0, len(ptm.tunnels))
	for id := range ptm.tunnels {
		ids = append(ids, id)
	}
	return ids
}

// SnapshotAllPools returns pool snapshots from all profile tunnels, tagged with profileID.
func (ptm *ProfileTunnelManager) SnapshotAllPools() []SSHPoolSnapshot {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	var all []SSHPoolSnapshot
	for _, entry := range ptm.tunnels {
		snapshots := entry.tunnel.snapshotPoolMembers()
		all = append(all, snapshots...)
	}
	return all
}

// ProbeMemberByID searches all profile tunnels for a member with the given ID and probes it.
func (ptm *ProfileTunnelManager) ProbeMemberByID(ctx context.Context, memberID uint64) (bool, error) {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	for _, entry := range ptm.tunnels {
		found, err := entry.tunnel.ProbeMemberByID(ctx, memberID)
		if found {
			return true, err
		}
	}
	return false, nil
}

// ClearEvictedMembers removes all evicted member history from all profile tunnels.
// Returns the total number of entries cleared.
func (ptm *ProfileTunnelManager) ClearEvictedMembers() int {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	total := 0
	for _, entry := range ptm.tunnels {
		total += entry.tunnel.ClearEvictedMembers()
	}
	return total
}

// EvictMemberByID searches all profile tunnels for an active member with the given ID and evicts it.
// Returns true if the member was found and evicted.
func (ptm *ProfileTunnelManager) EvictMemberByID(memberID uint64) bool {
	ptm.mu.RLock()
	defer ptm.mu.RUnlock()
	for _, entry := range ptm.tunnels {
		if entry.tunnel.EvictMemberByID(memberID) {
			return true
		}
	}
	return false
}

// newProfileTunnel creates a Tunnel configured for a specific profile.
// It only sets up SSH connectivity — no proxy listeners.
func newProfileTunnel(profileID string, profile cfg.SSHProfile) (*Tunnel, error) {
	t := &Tunnel{}
	t.profileID = profileID
	t.profileIdentity = cfg.SSHProfileConnectionFingerprint(profile)
	t.enableHttpOverSSH = true // Profile tunnels are always SSH-capable
	t.serverAddress = profile.ServerIp + ":" + strconv.Itoa(profile.ServerSshPort)
	t.user = profile.LoginUser

	// Pool size: use profile-specific setting, fallback to 2
	t.sshPoolSize = profile.SSHPoolSize
	if t.sshPoolSize <= 0 {
		t.sshPoolSize = 2
	}

	// Set sensible defaults for SSH-related timeouts
	t.sshDialTimeout = 5 * time.Second
	t.sshDestTimeout = 3 * time.Second
	t.keepAlive = KeepAliveConfig{Interval: 2, CountMax: 2}
	t.retryInterval = time.Duration(profile.RetryIntervalSec) * time.Second
	if t.retryInterval <= 0 {
		t.retryInterval = 3 * time.Second
	}
	t.reconnectMaxRetries = 20
	t.reconnectMaxInterval = 5 * time.Second
	t.sshPoolReplenishInterval = 1 * time.Second
	t.sshPoolBalanceStrategy = sshPoolBalanceLeastActive
	t.sshProbeURLs = defaultSSHProbeURLs
	t.sshProbeTimeout = 3 * time.Second
	t.sshProbeFailureThreshold = 2
	t.sshSuspectCooldown = 10 * time.Second
	t.hostKeys = ssh.InsecureIgnoreHostKey()

	// Read SSH key
	b, err := ioutil.ReadFile(profile.SshPrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("读取SSH私钥失败(profile=%s): %w", profileID, err)
	}
	k, err := ssh.ParsePrivateKey(b)
	if err != nil {
		return nil, fmt.Errorf("解析SSH私钥失败(profile=%s): %w", profileID, err)
	}
	t.auth = []ssh.AuthMethod{ssh.PublicKeys(k)}

	return t, nil
}

// CreateSSHConnForProfile creates an SSH connection through the profile's tunnel.
// This is the entry point used by getDestConn for routed requests.
func (ptm *ProfileTunnelManager) CreateSSHConnForProfile(profileID string, host string, retryState *requestRetryState) (destinationConn, error) {
	return ptm.createSSHConnForProfile(profileID, host, retryState, false)
}

func (ptm *ProfileTunnelManager) createSSHConnForProfileUntracked(profileID string, host string, retryState *requestRetryState) (destinationConn, error) {
	return ptm.createSSHConnForProfile(profileID, host, retryState, true)
}

func (ptm *ProfileTunnelManager) createSSHConnForProfile(profileID string, host string, retryState *requestRetryState, untracked bool) (destinationConn, error) {
	ptm.mu.RLock()
	entry, ok := ptm.tunnels[profileID]
	ptm.mu.RUnlock()

	if !ok || entry == nil || entry.tunnel == nil {
		return destinationConn{}, fmt.Errorf("profile tunnel not available: %s", profileID)
	}

	t := entry.tunnel
	if retryState == nil {
		retryState = t.newRequestRetryState()
	}

	var conn net.Conn
	var client *ssh.Client
	var generation, memberID uint64
	var retryInfo SSHRetryInfo
	var err error
	if untracked {
		conn, client, generation, memberID, retryInfo, err = t.createSSHConnUntracked(host, retryState)
	} else {
		conn, client, generation, memberID, retryInfo, err = t.createSSHConn(host, retryState)
	}
	dest := destinationConn{
		conn:        conn,
		sshClient:   client,
		sshMemberID: memberID,
		profileID:   profileID,
		viaSSH:      true,
		generation:  generation,
		retryInfo:   retryInfo,
	}
	return dest, err
}
