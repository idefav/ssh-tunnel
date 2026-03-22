package buildinfo

import "strings"

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func CurrentVersion() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		return "dev"
	}
	return version
}

func CurrentBuildTime() string {
	buildTime := strings.TrimSpace(BuildTime)
	if buildTime == "" {
		return "unknown"
	}
	return buildTime
}
