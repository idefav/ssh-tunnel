//go:build windows
// +build windows

package os_config

import (
	"github.com/kardianos/service"
	"github.com/spf13/viper"
	"path"
)

const DEFAULT_HOME = "C:\\ssh-tunnel"

func SetConfig(vConfig *viper.Viper) {
	// Windows 系统特定配置
	// 是否使用OS 服务管理器运行
	interactive := service.Interactive()
	if !interactive {
		// 通过服务管理器运行时，配置文件路径可能在特定目录下
		vConfig.AddConfigPath(path.Join(DEFAULT_HOME, ".ssh-tunnel"))
	}
}
