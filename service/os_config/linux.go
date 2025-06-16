//go:build linux
// +build linux

package os_config

import "github.com/spf13/viper"

func SetConfig(vConfig *viper.Viper) {
	// Linux 系统特定配置
	// 通过服务管理器运行时，配置文件路径可能在特定目录下
	vConfig.AddConfigPath("/etc/ssh-tunnel")
	vConfig.AddConfigPath("/usr/local/etc/ssh-tunnel")
	vConfig.AddConfigPath("/var/lib/ssh-tunnel")
	vConfig.AddConfigPath("/home/$(whoami)/.ssh-tunnel")
	vConfig.AddConfigPath("/opt/ssh-tunnel")
}
