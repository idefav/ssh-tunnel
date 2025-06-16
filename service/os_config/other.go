//go:build !windows && !darwin && !linux
// +build !windows,!darwin,!linux

package os_config

import (
	"log"
	"runtime"
)

func SetConfig(vConfig *viper.Viper) {
	log.Println("未知的操作系统, " + runtime.GOOS)
}
