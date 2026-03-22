package updater

import (
	"strconv"
	"strings"
)

func NormalizeVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") || strings.HasPrefix(trimmed, "V") {
		return "v" + strings.TrimSpace(trimmed[1:])
	}
	return "v" + trimmed
}

func CompareVersions(left, right string) int {
	parse := func(version string) [3]int {
		version = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V"))
		parts := strings.Split(version, ".")
		result := [3]int{0, 0, 0}
		for i := 0; i < len(parts) && i < 3; i++ {
			segment := strings.TrimSpace(parts[i])
			if segment == "" {
				continue
			}
			end := 0
			for end < len(segment) && segment[end] >= '0' && segment[end] <= '9' {
				end++
			}
			if end == 0 {
				continue
			}
			value, err := strconv.Atoi(segment[:end])
			if err == nil {
				result[i] = value
			}
		}
		return result
	}

	leftParts := parse(left)
	rightParts := parse(right)
	for i := 0; i < 3; i++ {
		if leftParts[i] > rightParts[i] {
			return 1
		}
		if leftParts[i] < rightParts[i] {
			return -1
		}
	}

	return 0
}

func IsNewerVersion(newVersion, currentVersion string) bool {
	return CompareVersions(newVersion, currentVersion) > 0
}
