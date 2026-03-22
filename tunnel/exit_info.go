package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"ssh-tunnel/safe"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	defaultExitInfoSuccessTTL = time.Minute
	defaultExitInfoErrorTTL   = 15 * time.Second
	defaultExitInfoTimeout    = 8 * time.Second
	ipInfoEndpoint            = "https://ipinfo.io/json"
)

type ipInfoResponse struct {
	IP       string `json:"ip"`
	City     string `json:"city"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	Loc      string `json:"loc"`
	Org      string `json:"org"`
	Timezone string `json:"timezone"`
}

func (t *Tunnel) SnapshotExitIPInfo() ExitIPInfo {
	t.exitInfoMu.RLock()
	defer t.exitInfoMu.RUnlock()
	return t.lastExitIPInfo
}

func (t *Tunnel) GetExitIPInfo(ctx context.Context, force bool) ExitIPInfo {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot := t.SnapshotExitIPInfo()
	if !t.shouldRefreshExitIPInfo(snapshot, force) {
		return snapshot
	}
	return t.refreshExitIPInfo(ctx, force)
}

func (t *Tunnel) shouldRefreshExitIPInfo(info ExitIPInfo, force bool) bool {
	if force || info.UpdatedAt.IsZero() {
		return true
	}

	ttl := defaultExitInfoSuccessTTL
	if !info.Available {
		ttl = defaultExitInfoErrorTTL
	}

	return time.Since(info.UpdatedAt) >= ttl
}

func (t *Tunnel) refreshExitIPInfo(ctx context.Context, force bool) ExitIPInfo {
	t.exitInfoRefreshMu.Lock()
	defer t.exitInfoRefreshMu.Unlock()

	snapshot := t.SnapshotExitIPInfo()
	if !t.shouldRefreshExitIPInfo(snapshot, force) {
		return snapshot
	}

	sshClient := t.PeekSSHClient()
	if sshClient == nil {
		info := ExitIPInfo{
			Available: false,
			Error:     "SSH client is not connected",
			UpdatedAt: time.Now(),
		}
		t.storeExitIPInfo(info)
		return info
	}

	httpClient, err := t.sshHTTPClientForClient(sshClient, defaultExitInfoTimeout)
	if err != nil {
		info := ExitIPInfo{
			Available: false,
			Error:     err.Error(),
			UpdatedAt: time.Now(),
		}
		t.storeExitIPInfo(info)
		return info
	}
	if transport, ok := httpClient.Transport.(*http.Transport); ok {
		defer transport.CloseIdleConnections()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ipInfoEndpoint, nil)
	if err != nil {
		info := ExitIPInfo{
			Available: false,
			Error:     err.Error(),
			UpdatedAt: time.Now(),
		}
		t.storeExitIPInfo(info)
		return info
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ssh-tunnel/exit-info")

	resp, err := httpClient.Do(req)
	if err != nil {
		t.handleExitIPInfoFetchError(ctx, sshClient, err)
		info := ExitIPInfo{
			Available: false,
			Error:     err.Error(),
			UpdatedAt: time.Now(),
		}
		t.storeExitIPInfo(info)
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		errMsg := strings.TrimSpace(string(body))
		if errMsg == "" {
			errMsg = http.StatusText(resp.StatusCode)
		}
		info := ExitIPInfo{
			Available: false,
			Error:     fmt.Sprintf("ipinfo request failed: %s (%s)", resp.Status, errMsg),
			UpdatedAt: time.Now(),
		}
		t.storeExitIPInfo(info)
		return info
	}

	var payload ipInfoResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&payload); err != nil {
		info := ExitIPInfo{
			Available: false,
			Error:     fmt.Sprintf("decode ipinfo response failed: %v", err),
			UpdatedAt: time.Now(),
		}
		t.storeExitIPInfo(info)
		return info
	}

	info := ExitIPInfo{
		Available:    true,
		IP:           strings.TrimSpace(payload.IP),
		City:         strings.TrimSpace(payload.City),
		Region:       strings.TrimSpace(payload.Region),
		Country:      strings.TrimSpace(payload.Country),
		Location:     strings.TrimSpace(payload.Loc),
		Organization: strings.TrimSpace(payload.Org),
		Timezone:     strings.TrimSpace(payload.Timezone),
		UpdatedAt:    time.Now(),
	}
	t.storeExitIPInfo(info)
	return info
}

func (t *Tunnel) storeExitIPInfo(info ExitIPInfo) {
	t.exitInfoMu.Lock()
	t.lastExitIPInfo = info
	t.exitInfoMu.Unlock()
}

func (t *Tunnel) resetExitIPInfo() {
	t.exitInfoMu.Lock()
	t.lastExitIPInfo = ExitIPInfo{}
	t.exitInfoMu.Unlock()
}

func (t *Tunnel) handleExitIPInfoFetchError(ctx context.Context, sshClient *ssh.Client, err error) {
	if err == nil || sshClient == nil || !isSSHReconnectError(err) {
		return
	}

	if !t.invalidateSSHClientIfMatch(sshClient, "fetch exit ip info failed: "+err.Error()) {
		return
	}

	safe.GO(func() {
		t.ReconnectSSHWithSource(t.reconnectContext(ctx), "ssh-exit-info")
	})
}
