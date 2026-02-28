package tunnel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime"
	"ssh-tunnel/safe"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SpeedTestStatus 速测状态的 JSON 输出
type SpeedTestStatus struct {
	Running        bool    `json:"running"`
	ElapsedSec     float64 `json:"elapsedSec"`
	TotalSec       int     `json:"totalSec"`
	LatencySamples []int64 `json:"latencySamples"`
	AvgLatencyMs   float64 `json:"avgLatencyMs"`
	MinLatencyMs   int64   `json:"minLatencyMs"`
	MaxLatencyMs   int64   `json:"maxLatencyMs"`
	DownloadBytes  int64   `json:"downloadBytes"`
	DownloadBps    float64 `json:"downloadBps"`
	DownloadURL    string  `json:"downloadUrl,omitempty"`
	FileSize       int64   `json:"fileSize,omitempty"`
	Message        string  `json:"message"`
}

// speedTest 内部速测状态
type speedTest struct {
	mu            sync.Mutex
	running       bool
	startTime     time.Time
	totalDuration time.Duration
	ctx           context.Context
	cancel        context.CancelFunc

	latencySamples []int64
	downloadBytes  int64 // accessed with atomic
	downloadURL    string
	fileSize       int64
	message        string
	downloadFailed bool
}

// StartSpeedTest 启动一次速度测试，durationSec 为持续秒数
func (t *Tunnel) StartSpeedTest(durationSec int) error {
	t.speedTestMu.Lock()
	defer t.speedTestMu.Unlock()

	if t.activeSpeedTest != nil && t.activeSpeedTest.running {
		return errors.New("speed test already running")
	}

	if durationSec <= 0 {
		durationSec = 60
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(durationSec)*time.Second)

	st := &speedTest{
		running:       true,
		startTime:     time.Now(),
		totalDuration: time.Duration(durationSec) * time.Second,
		ctx:           ctx,
		cancel:        cancel,
		message:       "正在初始化测试...",
	}
	t.activeSpeedTest = st

	// 延迟采样协程（通过 SSH 隧道连接 github.com 测延迟）
	safe.GO(func() {
		t.speedTestLatencyLoop(st)
	})

	// 下载带宽测试协程（通过 SSH 隧道下载 GitHub 最新 release）
	safe.GO(func() {
		t.speedTestDownloadLoop(st)
	})

	return nil
}

// StopSpeedTest 手动停止当前速测
func (t *Tunnel) StopSpeedTest() {
	t.speedTestMu.Lock()
	st := t.activeSpeedTest
	t.speedTestMu.Unlock()

	if st != nil {
		st.cancel()
	}
}

// GetSpeedTestStatus 获取当前速测状态快照
func (t *Tunnel) GetSpeedTestStatus() *SpeedTestStatus {
	t.speedTestMu.Lock()
	st := t.activeSpeedTest
	t.speedTestMu.Unlock()

	if st == nil {
		return &SpeedTestStatus{Running: false, Message: "未启动测试"}
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	elapsed := time.Since(st.startTime).Seconds()
	dlBytes := atomic.LoadInt64(&st.downloadBytes)

	samples := make([]int64, len(st.latencySamples))
	copy(samples, st.latencySamples)

	var avgLatency float64
	var minLatency, maxLatency int64
	if len(samples) > 0 {
		var sum int64
		minLatency = samples[0]
		maxLatency = samples[0]
		for _, s := range samples {
			sum += s
			if s < minLatency {
				minLatency = s
			}
			if s > maxLatency {
				maxLatency = s
			}
		}
		avgLatency = float64(sum) / float64(len(samples))
	}

	var dlBps float64
	if elapsed > 0 {
		dlBps = float64(dlBytes) / elapsed
	}

	return &SpeedTestStatus{
		Running:        st.running,
		ElapsedSec:     elapsed,
		TotalSec:       int(st.totalDuration.Seconds()),
		LatencySamples: samples,
		AvgLatencyMs:   avgLatency,
		MinLatencyMs:   minLatency,
		MaxLatencyMs:   maxLatency,
		DownloadBytes:  dlBytes,
		DownloadBps:    dlBps,
		DownloadURL:    st.downloadURL,
		FileSize:       st.fileSize,
		Message:        st.message,
	}
}

// ======================== 内部实现 ========================

// sshHTTPClient 构建一个通过 SSH 隧道访问外网的 HTTP Client
func (t *Tunnel) sshHTTPClient(timeout time.Duration) (*http.Client, error) {
	sshClient := t.GetSSHClient()
	if sshClient == nil {
		return nil, errors.New("SSH client not connected")
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return sshClient.Dial(network, addr)
		},
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: false,
		},
		// TLS 也需要通过 SSH 隧道
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// 先通过 SSH 建立 TCP 连接
			rawConn, err := sshClient.Dial(network, addr)
			if err != nil {
				return nil, err
			}
			// 在 TCP 连接之上建立 TLS
			host, _, _ := net.SplitHostPort(addr)
			tlsConn := tls.Client(rawConn, &tls.Config{
				ServerName: host,
			})
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				rawConn.Close()
				return nil, err
			}
			return tlsConn, nil
		},
		MaxIdleConns:       1,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: true,
	}

	client := &http.Client{
		Transport: transport,
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	return client, nil
}

// measureExternalLatency 通过 SSH 隧道连接 github.com 测量 TCP 建连延迟
func (t *Tunnel) measureExternalLatency() (int64, error) {
	sshClient := t.GetSSHClient()
	if sshClient == nil {
		return 0, errors.New("SSH client not connected")
	}

	start := time.Now()
	conn, err := sshClient.Dial("tcp", "github.com:443")
	if err != nil {
		return 0, err
	}
	latency := time.Since(start).Milliseconds()
	conn.Close()
	return latency, nil
}

func (t *Tunnel) speedTestLatencyLoop(st *speedTest) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// 立即采样一次
	t.sampleExternalLatency(st)

	for {
		select {
		case <-st.ctx.Done():
			return
		case <-ticker.C:
			t.sampleExternalLatency(st)
		}
	}
}

func (t *Tunnel) sampleExternalLatency(st *speedTest) {
	latencyMs, err := t.measureExternalLatency()
	if err != nil {
		log.Printf("speed test external latency sample failed: %v", err)
		return
	}
	st.mu.Lock()
	st.latencySamples = append(st.latencySamples, latencyMs)
	st.mu.Unlock()
}

// githubRelease (minimal) for parsing releases API
type githubReleaseForTest struct {
	TagName string               `json:"tag_name"`
	Assets  []githubAssetForTest `json:"assets"`
}

type githubAssetForTest struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

// findGitHubDownloadURL 通过 SSH 隧道查询 GitHub latest release，找到当前平台的安装包 URL
func (t *Tunnel) findGitHubDownloadURL(st *speedTest) (string, int64, error) {
	owner := "idefav"
	repo := "ssh-tunnel"
	if t.appConfig != nil {
		if v := t.appConfig.AutoUpdateOwner.GetValue(); v != "" {
			owner = v
		}
		if v := t.appConfig.AutoUpdateRepo.GetValue(); v != "" {
			repo = v
		}
	}

	st.mu.Lock()
	st.message = "正在通过 SSH 查询 GitHub 最新版本..."
	st.mu.Unlock()

	httpClient, err := t.sshHTTPClient(30 * time.Second)
	if err != nil {
		return "", 0, err
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(st.ctx, "GET", apiURL, nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("User-Agent", "SSH-Tunnel-SpeedTest/1.0")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("GitHub API 请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("GitHub API 返回 %d: %s", resp.StatusCode, string(body)[:min(200, len(body))])
	}

	var release githubReleaseForTest
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", 0, fmt.Errorf("解析 release 失败: %v", err)
	}

	// 查找当前平台匹配的 asset
	osName := runtime.GOOS
	archName := runtime.GOARCH
	platforms := []string{
		fmt.Sprintf("%s-%s", osName, archName),
		fmt.Sprintf("%s_%s", osName, archName),
	}

	for _, asset := range release.Assets {
		nameLower := strings.ToLower(asset.Name)
		for _, p := range platforms {
			if strings.Contains(nameLower, strings.ToLower(p)) {
				log.Printf("speed test: found asset %s (%d bytes) from release %s", asset.Name, asset.Size, release.TagName)
				return asset.DownloadURL, asset.Size, nil
			}
		}
	}

	// 如果没找到匹配平台的，选第一个非 SHA256SUMS 的 asset
	for _, asset := range release.Assets {
		if !strings.Contains(strings.ToLower(asset.Name), "sha256") {
			log.Printf("speed test: using fallback asset %s (%d bytes)", asset.Name, asset.Size)
			return asset.DownloadURL, asset.Size, nil
		}
	}

	return "", 0, fmt.Errorf("release %s 没有可下载资源", release.TagName)
}

func (t *Tunnel) speedTestDownloadLoop(st *speedTest) {
	sshClient := t.GetSSHClient()
	if sshClient == nil {
		t.finishSpeedTestWithMsg(st, true, "SSH 未连接，仅测试延迟")
		return
	}

	// Step 1: 查询 GitHub 最新 release 资源
	downloadURL, fileSize, err := t.findGitHubDownloadURL(st)
	if err != nil {
		log.Printf("speed test: find GitHub URL failed: %v", err)
		t.finishSpeedTestWithMsg(st, true, "无法获取 GitHub 下载链接: "+err.Error())
		return
	}

	st.mu.Lock()
	st.downloadURL = downloadURL
	st.fileSize = fileSize
	st.message = "正在通过 SSH 隧道下载 GitHub 资源..."
	st.mu.Unlock()

	// Step 2: 通过 SSH 隧道下载该文件，循环下载直到测试时间结束
	t.downloadViaSSH(st, downloadURL)

	st.mu.Lock()
	st.running = false
	if st.downloadFailed {
		st.message = "测试完成（仅延迟）"
	} else {
		st.message = "测试完成"
	}
	st.mu.Unlock()
}

// downloadViaSSH 通过 SSH 隧道 HTTP 下载文件，持续到 ctx 结束，可循环下载
func (t *Tunnel) downloadViaSSH(st *speedTest, downloadURL string) {
	httpClient, err := t.sshHTTPClient(0)
	if err != nil {
		st.mu.Lock()
		st.downloadFailed = true
		st.mu.Unlock()
		return
	}
	// GitHub release 下载 URL 会 302 到 CDN, 需要 follow redirect
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	}

	buf := make([]byte, 64*1024)

	for {
		if st.ctx.Err() != nil {
			return
		}

		req, err := http.NewRequestWithContext(st.ctx, "GET", downloadURL, nil)
		if err != nil {
			log.Printf("speed test download: create request failed: %v", err)
			st.mu.Lock()
			st.downloadFailed = true
			st.mu.Unlock()
			return
		}
		req.Header.Set("User-Agent", "SSH-Tunnel-SpeedTest/1.0")

		resp, err := httpClient.Do(req)
		if err != nil {
			if st.ctx.Err() != nil {
				return // 正常超时结束
			}
			log.Printf("speed test download: HTTP request failed: %v", err)
			st.mu.Lock()
			st.downloadFailed = true
			st.mu.Unlock()
			return
		}

		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				atomic.AddInt64(&st.downloadBytes, int64(n))
			}
			if readErr != nil {
				break
			}
		}
		resp.Body.Close()

		if st.ctx.Err() != nil {
			return
		}

		// 文件下载完但测试时间未到，重新下载
		log.Printf("speed test: download round completed, restarting for continued measurement")
	}
}

// finishSpeedTestWithMsg 下载失败时等待 context 结束再标记完成
func (t *Tunnel) finishSpeedTestWithMsg(st *speedTest, downloadFailed bool, msg string) {
	st.mu.Lock()
	st.downloadFailed = downloadFailed
	st.message = msg
	st.mu.Unlock()

	<-st.ctx.Done()

	st.mu.Lock()
	st.running = false
	st.message = "测试完成（仅延迟）"
	st.mu.Unlock()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
