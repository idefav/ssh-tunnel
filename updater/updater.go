package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	GITHUB_API_URL = "https://api.github.com/repos/%s/%s/releases"
	USER_AGENT     = "SSH-Tunnel-Updater/1.0"
)

type Release struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
	PublishedAt string `json:"published_at"`
	Assets      []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	DownloadURL        string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	ContentType        string `json:"content_type"`
	DownloadCount      int    `json:"download_count"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type UpdaterConfig struct {
	Enabled         bool
	Owner           string
	Repo            string
	CurrentVersion  string
	CheckInterval   time.Duration
	AutoDownload    bool
	AutoInstall     bool
}

type Updater struct {
	config    *UpdaterConfig
	mu        sync.RWMutex
	ticker    *time.Ticker
	stopChan  chan bool
	isRunning bool
	onUpdate  func(release *Release)
}

func NewUpdater(config *UpdaterConfig) *Updater {
	return &Updater{
		config:   config,
		stopChan: make(chan bool),
	}
}

// Start 启动自动更新检查
func (u *Updater) Start() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.config.Enabled || u.isRunning {
		return
	}

	u.ticker = time.NewTicker(u.config.CheckInterval)
	u.isRunning = true

	go u.checkLoop()
}

// Stop 停止自动更新检查
func (u *Updater) Stop() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.isRunning {
		return
	}

	u.stopChan <- true
	if u.ticker != nil {
		u.ticker.Stop()
	}
	u.isRunning = false
}

// checkLoop 定时检查更新的循环
func (u *Updater) checkLoop() {
	for {
		select {
		case <-u.ticker.C:
			if release, hasUpdate := u.CheckForUpdates(); hasUpdate {
				log.Printf("发现新版本: %s", release.TagName)
				if u.onUpdate != nil {
					u.onUpdate(release)
				}
			}
		case <-u.stopChan:
			return
		}
	}
}

// CheckForUpdates 检查是否有新版本
func (u *Updater) CheckForUpdates() (*Release, bool) {
	releases, err := u.GetReleases()
	if err != nil {
		log.Printf("检查更新失败: %v", err)
		return nil, false
	}

	if len(releases) == 0 {
		return nil, false
	}

	latestRelease := releases[0]
	
	// 跳过预发布版本和草稿版本
	for _, release := range releases {
		if !release.Prerelease && !release.Draft {
			latestRelease = release
			break
		}
	}

	if u.isNewerVersion(latestRelease.TagName, u.config.CurrentVersion) {
		return &latestRelease, true
	}

	return nil, false
}

// GetReleases 获取所有发布版本
func (u *Updater) GetReleases() ([]Release, error) {
	url := fmt.Sprintf(GITHUB_API_URL, u.config.Owner, u.config.Repo)
	
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API请求失败: %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	return releases, nil
}

// DownloadRelease 下载指定版本
func (u *Updater) DownloadRelease(release *Release, targetDir string) (string, error) {
	asset := u.findAssetForCurrentPlatform(release.Assets)
	if asset == nil {
		return "", fmt.Errorf("未找到适合当前平台的安装包")
	}

	filename := filepath.Join(targetDir, asset.Name)
	
	// 下载文件
	if err := u.downloadFile(asset.DownloadURL, filename); err != nil {
		return "", fmt.Errorf("下载失败: %v", err)
	}

	return filename, nil
}

// VerifyChecksum 验证文件SHA256校验和
func (u *Updater) VerifyChecksum(filePath, expectedChecksum string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}

	actualChecksum := hex.EncodeToString(hash.Sum(nil))
	return strings.EqualFold(actualChecksum, expectedChecksum), nil
}

// GetFileChecksum 计算文件的SHA256校验和
func (u *Updater) GetFileChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// SetUpdateCallback 设置更新回调函数
func (u *Updater) SetUpdateCallback(callback func(release *Release)) {
	u.onUpdate = callback
}

// IsEnabled 检查是否启用自动更新
func (u *Updater) IsEnabled() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.config.Enabled
}

// SetEnabled 设置是否启用自动更新
func (u *Updater) SetEnabled(enabled bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	
	u.config.Enabled = enabled
	
	if enabled && !u.isRunning {
		go u.Start()
	} else if !enabled && u.isRunning {
		u.Stop()
	}
}

// isNewerVersion 比较版本号，判断是否为更新版本
func (u *Updater) isNewerVersion(newVersion, currentVersion string) bool {
	// 移除版本号前缀 v
	newVersion = strings.TrimPrefix(newVersion, "v")
	currentVersion = strings.TrimPrefix(currentVersion, "v")
	
	// 简单的版本比较，可以根据需要改进
	return newVersion != currentVersion && newVersion > currentVersion
}

// findAssetForCurrentPlatform 根据当前平台查找合适的资源文件
func (u *Updater) findAssetForCurrentPlatform(assets []Asset) *Asset {
	osName := runtime.GOOS
	archName := runtime.GOARCH
	
	// 构建平台标识符
	platformIdentifiers := []string{
		fmt.Sprintf("%s-%s", osName, archName),
		fmt.Sprintf("%s_%s", osName, archName),
		osName,
	}
	
	// 查找匹配的资源文件
	for _, asset := range assets {
		assetNameLower := strings.ToLower(asset.Name)
		for _, platform := range platformIdentifiers {
			if strings.Contains(assetNameLower, strings.ToLower(platform)) {
				return &asset
			}
		}
	}
	
	return nil
}

// downloadFile 下载文件
func (u *Updater) downloadFile(url, filepath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	
	req.Header.Set("User-Agent", USER_AGENT)

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
