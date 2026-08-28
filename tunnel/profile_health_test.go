package tunnel

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestActiveProfileDialBoundaryRecordsOnceAndEmptyProfileDoesNot(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.UTC)
	tunnel := newTestTunnel()
	tunnel.profileID = "active"
	tunnel.profileIdentity = "active-id"
	tunnel.trafficStore = store
	_, _, _, _, _, err := tunnel.dialSSHConn(context.Background(), "api.example:443", tunnel.newRequestRetryState())
	if err == nil {
		t.Fatal("dial without an SSH pool unexpectedly succeeded")
	}
	summaries, err := store.ProfileHealthSummaries(map[string]string{"active": "active-id"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if summaries["active"].SampleCount != 1 || summaries["active"].FailureCount != 1 {
		t.Fatalf("active profile dial was not recorded exactly once: %+v", summaries["active"])
	}

	tunnel.profileID = ""
	_, _, _, _, _, _ = tunnel.dialSSHConn(context.Background(), "direct.example:443", tunnel.newRequestRetryState())
	summaries, err = store.ProfileHealthSummaries(map[string]string{"active": "active-id"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if summaries["active"].SampleCount != 1 {
		t.Fatalf("empty/direct profile polluted health statistics: %+v", summaries["active"])
	}
}

func TestProfileHealthAggregatesLatencyFailuresAndStatus(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.UTC)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	identity := "node-v1"
	store.recordProfileAccessAt("jp", identity, "one.example:443", 12*time.Millisecond, true, "", nil, now.Add(-3*time.Minute))
	store.recordProfileAccessAt("jp", identity, "two.example:443", 77*time.Millisecond, true, "", nil, now.Add(-2*time.Minute))
	store.recordProfileAccessAt("jp", identity, "bad.example:443", 3*time.Second, false, failureClassSSHChannelTimeout, errors.New("timeout"), now.Add(-time.Minute))

	summaries, err := store.profileHealthSummariesAt(map[string]string{"jp": identity}, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	summary := summaries["jp"]
	if summary.SampleCount != 3 || summary.SuccessCount != 2 || summary.FailureCount != 1 {
		t.Fatalf("unexpected counts: %+v", summary)
	}
	if math.Abs(summary.SuccessRate-66.6666667) > 0.001 || summary.AvgDialLatencyMs != 44.5 || summary.P95DialLatencyMs != 100 {
		t.Fatalf("unexpected access quality: %+v", summary)
	}
	if summary.Status != ProfileHealthStatusDegraded || summary.ConsecutiveFailures != 1 || summary.LastAccess == nil || summary.LastAccess.Target != "bad.example:443" {
		t.Fatalf("unexpected degraded status: %+v", summary)
	}

	store.recordProfileAccessAt("jp", identity, "bad2.example:443", 3*time.Second, false, failureClassSSHChannelTimeout, errors.New("timeout again"), now)
	summaries, err = store.profileHealthSummariesAt(map[string]string{"jp": identity}, 2, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if summaries["jp"].Status != ProfileHealthStatusUnreachable || summaries["jp"].ConsecutiveFailures != 2 {
		t.Fatalf("expected unreachable after threshold: %+v", summaries["jp"])
	}

	store.recordProfileAccessAt("jp", identity, "ok.example:443", 9*time.Millisecond, true, "", nil, now.Add(2*time.Second))
	summaries, err = store.profileHealthSummariesAt(map[string]string{"jp": identity}, 2, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if summaries["jp"].Status != ProfileHealthStatusReachable || summaries["jp"].ConsecutiveFailures != 0 {
		t.Fatalf("success did not recover health: %+v", summaries["jp"])
	}
}

func TestProfileHealthPersistsManualResultAndFiltersIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.db")
	store, err := openTrafficStoreAt(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store.recordProfileAccessAt("sg", "identity-a", "example.com:443", 30*time.Millisecond, true, "", nil, now)
	store.RecordProfileManualTest("identity-a", ProfileManualTestResult{
		ProfileID:             "sg",
		Status:                "completed",
		Reachable:             true,
		SSHHandshakeLatencyMs: 18,
		EgressProbeLatencyMs:  42,
		ProbeTarget:           "https://probe.example",
		StartedAt:             now,
		CompletedAt:           now.Add(time.Second),
	})
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openTrafficStoreAt(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	summaries, err := reopened.ProfileHealthSummaries(map[string]string{"sg": "identity-a"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if summaries["sg"].SampleCount != 1 || summaries["sg"].LastManualTest == nil || summaries["sg"].LastManualTest.EgressProbeLatencyMs != 42 {
		t.Fatalf("health state not restored: %+v", summaries["sg"])
	}
	mismatched, err := reopened.ProfileHealthSummaries(map[string]string{"sg": "identity-b"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if mismatched["sg"].Status != ProfileHealthStatusUnknown || mismatched["sg"].SampleCount != 0 || mismatched["sg"].LastManualTest != nil {
		t.Fatalf("old identity leaked into edited profile: %+v", mismatched["sg"])
	}
}

func TestProfileHealthRetentionAndReset(t *testing.T) {
	store, _ := newTestTrafficStore(t, time.UTC)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	store.recordProfileAccessAt("us", "identity", "expired.example:443", time.Second, false, failureClassUnknown, errors.New("expired"), now.Add(-25*time.Hour))
	summaries, err := store.profileHealthSummariesAt(map[string]string{"us": "identity"}, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	if summaries["us"].Status != ProfileHealthStatusUnknown || summaries["us"].SampleCount != 0 || summaries["us"].LastAccess != nil {
		t.Fatalf("expired evidence remained active: %+v", summaries["us"])
	}

	store.recordProfileAccessAt("us", "identity", "fresh.example:443", 20*time.Millisecond, true, "", nil, now)
	if err := store.ResetProfileHealth("us"); err != nil {
		t.Fatal(err)
	}
	summaries, err = store.profileHealthSummariesAt(map[string]string{"us": "identity"}, 1, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if summaries["us"].Status != ProfileHealthStatusUnknown || summaries["us"].SampleCount != 0 || summaries["us"].LastAccess != nil {
		t.Fatalf("reset left health data behind: %+v", summaries["us"])
	}
}
