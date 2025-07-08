package updater

import (
	"log"
	"ssh-tunnel/cfg"
	"time"
)

var (
	globalUpdater *Updater
)

// InitializeUpdater 初始化全局更新器
func InitializeUpdater() {
	appConfig := cfg.NewAppConfig()
	
	config := &UpdaterConfig{
		Enabled:        appConfig.AutoUpdateEnabled.GetValue(),
		Owner:          appConfig.AutoUpdateOwner.GetValue(),
		Repo:           appConfig.AutoUpdateRepo.GetValue(),
		CurrentVersion: appConfig.AutoUpdateCurrentVersion.GetValue(),
		CheckInterval:  time.Duration(appConfig.AutoUpdateCheckInterval.GetValue()) * time.Second,
		AutoDownload:   false, // 默认不自动下载
		AutoInstall:    false, // 默认不自动安装
	}
	
	globalUpdater = NewUpdater(config)
	
	// 设置更新回调
	globalUpdater.SetUpdateCallback(func(release *Release) {
		log.Printf("发现新版本 %s: %s", release.TagName, release.Name)
		// 这里可以添加更多的处理逻辑，比如发送通知等
	})
	
	// 如果启用了自动更新，启动检查
	if config.Enabled {
		globalUpdater.Start()
		log.Println("自动更新检查已启动")
	}
}

// GetGlobalUpdater 获取全局更新器实例
func GetGlobalUpdater() *Updater {
	return globalUpdater
}

// UpdateConfig 更新配置
func UpdateConfig() {
	if globalUpdater == nil {
		return
	}
	
	appConfig := cfg.NewAppConfig()
	
	// 停止当前的更新器
	globalUpdater.Stop()
	
	// 更新配置
	globalUpdater.config.Enabled = appConfig.AutoUpdateEnabled.GetValue()
	globalUpdater.config.Owner = appConfig.AutoUpdateOwner.GetValue()
	globalUpdater.config.Repo = appConfig.AutoUpdateRepo.GetValue()
	globalUpdater.config.CurrentVersion = appConfig.AutoUpdateCurrentVersion.GetValue()
	globalUpdater.config.CheckInterval = time.Duration(appConfig.AutoUpdateCheckInterval.GetValue()) * time.Second
	
	// 如果启用了自动更新，重新启动检查
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
