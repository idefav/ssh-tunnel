package constants

import "runtime"

const (
	ServiceNameWindows = "SSHTunnelService"
	ServiceNameLinux   = "ssh-tunnel"
	ServiceNameDarwin  = "com.idefav.ssh-tunnel"

	ServiceDisplayNameWindows = "SSHTunnelService"
	ServiceDisplayNameUnix    = "ssh-tunnel"

	WindowsInstallRoot = "C:\\ssh-tunnel"
	WindowsConfigPath  = "C:\\ssh-tunnel\\.ssh-tunnel\\config.properties"
	WindowsStateDir    = "C:\\ssh-tunnel\\.ssh-tunnel"
	WindowsServiceExe  = "ssh-tunnel-svc.exe"
	WindowsVersionURL  = "http://127.0.0.1:1083/view/version"
	UnixConfigPath     = "/etc/ssh-tunnel/config.properties"
	UnixBinaryPath     = "/usr/local/bin/ssh-tunnel"
	LinuxSystemdUnit   = "/etc/systemd/system/ssh-tunnel.service"
	LinuxSysVScript    = "/etc/init.d/ssh-tunnel"
	DarwinLaunchdPlist = "/Library/LaunchDaemons/com.idefav.ssh-tunnel.plist"
)

func ServiceNameForGOOS(goos string) string {
	switch goos {
	case "windows":
		return ServiceNameWindows
	case "darwin":
		return ServiceNameDarwin
	default:
		return ServiceNameLinux
	}
}

func ServiceDisplayNameForGOOS(goos string) string {
	if goos == "windows" {
		return ServiceDisplayNameWindows
	}
	return ServiceDisplayNameUnix
}

func CurrentServiceName() string {
	return ServiceNameForGOOS(runtime.GOOS)
}
