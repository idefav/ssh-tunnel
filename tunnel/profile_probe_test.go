package tunnel

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ssh-tunnel/cfg"
)

type fakeProfileTestConnection struct {
	closed atomic.Bool
}

func (c *fakeProfileTestConnection) Close() error {
	c.closed.Store(true)
	return nil
}

func TestProfileTestManagerRejectsOverlapAndLimitsConcurrency(t *testing.T) {
	manager := NewProfileTestManager(&Tunnel{})
	release := make(chan struct{})
	var active atomic.Int64
	var maximum atomic.Int64
	manager.testFn = func(_ context.Context, profileID string, _ cfg.SSHProfile) ProfileManualTestResult {
		current := active.Add(1)
		for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
		}
		<-release
		active.Add(-1)
		now := time.Now()
		return ProfileManualTestResult{ProfileID: profileID, Status: "completed", Reachable: true, StartedAt: now, CompletedAt: now}
	}
	profiles := make(map[string]cfg.SSHProfile)
	for _, profileID := range []string{"a", "b", "c", "d", "e"} {
		profiles[profileID] = cfg.SSHProfile{}
	}
	batch, err := manager.Start(profiles)
	if err != nil || batch.Status != "RUNNING" || batch.Total != 5 {
		t.Fatalf("unexpected start result: %+v err=%v", batch, err)
	}
	if _, err := manager.Start(map[string]cfg.SSHProfile{"other": {}}); !errors.Is(err, ErrProfileTestRunning) {
		t.Fatalf("expected overlap conflict, got %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for maximum.Load() < profileTestConcurrency && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if maximum.Load() != profileTestConcurrency {
		t.Fatalf("maximum concurrency=%d want=%d", maximum.Load(), profileTestConcurrency)
	}
	close(release)
	for time.Now().Before(deadline) {
		status, ok := manager.Status(batch.TestID)
		if ok && status.Status == "COMPLETED" {
			if status.Completed != 5 || maximum.Load() > profileTestConcurrency {
				t.Fatalf("unexpected completed batch: %+v maximum=%d", status, maximum.Load())
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("profile test batch did not complete")
}

func TestProfileTestManagerTracksFailuresAndUnknownIDs(t *testing.T) {
	manager := NewProfileTestManager(&Tunnel{})
	manager.testFn = func(_ context.Context, profileID string, _ cfg.SSHProfile) ProfileManualTestResult {
		now := time.Now()
		return ProfileManualTestResult{ProfileID: profileID, Status: "failed", Error: "unreachable", StartedAt: now, CompletedAt: now}
	}
	batch, err := manager.Start(map[string]cfg.SSHProfile{"jp": {}})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status, ok := manager.Status(batch.TestID)
		if ok && status.Status == "COMPLETED" {
			if status.Results["jp"].Status != "failed" || status.Results["jp"].Error != "unreachable" {
				t.Fatalf("failure result missing: %+v", status)
			}
			if _, ok := manager.Status("missing"); ok {
				t.Fatal("unknown test ID unexpectedly resolved")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("profile test batch did not complete")
}

func TestProfileTestInjectableHandshakeAndEgressFailuresCloseConnection(t *testing.T) {
	manager := NewProfileTestManager(&Tunnel{})
	probeCalled := false
	manager.dialFn = func(context.Context, string, cfg.SSHProfile) (profileTestConnection, time.Duration, error) {
		return nil, 42 * time.Millisecond, errors.New("key rejected")
	}
	manager.probeFn = func(context.Context, profileTestConnection) (string, time.Duration, error) {
		probeCalled = true
		return "", 0, nil
	}
	result := manager.runProfileTest(context.Background(), "handshake", cfg.SSHProfile{})
	if result.Status != "failed" || result.SSHHandshakeLatencyMs != 42 || !strings.Contains(result.Error, "SSH握手失败") || probeCalled {
		t.Fatalf("unexpected handshake failure: %+v probeCalled=%v", result, probeCalled)
	}

	connection := &fakeProfileTestConnection{}
	manager.dialFn = func(context.Context, string, cfg.SSHProfile) (profileTestConnection, time.Duration, error) {
		return connection, 12 * time.Millisecond, nil
	}
	manager.probeFn = func(context.Context, profileTestConnection) (string, time.Duration, error) {
		return "https://probe.example", 34 * time.Millisecond, errors.New("bad gateway")
	}
	result = manager.runProfileTest(context.Background(), "egress", cfg.SSHProfile{})
	if result.Status != "failed" || result.SSHHandshakeLatencyMs != 12 || result.EgressProbeLatencyMs != 34 || result.ProbeTarget != "https://probe.example" || !strings.Contains(result.Error, "出口探测失败") {
		t.Fatalf("unexpected egress failure: %+v", result)
	}
	if !connection.closed.Load() {
		t.Fatal("one-shot SSH connection was not closed after egress failure")
	}
}

func TestProfileTestInjectableProbeHonorsTimeout(t *testing.T) {
	manager := NewProfileTestManager(&Tunnel{})
	connection := &fakeProfileTestConnection{}
	manager.dialFn = func(context.Context, string, cfg.SSHProfile) (profileTestConnection, time.Duration, error) {
		return connection, time.Millisecond, nil
	}
	manager.probeFn = func(ctx context.Context, _ profileTestConnection) (string, time.Duration, error) {
		startedAt := time.Now()
		<-ctx.Done()
		return "https://slow.example", time.Since(startedAt), ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result := manager.runProfileTest(ctx, "slow", cfg.SSHProfile{})
	if result.Status != "failed" || !strings.Contains(result.Error, context.DeadlineExceeded.Error()) || !connection.closed.Load() {
		t.Fatalf("timeout result did not fail and close: %+v closed=%v", result, connection.closed.Load())
	}
}

func TestProfileTestBatchKeepsPartialResultsAndCompletedHistory(t *testing.T) {
	manager := NewProfileTestManager(&Tunnel{})
	manager.testFn = func(_ context.Context, profileID string, _ cfg.SSHProfile) ProfileManualTestResult {
		now := time.Now()
		result := ProfileManualTestResult{ProfileID: profileID, Status: "completed", Reachable: true, StartedAt: now, CompletedAt: now}
		if profileID == "bad" {
			result.Status = "failed"
			result.Reachable = false
			result.Error = "unreachable"
		}
		return result
	}
	first, err := manager.Start(map[string]cfg.SSHProfile{"good": {}, "bad": {}})
	if err != nil {
		t.Fatal(err)
	}
	waitProfileTestBatch(t, manager, first.TestID)
	second, err := manager.Start(map[string]cfg.SSHProfile{"next": {}})
	if err != nil {
		t.Fatal(err)
	}
	waitProfileTestBatch(t, manager, second.TestID)
	retained, ok := manager.Status(first.TestID)
	if !ok || retained.Results["good"].Status != "completed" || retained.Results["bad"].Status != "failed" {
		t.Fatalf("completed partial-success batch was not retained: %+v", retained)
	}
}

func waitProfileTestBatch(t *testing.T, manager *ProfileTestManager, testID string) ProfileTestBatch {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		batch, ok := manager.Status(testID)
		if ok && batch.Status == "COMPLETED" {
			return batch
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("profile test batch %s did not complete", testID)
	return ProfileTestBatch{}
}
