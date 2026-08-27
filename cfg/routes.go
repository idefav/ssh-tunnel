package cfg

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	RouteStoreVersion   = 1
	RouteTypeDomain     = "domain"
	RouteTypeIP         = "ip"
	RouteTypeCIDR       = "cidr"
	RouteStrategyFixed  = "fixed"
	RouteStrategyRandom = "random"
)

// RouteRule maps one domain/IP/CIDR pattern to one or more SSH profiles.
// A fixed rule has exactly one target; a random rule has at least two targets.
type RouteRule struct {
	ID               string   `json:"id"`
	Pattern          string   `json:"pattern"`
	Type             string   `json:"type"`
	Enabled          bool     `json:"enabled"`
	Strategy         string   `json:"strategy"`
	TargetProfileIDs []string `json:"targetProfileIds"`
}

type RouteStore struct {
	Version int         `json:"version"`
	Routes  []RouteRule `json:"routes"`
}

func ResolveStateFilePath(name string) (string, error) {
	config := GetConfigInstance()
	if config == nil {
		return "", fmt.Errorf("配置实例未初始化")
	}
	if configFile := strings.TrimSpace(config.ConfigFileUsed()); configFile != "" {
		return filepath.Join(filepath.Dir(configFile), name), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法确定状态文件目录: %w", err)
	}
	if strings.TrimSpace(homeDir) == "" {
		return "", fmt.Errorf("无法确定状态文件目录")
	}
	return filepath.Join(homeDir, ".ssh-tunnel", name), nil
}

func routeStorePath() (string, error) {
	return ResolveStateFilePath("routes.json")
}

func loadRouteStoreFile() (RouteStore, error) {
	store := RouteStore{Version: RouteStoreVersion, Routes: []RouteRule{}}
	filePath, err := routeStorePath()
	if err != nil {
		return RouteStore{}, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return RouteStore{}, fmt.Errorf("读取routes文件失败(%s): %w", filePath, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return RouteStore{}, fmt.Errorf("解析routes文件失败(%s): %w", filePath, err)
	}
	if store.Version == 0 {
		store.Version = RouteStoreVersion
	}
	if store.Version != RouteStoreVersion {
		return RouteStore{}, fmt.Errorf("不支持的routes版本: %d", store.Version)
	}
	if store.Routes == nil {
		store.Routes = []RouteRule{}
	}
	return store, nil
}

func saveRouteStoreFile(store RouteStore) error {
	filePath, err := routeStorePath()
	if err != nil {
		return err
	}
	return saveRouteStoreAt(filePath, store)
}

func saveRouteStoreAt(filePath string, store RouteStore) error {
	store.Version = RouteStoreVersion
	if store.Routes == nil {
		store.Routes = []RouteRule{}
	}
	content, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化routes配置失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("创建routes目录失败(%s): %w", filePath, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(filePath), ".routes-*.tmp")
	if err != nil {
		return fmt.Errorf("创建routes临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(content, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("写入routes临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("同步routes临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭routes临时文件失败: %w", err)
	}
	if err := replaceFileAtomically(tmpPath, filePath); err != nil {
		return fmt.Errorf("原子替换routes文件失败(%s): %w", filePath, err)
	}
	if directory, err := os.Open(filepath.Dir(filePath)); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func newRouteID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成route id失败: %w", err)
	}
	return "route_" + hex.EncodeToString(buf), nil
}

func legacyRouteID(profileID, routeType, pattern string) string {
	sum := sha256.Sum256([]byte(profileID + "\x00" + routeType + "\x00" + pattern))
	return "legacy_" + hex.EncodeToString(sum[:16])
}

func guessStandaloneRouteType(pattern string) string {
	if strings.Contains(pattern, "/") {
		return RouteTypeCIDR
	}
	if strings.HasPrefix(pattern, "*.") {
		return RouteTypeDomain
	}
	if looksLikeIPv4Pattern(pattern) {
		return RouteTypeIP
	}
	return RouteTypeDomain
}

func normalizeRoutePattern(routeType, pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", fmt.Errorf("匹配模式不能为空")
	}
	switch routeType {
	case RouteTypeDomain:
		pattern = strings.ToLower(strings.TrimRight(pattern, "."))
		base := strings.TrimPrefix(pattern, "*.")
		if base == "" || strings.ContainsAny(base, " /:") || strings.Contains(base, "*") {
			return "", fmt.Errorf("无效域名模式: %s", pattern)
		}
		return pattern, nil
	case RouteTypeIP:
		parts := strings.Split(pattern, ".")
		if len(parts) == 0 || len(parts) > 4 {
			return "", fmt.Errorf("无效IP模式: %s", pattern)
		}
		for _, part := range parts {
			if part == "*" {
				continue
			}
			value, err := strconv.Atoi(part)
			if err != nil || value < 0 || value > 255 {
				return "", fmt.Errorf("无效IP模式: %s", pattern)
			}
		}
		if len(parts) < 4 {
			if parts[len(parts)-1] != "*" {
				return "", fmt.Errorf("缩写IP模式必须以通配符结尾: %s", pattern)
			}
			for len(parts) < 4 {
				parts = append(parts, "*")
			}
		}
		return strings.Join(parts, "."), nil
	case RouteTypeCIDR:
		_, network, err := net.ParseCIDR(pattern)
		if err != nil {
			return "", fmt.Errorf("无效CIDR模式: %s", pattern)
		}
		return network.String(), nil
	default:
		return "", fmt.Errorf("不支持的路由类型: %s", routeType)
	}
}

func looksLikeIPv4Pattern(pattern string) bool {
	parts := strings.Split(strings.TrimSpace(pattern), ".")
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	for _, part := range parts {
		if part == "*" {
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 255 {
			return false
		}
	}
	return len(parts) == 4 || parts[len(parts)-1] == "*"
}

func NormalizeAndValidateRoute(rule RouteRule, profiles map[string]SSHProfile) (RouteRule, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Type = strings.ToLower(strings.TrimSpace(rule.Type))
	if rule.Type == "" {
		rule.Type = guessStandaloneRouteType(strings.TrimSpace(rule.Pattern))
	}
	pattern, err := normalizeRoutePattern(rule.Type, rule.Pattern)
	if err != nil {
		return RouteRule{}, err
	}
	rule.Pattern = pattern
	rule.Strategy = strings.ToLower(strings.TrimSpace(rule.Strategy))
	if rule.Strategy == "" {
		rule.Strategy = RouteStrategyFixed
	}

	seenTargets := make(map[string]bool)
	targets := make([]string, 0, len(rule.TargetProfileIDs))
	for _, target := range rule.TargetProfileIDs {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if seenTargets[target] {
			return RouteRule{}, fmt.Errorf("目标Profile重复: %s", target)
		}
		if _, ok := profiles[target]; !ok {
			return RouteRule{}, fmt.Errorf("目标Profile不存在: %s", target)
		}
		seenTargets[target] = true
		targets = append(targets, target)
	}
	rule.TargetProfileIDs = targets
	switch rule.Strategy {
	case RouteStrategyFixed:
		if len(targets) != 1 {
			return RouteRule{}, fmt.Errorf("fixed路由必须且只能指定一个目标Profile")
		}
	case RouteStrategyRandom:
		if len(targets) < 2 {
			return RouteRule{}, fmt.Errorf("random路由至少需要两个目标Profile")
		}
	default:
		return RouteRule{}, fmt.Errorf("不支持的路由策略: %s", rule.Strategy)
	}
	return rule, nil
}

func routeUniquenessKey(rule RouteRule) string {
	return strings.ToLower(rule.Type) + "\x00" + strings.ToLower(rule.Pattern)
}

func validateRouteStore(store RouteStore, profiles map[string]SSHProfile) (RouteStore, error) {
	seenIDs := make(map[string]bool)
	seenPatterns := make(map[string]bool)
	out := RouteStore{Version: RouteStoreVersion, Routes: make([]RouteRule, 0, len(store.Routes))}
	for _, rule := range store.Routes {
		normalized, err := NormalizeAndValidateRoute(rule, profiles)
		if err != nil {
			return RouteStore{}, fmt.Errorf("路由%s无效: %w", rule.ID, err)
		}
		if normalized.ID == "" {
			return RouteStore{}, fmt.Errorf("route id不能为空")
		}
		if seenIDs[normalized.ID] {
			return RouteStore{}, fmt.Errorf("route id重复: %s", normalized.ID)
		}
		key := routeUniquenessKey(normalized)
		if seenPatterns[key] {
			return RouteStore{}, fmt.Errorf("路由匹配模式重复: %s/%s", normalized.Type, normalized.Pattern)
		}
		seenIDs[normalized.ID] = true
		seenPatterns[key] = true
		out.Routes = append(out.Routes, normalized)
	}
	return out, nil
}

func migrateLegacyDomainRoutes(store RouteStore, profiles ProfileStore) (RouteStore, ProfileStore, bool, error) {
	profileIDs := make([]string, 0, len(profiles.Profiles))
	for id := range profiles.Profiles {
		profileIDs = append(profileIDs, id)
	}
	sort.Strings(profileIDs)
	legacyFound := false
	existingPatterns := make(map[string]RouteRule)
	for _, rule := range store.Routes {
		existingPatterns[routeUniquenessKey(rule)] = rule
	}
	for _, profileID := range profileIDs {
		profile := profiles.Profiles[profileID]
		if len(profile.DomainRoutes) == 0 {
			continue
		}
		legacyFound = true
		legacyRoutes := append([]DomainRoute(nil), profile.DomainRoutes...)
		sort.SliceStable(legacyRoutes, func(i, j int) bool {
			left := strings.ToLower(strings.TrimSpace(legacyRoutes[i].Type)) + "\x00" + strings.ToLower(strings.TrimSpace(legacyRoutes[i].Pattern))
			right := strings.ToLower(strings.TrimSpace(legacyRoutes[j].Type)) + "\x00" + strings.ToLower(strings.TrimSpace(legacyRoutes[j].Pattern))
			return left < right
		})
		for _, legacy := range legacyRoutes {
			routeType := strings.ToLower(strings.TrimSpace(legacy.Type))
			if routeType == "" {
				routeType = guessStandaloneRouteType(strings.TrimSpace(legacy.Pattern))
			}
			pattern, err := normalizeRoutePattern(routeType, legacy.Pattern)
			if err != nil {
				return store, profiles, false, fmt.Errorf("Profile %s 的旧路由无效: %w", profileID, err)
			}
			key := routeType + "\x00" + pattern
			candidate := RouteRule{
				ID:               legacyRouteID(profileID, routeType, pattern),
				Pattern:          pattern,
				Type:             routeType,
				Enabled:          true,
				Strategy:         RouteStrategyFixed,
				TargetProfileIDs: []string{profileID},
			}
			if existing, exists := existingPatterns[key]; exists {
				if existing.ID == candidate.ID {
					continue
				}
				return store, profiles, false, fmt.Errorf("旧路由迁移冲突: %s/%s 已被规则 %s 使用", routeType, pattern, existing.ID)
			}
			store.Routes = append(store.Routes, candidate)
			existingPatterns[key] = candidate
		}
		profile.DomainRoutes = nil
		profiles.Profiles[profileID] = profile
	}
	if !legacyFound {
		return store, profiles, false, nil
	}
	return store, profiles, true, nil
}

// ListRoutes loads, validates, and if necessary migrates profile-embedded routes.
func ListRoutes(appConfig *AppConfig) (RouteStore, error) {
	store, err := loadRouteStoreFile()
	if err != nil {
		return RouteStore{}, err
	}
	profiles, err := ListProfiles(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	store, migratedProfiles, migrated, err := migrateLegacyDomainRoutes(store, profiles)
	if err != nil {
		return RouteStore{}, err
	}
	validated, err := validateRouteStore(store, profiles.Profiles)
	if err != nil {
		return RouteStore{}, err
	}
	if migrated {
		if err := saveRouteStoreFile(validated); err != nil {
			return RouteStore{}, err
		}
		if err := saveProfileStore(migratedProfiles); err != nil {
			return RouteStore{}, fmt.Errorf("清理Profile旧路由字段失败: %w", err)
		}
	}
	return validated, nil
}

func UpsertRoute(rule RouteRule, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	profiles, err := ListProfiles(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	creating := strings.TrimSpace(rule.ID) == ""
	if creating {
		rule.ID, err = newRouteID()
		if err != nil {
			return RouteStore{}, err
		}
	} else {
		found := false
		for _, existing := range store.Routes {
			if existing.ID == strings.TrimSpace(rule.ID) {
				found = true
				break
			}
		}
		if !found {
			return RouteStore{}, fmt.Errorf("路由不存在: %s", rule.ID)
		}
	}
	normalized, err := NormalizeAndValidateRoute(rule, profiles.Profiles)
	if err != nil {
		return RouteStore{}, err
	}
	updated := false
	for i := range store.Routes {
		if store.Routes[i].ID == normalized.ID {
			store.Routes[i] = normalized
			updated = true
			break
		}
	}
	if creating && !updated {
		store.Routes = append(store.Routes, normalized)
	}
	store, err = validateRouteStore(store, profiles.Profiles)
	if err != nil {
		return RouteStore{}, err
	}
	if err := saveRouteStoreFile(store); err != nil {
		return RouteStore{}, err
	}
	return store, nil
}

func ToggleRoute(routeID string, enabled bool, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	found := false
	for i := range store.Routes {
		if store.Routes[i].ID == strings.TrimSpace(routeID) {
			store.Routes[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		return RouteStore{}, fmt.Errorf("路由不存在: %s", routeID)
	}
	if err := saveRouteStoreFile(store); err != nil {
		return RouteStore{}, err
	}
	return store, nil
}

func DeleteRoute(routeID string, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	routeID = strings.TrimSpace(routeID)
	out := make([]RouteRule, 0, len(store.Routes))
	found := false
	for _, rule := range store.Routes {
		if rule.ID == routeID {
			found = true
			continue
		}
		out = append(out, rule)
	}
	if !found {
		return RouteStore{}, fmt.Errorf("路由不存在: %s", routeID)
	}
	store.Routes = out
	if err := saveRouteStoreFile(store); err != nil {
		return RouteStore{}, err
	}
	return store, nil
}

func ReferencingRouteIDs(profileID string, appConfig *AppConfig) ([]string, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, rule := range store.Routes {
		for _, target := range rule.TargetProfileIDs {
			if target == profileID {
				ids = append(ids, rule.ID)
				break
			}
		}
	}
	return ids, nil
}
