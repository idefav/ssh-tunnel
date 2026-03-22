package updater

import (
	"log"
	"ssh-tunnel/buildinfo"
	"ssh-tunnel/cfg"
	"time"

	"github.com/kardianos/service"
)

var (
	globalUpdater *Updater
)

// InitializeUpdater 初始化全局更新器
func InitializeUpdater() {
	appConfig := cfg.NewAppConfig()
	owner := appConfig.AutoUpdateOwner.GetValue()
	if owner == "" {
		owner = "idefav"
	}
	repo := appConfig.AutoUpdateRepo.GetValue()
	if repo == "" {
		repo = "ssh-tunnel"
	}
	checkIntervalSec := appConfig.AutoUpdateCheckInterval.GetValue()
	if checkIntervalSec <= 0 {
		checkIntervalSec = 3600
	}

	config := &UpdaterConfig{
		Enabled:        appConfig.AutoUpdateEnabled.GetValue(),
		Owner:          owner,
		Repo:           repo,
		CurrentVersion: buildinfo.CurrentVersion(),
		CheckInterval:  time.Duration(checkIntervalSec) * time.Second,
		ServiceMode:    !service.Interactive(),
		AutoDownload:   false,
		AutoInstall:    false,
	}

	globalUpdater = NewUpdater(config)
	globalUpdater.SetUpdateCallback(func(release *Release) {
		log.Printf("发现新版本 %s: %s", release.TagName, release.Name)
	})

	if config.Enabled {
		globalUpdater.Start()
		log.Println("自动更新检查已启动")
	}
}

// GetGlobalUpdater 获取全局更新器实例
func GetGlobalUpdater() *Updater {
	return globalUpdater
}

// UpdateConfig 更新更新器配置
func UpdateConfig() {
	if globalUpdater == nil {
		return
	}

	appConfig := cfg.NewAppConfig()
	globalUpdater.Stop()

	globalUpdater.config.Enabled = appConfig.AutoUpdateEnabled.GetValue()
	globalUpdater.config.Owner = appConfig.AutoUpdateOwner.GetValue()
	globalUpdater.config.Repo = appConfig.AutoUpdateRepo.GetValue()
	globalUpdater.config.CurrentVersion = buildinfo.CurrentVersion()
	globalUpdater.config.CheckInterval = time.Duration(appConfig.AutoUpdateCheckInterval.GetValue()) * time.Second
	globalUpdater.config.ServiceMode = !service.Interactive()

	if globalUpdater.config.Enabled {
		globalUpdater.Start()
		log.Println("自动更新检查已重新启动")
	} else {
		log.Println("自动更新检查已停止")
	}
}

// StopUpdater 停止更新器
func StopUpdater() {
	if globalUpdater != nil {
		globalUpdater.Stop()
		log.Println("自动更新检查已停止")
	}
}
