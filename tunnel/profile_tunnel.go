package tunnel

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ProfileTunnelManager manages per-profile Tunnel instances for domain-based routing.
// Each profile with DomainRoutes gets its own SSH connection pool.
type ProfileTunnelManager struct {
	mu      sync.RWMutex
	tunnels map[string]*profileTunnelEntry
	matcher *RouteMatcher
}

type profileTunnelEntry struct {
	tunnel  *Tunnel
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	profile cfg.SSHProfile
}

func NewProfileTunnelManager(matcher *RouteMatcher) *ProfileTunnelManager {
	return &ProfileTunnelManager{
		tunnels: make(map[string]*profileTunnelEntry),
		matcher: matcher,
	}
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
func (ptm *ProfileTunnelManager) ReloadProfiles(parentCtx context.Context, profiles map[string]cfg.SSHProfile, activeProfileID string) {
	ptm.mu.Lock()

	// Determine which profiles need tunnels (have DomainRoutes and are not the active profile)
	wanted := make(map[string]cfg.SSHProfile)
	for id, profile := range profiles {
		if len(profile.DomainRoutes) > 0 && id != activeProfileID {
			wanted[id] = profile
		}
	}

	// Stop tunnels that are no longer needed
	for id, entry := range ptm.tunnels {
		if _, ok := wanted[id]; !ok {
			ptm.stopEntryLocked(id, entry)
		}
	}
	ptm.mu.Unlock()

	// Start new tunnels or update existing ones
	for id, profile := range wanted {
		ptm.mu.RLock()
		entry, exists := ptm.tunnels[id]
		ptm.mu.RUnlock()
		if !exists {
			if err := ptm.StartProfileTunnel(parentCtx, id, profile); err != nil {
				log.Printf("启动Profile隧道失败(profileId=%s): %v", id, err)
			}
		} else {
			// Update pool size if changed
			newSize := profile.SSHPoolSize
			if newSize <= 0 {
				newSize = 2
			}
			oldSize := entry.profile.SSHPoolSize
			if oldSize <= 0 {
				oldSize = 2
			}
			if newSize != oldSize {
				entry.tunnel.sshPoolSize = newSize
				ptm.mu.Lock()
				entry.profile = profile
				ptm.mu.Unlock()
				log.Printf("已更新Profile隧道池大小(profileId=%s, %d -> %d)", id, oldSize, newSize)
				if newSize < oldSize {
					entry.tunnel.shrinkPoolTo(newSize)
				}
			}
		}
	}

	// Update route matcher
	if ptm.matcher != nil {
		ptm.matcher.LoadRoutes(profiles, activeProfileID)
	}
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

// newProfileTunnel creates a Tunnel configured for a specific profile.
// It only sets up SSH connectivity — no proxy listeners.
func newProfileTunnel(profileID string, profile cfg.SSHProfile) (*Tunnel, error) {
	t := &Tunnel{}
	t.profileID = profileID
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

	conn, client, generation, memberID, retryInfo, err := t.createSSHConn(host, retryState)
	if err != nil {
		return destinationConn{}, err
	}
	return destinationConn{
		conn:        conn,
		sshClient:   client,
		sshMemberID: memberID,
		profileID:   profileID,
		viaSSH:      true,
		generation:  generation,
		retryInfo:   retryInfo,
	}, nil
}
