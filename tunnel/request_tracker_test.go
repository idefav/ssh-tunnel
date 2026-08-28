package tunnel

import "testing"

func TestRequestTrackerStoresRouteGroupAndAttempts(t *testing.T) {
	tracker := NewProxyRequestTracker(4)
	req := tracker.StartRequest("api.openai.com", "443", "HTTPS", true)
	tracker.UpdateRouteInfo(req, "route-openai", "random", []string{"jp", "us"}, "group-openai", "OpenAI")
	snapshot := tracker.Snapshot()
	if len(snapshot) != 1 {
		t.Fatalf("snapshot size = %d", len(snapshot))
	}
	got := snapshot[0]
	if got.RouteID != "route-openai" || got.RouteGroupID != "group-openai" || got.RouteGroupName != "OpenAI" || got.RouteStrategy != "random" {
		t.Fatalf("route tracking mismatch: %+v", got)
	}
	if len(got.AttemptedProfileIDs) != 2 || got.AttemptedProfileIDs[0] != "jp" || got.AttemptedProfileIDs[1] != "us" {
		t.Fatalf("attempted profiles mismatch: %v", got.AttemptedProfileIDs)
	}
}
