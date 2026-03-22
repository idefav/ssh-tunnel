package handler

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"ssh-tunnel/buildinfo"
	"ssh-tunnel/cfg"
	"ssh-tunnel/safe"
	"ssh-tunnel/updater"
	"ssh-tunnel/views"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"
)

type versionPageDataV2 struct {
	IsServiceMode        bool
	CurrentVersion       string
	CurrentBuildTime     string
	CurrentChecksum      string
	InstallTime          string
	CurrentFileSize      string
	Platform             string
	Architecture         string
	AutoUpdateEnabled    bool
	CheckIntervalMinutes int
	GitHubOwner          string
	GitHubRepo           string
	Releases             []versionReleaseView
	LatestRelease        *versionReleaseView
	HasUpdate            bool
	UpdateCount          int
	LatestVersion        string
	ProxyEnabled         bool
	ProxyURL             string
	ProxyUsername        string
	ProxyPassword        string
	StatusMessage        string
	StatusMessageType    string
	HasPendingInstall    bool
	PendingInstallTag    string
	PendingInstallAsset  string
	PendingInstallAt     string
}

type versionReleaseView struct {
	TagName             string
	Name                string
	Body                string
	PublishedAt         string
	MatchingAsset       *versionAssetView
	IsNewer             bool
	IsCurrent           bool
	IsCached            bool
	IsInstalling        bool
	CanInstall          bool
	CacheStatus         string
	VerificationStatus  string
	ActiveDownloadID    string
	StatusText          string
	StatusClass         string
}

type versionAssetView struct {
	Name          string
	DownloadURL   string
	Size          string
	DownloadCount int
	FilePath      string
}

type versionDownloadTask struct {
	ID                string
	Key               string
	Version           string
	AssetName         string
	DownloadURL       string
	FilePath          string
	TotalSize         int64
	DownloadedSize    int64
	Progress          float64
	Status            string
	Error             string
	VerificationState string
	StartTime         time.Time
	ctx               context.Context
	cancel            context.CancelFunc
	mutex             sync.RWMutex
}

type versionDownloadManager struct {
	mutex      sync.RWMutex
	downloads  map[string]*versionDownloadTask
	activeKeys map[string]string
}

var versionDownloads = &versionDownloadManager{
	downloads:  make(map[string]*versionDownloadTask),
	activeKeys: make(map[string]string),
}

func showVersionViewV2(w http.ResponseWriter, _ *http.Request) {
	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml", "nav.gohtml", "version.gohtml")
	if err != nil {
		http.Error(w, "加载版本页失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := buildVersionPageDataV2()
	if err != nil {
		http.Error(w, "构建版本页失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "渲染版本页失败: "+err.Error(), http.StatusInternalServerError)
	}
}

func buildVersionPageDataV2() (versionPageDataV2, error) {
	appConfig := cfg.NewAppConfig()
	isServiceMode := !service.Interactive()
	currentVersion := buildinfo.CurrentVersion()
	currentChecksum, _ := getCurrentFileChecksum()
	fileInfo, _ := getCurrentFileInfo()

	manifest, err := updater.LoadManifest(appConfig.HomeDir.GetValue())
	if err != nil {
		return versionPageDataV2{}, err
	}
	manifest.CleanupMissingCacheFiles()
	_ = updater.SaveManifest(appConfig.HomeDir.GetValue(), manifest)

	releases, latestRelease, hasUpdate, updateCount, latestVersion := buildReleaseViews(currentVersion, isServiceMode, manifest)
	statusType := ""
	statusMessage := strings.TrimSpace(manifest.LastInstallMessage)
	if statusMessage != "" {
		statusType = "info"
		if strings.Contains(statusMessage, "未生效") {
			statusType = "warning"
		}
	}

	return versionPageDataV2{
		IsServiceMode:        isServiceMode,
		CurrentVersion:       currentVersion,
		CurrentBuildTime:     buildinfo.CurrentBuildTime(),
		CurrentChecksum:      currentChecksum,
		InstallTime:          fileInfo.InstallTime,
		CurrentFileSize:      fileInfo.FileSize,
		Platform:             runtime.GOOS,
		Architecture:         runtime.GOARCH,
		AutoUpdateEnabled:    appConfig.AutoUpdateEnabled.GetValue(),
		CheckIntervalMinutes: appConfig.AutoUpdateCheckInterval.GetValue() / 60,
		GitHubOwner:          appConfig.AutoUpdateOwner.GetValue(),
		GitHubRepo:           appConfig.AutoUpdateRepo.GetValue(),
		Releases:             releases,
		LatestRelease:        latestRelease,
		HasUpdate:            hasUpdate,
		UpdateCount:          updateCount,
		LatestVersion:        latestVersion,
		ProxyEnabled:         appConfig.DownloadProxyEnabled.GetValue(),
		ProxyURL:             appConfig.DownloadProxyURL.GetValue(),
		ProxyUsername:        appConfig.DownloadProxyUsername.GetValue(),
		ProxyPassword:        appConfig.DownloadProxyPassword.GetValue(),
		StatusMessage:        statusMessage,
		StatusMessageType:    statusType,
		HasPendingInstall:    manifest.PendingInstall != nil,
		PendingInstallTag:    pendingInstallTag(manifest),
		PendingInstallAsset:  pendingInstallAsset(manifest),
		PendingInstallAt:     pendingInstallAt(manifest),
	}, nil
}

func pendingInstallTag(manifest *updater.UpdateManifest) string {
	if manifest == nil || manifest.PendingInstall == nil {
		return ""
	}
	return manifest.PendingInstall.Version
}

func pendingInstallAsset(manifest *updater.UpdateManifest) string {
	if manifest == nil || manifest.PendingInstall == nil {
		return ""
	}
	return manifest.PendingInstall.AssetName
}

func pendingInstallAt(manifest *updater.UpdateManifest) string {
	if manifest == nil || manifest.PendingInstall == nil {
		return ""
	}
	return formatReleasePublishedAt(manifest.PendingInstall.RequestedAt)
}

func buildReleaseViews(currentVersion string, isServiceMode bool, manifest *updater.UpdateManifest) ([]versionReleaseView, *versionReleaseView, bool, int, string) {
	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance == nil {
		return nil, nil, false, 0, currentVersion
	}

	githubReleases, err := updaterInstance.GetReleases()
	if err != nil {
		return nil, nil, false, 0, currentVersion
	}

	releases := make([]versionReleaseView, 0, len(githubReleases))
	var latestRelease *versionReleaseView
	hasUpdate := false
	updateCount := 0
	latestVersion := currentVersion

	for _, release := range githubReleases {
		if release.Prerelease || release.Draft {
			continue
		}

		asset := updater.SelectAsset(release.Assets, isServiceMode, runtime.GOOS, runtime.GOARCH)
		releaseView := versionReleaseView{
			TagName:      release.TagName,
			Name:         release.Name,
			Body:         release.Body,
			PublishedAt:  formatReleasePublishedAt(release.PublishedAt),
			IsCurrent:    updater.NormalizeVersion(release.TagName) == updater.NormalizeVersion(currentVersion),
			IsNewer:      updater.IsNewerVersion(release.TagName, currentVersion),
			StatusClass:  "secondary",
			StatusText:   "旧版本",
			CacheStatus:  "uncached",
			VerificationStatus: updater.VerificationStatusUnknown,
		}
		if asset != nil {
			releaseView.MatchingAsset = &versionAssetView{
				Name:          asset.Name,
				DownloadURL:   asset.DownloadURL,
				Size:          formatFileSize(asset.Size),
				DownloadCount: asset.DownloadCount,
			}

			cacheState, ok := manifest.GetCachedAsset(release.TagName, asset.Name)
			if ok && isCacheUsable(cacheState) {
				releaseView.IsCached = true
				releaseView.CanInstall = !releaseView.IsCurrent
				releaseView.CacheStatus = "cached"
				releaseView.VerificationStatus = cacheState.VerificationStatus
				releaseView.MatchingAsset.FilePath = cacheState.FilePath
				if cacheState.VerificationStatus == updater.VerificationStatusVerified {
					releaseView.StatusText = "已缓存"
					releaseView.StatusClass = "success"
				} else {
					releaseView.StatusText = "未校验缓存"
					releaseView.StatusClass = "warning"
					releaseView.CacheStatus = "unverified"
				}
			}

			if task := versionDownloads.GetByKey(updater.CacheKey(release.TagName, asset.Name)); task != nil && task.Status == "downloading" {
				releaseView.ActiveDownloadID = task.ID
				releaseView.CacheStatus = "downloading"
				releaseView.StatusText = "下载中"
				releaseView.StatusClass = "info"
			}

			if manifest.PendingInstall != nil &&
				updater.NormalizeVersion(manifest.PendingInstall.Version) == updater.NormalizeVersion(release.TagName) &&
				strings.EqualFold(manifest.PendingInstall.AssetName, asset.Name) {
				releaseView.IsInstalling = true
				releaseView.CanInstall = false
				releaseView.CacheStatus = "installing"
				releaseView.StatusText = "安装中"
				releaseView.StatusClass = "warning"
			}
		}

		if releaseView.IsCurrent {
			releaseView.CanInstall = false
			releaseView.IsCached = false
			releaseView.CacheStatus = "current"
			releaseView.StatusText = "当前版本"
			releaseView.StatusClass = "primary"
		} else if releaseView.StatusText == "旧版本" && releaseView.IsNewer {
			releaseView.StatusText = "可更新"
			releaseView.StatusClass = "success"
		}

		releases = append(releases, releaseView)
		if latestRelease == nil || updater.IsNewerVersion(releaseView.TagName, latestRelease.TagName) {
			clone := releaseView
			latestRelease = &clone
			latestVersion = releaseView.TagName
		}
		if releaseView.IsNewer {
			hasUpdate = true
			updateCount++
		}
	}

	return releases, latestRelease, hasUpdate, updateCount, latestVersion
}

func checkForUpdatesV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance == nil {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "更新器未初始化"})
		return
	}

	release, hasUpdate := updaterInstance.CheckForUpdates()
	response := map[string]interface{}{
		"success":   true,
		"hasUpdate": hasUpdate,
	}
	if release != nil {
		response["latestVersion"] = release.TagName
		response["releaseName"] = release.Name
		response["releaseBody"] = release.Body
	}
	writeJSONResponse(w, response)
}

func downloadReleaseV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	version := strings.TrimSpace(r.FormValue("version"))
	fileName := strings.TrimSpace(r.FormValue("fileName"))
	downloadURL := strings.TrimSpace(r.FormValue("downloadUrl"))
	if version == "" || fileName == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少版本或文件名"})
		return
	}

	appConfig := cfg.NewAppConfig()
	manifest, err := updater.LoadManifest(appConfig.HomeDir.GetValue())
	if err != nil {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	manifest.CleanupMissingCacheFiles()

	cacheState, ok := manifest.GetCachedAsset(version, fileName)
	if ok && isCacheUsable(cacheState) {
		writeJSONResponse(w, map[string]interface{}{
			"success":            true,
			"cacheHit":           true,
			"cacheStatus":        cacheStateForView(cacheState.VerificationStatus),
			"verificationStatus": cacheState.VerificationStatus,
			"filePath":           cacheState.FilePath,
		})
		return
	}

	key := updater.CacheKey(version, fileName)
	if existing := versionDownloads.GetByKey(key); existing != nil && existing.Status == "downloading" {
		writeJSONResponse(w, map[string]interface{}{
			"success":            true,
			"cacheHit":           false,
			"cacheStatus":        "downloading",
			"downloadId":         existing.ID,
			"verificationStatus": existing.VerificationState,
			"filePath":           existing.FilePath,
		})
		return
	}

	if downloadURL == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少下载地址"})
		return
	}

	expectedChecksum, verificationStatus := resolveExpectedChecksum(version, fileName)
	finalPath := updater.CacheFilePath(appConfig.HomeDir.GetValue(), version, fileName)
	task := versionDownloads.Create(key, version, fileName, downloadURL, finalPath)
	task.mutex.Lock()
	task.VerificationState = verificationStatus
	task.mutex.Unlock()

	safe.GO(func() {
		runVersionDownload(task, expectedChecksum, verificationStatus, appConfig.HomeDir.GetValue())
	})

	writeJSONResponse(w, map[string]interface{}{
		"success":            true,
		"cacheHit":           false,
		"cacheStatus":        "downloading",
		"downloadId":         task.ID,
		"verificationStatus": verificationStatus,
		"filePath":           finalPath,
	})
}

func installCachedReleaseV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	version := strings.TrimSpace(r.FormValue("version"))
	fileName := strings.TrimSpace(r.FormValue("fileName"))
	if version == "" || fileName == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少版本或文件名"})
		return
	}

	message, err := installFromCache(version, fileName)
	if err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success":   false,
			"error":     err.Error(),
			"cacheMiss": strings.Contains(strings.ToLower(err.Error()), "cache"),
		})
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": message,
	})
}

func InstallCachedReleaseHandler(w http.ResponseWriter, r *http.Request) {
	installCachedReleaseV2(w, r)
}

func updateToVersionV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	version := strings.TrimSpace(r.FormValue("version"))
	fileName := strings.TrimSpace(r.FormValue("fileName"))
	downloadURL := strings.TrimSpace(r.FormValue("downloadUrl"))
	if version == "" || fileName == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少版本或文件名"})
		return
	}

	appConfig := cfg.NewAppConfig()
	manifest, err := updater.LoadManifest(appConfig.HomeDir.GetValue())
	if err != nil {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	manifest.CleanupMissingCacheFiles()

	cacheState, ok := manifest.GetCachedAsset(version, fileName)
	if ok && isCacheUsable(cacheState) {
		message, installErr := installFromCache(version, fileName)
		if installErr != nil {
			writeJSONResponse(w, map[string]interface{}{"success": false, "error": installErr.Error()})
			return
		}
		writeJSONResponse(w, map[string]interface{}{
			"success": true,
			"stage":   "installing",
			"message": message,
		})
		return
	}

	if downloadURL == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少下载地址"})
		return
	}

	key := updater.CacheKey(version, fileName)
	if existing := versionDownloads.GetByKey(key); existing != nil && existing.Status == "downloading" {
		writeJSONResponse(w, map[string]interface{}{
			"success":     true,
			"stage":       "downloading",
			"downloadId":  existing.ID,
			"autoInstall": true,
		})
		return
	}

	expectedChecksum, verificationStatus := resolveExpectedChecksum(version, fileName)
	finalPath := updater.CacheFilePath(appConfig.HomeDir.GetValue(), version, fileName)
	task := versionDownloads.Create(key, version, fileName, downloadURL, finalPath)
	task.mutex.Lock()
	task.VerificationState = verificationStatus
	task.mutex.Unlock()
	safe.GO(func() {
		runVersionDownload(task, expectedChecksum, verificationStatus, appConfig.HomeDir.GetValue())
	})

	writeJSONResponse(w, map[string]interface{}{
		"success":            true,
		"stage":              "downloading",
		"cacheHit":           false,
		"cacheStatus":        "downloading",
		"downloadId":         task.ID,
		"verificationStatus": verificationStatus,
		"autoInstall":        true,
	})
}

func cancelDownloadV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少下载 ID"})
		return
	}

	ok, message := versionDownloads.Cancel(id)
	if !ok {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": message})
		return
	}

	writeJSONResponse(w, map[string]interface{}{"success": true, "message": message})
}

func getDownloadProgressV2(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "缺少下载 ID"})
		return
	}

	task := versionDownloads.Get(id)
	if task == nil {
		writeJSONResponse(w, map[string]interface{}{"success": false, "error": "下载任务不存在"})
		return
	}

	task.mutex.RLock()
	data := map[string]interface{}{
		"success":            true,
		"id":                 task.ID,
		"fileName":           task.AssetName,
		"totalSize":          task.TotalSize,
		"downloadedSize":     task.DownloadedSize,
		"progress":           task.Progress,
		"status":             task.Status,
		"error":              task.Error,
		"startTime":          task.StartTime,
		"verificationStatus": task.VerificationState,
		"filePath":           task.FilePath,
	}
	task.mutex.RUnlock()

	writeJSONResponse(w, data)
}

func installFromCache(version, fileName string) (string, error) {
	appConfig := cfg.NewAppConfig()
	manifest, err := updater.LoadManifest(appConfig.HomeDir.GetValue())
	if err != nil {
		return "", err
	}
	manifest.CleanupMissingCacheFiles()

	cacheState, ok := manifest.GetCachedAsset(version, fileName)
	if !ok || !isCacheUsable(cacheState) {
		return "", fmt.Errorf("cache miss: 该版本尚未下载或缓存不可用")
	}

	currentExe, err := os.Executable()
	if err != nil {
		return "", err
	}

	stagedPath, err := createInstallStagingFile(appConfig.HomeDir.GetValue(), version, fileName, cacheState.FilePath)
	if err != nil {
		return "", err
	}

	backupPath := currentExe + ".backup." + time.Now().Format("20060102150405")
	if err := copyFile(currentExe, backupPath); err != nil {
		return "", err
	}

	manifest.PendingInstall = &updater.PendingInstallState{
		Version:     version,
		AssetName:   fileName,
		FilePath:    cacheState.FilePath,
		BackupPath:  backupPath,
		ServiceMode: !service.Interactive(),
		RequestedAt: time.Now().Format(time.RFC3339),
	}
	manifest.LastInstallMessage = ""
	if err := updater.SaveManifest(appConfig.HomeDir.GetValue(), manifest); err != nil {
		return "", err
	}

	if err := replaceExecutableV2(currentExe, stagedPath, !service.Interactive()); err != nil {
		manifest.PendingInstall = nil
		manifest.LastInstallMessage = "安装未启动: " + err.Error()
		_ = updater.SaveManifest(appConfig.HomeDir.GetValue(), manifest)
		return "", err
	}

	if runtime.GOOS == "windows" {
		if !service.Interactive() {
			safe.GO(func() {
				time.Sleep(2 * time.Second)
				os.Exit(0)
			})
			return "安装脚本已启动，服务将使用已缓存安装包完成替换并重新启动。", nil
		}
		safe.GO(func() {
			time.Sleep(2 * time.Second)
			os.Exit(0)
		})
		return "安装脚本已启动，程序退出后将使用已缓存安装包完成替换，请随后手动重新启动。", nil
	}

	if !service.Interactive() {
		safe.GO(func() {
			time.Sleep(2 * time.Second)
			os.Exit(0)
		})
		return "安装已完成，服务进程即将退出并重新启动。", nil
	}
	return "安装已完成，请重新启动程序以运行新版本。", nil
}

func runVersionDownload(task *versionDownloadTask, expectedChecksum, verificationStatus, homeDir string) {
	partPath := task.FilePath + ".part"
	_ = os.MkdirAll(filepath.Dir(task.FilePath), 0755)
	_ = os.Remove(partPath)

	if err := downloadFileToPath(task, partPath); err != nil {
		if isDownloadCanceledError(err) {
			versionDownloads.SetStatus(task.ID, "cancelled", "")
			_ = os.Remove(partPath)
			return
		}
		versionDownloads.SetStatus(task.ID, "error", err.Error())
		_ = os.Remove(partPath)
		return
	}

	actualChecksum := ""
	if expectedChecksum != "" {
		actualChecksum, _ = calculateFileChecksum(partPath)
		if !strings.EqualFold(actualChecksum, expectedChecksum) {
			versionDownloads.SetStatus(task.ID, "error", "SHA256 校验失败")
			_ = os.Remove(partPath)
			return
		}
		verificationStatus = updater.VerificationStatusVerified
	}

	if err := os.Rename(partPath, task.FilePath); err != nil {
		versionDownloads.SetStatus(task.ID, "error", err.Error())
		_ = os.Remove(partPath)
		return
	}

	info, err := os.Stat(task.FilePath)
	if err != nil {
		versionDownloads.SetStatus(task.ID, "error", err.Error())
		return
	}

	manifest, err := updater.LoadManifest(homeDir)
	if err != nil {
		versionDownloads.SetStatus(task.ID, "error", err.Error())
		return
	}

	manifest.SetCachedAsset(updater.CachedAssetState{
		Version:            task.Version,
		AssetName:          task.AssetName,
		FilePath:           task.FilePath,
		FileSize:           info.Size(),
		DownloadURL:        task.DownloadURL,
		VerificationStatus: verificationStatus,
		ExpectedChecksum:   expectedChecksum,
		ActualChecksum:     actualChecksum,
		DownloadedAt:       time.Now().Format(time.RFC3339),
	})
	if err := updater.SaveManifest(homeDir, manifest); err != nil {
		versionDownloads.SetStatus(task.ID, "error", err.Error())
		return
	}

	versionDownloads.Complete(task.ID, verificationStatus)
}

func resolveExpectedChecksum(version, fileName string) (string, string) {
	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance == nil {
		return "", updater.VerificationStatusUnverified
	}

	releases, err := updaterInstance.GetReleases()
	if err != nil {
		return "", updater.VerificationStatusUnverified
	}
	release := updater.FindReleaseByTag(releases, version)
	if release == nil {
		return "", updater.VerificationStatusUnverified
	}
	checksumAsset := updater.FindChecksumAsset(release.Assets)
	if checksumAsset == nil {
		return "", updater.VerificationStatusUnverified
	}

	content, err := downloadTextFile(checksumAsset.DownloadURL)
	if err != nil {
		return "", updater.VerificationStatusUnverified
	}
	checksum := updater.ParseChecksumFile(content)[fileName]
	if checksum == "" {
		return "", updater.VerificationStatusUnverified
	}
	return checksum, updater.VerificationStatusUnknown
}

func createInstallStagingFile(homeDir, version, fileName, cachePath string) (string, error) {
	stagingDir := filepath.Join(updater.UpdateRoot(homeDir), "staging", strings.TrimPrefix(updater.NormalizeVersion(version), "v"))
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return "", err
	}
	stagedPath := filepath.Join(stagingDir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), fileName))
	if err := copyFile(cachePath, stagedPath); err != nil {
		return "", err
	}
	return stagedPath, nil
}

func replaceExecutableV2(currentExe, stagedPath string, isServiceMode bool) error {
	if runtime.GOOS != "windows" {
		return os.Rename(stagedPath, currentExe)
	}
	return replaceExecutableWindowsV2(currentExe, stagedPath, isServiceMode)
}

func replaceExecutableWindowsV2(currentExe, stagedPath string, isServiceMode bool) error {
	dir := filepath.Dir(currentExe)
	baseName := filepath.Base(currentExe)
	batchFile := filepath.Join(dir, "update_"+time.Now().Format("20060102150405")+".bat")

	var batchContent string
	if isServiceMode {
		batchContent = fmt.Sprintf(`@echo off
timeout /t 3 /nobreak >nul
if exist "%s.backup" del "%s.backup"
if exist "%s" ren "%s" "%s.backup"
move /Y "%s" "%s"
sc start SSHTunnelService
del "%%~f0"
`, currentExe, currentExe, currentExe, baseName, baseName, stagedPath, currentExe)
	} else {
		batchContent = fmt.Sprintf(`@echo off
timeout /t 3 /nobreak >nul
if exist "%s.backup" del "%s.backup"
if exist "%s" ren "%s" "%s.backup"
move /Y "%s" "%s"
echo Update completed successfully.
echo Please restart the SSH Tunnel manually.
pause
del "%%~f0"
`, currentExe, currentExe, currentExe, baseName, baseName, stagedPath, currentExe)
	}

	if err := os.WriteFile(batchFile, []byte(batchContent), 0755); err != nil {
		return err
	}

	cmd := exec.Command("cmd", "/c", "start", "/min", batchFile)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		_ = os.Remove(batchFile)
		return err
	}
	return nil
}

func downloadFileToPath(task *versionDownloadTask, targetPath string) error {
	client := newVersionHTTPClient()

	requestCtx := context.Background()
	if task.ctx != nil {
		requestCtx = task.ctx
	}

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, task.DownloadURL, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer out.Close()

	task.mutex.Lock()
	task.TotalSize = resp.ContentLength
	task.mutex.Unlock()

	reader := &ProgressReader{
		Reader: resp.Body,
		OnProgress: func(downloaded int64) {
			versionDownloads.UpdateProgress(task.ID, downloaded)
		},
	}
	_, err = io.Copy(out, reader)
	return err
}

func downloadTextFile(downloadURL string) (string, error) {
	client := newVersionHTTPClient()
	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func newVersionHTTPClient() *http.Client {
	appConfig := cfg.NewAppConfig()
	client := &http.Client{Timeout: 30 * time.Minute}
	if appConfig.DownloadProxyEnabled.GetValue() && appConfig.DownloadProxyURL.GetValue() != "" {
		proxyURL, err := url.Parse(appConfig.DownloadProxyURL.GetValue())
		if err == nil {
			if appConfig.DownloadProxyUsername.GetValue() != "" {
				proxyURL.User = url.UserPassword(
					appConfig.DownloadProxyUsername.GetValue(),
					appConfig.DownloadProxyPassword.GetValue(),
				)
			}
			client.Transport = &http.Transport{
				Proxy:           http.ProxyURL(proxyURL),
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		}
	}
	return client
}

func calculateFileChecksum(filePath string) (string, error) {
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

func isCacheUsable(state updater.CachedAssetState) bool {
	if strings.TrimSpace(state.FilePath) == "" {
		return false
	}
	if state.VerificationStatus == updater.VerificationStatusFailed {
		return false
	}
	if _, err := os.Stat(state.FilePath); err != nil {
		return false
	}
	return true
}

func cacheStateForView(verificationStatus string) string {
	if verificationStatus == updater.VerificationStatusVerified {
		return "cached"
	}
	if verificationStatus == updater.VerificationStatusUnverified {
		return "unverified"
	}
	return "uncached"
}

func (m *versionDownloadManager) Create(key, version, assetName, downloadURL, filePath string) *versionDownloadTask {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if id, ok := m.activeKeys[key]; ok {
		if existing := m.downloads[id]; existing != nil {
			return existing
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	task := &versionDownloadTask{
		ID:                fmt.Sprintf("download_%d", time.Now().UnixNano()),
		Key:               key,
		Version:           version,
		AssetName:         assetName,
		DownloadURL:       downloadURL,
		FilePath:          filePath,
		Status:            "downloading",
		VerificationState: updater.VerificationStatusUnknown,
		StartTime:         time.Now(),
		ctx:               ctx,
		cancel:            cancel,
	}
	m.downloads[task.ID] = task
	m.activeKeys[key] = task.ID
	return task
}

func (m *versionDownloadManager) Get(id string) *versionDownloadTask {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.downloads[id]
}

func (m *versionDownloadManager) GetByKey(key string) *versionDownloadTask {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	if id, ok := m.activeKeys[key]; ok {
		return m.downloads[id]
	}
	return nil
}

func (m *versionDownloadManager) UpdateProgress(id string, downloaded int64) {
	task := m.Get(id)
	if task == nil {
		return
	}
	task.mutex.Lock()
	defer task.mutex.Unlock()
	task.DownloadedSize = downloaded
	if task.TotalSize > 0 {
		task.Progress = float64(downloaded) / float64(task.TotalSize) * 100
	}
}

func (m *versionDownloadManager) SetStatus(id, status, errMessage string) {
	task := m.Get(id)
	if task == nil {
		return
	}
	task.mutex.Lock()
	task.Status = status
	task.Error = errMessage
	task.mutex.Unlock()

	if status == "completed" || status == "error" || status == "cancelled" {
		m.mutex.Lock()
		if currentID, ok := m.activeKeys[task.Key]; ok && currentID == id {
			delete(m.activeKeys, task.Key)
		}
		m.mutex.Unlock()
	}
}

func (m *versionDownloadManager) Complete(id, verificationStatus string) {
	task := m.Get(id)
	if task == nil {
		return
	}
	task.mutex.Lock()
	task.Status = "completed"
	task.Progress = 100
	task.VerificationState = verificationStatus
	task.mutex.Unlock()

	m.mutex.Lock()
	if currentID, ok := m.activeKeys[task.Key]; ok && currentID == id {
		delete(m.activeKeys, task.Key)
	}
	m.mutex.Unlock()
}

func (m *versionDownloadManager) Cancel(id string) (bool, string) {
	task := m.Get(id)
	if task == nil {
		return false, "下载任务不存在"
	}

	task.mutex.Lock()
	defer task.mutex.Unlock()
	if task.Status != "downloading" {
		return false, "当前任务不可取消"
	}
	task.Status = "cancelled"
	task.Error = "已取消下载"
	if task.cancel != nil {
		task.cancel()
	}

	m.mutex.Lock()
	if currentID, ok := m.activeKeys[task.Key]; ok && currentID == id {
		delete(m.activeKeys, task.Key)
	}
	m.mutex.Unlock()

	return true, "已取消本次下载"
}
