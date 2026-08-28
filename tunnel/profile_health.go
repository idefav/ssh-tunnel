package tunnel

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

const profileHealthRetention = 24 * time.Hour

var (
	profileHealthMinuteBucket = []byte("profile_health_minute_v1")
	profileHealthLatestBucket = []byte("profile_health_latest_v1")
	profileLatencyBoundsMs    = [...]int64{10, 25, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 30000}
)

const (
	ProfileHealthStatusTesting     = "testing"
	ProfileHealthStatusReachable   = "reachable"
	ProfileHealthStatusDegraded    = "degraded"
	ProfileHealthStatusUnreachable = "unreachable"
	ProfileHealthStatusUnknown     = "unknown"
)

type profileHealthMinuteKey struct {
	ProfileID string
	Identity  string
	Minute    int64
}

type profileHealthMinuteValue struct {
	ProfileID         string                                  `json:"profileId"`
	Identity          string                                  `json:"identity"`
	Minute            int64                                   `json:"minute"`
	SuccessCount      uint64                                  `json:"successCount"`
	FailureCount      uint64                                  `json:"failureCount"`
	SuccessLatencySum uint64                                  `json:"successLatencySumMs"`
	LatencyHistogram  [len(profileLatencyBoundsMs) + 1]uint64 `json:"latencyHistogram"`
}

type ProfileAccessEvidence struct {
	At                  time.Time `json:"at"`
	Success             bool      `json:"success"`
	DialLatencyMs       int64     `json:"dialLatencyMs"`
	Target              string    `json:"target,omitempty"`
	FailureClass        string    `json:"failureClass,omitempty"`
	Error               string    `json:"error,omitempty"`
	ConsecutiveFailures uint64    `json:"consecutiveFailures"`
}

type ProfileManualTestResult struct {
	ProfileID             string    `json:"profileId"`
	Status                string    `json:"status"`
	Reachable             bool      `json:"reachable"`
	SSHHandshakeLatencyMs int64     `json:"sshHandshakeLatencyMs,omitempty"`
	EgressProbeLatencyMs  int64     `json:"egressProbeLatencyMs,omitempty"`
	ProbeTarget           string    `json:"probeTarget,omitempty"`
	Error                 string    `json:"error,omitempty"`
	StartedAt             time.Time `json:"startedAt"`
	CompletedAt           time.Time `json:"completedAt,omitempty"`
}

type profileHealthLatestValue struct {
	Identity string                   `json:"identity"`
	Access   *ProfileAccessEvidence   `json:"access,omitempty"`
	Manual   *ProfileManualTestResult `json:"manual,omitempty"`
}

type ProfilePoolHealth struct {
	Healthy      int `json:"healthy"`
	Suspect      int `json:"suspect"`
	Probing      int `json:"probing"`
	Reconnecting int `json:"reconnecting"`
	Evicted      int `json:"evicted"`
}

type ProfileHealthSummary struct {
	ProfileID           string                   `json:"profileId"`
	Status              string                   `json:"status"`
	WindowHours         int                      `json:"windowHours"`
	SampleCount         uint64                   `json:"sampleCount"`
	SuccessCount        uint64                   `json:"successCount"`
	FailureCount        uint64                   `json:"failureCount"`
	SuccessRate         float64                  `json:"successRate"`
	AvgDialLatencyMs    float64                  `json:"avgDialLatencyMs"`
	P95DialLatencyMs    int64                    `json:"p95DialLatencyMs"`
	ConsecutiveFailures uint64                   `json:"consecutiveFailures"`
	LastAccess          *ProfileAccessEvidence   `json:"lastAccess,omitempty"`
	LastManualTest      *ProfileManualTestResult `json:"lastManualTest,omitempty"`
	Pool                ProfilePoolHealth        `json:"pool"`
}

func localMinuteStart(value time.Time, location *time.Location) time.Time {
	local := value.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, location)
}

func encodeProfileHealthMinuteKey(key profileHealthMinuteKey) []byte {
	prefix := key.ProfileID + "\x00" + key.Identity + "\x00"
	result := make([]byte, len(prefix)+8)
	copy(result, prefix)
	binary.BigEndian.PutUint64(result[len(prefix):], uint64(key.Minute))
	return result
}

func profileHealthPrefix(profileID string) []byte {
	return []byte(strings.TrimSpace(profileID) + "\x00")
}

func latencyHistogramIndex(latencyMs int64) int {
	for index, bound := range profileLatencyBoundsMs {
		if latencyMs <= bound {
			return index
		}
	}
	return len(profileLatencyBoundsMs)
}

func mergeProfileHealthMinute(left, right profileHealthMinuteValue) profileHealthMinuteValue {
	if left.ProfileID == "" {
		left.ProfileID = right.ProfileID
		left.Identity = right.Identity
		left.Minute = right.Minute
	}
	left.SuccessCount += right.SuccessCount
	left.FailureCount += right.FailureCount
	left.SuccessLatencySum += right.SuccessLatencySum
	for index := range left.LatencyHistogram {
		left.LatencyHistogram[index] += right.LatencyHistogram[index]
	}
	return left
}

func boundedHealthText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func (s *TrafficStore) loadProfileHealthLatest() error {
	return s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(profileHealthLatestBucket)
		return bucket.ForEach(func(key, value []byte) error {
			var latest profileHealthLatestValue
			if err := json.Unmarshal(value, &latest); err != nil {
				return fmt.Errorf("解析Profile健康状态%s失败: %w", string(key), err)
			}
			s.healthLatest[string(key)] = latest
			return nil
		})
	})
}

func (s *TrafficStore) RecordProfileAccess(profileID, identity, target string, duration time.Duration, success bool, failureClass string, accessErr error) {
	if s == nil || strings.TrimSpace(profileID) == "" {
		return
	}
	s.recordProfileAccessAt(profileID, identity, target, duration, success, failureClass, accessErr, time.Now())
}

func (s *TrafficStore) recordProfileAccessAt(profileID, identity, target string, duration time.Duration, success bool, failureClass string, accessErr error, now time.Time) {
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.closed.Load() {
		return
	}
	profileID = strings.TrimSpace(profileID)
	identity = strings.TrimSpace(identity)
	latencyMs := duration.Milliseconds()
	if latencyMs < 0 {
		latencyMs = 0
	}
	key := profileHealthMinuteKey{ProfileID: profileID, Identity: identity, Minute: localMinuteStart(now, s.location).Unix()}

	s.healthMu.Lock()
	latest := s.healthLatest[profileID]
	if latest.Identity != identity {
		latest = profileHealthLatestValue{Identity: identity}
	}
	consecutiveFailures := uint64(0)
	if !success {
		consecutiveFailures = 1
		if latest.Access != nil {
			consecutiveFailures += latest.Access.ConsecutiveFailures
		}
	}
	errorText := ""
	if accessErr != nil {
		errorText = accessErr.Error()
	}
	latest.Access = &ProfileAccessEvidence{
		At:                  now,
		Success:             success,
		DialLatencyMs:       latencyMs,
		Target:              boundedHealthText(target, 255),
		FailureClass:        boundedHealthText(failureClass, 96),
		Error:               boundedHealthText(errorText, 512),
		ConsecutiveFailures: consecutiveFailures,
	}
	s.healthLatest[profileID] = latest
	s.healthDirty[profileID] = true
	pending := s.healthPending[key]
	pending.ProfileID = profileID
	pending.Identity = identity
	pending.Minute = key.Minute
	if success {
		pending.SuccessCount++
		pending.SuccessLatencySum += uint64(latencyMs)
		pending.LatencyHistogram[latencyHistogramIndex(latencyMs)]++
	} else {
		pending.FailureCount++
	}
	s.healthPending[key] = pending
	s.healthMu.Unlock()
}

func (s *TrafficStore) RecordProfileManualTest(identity string, result ProfileManualTestResult) {
	if s == nil || strings.TrimSpace(result.ProfileID) == "" {
		return
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.closed.Load() {
		return
	}
	result.Error = boundedHealthText(result.Error, 512)
	result.ProbeTarget = boundedHealthText(result.ProbeTarget, 255)
	s.healthMu.Lock()
	latest := s.healthLatest[result.ProfileID]
	if latest.Identity != identity {
		latest = profileHealthLatestValue{Identity: identity}
	}
	copyResult := result
	latest.Manual = &copyResult
	s.healthLatest[result.ProfileID] = latest
	s.healthDirty[result.ProfileID] = true
	s.healthMu.Unlock()
}

func (s *TrafficStore) takeProfileHealthPending() (map[profileHealthMinuteKey]profileHealthMinuteValue, map[string]profileHealthLatestValue) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	pending := s.healthPending
	s.healthPending = make(map[profileHealthMinuteKey]profileHealthMinuteValue)
	latest := make(map[string]profileHealthLatestValue, len(s.healthDirty))
	for profileID := range s.healthDirty {
		latest[profileID] = s.healthLatest[profileID]
	}
	s.healthDirty = make(map[string]bool)
	return pending, latest
}

func (s *TrafficStore) restoreProfileHealthPending(pending map[profileHealthMinuteKey]profileHealthMinuteValue, latest map[string]profileHealthLatestValue) {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	for key, value := range pending {
		s.healthPending[key] = mergeProfileHealthMinute(s.healthPending[key], value)
	}
	for profileID := range latest {
		s.healthDirty[profileID] = true
	}
}

func persistProfileHealth(tx *bolt.Tx, pending map[profileHealthMinuteKey]profileHealthMinuteValue, latest map[string]profileHealthLatestValue) error {
	minuteBucket := tx.Bucket(profileHealthMinuteBucket)
	for key, delta := range pending {
		dbKey := encodeProfileHealthMinuteKey(key)
		combined := delta
		if current := minuteBucket.Get(dbKey); current != nil {
			var stored profileHealthMinuteValue
			if err := json.Unmarshal(current, &stored); err != nil {
				return err
			}
			combined = mergeProfileHealthMinute(stored, delta)
		}
		content, err := json.Marshal(combined)
		if err != nil {
			return err
		}
		if err := minuteBucket.Put(dbKey, content); err != nil {
			return err
		}
	}
	latestBucket := tx.Bucket(profileHealthLatestBucket)
	for profileID, value := range latest {
		content, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err := latestBucket.Put([]byte(profileID), content); err != nil {
			return err
		}
	}
	return nil
}

func (s *TrafficStore) ResetProfileHealth(profileID string) error {
	if s == nil {
		return nil
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("profileId不能为空")
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if err := s.flushLocked(time.Now()); err != nil {
		return err
	}
	s.healthMu.Lock()
	for key := range s.healthPending {
		if key.ProfileID == profileID {
			delete(s.healthPending, key)
		}
	}
	delete(s.healthLatest, profileID)
	delete(s.healthDirty, profileID)
	s.healthMu.Unlock()
	prefix := profileHealthPrefix(profileID)
	if err := s.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(profileHealthMinuteBucket)
		cursor := bucket.Cursor()
		var keys [][]byte
		for key, _ := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix); key, _ = cursor.Next() {
			keys = append(keys, append([]byte(nil), key...))
		}
		for _, key := range keys {
			if err := bucket.Delete(key); err != nil {
				return err
			}
		}
		return tx.Bucket(profileHealthLatestBucket).Delete([]byte(profileID))
	}); err != nil {
		return fmt.Errorf("清除Profile健康统计失败: %w", err)
	}
	return nil
}

func profileHealthP95(histogram [len(profileLatencyBoundsMs) + 1]uint64, successCount uint64) int64 {
	if successCount == 0 {
		return 0
	}
	target := uint64(math.Ceil(float64(successCount) * 0.95))
	var cumulative uint64
	for index, count := range histogram {
		cumulative += count
		if cumulative < target {
			continue
		}
		if index < len(profileLatencyBoundsMs) {
			return profileLatencyBoundsMs[index]
		}
		return profileLatencyBoundsMs[len(profileLatencyBoundsMs)-1] + 1
	}
	return 0
}

func (s *TrafficStore) ProfileHealthSummaries(profiles map[string]string, failureThreshold uint64) (map[string]ProfileHealthSummary, error) {
	return s.profileHealthSummariesAt(profiles, failureThreshold, time.Now())
}

func (s *TrafficStore) profileHealthSummariesAt(profiles map[string]string, failureThreshold uint64, now time.Time) (map[string]ProfileHealthSummary, error) {
	if failureThreshold == 0 {
		failureThreshold = 1
	}
	result := make(map[string]ProfileHealthSummary, len(profiles))
	for profileID := range profiles {
		result[profileID] = ProfileHealthSummary{ProfileID: profileID, Status: ProfileHealthStatusUnknown, WindowHours: 24}
	}
	if s == nil {
		return result, nil
	}
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	if s.closed.Load() {
		return nil, fmt.Errorf("流量数据库已关闭")
	}
	if err := s.flushLocked(now); err != nil {
		return nil, err
	}
	cutoff := localMinuteStart(now.Add(-profileHealthRetention), s.location).Unix()
	upper := localMinuteStart(now, s.location).Unix()
	aggregates := make(map[string]profileHealthMinuteValue, len(profiles))
	if err := s.db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(profileHealthMinuteBucket)
		return bucket.ForEach(func(_, value []byte) error {
			var item profileHealthMinuteValue
			if err := json.Unmarshal(value, &item); err != nil {
				return err
			}
			identity, ok := profiles[item.ProfileID]
			if !ok || item.Identity != identity || item.Minute < cutoff || item.Minute > upper {
				return nil
			}
			aggregates[item.ProfileID] = mergeProfileHealthMinute(aggregates[item.ProfileID], item)
			return nil
		})
	}); err != nil {
		return nil, fmt.Errorf("读取Profile健康统计失败: %w", err)
	}

	s.healthMu.Lock()
	latest := make(map[string]profileHealthLatestValue, len(profiles))
	for profileID, identity := range profiles {
		value := s.healthLatest[profileID]
		if value.Identity == identity {
			latest[profileID] = value
		}
	}
	s.healthMu.Unlock()

	profileIDs := make([]string, 0, len(profiles))
	for profileID := range profiles {
		profileIDs = append(profileIDs, profileID)
	}
	sort.Strings(profileIDs)
	for _, profileID := range profileIDs {
		summary := result[profileID]
		aggregate := aggregates[profileID]
		summary.SuccessCount = aggregate.SuccessCount
		summary.FailureCount = aggregate.FailureCount
		summary.SampleCount = aggregate.SuccessCount + aggregate.FailureCount
		if summary.SampleCount > 0 {
			summary.SuccessRate = float64(summary.SuccessCount) * 100 / float64(summary.SampleCount)
		}
		if summary.SuccessCount > 0 {
			summary.AvgDialLatencyMs = float64(aggregate.SuccessLatencySum) / float64(summary.SuccessCount)
			summary.P95DialLatencyMs = profileHealthP95(aggregate.LatencyHistogram, summary.SuccessCount)
		}
		value := latest[profileID]
		if value.Access != nil && healthEvidenceExpired(value.Access.At, now) {
			value.Access = nil
		}
		if value.Manual != nil && healthEvidenceExpired(value.Manual.CompletedAt, now) {
			value.Manual = nil
		}
		summary.LastAccess = value.Access
		summary.LastManualTest = value.Manual
		if value.Access != nil {
			summary.ConsecutiveFailures = value.Access.ConsecutiveFailures
		}
		summary.Status = profileHealthStatusAt(value, failureThreshold, now)
		result[profileID] = summary
	}
	return result, nil
}

func healthEvidenceExpired(at, now time.Time) bool {
	return at.IsZero() || now.Sub(at) > profileHealthRetention
}

func profileHealthStatusAt(latest profileHealthLatestValue, failureThreshold uint64, now time.Time) string {
	var evidenceAt time.Time
	var success bool
	manual := false
	consecutiveFailures := uint64(0)
	if latest.Access != nil {
		evidenceAt = latest.Access.At
		success = latest.Access.Success
		consecutiveFailures = latest.Access.ConsecutiveFailures
	}
	if latest.Manual != nil && latest.Manual.CompletedAt.After(evidenceAt) {
		evidenceAt = latest.Manual.CompletedAt
		success = latest.Manual.Reachable
		manual = true
	}
	if healthEvidenceExpired(evidenceAt, now) {
		return ProfileHealthStatusUnknown
	}
	if success {
		return ProfileHealthStatusReachable
	}
	if manual || consecutiveFailures >= failureThreshold {
		return ProfileHealthStatusUnreachable
	}
	return ProfileHealthStatusDegraded
}
