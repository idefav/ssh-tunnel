package admin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"ssh-tunnel/cfg"
	"ssh-tunnel/tunnel"
	"testing"
)

func TestParseTailLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty uses default", input: "", want: defaultLogTailLines},
		{name: "invalid uses default", input: "abc", want: defaultLogTailLines},
		{name: "zero uses default", input: "0", want: defaultLogTailLines},
		{name: "valid value", input: "200", want: 200},
		{name: "clamps large value", input: "999999", want: maxLogTailLines},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTailLines(tt.input); got != tt.want {
				t.Fatalf("parseTailLines(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestDNSOverrideDoesNotRequireProfileReconnect(t *testing.T) {
	old := cfg.SSHProfile{ServerIp: "192.0.2.1", LoginUser: "test", DNSUpstreams: []string{"10.0.0.53:53"}}
	next := old
	next.DNSUpstreams = []string{"10.0.0.54:53"}
	if profileRuntimeChanged(old, next) {
		t.Fatal("DNS-only edit would disconnect TCP")
	}
	next.DNSUpstreams = nil
	if profileRuntimeChanged(old, next) {
		t.Fatal("clearing override would disconnect TCP")
	}
	next.ServerIp = "192.0.2.2"
	if !profileRuntimeChanged(old, next) {
		t.Fatal("SSH change must refresh runtime")
	}
}

func TestReadLastLogLines(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLines int
		want     []string
	}{
		{
			name:     "returns last N lines",
			content:  "line1\nline2\nline3\nline4\n",
			maxLines: 2,
			want:     []string{"line3", "line4"},
		},
		{
			name:     "handles missing trailing newline",
			content:  "line1\nline2\nline3",
			maxLines: 2,
			want:     []string{"line2", "line3"},
		},
		{
			name:     "skips blank lines",
			content:  "line1\n\nline2\r\nline3\n",
			maxLines: 3,
			want:     []string{"line1", "line2", "line3"},
		},
		{
			name:     "empty file",
			content:  "",
			maxLines: 5,
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "app.log")
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write file: %v", err)
			}

			file, err := os.Open(path)
			if err != nil {
				t.Fatalf("open file: %v", err)
			}
			defer file.Close()

			got, err := readLastLogLines(file, tt.maxLines)
			if err != nil {
				t.Fatalf("readLastLogLines returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("readLastLogLines() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRouteBatchHandlerSuccessReloadsOnce(t *testing.T) {
	var captured cfg.RouteBatchUpdate
	reloads := 0
	handler := newRouteBatchHandler(func(update cfg.RouteBatchUpdate) (cfg.RouteBatchResult, error) {
		captured = update
		return cfg.RouteBatchResult{Store: cfg.RouteStore{Version: 2}, ChangedCount: 2, MovedCount: 1, CreatedGroupID: "group-new"}, nil
	}, func() error {
		reloads++
		return nil
	})
	body := []byte(`{"routeIds":["a","b"],"inheritanceMode":"inherit","enabled":false}`)
	request := httptest.NewRequest(http.MethodPost, "/admin/routes/batch", bytes.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || reloads != 1 || !reflect.DeepEqual(captured.RouteIDs, []string{"a", "b"}) || captured.Enabled == nil || *captured.Enabled {
		t.Fatalf("unexpected handler result: status=%d reloads=%d captured=%+v body=%s", response.Code, reloads, captured, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["success"] != true || payload["changedCount"] != float64(2) || payload["movedCount"] != float64(1) || payload["createdGroupId"] != "group-new" {
		t.Fatalf("unexpected response payload: %+v", payload)
	}
}

func TestRouteBatchHandlerRejectsInvalidMethodPayloadAndUpdate(t *testing.T) {
	updates := 0
	reloads := 0
	handler := newRouteBatchHandler(func(update cfg.RouteBatchUpdate) (cfg.RouteBatchResult, error) {
		updates++
		return cfg.RouteBatchResult{}, errors.New("invalid batch")
	}, func() error {
		reloads++
		return nil
	})
	tests := []struct {
		method string
		body   string
		status int
	}{
		{method: http.MethodGet, status: http.StatusMethodNotAllowed},
		{method: http.MethodPost, body: "{", status: http.StatusBadRequest},
		{method: http.MethodPost, body: `{"routeIds":["a"],"enabled":true}`, status: http.StatusBadRequest},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, "/admin/routes/batch", bytes.NewBufferString(test.body)))
		if response.Code != test.status {
			t.Fatalf("%s %q status=%d want=%d body=%s", test.method, test.body, response.Code, test.status, response.Body.String())
		}
	}
	if updates != 1 || reloads != 0 {
		t.Fatalf("updates=%d reloads=%d", updates, reloads)
	}
}

func TestRouteBatchHandlerReturnsReloadFailureAfterSuccessfulUpdate(t *testing.T) {
	handler := newRouteBatchHandler(func(update cfg.RouteBatchUpdate) (cfg.RouteBatchResult, error) {
		return cfg.RouteBatchResult{Store: cfg.RouteStore{Version: 2}}, nil
	}, func() error { return errors.New("reload failed") })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/routes/batch", bytes.NewBufferString(`{"routeIds":["a"],"enabled":true}`)))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProfileHealthHandlerReturnsStableShape(t *testing.T) {
	handler := newProfileHealthHandler(func() (cfg.ProfileStore, error) {
		return cfg.ProfileStore{Profiles: map[string]cfg.SSHProfile{"jp": {}}}, nil
	}, func(store cfg.ProfileStore) (map[string]tunnel.ProfileHealthSummary, error) {
		if _, ok := store.Profiles["jp"]; !ok {
			t.Fatal("profile store was not passed to snapshot")
		}
		return map[string]tunnel.ProfileHealthSummary{"jp": {ProfileID: "jp", Status: tunnel.ProfileHealthStatusReachable, WindowHours: 24}}, nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/admin/profiles/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			WindowHours int                                    `json:"windowHours"`
			Profiles    map[string]tunnel.ProfileHealthSummary `json:"profiles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Data.WindowHours != 24 || payload.Data.Profiles["jp"].Status != tunnel.ProfileHealthStatusReachable {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/profiles/health", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status=%d", response.Code)
	}
}

func TestProfileTestStartHandlerValidationSuccessAndConflict(t *testing.T) {
	load := func() (cfg.ProfileStore, error) {
		return cfg.ProfileStore{Profiles: map[string]cfg.SSHProfile{"jp": {ServerIp: "127.0.0.1"}}}, nil
	}
	var selected map[string]cfg.SSHProfile
	handler := newProfileTestStartHandler(load, func(profiles map[string]cfg.SSHProfile) (tunnel.ProfileTestBatch, error) {
		selected = profiles
		return tunnel.ProfileTestBatch{TestID: "pt_1", Status: "RUNNING", Total: len(profiles), Results: map[string]tunnel.ProfileManualTestResult{}}, nil
	})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/profiles/test", bytes.NewBufferString(`{"profileIds":["jp","jp"]}`)))
	if response.Code != http.StatusAccepted || len(selected) != 1 || selected["jp"].ServerIp != "127.0.0.1" {
		t.Fatalf("unexpected start: status=%d selected=%+v body=%s", response.Code, selected, response.Body.String())
	}

	for _, body := range []string{`{"profileIds":[]}`, `{"profileIds":["missing"]}`, `{`} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/profiles/test", bytes.NewBufferString(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%q status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
	conflict := newProfileTestStartHandler(load, func(map[string]cfg.SSHProfile) (tunnel.ProfileTestBatch, error) {
		return tunnel.ProfileTestBatch{}, tunnel.ErrProfileTestRunning
	})
	response = httptest.NewRecorder()
	conflict.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/admin/profiles/test", bytes.NewBufferString(`{"profileIds":["jp"]}`)))
	if response.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestProfileTestStatusHandlerValidationAndLookup(t *testing.T) {
	handler := newProfileTestStatusHandler(func(testID string) (tunnel.ProfileTestBatch, bool) {
		if testID != "pt_1" {
			return tunnel.ProfileTestBatch{}, false
		}
		return tunnel.ProfileTestBatch{TestID: testID, Status: "COMPLETED", Total: 1, Completed: 1}, true
	})
	tests := []struct {
		url    string
		status int
	}{
		{url: "/admin/profiles/test/status", status: http.StatusBadRequest},
		{url: "/admin/profiles/test/status?testId=missing", status: http.StatusNotFound},
		{url: "/admin/profiles/test/status?testId=pt_1", status: http.StatusOK},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.url, nil))
		if response.Code != test.status {
			t.Fatalf("url=%s status=%d want=%d body=%s", test.url, response.Code, test.status, response.Body.String())
		}
	}
}
