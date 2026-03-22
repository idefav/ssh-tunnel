package updater

import (
	"bufio"
	"fmt"
	"strings"
)

func FindReleaseByTag(releases []Release, tag string) *Release {
	target := NormalizeVersion(tag)
	for i := range releases {
		if NormalizeVersion(releases[i].TagName) == target {
			return &releases[i]
		}
	}
	return nil
}

func SelectAsset(assets []Asset, serviceMode bool, osName, archName string) *Asset {
	bestScore := -1
	var best *Asset
	for i := range assets {
		asset := &assets[i]
		score := scoreAsset(asset.Name, serviceMode, osName, archName)
		if score > bestScore {
			bestScore = score
			best = asset
		}
	}
	if bestScore <= 0 {
		return nil
	}
	return best
}

func FindChecksumAsset(assets []Asset) *Asset {
	for i := range assets {
		name := strings.ToLower(strings.TrimSpace(assets[i].Name))
		if name == "sha256sums" || name == "sha256sums.txt" {
			return &assets[i]
		}
	}
	return nil
}

func ParseChecksumFile(content string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		checksum := strings.ToLower(strings.TrimSpace(fields[0]))
		fileName := strings.TrimSpace(strings.TrimPrefix(fields[len(fields)-1], "*"))
		if checksum == "" || fileName == "" {
			continue
		}
		result[fileName] = checksum
	}
	return result
}

func scoreAsset(name string, serviceMode bool, osName, archName string) int {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	if lowerName == "" {
		return 0
	}
	if lowerName == "sha256sums" || lowerName == "sha256sums.txt" {
		return 0
	}

	osToken := strings.ToLower(osName)
	archToken := strings.ToLower(archName)
	if !strings.Contains(lowerName, osToken) || !strings.Contains(lowerName, archToken) {
		return 0
	}

	score := 10
	if strings.Contains(lowerName, fmt.Sprintf("-%s-%s", osToken, archToken)) {
		score += 5
	}
	if strings.Contains(lowerName, fmt.Sprintf("_%s_%s", osToken, archToken)) {
		score += 5
	}

	isServiceAsset := strings.Contains(lowerName, "svc-") || strings.Contains(lowerName, "svc_") || strings.Contains(lowerName, "-svc-") || strings.Contains(lowerName, "_svc_")
	if serviceMode {
		if isServiceAsset {
			score += 50
		} else {
			score -= 20
		}
	} else if isServiceAsset {
		score -= 30
	}

	return score
}
