package cfg

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

const (
	PROFILES_JSON_KEY     = "profiles.json"
	ACTIVE_PROFILE_ID_KEY = "profiles.active"
	DEFAULT_PROFILE_ID    = "default"
)

type SSHProfile struct {
	ServerIp                 string `json:"serverIp"`
	ServerSshPort            int    `json:"serverSshPort"`
	LoginUser                string `json:"loginUser"`
	SshPrivateKeyPath        string `json:"sshPrivateKeyPath"`
	LocalAddress             string `json:"localAddress"`
	HttpLocalAddress         string `json:"httpLocalAddress"`
	EnableHttp               bool   `json:"enableHttp"`
	EnableSocks5             bool   `json:"enableSocks5"`
	EnableHttpOverSSH        bool   `json:"enableHttpOverSSH"`
	HttpBasicAuthEnable      bool   `json:"httpBasicAuthEnable"`
	HttpBasicUserName        string `json:"httpBasicUserName"`
	HttpBasicPassword        string `json:"httpBasicPassword"`
	EnableHttpDomainFilter   bool   `json:"enableHttpDomainFilter"`
	HttpDomainFilterFilePath string `json:"httpDomainFilterFilePath"`
	RetryIntervalSec         int    `json:"retryIntervalSec"`
}

type ProfileStore struct {
	ActiveProfileID string                `json:"activeProfileId"`
	Profiles        map[string]SSHProfile `json:"profiles"`
}

func defaultProfileFromAppConfig(appConfig *AppConfig) SSHProfile {
	if appConfig == nil {
		return SSHProfile{}
	}
	return SSHProfile{
		ServerIp:                 appConfig.ServerIp.GetValue(),
		ServerSshPort:            appConfig.ServerSshPort.GetValue(),
		LoginUser:                appConfig.LoginUser.GetValue(),
		SshPrivateKeyPath:        appConfig.SshPrivateKeyPath.GetValue(),
		LocalAddress:             appConfig.LocalAddress.GetValue(),
		HttpLocalAddress:         appConfig.HttpLocalAddress.GetValue(),
		EnableHttp:               appConfig.EnableHttp.GetValue(),
		EnableSocks5:             appConfig.EnableSocks5.GetValue(),
		EnableHttpOverSSH:        appConfig.EnableHttpOverSSH.GetValue(),
		HttpBasicAuthEnable:      appConfig.HttpBasicAuthEnable.GetValue(),
		HttpBasicUserName:        appConfig.HttpBasicUserName.GetValue(),
		HttpBasicPassword:        appConfig.HttpBasicPassword.GetValue(),
		EnableHttpDomainFilter:   appConfig.EnableHttpDomainFilter.GetValue(),
		HttpDomainFilterFilePath: appConfig.HttpDomainFilterFilePath.GetValue(),
		RetryIntervalSec:         appConfig.RetryIntervalSec.GetValue(),
	}
}

func loadProfileStoreFromConfig() (ProfileStore, error) {
	config := GetConfigInstance()
	if config == nil {
		return ProfileStore{}, fmt.Errorf("配置实例未初始化")
	}

	store := ProfileStore{Profiles: make(map[string]SSHProfile)}

	// 优先读取独立 profiles.json 文件（配置目录 -> 用户目录）
	profileFilePaths := resolveProfilesFilePaths(config)
	if err := ensureProfilesFileExists(profileFilePaths); err != nil {
		return ProfileStore{}, err
	}
	loadedFromFile := false
	for _, profilesFilePath := range profileFilePaths {
		fileStore, fileLoaded, err := tryLoadProfileStoreFromFile(profilesFilePath)
		if err != nil {
			return ProfileStore{}, err
		}
		if fileLoaded {
			store = fileStore
			loadedFromFile = true
			break
		}
	}

	if !loadedFromFile {
		// 回退到配置键读取
		profilesRaw := strings.TrimSpace(config.GetString(PROFILES_JSON_KEY))
		if profilesRaw != "" {
			if err := json.Unmarshal([]byte(profilesRaw), &store); err != nil {
				return ProfileStore{}, fmt.Errorf("解析profiles配置失败: %w", err)
			}
			if store.Profiles == nil {
				store.Profiles = make(map[string]SSHProfile)
			}
		}
	}

	if store.ActiveProfileID == "" {
		store.ActiveProfileID = config.GetString(ACTIVE_PROFILE_ID_KEY)
	}
	if store.Profiles == nil {
		store.Profiles = make(map[string]SSHProfile)
	}

	return store, nil
}

func saveProfileStore(store ProfileStore) error {
	config := GetConfigInstance()
	if config == nil {
		return fmt.Errorf("配置实例未初始化")
	}
	if store.Profiles == nil {
		store.Profiles = make(map[string]SSHProfile)
	}

	compactJSON, err := json.Marshal(store)
	if err != nil {
		return fmt.Errorf("序列化profiles配置失败: %w", err)
	}
	config.Set(PROFILES_JSON_KEY, string(compactJSON))
	config.Set(ACTIVE_PROFILE_ID_KEY, store.ActiveProfileID)
	if err := SaveConfig(); err != nil {
		return err
	}

	prettyJSON, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化profiles美化JSON失败: %w", err)
	}

	profileFilePaths := resolveProfilesFilePaths(config)
	writeSuccess := 0
	var firstErr error
	for _, profilesFilePath := range profileFilePaths {
		if strings.TrimSpace(profilesFilePath) == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(profilesFilePath), 0755); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("创建profiles目录失败(%s): %w", profilesFilePath, err)
			}
			log.Printf("创建profiles目录失败(%s): %v", profilesFilePath, err)
			continue
		}
		if err := os.WriteFile(profilesFilePath, append(prettyJSON, '\n'), 0644); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("写入profiles文件失败(%s): %w", profilesFilePath, err)
			}
			log.Printf("写入profiles文件失败(%s): %v", profilesFilePath, err)
			continue
		}
		writeSuccess++
		log.Printf("已写入profiles文件: %s", profilesFilePath)
	}

	if writeSuccess == 0 && firstErr != nil {
		return firstErr
	}

	return nil
}

func resolveProfilesFilePaths(config interface{ ConfigFileUsed() string }) []string {
	paths := make([]string, 0, 3)
	added := make(map[string]bool)
	addPath := func(p string) {
		normalized := strings.TrimSpace(p)
		if normalized == "" || added[normalized] {
			return
		}
		added[normalized] = true
		paths = append(paths, normalized)
	}

	configFilePath := strings.TrimSpace(config.ConfigFileUsed())
	if configFilePath != "" {
		addPath(filepath.Join(filepath.Dir(configFilePath), "profiles.json"))
	}

	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		addPath(filepath.Join(homeDir, ".ssh-tunnel", "profiles.json"))
	}

	if len(paths) == 0 {
		addPath("profiles.json")
	}

	return paths
}

func ensureProfilesFileExists(paths []string) error {
	hasExisting := false
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			hasExisting = true
			break
		}
	}

	if hasExisting {
		return nil
	}

	if len(paths) == 0 || strings.TrimSpace(paths[0]) == "" {
		return nil
	}

	target := paths[0]
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("创建profiles目录失败(%s): %w", target, err)
	}

	emptyStore := ProfileStore{
		ActiveProfileID: "",
		Profiles:        map[string]SSHProfile{},
	}
	content, err := json.MarshalIndent(emptyStore, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化空profiles配置失败: %w", err)
	}
	if err := os.WriteFile(target, append(content, '\n'), 0644); err != nil {
		return fmt.Errorf("创建空profiles文件失败(%s): %w", target, err)
	}
	log.Printf("profiles文件不存在，已自动创建空文件: %s", target)
	return nil
}

func tryLoadProfileStoreFromFile(filePath string) (ProfileStore, bool, error) {
	store := ProfileStore{Profiles: make(map[string]SSHProfile)}
	if strings.TrimSpace(filePath) == "" {
		return store, false, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return store, false, nil
		}
		return ProfileStore{}, false, fmt.Errorf("读取profiles文件失败(%s): %w", filePath, err)
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return store, false, nil
	}

	if err := json.Unmarshal(data, &store); err != nil {
		return ProfileStore{}, false, fmt.Errorf("解析profiles文件失败(%s): %w", filePath, err)
	}
	if store.Profiles == nil {
		store.Profiles = make(map[string]SSHProfile)
	}

	log.Printf("已从profiles文件加载配置: %s", filePath)
	return store, true, nil
}

func EnsureAndApplyActiveProfile(appConfig *AppConfig) error {
	store, err := loadProfileStoreFromConfig()
	if err != nil {
		return err
	}

	if len(store.Profiles) == 0 {
		log.Printf("profiles为空，跳过active profile应用，保持当前配置")
		return nil
	}

	activeProfileID := strings.TrimSpace(store.ActiveProfileID)
	if activeProfileID == "" {
		activeProfileID = DEFAULT_PROFILE_ID
	}
	activeProfile, ok := store.Profiles[activeProfileID]
	if !ok {
		log.Printf("active profile不存在(%s)，跳过应用，保持当前配置", activeProfileID)
		return nil
	}

	ApplyProfileToAppConfig(appConfig, activeProfile)
	return nil
}

func ApplyProfileToAppConfig(appConfig *AppConfig, profile SSHProfile) {
	if appConfig == nil {
		return
	}
	appConfig.ServerIp.SetValue(profile.ServerIp)
	appConfig.ServerSshPort.SetValue(profile.ServerSshPort)
	appConfig.LoginUser.SetValue(profile.LoginUser)
	appConfig.SshPrivateKeyPath.SetValue(profile.SshPrivateKeyPath)
	appConfig.LocalAddress.SetValue(profile.LocalAddress)
	appConfig.HttpLocalAddress.SetValue(profile.HttpLocalAddress)
	appConfig.EnableHttp.SetValue(profile.EnableHttp)
	appConfig.EnableSocks5.SetValue(profile.EnableSocks5)
	appConfig.EnableHttpOverSSH.SetValue(profile.EnableHttpOverSSH)
	appConfig.HttpBasicAuthEnable.SetValue(profile.HttpBasicAuthEnable)
	appConfig.HttpBasicUserName.SetValue(profile.HttpBasicUserName)
	appConfig.HttpBasicPassword.SetValue(profile.HttpBasicPassword)
	appConfig.EnableHttpDomainFilter.SetValue(profile.EnableHttpDomainFilter)
	appConfig.HttpDomainFilterFilePath.SetValue(profile.HttpDomainFilterFilePath)
	if profile.RetryIntervalSec > 0 {
		appConfig.RetryIntervalSec.SetValue(profile.RetryIntervalSec)
	}
}

func ListProfiles(appConfig *AppConfig) (ProfileStore, error) {
	store, err := loadProfileStoreFromConfig()
	if err != nil {
		return ProfileStore{}, err
	}
	return store, nil
}

func SwitchActiveProfile(profileID string, appConfig *AppConfig) (ProfileStore, error) {
	if profileID == "" {
		return ProfileStore{}, fmt.Errorf("profile id 不能为空")
	}
	store, err := ListProfiles(appConfig)
	if err != nil {
		return ProfileStore{}, err
	}
	profile, ok := store.Profiles[profileID]
	if !ok {
		return ProfileStore{}, fmt.Errorf("profile不存在: %s", profileID)
	}

	store.ActiveProfileID = profileID
	if err := saveProfileStore(store); err != nil {
		return ProfileStore{}, err
	}
	ApplyProfileToAppConfig(appConfig, profile)
	return store, nil
}

func UpsertProfile(profileID string, profile SSHProfile, appConfig *AppConfig) (ProfileStore, error) {
	if profileID == "" {
		return ProfileStore{}, fmt.Errorf("profile id 不能为空")
	}
	store, err := ListProfiles(appConfig)
	if err != nil {
		return ProfileStore{}, err
	}
	if store.Profiles == nil {
		store.Profiles = make(map[string]SSHProfile)
	}
	store.Profiles[profileID] = profile
	if err := saveProfileStore(store); err != nil {
		return ProfileStore{}, err
	}
	return store, nil
}

func DeleteProfile(profileID string, appConfig *AppConfig) (ProfileStore, error) {
	if profileID == "" {
		return ProfileStore{}, fmt.Errorf("profile id 不能为空")
	}
	store, err := ListProfiles(appConfig)
	if err != nil {
		return ProfileStore{}, err
	}
	if profileID == store.ActiveProfileID {
		return ProfileStore{}, fmt.Errorf("不能删除当前激活的profile: %s", profileID)
	}
	if _, ok := store.Profiles[profileID]; !ok {
		return ProfileStore{}, fmt.Errorf("profile不存在: %s", profileID)
	}
	delete(store.Profiles, profileID)
	if len(store.Profiles) == 0 {
		store.Profiles[DEFAULT_PROFILE_ID] = defaultProfileFromAppConfig(appConfig)
		store.ActiveProfileID = DEFAULT_PROFILE_ID
	}
	if err := saveProfileStore(store); err != nil {
		return ProfileStore{}, err
	}
	return store, nil
}
