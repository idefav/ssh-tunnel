package updater

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsNewerVersion(t *testing.T) {
	if !IsNewerVersion("v1.2.0", "v1.1.9") {
		t.Fatalf("expected newer version to be detected")
	}
	if IsNewerVersion("v1.2.0", "v1.2.0") {
		t.Fatalf("same version should not be newer")
	}
	if IsNewerVersion("v1.1.9", "v1.2.0") {
		t.Fatalf("older version should not be newer")
	}
}

func TestSelectAssetPrefersServiceBinary(t *testing.T) {
	assets := []Asset{
		{Name: "ssh-tunnel-windows-amd64.exe"},
		{Name: "ssh-tunnel-svc-windows-amd64.exe"},
	}

	serviceAsset := SelectAsset(assets, true, "windows", "amd64")
	if serviceAsset == nil || serviceAsset.Name != "ssh-tunnel-svc-windows-amd64.exe" {
		t.Fatalf("expected service asset, got %+v", serviceAsset)
	}

	interactiveAsset := SelectAsset(assets, false, "windows", "amd64")
	if interactiveAsset == nil || interactiveAsset.Name != "ssh-tunnel-windows-amd64.exe" {
		t.Fatalf("expected interactive asset, got %+v", interactiveAsset)
	}
}

func TestManifestReadWriteAndReconcile(t *testing.T) {
	homeDir := t.TempDir()
	manifest := &UpdateManifest{}
	manifest.SetCachedAsset(CachedAssetState{
		Version:            "v1.4.11",
		AssetName:          "ssh-tunnel-svc-windows-amd64.exe",
		FilePath:           CacheFilePath(homeDir, "v1.4.11", "ssh-tunnel-svc-windows-amd64.exe"),
		FileSize:           12,
		VerificationStatus: VerificationStatusVerified,
	})
	manifest.PendingInstall = &PendingInstallState{
		Version:     "v1.4.11",
		AssetName:   "ssh-tunnel-svc-windows-amd64.exe",
		FilePath:    CacheFilePath(homeDir, "v1.4.11", "ssh-tunnel-svc-windows-amd64.exe"),
		ServiceMode: true,
	}

	cacheFile := manifest.CachedAssets[CacheKey("v1.4.11", "ssh-tunnel-svc-windows-amd64.exe")].FilePath
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		t.Fatalf("create cache dir: %v", err)
	}
	if err := os.WriteFile(cacheFile, []byte("demo"), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	if err := SaveManifest(homeDir, manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	loaded, err := LoadManifest(homeDir)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}

	loaded.CleanupMissingCacheFiles()
	if _, ok := loaded.GetCachedAsset("v1.4.11", "ssh-tunnel-svc-windows-amd64.exe"); !ok {
		t.Fatalf("expected cached asset to be present after load")
	}

	if !ReconcilePendingInstall(loaded, "v1.4.11") {
		t.Fatalf("expected pending install to reconcile")
	}
	if loaded.PendingInstall != nil {
		t.Fatalf("expected pending install to be cleared")
	}
	if loaded.LastInstallMessage == "" {
		t.Fatalf("expected install message after reconcile")
	}
}
