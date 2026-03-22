package updater

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	VerificationStatusUnknown    = "unknown"
	VerificationStatusUnverified = "unverified"
	VerificationStatusVerified   = "verified"
	VerificationStatusFailed     = "failed"
)

type UpdateManifest struct {
	CachedAssets       map[string]CachedAssetState `json:"cachedAssets"`
	PendingInstall     *PendingInstallState        `json:"pendingInstall,omitempty"`
	LastInstallMessage string                      `json:"lastInstallMessage,omitempty"`
	LastInstallAt      string                      `json:"lastInstallAt,omitempty"`
}

type CachedAssetState struct {
	Key                string `json:"key"`
	Version            string `json:"version"`
	AssetName          string `json:"assetName"`
	FilePath           string `json:"filePath"`
	FileSize           int64  `json:"fileSize"`
	DownloadURL        string `json:"downloadUrl,omitempty"`
	VerificationStatus string `json:"verificationStatus"`
	ExpectedChecksum   string `json:"expectedChecksum,omitempty"`
	ActualChecksum     string `json:"actualChecksum,omitempty"`
	DownloadedAt       string `json:"downloadedAt,omitempty"`
}

type PendingInstallState struct {
	Version     string `json:"version"`
	AssetName   string `json:"assetName"`
	FilePath    string `json:"filePath"`
	BackupPath  string `json:"backupPath,omitempty"`
	ServiceMode bool   `json:"serviceMode"`
	RequestedAt string `json:"requestedAt"`
}

func UpdateRoot(homeDir string) string {
	return filepath.Join(homeDir, "updates")
}

func ManifestPath(homeDir string) string {
	return filepath.Join(UpdateRoot(homeDir), "manifest.json")
}

func CacheRoot(homeDir string) string {
	return filepath.Join(UpdateRoot(homeDir), "cache")
}

func CacheKey(version, assetName string) string {
	return NormalizeVersion(version) + "|" + strings.TrimSpace(assetName)
}

func CacheFilePath(homeDir, version, assetName string) string {
	return filepath.Join(CacheRoot(homeDir), sanitizePathSegment(NormalizeVersion(version)), strings.TrimSpace(assetName))
}

func LoadManifest(homeDir string) (*UpdateManifest, error) {
	manifest := &UpdateManifest{
		CachedAssets: make(map[string]CachedAssetState),
	}

	path := ManifestPath(homeDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return manifest, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return manifest, nil
	}
	if err := json.Unmarshal(data, manifest); err != nil {
		return nil, err
	}
	if manifest.CachedAssets == nil {
		manifest.CachedAssets = make(map[string]CachedAssetState)
	}
	return manifest, nil
}

func SaveManifest(homeDir string, manifest *UpdateManifest) error {
	if manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	if manifest.CachedAssets == nil {
		manifest.CachedAssets = make(map[string]CachedAssetState)
	}
	root := UpdateRoot(homeDir)
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ManifestPath(homeDir), data, 0644)
}

func (manifest *UpdateManifest) GetCachedAsset(version, assetName string) (CachedAssetState, bool) {
	if manifest == nil || manifest.CachedAssets == nil {
		return CachedAssetState{}, false
	}
	state, ok := manifest.CachedAssets[CacheKey(version, assetName)]
	return state, ok
}

func (manifest *UpdateManifest) SetCachedAsset(state CachedAssetState) {
	if manifest.CachedAssets == nil {
		manifest.CachedAssets = make(map[string]CachedAssetState)
	}
	if state.Key == "" {
		state.Key = CacheKey(state.Version, state.AssetName)
	}
	manifest.CachedAssets[state.Key] = state
}

func (manifest *UpdateManifest) RemoveCachedAsset(version, assetName string) {
	if manifest == nil || manifest.CachedAssets == nil {
		return
	}
	delete(manifest.CachedAssets, CacheKey(version, assetName))
}

func (manifest *UpdateManifest) CleanupMissingCacheFiles() {
	if manifest == nil || manifest.CachedAssets == nil {
		return
	}
	keys := make([]string, 0, len(manifest.CachedAssets))
	for key := range manifest.CachedAssets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		state := manifest.CachedAssets[key]
		if strings.TrimSpace(state.FilePath) == "" {
			delete(manifest.CachedAssets, key)
			continue
		}
		if _, err := os.Stat(state.FilePath); err != nil {
			delete(manifest.CachedAssets, key)
		}
	}
}

func CleanupPartialDownloads(homeDir string) error {
	cacheRoot := CacheRoot(homeDir)
	if _, err := os.Stat(cacheRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return filepath.Walk(cacheRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(info.Name(), ".part") {
			if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
				return removeErr
			}
		}
		return nil
	})
}

func ReconcilePendingInstall(manifest *UpdateManifest, runtimeVersion string) bool {
	if manifest == nil || manifest.PendingInstall == nil {
		return false
	}

	now := time.Now().Format(time.RFC3339)
	if NormalizeVersion(runtimeVersion) == NormalizeVersion(manifest.PendingInstall.Version) {
		manifest.LastInstallMessage = fmt.Sprintf("版本 %s 已安装并生效", manifest.PendingInstall.Version)
		manifest.LastInstallAt = now
		manifest.PendingInstall = nil
		return true
	}

	manifest.LastInstallMessage = fmt.Sprintf("待安装版本 %s 未生效，当前仍运行 %s", manifest.PendingInstall.Version, runtimeVersion)
	manifest.LastInstallAt = now
	manifest.PendingInstall = nil
	return true
}

func SyncRuntimeState(homeDir, runtimeVersion string) (*UpdateManifest, error) {
	if err := CleanupPartialDownloads(homeDir); err != nil {
		return nil, err
	}

	manifest, err := LoadManifest(homeDir)
	if err != nil {
		return nil, err
	}
	manifest.CleanupMissingCacheFiles()

	changed := ReconcilePendingInstall(manifest, runtimeVersion)
	if changed {
		if err := SaveManifest(homeDir, manifest); err != nil {
			return nil, err
		}
	}

	return manifest, nil
}

func sanitizePathSegment(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(strings.TrimSpace(value))
}
