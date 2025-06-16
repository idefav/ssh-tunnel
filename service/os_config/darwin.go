//go:build darwin
// +build darwin

package os_config

import "github.com/spf13/viper"

func SetConfig(vConfig *viper.Viper) {
	// macOS 系统特定配置
	// 通过服务管理器运行时，配置文件路径可能在特定目录下
	vConfig.AddConfigPath("/Library/Application Support/ssh-tunnel")
	vConfig.AddConfigPath("/etc/ssh-tunnel")
	vConfig.AddConfigPath("/usr/local/etc/ssh-tunnel")
	vConfig.AddConfigPath("/Users/Shared/ssh-tunnel")
	vConfig.AddConfigPath("/Users/$(whoami)/.ssh-tunnel")
}
