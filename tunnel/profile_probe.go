package tunnel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var ErrProfileTestRunning = errors.New("已有Profile测试批次正在运行")

const (
	profileTestConcurrency = 3
	profileTestHistorySize = 32
)

type ProfileTestBatch struct {
	TestID    string                             `json:"testId"`
	Status    string                             `json:"status"`
	StartedAt time.Time                          `json:"startedAt"`
	UpdatedAt time.Time                          `json:"updatedAt"`
	Total     int                                `json:"total"`
	Completed int                                `json:"completed"`
	Results   map[string]ProfileManualTestResult `json:"results"`
}

type ProfileTestManager struct {
	mu      sync.Mutex
	tunnel  *Tunnel
	latest  *ProfileTestBatch
	batches map[string]*ProfileTestBatch
	testFn  func(context.Context, string, cfg.SSHProfile) ProfileManualTestResult
	dialFn  profileTestDialer
	probeFn profileTestProber
}

type profileTestConnection interface {
	Close() error
}

type profileTestDialer func(context.Context, string, cfg.SSHProfile) (profileTestConnection, time.Duration, error)
type profileTestProber func(context.Context, profileTestConnection) (string, time.Duration, error)

type sshProfileTestConnection struct {
	client *ssh.Client
	tunnel *Tunnel
}

func (c *sshProfileTestConnection) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	return c.client.Close()
}

func NewProfileTestManager(tunnel *Tunnel) *ProfileTestManager {
	manager := &ProfileTestManager{tunnel: tunnel, batches: make(map[string]*ProfileTestBatch)}
	manager.testFn = manager.runProfileTest
	manager.dialFn = manager.dialProfile
	manager.probeFn = manager.probeProfile
	return manager
}

func (t *Tunnel) GetProfileTestManager() *ProfileTestManager {
	t.profileTestOnce.Do(func() {
		t.profileTestMgr = NewProfileTestManager(t)
	})
	return t.profileTestMgr
}

func cloneProfileTestBatch(source *ProfileTestBatch) ProfileTestBatch {
	if source == nil {
		return ProfileTestBatch{}
	}
	result := *source
	result.Results = make(map[string]ProfileManualTestResult, len(source.Results))
	for profileID, item := range source.Results {
		result.Results[profileID] = item
	}
	return result
}

func (m *ProfileTestManager) Start(profiles map[string]cfg.SSHProfile) (ProfileTestBatch, error) {
	if len(profiles) == 0 {
		return ProfileTestBatch{}, fmt.Errorf("profileIds不能为空")
	}
	m.mu.Lock()
	if m.latest != nil && m.latest.Status == "RUNNING" {
		m.mu.Unlock()
		return ProfileTestBatch{}, ErrProfileTestRunning
	}
	now := time.Now()
	batch := &ProfileTestBatch{
		TestID:    fmt.Sprintf("pt_%d", now.UnixNano()),
		Status:    "RUNNING",
		StartedAt: now,
		UpdatedAt: now,
		Total:     len(profiles),
		Results:   make(map[string]ProfileManualTestResult, len(profiles)),
	}
	for profileID := range profiles {
		batch.Results[profileID] = ProfileManualTestResult{ProfileID: profileID, Status: "pending"}
	}
	m.latest = batch
	m.batches[batch.TestID] = batch
	if len(m.batches) > profileTestHistorySize {
		var oldestID string
		var oldestAt time.Time
		for testID, previous := range m.batches {
			if testID == batch.TestID || previous.Status == "RUNNING" {
				continue
			}
			if oldestID == "" || previous.StartedAt.Before(oldestAt) {
				oldestID = testID
				oldestAt = previous.StartedAt
			}
		}
		if oldestID != "" {
			delete(m.batches, oldestID)
		}
	}
	snapshot := cloneProfileTestBatch(batch)
	m.mu.Unlock()

	profileCopy := make(map[string]cfg.SSHProfile, len(profiles))
	for profileID, profile := range profiles {
		profileCopy[profileID] = profile
	}
	safe.GO(func() { m.runBatch(batch.TestID, profileCopy) })
	return snapshot, nil
}

func (m *ProfileTestManager) runBatch(testID string, profiles map[string]cfg.SSHProfile) {
	profileIDs := make([]string, 0, len(profiles))
	for profileID := range profiles {
		profileIDs = append(profileIDs, profileID)
	}
	sort.Strings(profileIDs)
	semaphore := make(chan struct{}, profileTestConcurrency)
	var workers sync.WaitGroup
	for _, profileID := range profileIDs {
		profileID := profileID
		profile := profiles[profileID]
		workers.Add(1)
		safe.GO(func() {
			defer workers.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			m.markRunning(testID, profileID)
			ctx, cancel := context.WithTimeout(context.Background(), m.testTimeout())
			result := m.runProfileTestSafely(ctx, profileID, profile)
			cancel()
			m.finishProfile(testID, profile, result)
		})
	}
	workers.Wait()
	m.mu.Lock()
	if m.latest != nil && m.latest.TestID == testID {
		m.latest.Status = "COMPLETED"
		m.latest.UpdatedAt = time.Now()
	}
	m.mu.Unlock()
}

func (m *ProfileTestManager) runProfileTestSafely(ctx context.Context, profileID string, profile cfg.SSHProfile) (result ProfileManualTestResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = failedProfileTest(ProfileManualTestResult{ProfileID: profileID, Status: "running", StartedAt: time.Now()}, fmt.Errorf("测试异常: %v", recovered))
		}
		result.ProfileID = profileID
	}()
	return m.testFn(ctx, profileID, profile)
}

func (m *ProfileTestManager) testTimeout() time.Duration {
	dialTimeout := 5 * time.Second
	probeTimeout := 3 * time.Second
	if m.tunnel != nil {
		if m.tunnel.sshDialTimeout > 0 {
			dialTimeout = m.tunnel.sshDialTimeout
		}
		if m.tunnel.configuredProbeTimeout() > 0 {
			probeTimeout = m.tunnel.configuredProbeTimeout()
		}
	}
	return dialTimeout + probeTimeout + 2*time.Second
}

func (m *ProfileTestManager) markRunning(testID, profileID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.latest == nil || m.latest.TestID != testID {
		return
	}
	result := m.latest.Results[profileID]
	result.Status = "running"
	result.StartedAt = time.Now()
	m.latest.Results[profileID] = result
	m.latest.UpdatedAt = time.Now()
}

func (m *ProfileTestManager) finishProfile(testID string, profile cfg.SSHProfile, result ProfileManualTestResult) {
	m.mu.Lock()
	if m.latest == nil || m.latest.TestID != testID {
		m.mu.Unlock()
		return
	}
	m.latest.Results[result.ProfileID] = result
	m.latest.Completed++
	m.latest.UpdatedAt = time.Now()
	m.mu.Unlock()
	if m.tunnel != nil && m.tunnel.trafficStore != nil {
		m.tunnel.trafficStore.RecordProfileManualTest(cfg.SSHProfileConnectionFingerprint(profile), result)
	}
}

func (m *ProfileTestManager) runProfileTest(ctx context.Context, profileID string, profile cfg.SSHProfile) ProfileManualTestResult {
	result := ProfileManualTestResult{ProfileID: profileID, Status: "running", StartedAt: time.Now()}
	client, handshakeDuration, err := m.dialFn(ctx, profileID, profile)
	result.SSHHandshakeLatencyMs = handshakeDuration.Milliseconds()
	if err != nil {
		return failedProfileTest(result, fmt.Errorf("SSH握手失败: %w", err))
	}
	if client == nil {
		return failedProfileTest(result, errors.New("SSH握手失败: 未返回连接"))
	}
	defer func() { _ = client.Close() }()
	probeTarget, probeDuration, err := m.probeFn(ctx, client)
	result.EgressProbeLatencyMs = probeDuration.Milliseconds()
	result.ProbeTarget = probeTarget
	if err != nil {
		return failedProfileTest(result, fmt.Errorf("出口探测失败: %w", err))
	}
	result.Status = "completed"
	result.Reachable = true
	result.CompletedAt = time.Now()
	return result
}

func (m *ProfileTestManager) dialProfile(ctx context.Context, profileID string, profile cfg.SSHProfile) (profileTestConnection, time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	probeTunnel, err := newProfileTunnel(profileID, profile)
	if err != nil {
		return nil, 0, err
	}
	if m.tunnel != nil {
		probeTunnel.sshDialTimeout = m.tunnel.sshDialTimeout
		probeTunnel.sshProbeURL = m.tunnel.sshProbeURL
		probeTunnel.sshProbeURLs = m.tunnel.sshProbeURLs
		probeTunnel.sshProbeTimeout = m.tunnel.sshProbeTimeout
	}
	handshakeStartedAt := time.Now()
	client, err := probeTunnel.dialSSH()
	handshakeDuration := time.Since(handshakeStartedAt)
	if err != nil {
		return nil, handshakeDuration, err
	}
	return &sshProfileTestConnection{client: client, tunnel: probeTunnel}, handshakeDuration, nil
}

func (m *ProfileTestManager) probeProfile(ctx context.Context, connection profileTestConnection) (string, time.Duration, error) {
	client, ok := connection.(*sshProfileTestConnection)
	if !ok || client == nil || client.client == nil || client.tunnel == nil {
		return "", 0, errors.New("无效的SSH测试连接")
	}
	member := &SSHPoolMember{Client: client.client, State: sshMemberHealthy}
	return client.tunnel.probeSSHMemberDetailed(ctx, member)
}

func failedProfileTest(result ProfileManualTestResult, err error) ProfileManualTestResult {
	result.Status = "failed"
	result.Reachable = false
	result.CompletedAt = time.Now()
	if err != nil {
		result.Error = err.Error()
	}
	return result
}

func (m *ProfileTestManager) Status(testID string) (ProfileTestBatch, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	testID = strings.TrimSpace(testID)
	if testID == "" && m.latest != nil {
		return cloneProfileTestBatch(m.latest), true
	}
	batch, ok := m.batches[testID]
	if !ok {
		return ProfileTestBatch{}, false
	}
	return cloneProfileTestBatch(batch), true
}

func (m *ProfileTestManager) TestingProfiles() map[string]bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]bool)
	if m.latest == nil || m.latest.Status != "RUNNING" {
		return result
	}
	for profileID, item := range m.latest.Results {
		if item.Status == "pending" || item.Status == "running" {
			result[profileID] = true
		}
	}
	return result
}

func (t *Tunnel) SnapshotProfileHealth(store cfg.ProfileStore) (map[string]ProfileHealthSummary, error) {
	identities := make(map[string]string, len(store.Profiles))
	for profileID, profile := range store.Profiles {
		identities[profileID] = cfg.SSHProfileConnectionFingerprint(profile)
	}
	failureThreshold := t.sshProbeFailureThreshold
	if failureThreshold == 0 {
		failureThreshold = 1
	}
	summaries, err := t.trafficStore.ProfileHealthSummaries(identities, failureThreshold)
	if err != nil {
		return nil, err
	}
	members := t.snapshotPoolMembers()
	if t.profileTunnelMgr != nil {
		members = append(members, t.profileTunnelMgr.SnapshotAllPools()...)
	}
	for _, member := range members {
		summary, ok := summaries[member.ProfileID]
		if !ok {
			continue
		}
		switch member.State {
		case sshMemberHealthy:
			summary.Pool.Healthy++
		case sshMemberSuspect:
			summary.Pool.Suspect++
		case sshMemberProbing:
			summary.Pool.Probing++
		case sshMemberReconnecting:
			summary.Pool.Reconnecting++
		case sshMemberEvicted:
			summary.Pool.Evicted++
		}
		summaries[member.ProfileID] = summary
	}
	for profileID := range t.GetProfileTestManager().TestingProfiles() {
		summary, ok := summaries[profileID]
		if !ok {
			continue
		}
		summary.Status = ProfileHealthStatusTesting
		summaries[profileID] = summary
	}
	return summaries, nil
}
