package handler

import (
	_ "embed"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/kardianos/service"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"ssh-tunnel/cfg"
	"ssh-tunnel/constants"
	tunnel2 "ssh-tunnel/tunnel"
	"ssh-tunnel/updater"
	"ssh-tunnel/views"
	"strings"
	"sync"
	"time"
)

type Data struct {
	Domains                map[string]bool
	DomainMatchResultCache map[string]bool
}

type SSHClientState struct {
	Version    string
	LocalAddr  net.Addr
	RemoteAddr net.Addr
	SessionID  string
	User       string
}

func ListStaticFiles(w http.ResponseWriter, r *http.Request) {
	// 明确指定 charset=utf-8
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// 添加完整的 HTML 结构并确保 UTF-8 编码
	fmt.Fprintln(w, "<!DOCTYPE html>")
	fmt.Fprintln(w, "<html>")
	fmt.Fprintln(w, "<head>")
	fmt.Fprintln(w, "<meta charset=\"utf-8\">")
	fmt.Fprintln(w, "<title>StaticFs中的文件列表</title>")
	fmt.Fprintln(w, "</head>")
	fmt.Fprintln(w, "<body>")

	fmt.Fprintln(w, "<h1>StaticFs中的文件列表</h1>")
	fmt.Fprintln(w, "<ul>")

	fs.WalkDir(views.StaticFs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			// 将每个文件名转换为可点击的链接
			fmt.Fprintf(w, "<li><a href=\"/%s\">%s</a></li>\n", path, path)
		}
		return nil
	})

	fmt.Fprintln(w, "</ul>")
	fmt.Fprintln(w, "</body>")
	fmt.Fprintln(w, "</html>")
}

// ViewStaticFile 显示特定静态文件的内容
func ViewStaticFile(w http.ResponseWriter, r *http.Request) {
	// 从URL中提取文件路径
	vars := mux.Vars(r)
	filePath := "resources/" + vars["filepath"]

	// 读取文件内容
	content, err := fs.ReadFile(views.StaticFs, filePath)
	if err != nil {
		http.Error(w, "文件不存在或无法读取: "+err.Error(), http.StatusNotFound)
		return
	}

	// 设置适当的Content-Type
	contentType := http.DetectContentType(content)
	w.Header().Set("Content-Type", contentType)

	// 输出文件内容
	w.Write(content)
}

func ShowIndexView(response http.ResponseWriter, request *http.Request) {

	var tunnel = tunnel2.DefaultSshTunnel

	var data = Data{
		Domains:                tunnel.Domains(),
		DomainMatchResultCache: tunnel.DomainMatchCache(),
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"home.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

func ShowDomainsView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel

	var data = Data{
		Domains:                tunnel.Domains(),
		DomainMatchResultCache: tunnel.DomainMatchCache(),
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"domains.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

func ShowCacheView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel

	var data = Data{
		Domains:                tunnel.Domains(),
		DomainMatchResultCache: tunnel.DomainMatchCache(),
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"cache.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

func ShowSSHClientStateView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel
	client := tunnel.GetSSHClient()
	version := client.ClientVersion()
	addr := client.LocalAddr()
	remoteAddr := client.RemoteAddr()
	id := client.SessionID()
	user := client.User()

	marshal, _ := json.Marshal(id)

	var data = SSHClientState{
		Version:    string(version),
		LocalAddr:  addr,
		RemoteAddr: remoteAddr,
		SessionID:  string(marshal),
		User:       user,
	}

	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"ssh_state.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, data)
}

type AppConfig struct {
	ConfigFilePath   string
	Config           map[string]interface{}
	ConfigMeta       map[string]ConfigMetadata
	ConfigKeys       map[string]string // 添加实际配置键映射
	ExecutablePath   string            // 程序执行路径
	WorkingDirectory string            // 工作目录
}

type ConfigMetadata struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Required    bool   `json:"required"`
	ActualKey   string `json:"actualKey"` // 添加实际配置键
}

func ShowAppConfigView(response http.ResponseWriter, request *http.Request) {
	var tunnel = tunnel2.DefaultSshTunnel
	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"app_config.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	// 获取应用配置
	appConfig := tunnel.AppConfig()
	
	// 手动提取配置值，而不是序列化整个ConfigItem结构
	data := map[string]interface{}{
		"ServerIp":                 appConfig.ServerIp.GetValue(),
		"ServerSshPort":            appConfig.ServerSshPort.GetValue(),
		"LoginUser":                appConfig.LoginUser.GetValue(),
		"SshPrivateKeyPath":        appConfig.SshPrivateKeyPath.GetValue(),
		"SshKnownHostsPath":        appConfig.SshKnownHostsPath.GetValue(),
		"LocalAddress":             appConfig.LocalAddress.GetValue(),
		"HttpLocalAddress":         appConfig.HttpLocalAddress.GetValue(),
		"EnableHttp":               appConfig.EnableHttp.GetValue(),
		"EnableSocks5":             appConfig.EnableSocks5.GetValue(),
		"EnableHttpOverSSH":        appConfig.EnableHttpOverSSH.GetValue(),
		"HttpBasicAuthEnable":      appConfig.HttpBasicAuthEnable.GetValue(),
		"HttpBasicUserName":        appConfig.HttpBasicUserName.GetValue(),
		"HttpBasicPassword":        appConfig.HttpBasicPassword.GetValue(),
		"EnableHttpDomainFilter":   appConfig.EnableHttpDomainFilter.GetValue(),
		"HttpDomainFilterFilePath": appConfig.HttpDomainFilterFilePath.GetValue(),
		"EnableAdmin":              appConfig.EnableAdmin.GetValue(),
		"AdminAddress":             appConfig.AdminAddress.GetValue(),
		"RetryIntervalSec":         appConfig.RetryIntervalSec.GetValue(),
		"LogFilePath":              appConfig.LogFilePath.GetValue(),
		"HomeDir":                  appConfig.HomeDir.GetValue(),
	}

	// 定义配置项元数据（包含实际配置键）
	configMeta := map[string]ConfigMetadata{
		"ServerIp":                 {Type: "string", Description: "SSH服务器IP地址", Category: "服务器配置", Required: true, ActualKey: appConfig.ServerIp.Key},
		"ServerSshPort":            {Type: "int", Description: "SSH服务器端口", Category: "服务器配置", Required: true, ActualKey: appConfig.ServerSshPort.Key},
		"LoginUser":                {Type: "string", Description: "SSH登录用户名", Category: "服务器配置", Required: true, ActualKey: appConfig.LoginUser.Key},
		"SshPrivateKeyPath":        {Type: "string", Description: "SSH私钥文件路径", Category: "SSH配置", Required: true, ActualKey: appConfig.SshPrivateKeyPath.Key},
		"SshKnownHostsPath":        {Type: "string", Description: "SSH已知主机文件路径", Category: "SSH配置", Required: false, ActualKey: appConfig.SshKnownHostsPath.Key},
		"LocalAddress":             {Type: "string", Description: "本地SOCKS5代理监听地址", Category: "代理配置", Required: true, ActualKey: appConfig.LocalAddress.Key},
		"HttpLocalAddress":         {Type: "string", Description: "本地HTTP代理监听地址", Category: "代理配置", Required: false, ActualKey: appConfig.HttpLocalAddress.Key},
		"EnableHttp":               {Type: "bool", Description: "启用HTTP代理", Category: "代理配置", Required: false, ActualKey: appConfig.EnableHttp.Key},
		"EnableSocks5":             {Type: "bool", Description: "启用SOCKS5代理", Category: "代理配置", Required: false, ActualKey: appConfig.EnableSocks5.Key},
		"EnableHttpOverSSH":        {Type: "bool", Description: "启用HTTP Over SSH", Category: "代理配置", Required: false, ActualKey: appConfig.EnableHttpOverSSH.Key},
		"HttpBasicAuthEnable":      {Type: "bool", Description: "启用HTTP Basic认证", Category: "认证配置", Required: false, ActualKey: appConfig.HttpBasicAuthEnable.Key},
		"HttpBasicUserName":        {Type: "string", Description: "HTTP Basic认证用户名", Category: "认证配置", Required: false, ActualKey: appConfig.HttpBasicUserName.Key},
		"HttpBasicPassword":        {Type: "string", Description: "HTTP Basic认证密码", Category: "认证配置", Required: false, ActualKey: appConfig.HttpBasicPassword.Key},
		"EnableHttpDomainFilter":   {Type: "bool", Description: "启用域名过滤", Category: "过滤配置", Required: false, ActualKey: appConfig.EnableHttpDomainFilter.Key},
		"HttpDomainFilterFilePath": {Type: "string", Description: "域名过滤文件路径", Category: "过滤配置", Required: false, ActualKey: appConfig.HttpDomainFilterFilePath.Key},
		"EnableAdmin":              {Type: "bool", Description: "启用管理界面", Category: "管理配置", Required: false, ActualKey: appConfig.EnableAdmin.Key},
		"AdminAddress":             {Type: "string", Description: "管理界面监听地址", Category: "管理配置", Required: false, ActualKey: appConfig.AdminAddress.Key},
		"RetryIntervalSec":         {Type: "int", Description: "连接重试间隔(秒)", Category: "高级配置", Required: false, ActualKey: appConfig.RetryIntervalSec.Key},
		"LogFilePath":              {Type: "string", Description: "日志文件路径", Category: "高级配置", Required: false, ActualKey: appConfig.LogFilePath.Key},
		"HomeDir":                  {Type: "string", Description: "应用主目录", Category: "高级配置", Required: false, ActualKey: appConfig.HomeDir.Key},
	}

	// 创建配置键映射（前端属性名 -> 实际配置键）
	configKeys := map[string]string{
		"ServerIp":                 appConfig.ServerIp.Key,
		"ServerSshPort":            appConfig.ServerSshPort.Key,
		"LoginUser":                appConfig.LoginUser.Key,
		"SshPrivateKeyPath":        appConfig.SshPrivateKeyPath.Key,
		"SshKnownHostsPath":        appConfig.SshKnownHostsPath.Key,
		"LocalAddress":             appConfig.LocalAddress.Key,
		"HttpLocalAddress":         appConfig.HttpLocalAddress.Key,
		"EnableHttp":               appConfig.EnableHttp.Key,
		"EnableSocks5":             appConfig.EnableSocks5.Key,
		"EnableHttpOverSSH":        appConfig.EnableHttpOverSSH.Key,
		"HttpBasicAuthEnable":      appConfig.HttpBasicAuthEnable.Key,
		"HttpBasicUserName":        appConfig.HttpBasicUserName.Key,
		"HttpBasicPassword":        appConfig.HttpBasicPassword.Key,
		"EnableHttpDomainFilter":   appConfig.EnableHttpDomainFilter.Key,
		"HttpDomainFilterFilePath": appConfig.HttpDomainFilterFilePath.Key,
		"EnableAdmin":              appConfig.EnableAdmin.Key,
		"AdminAddress":             appConfig.AdminAddress.Key,
		"RetryIntervalSec":         appConfig.RetryIntervalSec.Key,
		"LogFilePath":              appConfig.LogFilePath.Key,
		"HomeDir":                  appConfig.HomeDir.Key,
	}

	app_config := AppConfig{
		ConfigFilePath:   constants.ConfigFilePath,
		Config:           data,
		ConfigMeta:       configMeta,
		ConfigKeys:       configKeys,
		ExecutablePath:   getExecutablePath(),
		WorkingDirectory: getWorkingDirectory(),
	}
	
	tmpl.Execute(response, app_config)
}

func ShowLogsView(response http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml",
		"nav.gohtml",
		"logs.gohtml")

	if err != nil {
		fmt.Println("Error " + err.Error())
	}
	tmpl.Execute(response, nil)
}

// 获取程序执行路径
func getExecutablePath() string {
	executable, err := os.Executable()
	if err != nil {
		return "无法获取程序路径: " + err.Error()
	}
	// 解析符号链接，获取真实路径
	realPath, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return executable // 如果无法解析符号链接，返回原始路径
	}
	return realPath
}

// 获取当前工作目录
func getWorkingDirectory() string {
	workDir, err := os.Getwd()
	if err != nil {
		return "无法获取工作目录: " + err.Error()
	}
	return workDir
}

func ShowVersionView(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFS(views.HtmlFs, "layout.gohtml", "nav.gohtml", "version.gohtml")
	if err != nil {
		http.Error(w, "模板解析错误: "+err.Error(), http.StatusInternalServerError)
		return
	}

	appConfig := cfg.NewAppConfig()
	
	// 检查是否运行在服务模式
	isServiceMode := !service.Interactive()
	
	// 获取当前版本信息
	currentVersion := appConfig.AutoUpdateCurrentVersion.GetValue()
	
	// 获取当前文件的校验和
	currentChecksum, _ := getCurrentFileChecksum()
	
	// 获取文件信息
	fileInfo, _ := getCurrentFileInfo()
	
	// 获取版本信息和检查更新（命令模式和服务模式都支持）
	var releases []VersionInfo
	var latestRelease *VersionInfo
	hasUpdate := false
	updateCount := 0
	latestVersion := currentVersion
	
	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance != nil {
		githubReleases, err := updaterInstance.GetReleases()
		if err == nil {
			for _, release := range githubReleases {
				// 只处理非预发布、非草稿版本
				if !release.Prerelease && !release.Draft {
					versionInfo := VersionInfo{
						TagName:      release.TagName,
						Name:         release.Name,
						Body:         release.Body,
						Prerelease:   release.Prerelease,
						PublishedAt:  release.PublishedAt,
						IsNewer:      isNewerVersion(release.TagName, currentVersion),
						MatchingAsset: findMatchingAsset(release.Assets),
					}
					
					releases = append(releases, versionInfo)
					
					// 找到最新版本
					if latestRelease == nil || isNewerVersion(versionInfo.TagName, latestRelease.TagName) {
						latestRelease = &versionInfo
						latestVersion = versionInfo.TagName
					}
					
					if versionInfo.IsNewer {
						hasUpdate = true
						updateCount++
					}
				}
			}
		}
	}
	
	data := VersionPageData{
		IsServiceMode:        isServiceMode,
		CurrentVersion:       currentVersion,
		CurrentChecksum:      currentChecksum,
		InstallTime:          fileInfo.InstallTime,
		CurrentFileSize:      fileInfo.FileSize,
		Platform:             runtime.GOOS,
		Architecture:         runtime.GOARCH,
		AutoUpdateEnabled:    appConfig.AutoUpdateEnabled.GetValue(),
		CheckInterval:        appConfig.AutoUpdateCheckInterval.GetValue() / 60, // 转换为分钟
		CheckIntervalMinutes: appConfig.AutoUpdateCheckInterval.GetValue() / 60,
		GitHubOwner:          appConfig.AutoUpdateOwner.GetValue(),
		GitHubRepo:           appConfig.AutoUpdateRepo.GetValue(),
		Releases:             releases,
		LatestRelease:        latestRelease,
		HasUpdate:            hasUpdate,
		UpdateCount:          updateCount,
		LatestVersion:        latestVersion,
		
		// 代理配置
		ProxyEnabled:         appConfig.DownloadProxyEnabled.GetValue(),
		ProxyURL:             appConfig.DownloadProxyURL.GetValue(),
		ProxyUsername:        appConfig.DownloadProxyUsername.GetValue(),
		ProxyPassword:        appConfig.DownloadProxyPassword.GetValue(),
	}
	
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "模板执行错误: "+err.Error(), http.StatusInternalServerError)
	}
}

// API处理函数
func CheckForUpdatesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持POST方法", http.StatusMethodNotAllowed)
		return
	}
	
	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance == nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "更新服务未初始化",
		})
		return
	}
	
	release, hasUpdate := updaterInstance.CheckForUpdates()
	
	response := map[string]interface{}{
		"success":   true,
		"hasUpdate": hasUpdate,
	}
	
	if hasUpdate && release != nil {
		response["latestVersion"] = release.TagName
		response["releaseName"] = release.Name
		response["releaseBody"] = release.Body
	}
	
	writeJSONResponse(w, response)
}

func DownloadReleaseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持POST方法", http.StatusMethodNotAllowed)
		return
	}
	
	version := r.FormValue("version")
	fileName := r.FormValue("fileName")
	downloadURL := r.FormValue("downloadUrl")
	
	if version == "" || fileName == "" || downloadURL == "" {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "缺少必要参数",
		})
		return
	}
	
	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance == nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "更新服务未初始化",
		})
		return
	}
	
	// 创建下载目录
	appConfig := cfg.NewAppConfig()
	downloadDir := filepath.Join(appConfig.HomeDir.GetValue(), "downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "创建下载目录失败: " + err.Error(),
		})
		return
	}
	
	// 生成下载ID
	downloadID := fmt.Sprintf("download_%d", time.Now().Unix())
	
	// 启动后台下载
	filePath := filepath.Join(downloadDir, fileName)
	go func() {
		if err := downloadFileWithProgress(downloadURL, filePath, downloadID); err != nil {
			downloadManager.SetError(downloadID, err.Error())
		}
	}()
	
	writeJSONResponse(w, map[string]interface{}{
		"success":    true,
		"downloadId": downloadID,
		"message":    "下载已开始",
	})
}

func UpdateToVersionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持POST方法", http.StatusMethodNotAllowed)
		return
	}
	
	version := r.FormValue("version")
	fileName := r.FormValue("fileName")
	downloadURL := r.FormValue("downloadUrl")
	
	if version == "" || fileName == "" || downloadURL == "" {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "缺少必要参数",
		})
		return
	}
	
	// 检查是否为服务模式
	isServiceMode := !service.Interactive()
	
	// 创建下载目录
	appConfig := cfg.NewAppConfig()
	downloadDir := filepath.Join(appConfig.HomeDir.GetValue(), "downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "创建下载目录失败: " + err.Error(),
		})
		return
	}
	
	// 生成下载ID用于进度跟踪
	downloadID := fmt.Sprintf("update_%d", time.Now().Unix())
	
	// 下载新版本文件
	newFilePath := filepath.Join(downloadDir, fileName)
	if err := downloadFileWithProgress(downloadURL, newFilePath, downloadID); err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success":    false,
			"error":      "下载失败: " + err.Error(),
			"downloadId": downloadID,
		})
		return
	}
	
	// 获取当前执行文件路径
	currentExe, err := os.Executable()
	if err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "获取当前程序路径失败: " + err.Error(),
		})
		return
	}
	
	// 备份当前文件
	backupPath := currentExe + ".backup." + time.Now().Format("20060102150405")
	if err := copyFile(currentExe, backupPath); err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "备份当前文件失败: " + err.Error(),
		})
		return
	}
	
	// 替换文件（Windows下使用批处理脚本）
	if err := replaceExecutable(currentExe, newFilePath); err != nil {
		// 恢复备份
		copyFile(backupPath, currentExe)
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "替换文件失败: " + err.Error(),
		})
		return
	}
	
	// 更新版本配置
	appConfig.AutoUpdateCurrentVersion.SetValue(version)
	cfg.SaveConfig()
	
	var message string
	if runtime.GOOS == "windows" {
		if isServiceMode {
			message = "更新脚本已启动，服务将自动重启"
		} else {
			message = "更新脚本已启动，程序即将退出，请按批处理提示操作"
		}
		// Windows下使用批处理脚本处理，程序需要退出
		go func() {
			time.Sleep(2 * time.Second)
			os.Exit(0)
		}()
	} else {
		if isServiceMode {
			message = "更新完成，服务将自动重启"
			// 非Windows服务模式下的重启
			go func() {
				time.Sleep(2 * time.Second)
				os.Exit(0)
			}()
		} else {
			message = "更新完成，请手动重启程序"
		}
	}
	
	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": message,
	})
}

func SaveUpdateSettingsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "仅支持POST方法", http.StatusMethodNotAllowed)
		return
	}
	
	var settings UpdateSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "解析请求数据失败: " + err.Error(),
		})
		return
	}
	
	// 更新配置
	appConfig := cfg.NewAppConfig()
	appConfig.AutoUpdateEnabled.SetValue(settings.Enabled)
	appConfig.AutoUpdateCheckInterval.SetValue(settings.CheckInterval * 60) // 转换为秒
	appConfig.AutoUpdateOwner.SetValue(settings.GitHubOwner)
	appConfig.AutoUpdateRepo.SetValue(settings.GitHubRepo)
	
	// 更新代理配置
	appConfig.DownloadProxyEnabled.SetValue(settings.ProxyEnabled)
	appConfig.DownloadProxyURL.SetValue(settings.ProxyURL)
	appConfig.DownloadProxyUsername.SetValue(settings.ProxyUsername)
	appConfig.DownloadProxyPassword.SetValue(settings.ProxyPassword)
	
	// 保存配置
	if err := cfg.SaveConfig(); err != nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "保存配置失败: " + err.Error(),
		})
		return
	}
	
	// 更新更新器配置
	updater.UpdateConfig()
	
	writeJSONResponse(w, map[string]interface{}{
		"success": true,
		"message": "设置已保存",
	})
}

// 下载进度管理
type DownloadProgress struct {
	ID             string
	FileName       string
	TotalSize      int64
	DownloadedSize int64
	Progress       float64
	Status         string
	Error          string
	StartTime      time.Time
	mutex          sync.RWMutex
}

type DownloadManager struct {
	downloads map[string]*DownloadProgress
	mutex     sync.RWMutex
}

var downloadManager = &DownloadManager{
	downloads: make(map[string]*DownloadProgress),
}

func (dm *DownloadManager) CreateDownload(id, fileName string, totalSize int64) *DownloadProgress {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	
	progress := &DownloadProgress{
		ID:        id,
		FileName:  fileName,
		TotalSize: totalSize,
		Status:    "downloading",
		StartTime: time.Now(),
	}
	
	dm.downloads[id] = progress
	return progress
}

func (dm *DownloadManager) GetDownload(id string) *DownloadProgress {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	return dm.downloads[id]
}

func (dm *DownloadManager) UpdateProgress(id string, downloaded int64) {
	dm.mutex.RLock()
	progress := dm.downloads[id]
	dm.mutex.RUnlock()
	
	if progress != nil {
		progress.mutex.Lock()
		progress.DownloadedSize = downloaded
		if progress.TotalSize > 0 {
			progress.Progress = float64(downloaded) / float64(progress.TotalSize) * 100
		}
		progress.mutex.Unlock()
	}
}

func (dm *DownloadManager) SetStatus(id, status string) {
	dm.mutex.RLock()
	progress := dm.downloads[id]
	dm.mutex.RUnlock()
	
	if progress != nil {
		progress.mutex.Lock()
		progress.Status = status
		progress.mutex.Unlock()
	}
}

func (dm *DownloadManager) SetError(id, error string) {
	dm.mutex.RLock()
	progress := dm.downloads[id]
	dm.mutex.RUnlock()
	
	if progress != nil {
		progress.mutex.Lock()
		progress.Status = "error"
		progress.Error = error
		progress.mutex.Unlock()
	}
}

func GetDownloadProgressHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "仅支持GET方法", http.StatusMethodNotAllowed)
		return
	}
	
	downloadID := r.URL.Query().Get("id")
	if downloadID == "" {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "缺少下载ID",
		})
		return
	}
	
	progress := downloadManager.GetDownload(downloadID)
	if progress == nil {
		writeJSONResponse(w, map[string]interface{}{
			"success": false,
			"error":   "下载不存在",
		})
		return
	}
	
	progress.mutex.RLock()
	data := map[string]interface{}{
		"success":        true,
		"id":             progress.ID,
		"fileName":       progress.FileName,
		"totalSize":      progress.TotalSize,
		"downloadedSize": progress.DownloadedSize,
		"progress":       progress.Progress,
		"status":         progress.Status,
		"error":          progress.Error,
		"startTime":      progress.StartTime,
	}
	progress.mutex.RUnlock()
	
	writeJSONResponse(w, data)
}

// 辅助函数
func getCurrentFileChecksum() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	
	updaterInstance := updater.GetGlobalUpdater()
	if updaterInstance == nil {
		return "", fmt.Errorf("更新服务未初始化")
	}
	
	return updaterInstance.GetFileChecksum(executable)
}

func getCurrentFileInfo() (FileInfo, error) {
	executable, err := os.Executable()
	if err != nil {
		return FileInfo{}, err
	}
	
	stat, err := os.Stat(executable)
	if err != nil {
		return FileInfo{}, err
	}
	
	return FileInfo{
		FileSize:    formatFileSize(stat.Size()),
		InstallTime: stat.ModTime().Format("2006-01-02 15:04:05"),
	}, nil
}

func isNewerVersion(newVersion, currentVersion string) bool {
	// 简单的版本比较逻辑
	return strings.TrimPrefix(newVersion, "v") > strings.TrimPrefix(currentVersion, "v")
}

func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func downloadFile(downloadURL, filepath string) error {
	return downloadFileWithProgress(downloadURL, filepath, "")
}

func downloadFileWithProgress(downloadURL, filepath, progressID string) error {
	appConfig := cfg.NewAppConfig()
	
	// 创建 HTTP 客户端
	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	
	// 配置代理
	if appConfig.DownloadProxyEnabled.GetValue() && appConfig.DownloadProxyURL.GetValue() != "" {
		proxyURL, err := url.Parse(appConfig.DownloadProxyURL.GetValue())
		if err != nil {
			return fmt.Errorf("代理URL解析失败: %v", err)
		}
		
		// 设置代理认证
		if appConfig.DownloadProxyUsername.GetValue() != "" {
			proxyURL.User = url.UserPassword(
				appConfig.DownloadProxyUsername.GetValue(),
				appConfig.DownloadProxyPassword.GetValue(),
			)
		}
		
		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.Transport = transport
	}
	
	req, err := http.NewRequest("GET", downloadURL, nil)
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
	
	// 创建目标文件
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()
	
	// 获取文件大小
	totalSize := resp.ContentLength
	
	// 如果提供了进度ID，创建进度跟踪
	var progress *DownloadProgress
	if progressID != "" {
		fileName := filepath[strings.LastIndex(filepath, "/")+1:]
		if strings.Contains(filepath, "\\") {
			fileName = filepath[strings.LastIndex(filepath, "\\")+1:]
		}
		progress = downloadManager.CreateDownload(progressID, fileName, totalSize)
	}
	
	// 创建进度读取器
	var reader io.Reader = resp.Body
	if progress != nil {
		reader = &ProgressReader{
			Reader:     resp.Body,
			Total:      totalSize,
			Downloaded: 0,
			OnProgress: func(downloaded int64) {
				downloadManager.UpdateProgress(progressID, downloaded)
			},
		}
	}
	
	// 复制文件内容
	_, err = io.Copy(out, reader)
	if err != nil {
		if progress != nil {
			downloadManager.SetError(progressID, err.Error())
		}
		return err
	}
	
	// 设置完成状态
	if progress != nil {
		downloadManager.SetStatus(progressID, "completed")
	}
	
	return nil
}

// 进度读取器
type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	Downloaded int64
	OnProgress func(int64)
}

func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.Reader.Read(p)
	pr.Downloaded += int64(n)
	if pr.OnProgress != nil {
		pr.OnProgress(pr.Downloaded)
	}
	return n, err
}

func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// 数据结构定义
type VersionPageData struct {
	IsServiceMode        bool
	CurrentVersion       string
	CurrentChecksum      string
	InstallTime          string
	CurrentFileSize      string
	Platform             string
	Architecture         string
	AutoUpdateEnabled    bool
	CheckInterval        int
	CheckIntervalMinutes int
	GitHubOwner          string
	GitHubRepo           string
	Releases             []VersionInfo
	LatestRelease        *VersionInfo
	HasUpdate            bool
	UpdateCount          int
	LatestVersion        string
	
	// 下载代理配置
	ProxyEnabled         bool
	ProxyURL             string
	ProxyUsername        string
	ProxyPassword        string
}

type VersionInfo struct {
	TagName       string
	Name          string
	Body          string
	Prerelease    bool
	Draft         bool
	PublishedAt   string
	Assets        []AssetInfo
	MatchingAsset *AssetInfo
	IsNewer       bool
}

type AssetInfo struct {
	Name          string
	DownloadURL   string
	Size          string
	DownloadCount int
}

type FileInfo struct {
	FileSize    string
	InstallTime string
}

type UpdateSettings struct {
	Enabled       bool   `json:"enabled"`
	CheckInterval int    `json:"checkInterval"`
	GitHubOwner   string `json:"githubOwner"`
	GitHubRepo    string `json:"githubRepo"`
	
	// 代理配置
	ProxyEnabled  bool   `json:"proxyEnabled"`
	ProxyURL      string `json:"proxyUrl"`
	ProxyUsername string `json:"proxyUsername"`
	ProxyPassword string `json:"proxyPassword"`
}

// findMatchingAsset 找到适合当前平台的资源文件
func findMatchingAsset(assets []updater.Asset) *AssetInfo {
	osName := runtime.GOOS
	archName := runtime.GOARCH
	
	// 构建平台标识符，优先匹配服务版本
	platformIdentifiers := []string{
		fmt.Sprintf("svc-%s-%s", osName, archName),
		fmt.Sprintf("svc_%s_%s", osName, archName),
		fmt.Sprintf("%s-%s", osName, archName),
		fmt.Sprintf("%s_%s", osName, archName),
	}
	
	// 查找匹配的资源文件
	for _, asset := range assets {
		assetNameLower := strings.ToLower(asset.Name)
		for _, platform := range platformIdentifiers {
			if strings.Contains(assetNameLower, strings.ToLower(platform)) {
				return &AssetInfo{
					Name:          asset.Name,
					DownloadURL:   asset.DownloadURL,
					Size:          formatFileSize(asset.Size),
					DownloadCount: asset.DownloadCount,
				}
			}
		}
	}
	
	return nil
}

// copyFile 复制文件
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()
	
	_, err = io.Copy(destFile, sourceFile)
	return err
}

// replaceExecutable 替换可执行文件
func replaceExecutable(currentExe, newFile string) error {
	if runtime.GOOS == "windows" {
		return replaceExecutableWindows(currentExe, newFile)
	} else {
		// Linux/macOS 可以直接替换
		return os.Rename(newFile, currentExe)
	}
}

// replaceExecutableWindows Windows下的可执行文件替换
func replaceExecutableWindows(currentExe, newFile string) error {
	// 获取当前程序的目录
	dir := filepath.Dir(currentExe)
	baseName := filepath.Base(currentExe)
	
	// 创建批处理更新脚本
	batchFile := filepath.Join(dir, "update_"+time.Now().Format("20060102150405")+".bat")
	
	// 检查是否为服务模式
	isServiceMode := !service.Interactive()
	
	var batchContent string
	if isServiceMode {
		// 服务模式：停止服务、替换文件、启动服务
		batchContent = fmt.Sprintf(`@echo off
echo Starting SSH Tunnel Update Process...
echo Waiting for main process to exit...
timeout /t 3 /nobreak >nul

echo Backing up current file...
if exist "%s.backup" del "%s.backup"
if exist "%s" ren "%s" "%s.backup"

echo Installing new version...
move "%s" "%s"

echo Starting service...
sc start ssh-tunnel-service

echo Cleaning up...
del "%%~f0"
`, currentExe, currentExe, currentExe, baseName, baseName, newFile, currentExe)
	} else {
		// 命令模式：替换文件，提示用户重启
		batchContent = fmt.Sprintf(`@echo off
echo Starting SSH Tunnel Update Process...
echo Waiting for main process to exit...
timeout /t 3 /nobreak >nul

echo Backing up current file...
if exist "%s.backup" del "%s.backup"
if exist "%s" ren "%s" "%s.backup"

echo Installing new version...
move "%s" "%s"

echo Update completed successfully!
echo Please restart the SSH Tunnel manually.
echo.
pause

echo Cleaning up...
del "%%~f0"
`, currentExe, currentExe, currentExe, baseName, baseName, newFile, currentExe)
	}
	
	// 写入批处理文件
	if err := os.WriteFile(batchFile, []byte(batchContent), 0755); err != nil {
		return fmt.Errorf("创建更新脚本失败: %v", err)
	}
	
	// 启动批处理文件
	cmd := exec.Command("cmd", "/c", "start", "/min", batchFile)
	cmd.Dir = dir
	
	if err := cmd.Start(); err != nil {
		os.Remove(batchFile) // 清理失败的脚本
		return fmt.Errorf("启动更新脚本失败: %v", err)
	}
	
	return nil
}
