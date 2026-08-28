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
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	RouteStoreVersion        = 2
	legacyRouteStoreVersion  = 1
	RouteTypeDomain          = "domain"
	RouteTypeIP              = "ip"
	RouteTypeCIDR            = "cidr"
	RouteStrategyFixed       = "fixed"
	RouteStrategyRandom      = "random"
	RouteInheritanceInherit  = "inherit"
	RouteInheritanceCurrent  = "preserve-current"
	RouteInheritanceOverride = "override"
	migratedRouteGroupName   = "未分组（自动迁移）"
)

type RouteRule struct {
	ID               string   `json:"id"`
	Pattern          string   `json:"pattern"`
	Type             string   `json:"type"`
	Enabled          bool     `json:"enabled"`
	Strategy         string   `json:"strategy,omitempty"`
	TargetProfileIDs []string `json:"targetProfileIds,omitempty"`
}

type RouteGroup struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	Description      string      `json:"description,omitempty"`
	Enabled          bool        `json:"enabled"`
	Strategy         string      `json:"strategy"`
	TargetProfileIDs []string    `json:"targetProfileIds"`
	Rules            []RouteRule `json:"rules"`
}

type RouteStore struct {
	Version int          `json:"version"`
	Groups  []RouteGroup `json:"groups"`
}

// EffectiveRoute is the fully resolved runtime representation of a rule.
type EffectiveRoute struct {
	ID               string
	GroupID          string
	GroupName        string
	Pattern          string
	Type             string
	Strategy         string
	TargetProfileIDs []string
}

type routeStoreFile struct {
	Version int          `json:"version"`
	Groups  []RouteGroup `json:"groups"`
	Routes  []RouteRule  `json:"routes"`
}

type RouteReferences struct {
	GroupIDs []string `json:"referencingGroupIds"`
	RouteIDs []string `json:"referencingRouteIds"`
}

type RoutePolicy struct {
	Strategy         string   `json:"strategy"`
	TargetProfileIDs []string `json:"targetProfileIds"`
}

type RouteBatchDestination struct {
	GroupID  string      `json:"groupId,omitempty"`
	NewGroup *RouteGroup `json:"newGroup,omitempty"`
}

type RouteBatchUpdate struct {
	RouteIDs        []string               `json:"routeIds"`
	Destination     *RouteBatchDestination `json:"destination,omitempty"`
	InheritanceMode string                 `json:"inheritanceMode,omitempty"`
	OverridePolicy  *RoutePolicy           `json:"overridePolicy,omitempty"`
	Enabled         *bool                  `json:"enabled,omitempty"`
}

type RouteBatchResult struct {
	Store          RouteStore `json:"data"`
	ChangedCount   int        `json:"changedCount"`
	MovedCount     int        `json:"movedCount"`
	CreatedGroupID string     `json:"createdGroupId,omitempty"`
}

type RouteGroupNotEmptyError struct {
	GroupID   string
	GroupName string
	RuleCount int
}

func (e *RouteGroupNotEmptyError) Error() string {
	return fmt.Sprintf("规则组%s包含%d条规则，删除时必须确认级联删除", e.GroupName, e.RuleCount)
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

func routeStorePath() (string, error) { return ResolveStateFilePath("routes.json") }

func loadRouteStoreFile() (routeStoreFile, error) {
	filePath, err := routeStorePath()
	if err != nil {
		return routeStoreFile{}, err
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return routeStoreFile{Version: RouteStoreVersion, Groups: []RouteGroup{}}, nil
		}
		return routeStoreFile{}, fmt.Errorf("读取routes文件失败(%s): %w", filePath, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return routeStoreFile{Version: RouteStoreVersion, Groups: []RouteGroup{}}, nil
	}
	var file routeStoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return routeStoreFile{}, fmt.Errorf("解析routes文件失败(%s): %w", filePath, err)
	}
	if file.Version == 0 {
		if len(file.Groups) > 0 {
			file.Version = RouteStoreVersion
		} else {
			file.Version = legacyRouteStoreVersion
		}
	}
	if file.Version != legacyRouteStoreVersion && file.Version != RouteStoreVersion {
		return routeStoreFile{}, fmt.Errorf("不支持的routes版本: %d", file.Version)
	}
	return file, nil
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
	if store.Groups == nil {
		store.Groups = []RouteGroup{}
	}
	for i := range store.Groups {
		if store.Groups[i].Rules == nil {
			store.Groups[i].Rules = []RouteRule{}
		}
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

func newPrefixedID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成%s id失败: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(buf), nil
}

func newRouteID() (string, error)      { return newPrefixedID("route") }
func newRouteGroupID() (string, error) { return newPrefixedID("group") }

func migratedRouteGroupID() string {
	sum := sha256.Sum256([]byte("ssh-tunnel:routes:v1:migrated-group"))
	return "group_" + hex.EncodeToString(sum[:16])
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

func normalizeAndValidatePolicy(strategy string, targetProfileIDs []string, profiles map[string]SSHProfile) (string, []string, error) {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	seenTargets := make(map[string]bool)
	targets := make([]string, 0, len(targetProfileIDs))
	for _, target := range targetProfileIDs {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if seenTargets[target] {
			return "", nil, fmt.Errorf("目标Profile重复: %s", target)
		}
		if _, ok := profiles[target]; !ok {
			return "", nil, fmt.Errorf("目标Profile不存在: %s", target)
		}
		seenTargets[target] = true
		targets = append(targets, target)
	}
	switch strategy {
	case RouteStrategyFixed:
		if len(targets) != 1 {
			return "", nil, fmt.Errorf("fixed路由必须且只能指定一个目标Profile")
		}
	case RouteStrategyRandom:
		if len(targets) < 2 {
			return "", nil, fmt.Errorf("random路由至少需要两个目标Profile")
		}
	default:
		return "", nil, fmt.Errorf("不支持的路由策略: %s", strategy)
	}
	return strategy, targets, nil
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
	hasStrategy := rule.Strategy != ""
	hasTargets := len(rule.TargetProfileIDs) > 0
	if hasStrategy != hasTargets {
		return RouteRule{}, fmt.Errorf("规则策略和目标Profile必须同时配置，或同时省略以继承规则组")
	}
	if !hasStrategy {
		rule.TargetProfileIDs = nil
		return rule, nil
	}
	rule.Strategy, rule.TargetProfileIDs, err = normalizeAndValidatePolicy(rule.Strategy, rule.TargetProfileIDs, profiles)
	if err != nil {
		return RouteRule{}, err
	}
	return rule, nil
}

func normalizeAndValidateGroup(group RouteGroup, profiles map[string]SSHProfile) (RouteGroup, error) {
	group.ID = strings.TrimSpace(group.ID)
	group.Name = strings.TrimSpace(group.Name)
	group.Description = strings.TrimSpace(group.Description)
	if group.ID == "" {
		return RouteGroup{}, fmt.Errorf("group id不能为空")
	}
	if group.Name == "" {
		return RouteGroup{}, fmt.Errorf("规则组名称不能为空")
	}
	var err error
	group.Strategy, group.TargetProfileIDs, err = normalizeAndValidatePolicy(group.Strategy, group.TargetProfileIDs, profiles)
	if err != nil {
		return RouteGroup{}, fmt.Errorf("规则组默认出口无效: %w", err)
	}
	if group.Rules == nil {
		group.Rules = []RouteRule{}
	}
	for i, rule := range group.Rules {
		normalized, err := NormalizeAndValidateRoute(rule, profiles)
		if err != nil {
			return RouteGroup{}, fmt.Errorf("路由%s无效: %w", rule.ID, err)
		}
		if normalized.ID == "" {
			return RouteGroup{}, fmt.Errorf("route id不能为空")
		}
		group.Rules[i] = normalized
	}
	return group, nil
}

func routeUniquenessKey(rule RouteRule) string {
	return strings.ToLower(rule.Type) + "\x00" + strings.ToLower(rule.Pattern)
}

func validateRouteStore(store RouteStore, profiles map[string]SSHProfile) (RouteStore, error) {
	seenGroupIDs := make(map[string]bool)
	seenGroupNames := make(map[string]bool)
	seenRouteIDs := make(map[string]bool)
	seenPatterns := make(map[string]bool)
	out := RouteStore{Version: RouteStoreVersion, Groups: make([]RouteGroup, 0, len(store.Groups))}
	for _, group := range store.Groups {
		normalized, err := normalizeAndValidateGroup(group, profiles)
		if err != nil {
			return RouteStore{}, err
		}
		if seenGroupIDs[normalized.ID] {
			return RouteStore{}, fmt.Errorf("group id重复: %s", normalized.ID)
		}
		nameKey := strings.ToLower(normalized.Name)
		if seenGroupNames[nameKey] {
			return RouteStore{}, fmt.Errorf("规则组名称重复: %s", normalized.Name)
		}
		seenGroupIDs[normalized.ID] = true
		seenGroupNames[nameKey] = true
		for _, rule := range normalized.Rules {
			if seenRouteIDs[rule.ID] {
				return RouteStore{}, fmt.Errorf("route id重复: %s", rule.ID)
			}
			key := routeUniquenessKey(rule)
			if seenPatterns[key] {
				return RouteStore{}, fmt.Errorf("路由匹配模式重复: %s/%s", rule.Type, rule.Pattern)
			}
			seenRouteIDs[rule.ID] = true
			seenPatterns[key] = true
		}
		out.Groups = append(out.Groups, normalized)
	}
	return out, nil
}

func explicitRoute(rule RouteRule) bool {
	return strings.TrimSpace(rule.Strategy) != "" && len(rule.TargetProfileIDs) > 0
}

func ResolveEffectiveRoutes(store RouteStore) []EffectiveRoute {
	var routes []EffectiveRoute
	for _, group := range store.Groups {
		if !group.Enabled {
			continue
		}
		for _, rule := range group.Rules {
			if !rule.Enabled {
				continue
			}
			strategy := group.Strategy
			targets := group.TargetProfileIDs
			if explicitRoute(rule) {
				strategy = rule.Strategy
				targets = rule.TargetProfileIDs
			}
			routes = append(routes, EffectiveRoute{ID: rule.ID, GroupID: group.ID, GroupName: group.Name, Pattern: rule.Pattern, Type: rule.Type, Strategy: strategy, TargetProfileIDs: append([]string(nil), targets...)})
		}
	}
	return routes
}

func migratedGroupForRules(rules []RouteRule) RouteGroup {
	group := RouteGroup{ID: migratedRouteGroupID(), Name: migratedRouteGroupName, Enabled: true, Rules: []RouteRule{}}
	if len(rules) > 0 {
		group.Strategy = rules[0].Strategy
		group.TargetProfileIDs = append([]string(nil), rules[0].TargetProfileIDs...)
	}
	return group
}

func ensureMigratedGroup(store *RouteStore, seed RouteRule) *RouteGroup {
	groupID := migratedRouteGroupID()
	for i := range store.Groups {
		if store.Groups[i].ID == groupID {
			return &store.Groups[i]
		}
	}
	group := migratedGroupForRules([]RouteRule{seed})
	store.Groups = append(store.Groups, group)
	return &store.Groups[len(store.Groups)-1]
}

func migrateRouteData(file routeStoreFile, profiles ProfileStore) (RouteStore, ProfileStore, bool, bool, error) {
	store := RouteStore{Version: RouteStoreVersion, Groups: append([]RouteGroup(nil), file.Groups...)}
	routesChanged := file.Version != RouteStoreVersion
	profilesChanged := false
	if file.Version == legacyRouteStoreVersion {
		store.Groups = nil
		if len(file.Routes) > 0 {
			group := migratedGroupForRules(file.Routes)
			group.Rules = append([]RouteRule(nil), file.Routes...)
			store.Groups = []RouteGroup{group}
		}
	}

	existingPatterns := make(map[string]RouteRule)
	existingIDs := make(map[string]bool)
	for _, group := range store.Groups {
		for _, rule := range group.Rules {
			existingPatterns[routeUniquenessKey(rule)] = rule
			existingIDs[rule.ID] = true
		}
	}
	profileIDs := make([]string, 0, len(profiles.Profiles))
	for id := range profiles.Profiles {
		profileIDs = append(profileIDs, id)
	}
	sort.Strings(profileIDs)
	for _, profileID := range profileIDs {
		profile := profiles.Profiles[profileID]
		if len(profile.DomainRoutes) == 0 {
			continue
		}
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
				return store, profiles, false, false, fmt.Errorf("Profile %s 的旧路由无效: %w", profileID, err)
			}
			candidate := RouteRule{ID: legacyRouteID(profileID, routeType, pattern), Pattern: pattern, Type: routeType, Enabled: true, Strategy: RouteStrategyFixed, TargetProfileIDs: []string{profileID}}
			key := routeUniquenessKey(candidate)
			if existing, exists := existingPatterns[key]; exists {
				if existing.ID == candidate.ID {
					continue
				}
				return store, profiles, false, false, fmt.Errorf("旧路由迁移冲突: %s/%s 已被规则 %s 使用", routeType, pattern, existing.ID)
			}
			if existingIDs[candidate.ID] {
				return store, profiles, false, false, fmt.Errorf("旧路由迁移ID冲突: %s", candidate.ID)
			}
			group := ensureMigratedGroup(&store, candidate)
			group.Rules = append(group.Rules, candidate)
			existingPatterns[key] = candidate
			existingIDs[candidate.ID] = true
			routesChanged = true
		}
		profile.DomainRoutes = nil
		profiles.Profiles[profileID] = profile
		profilesChanged = true
	}
	if store.Groups == nil {
		store.Groups = []RouteGroup{}
	}
	validated, err := validateRouteStore(store, profiles.Profiles)
	if err != nil {
		return RouteStore{}, profiles, false, false, err
	}
	return validated, profiles, routesChanged, profilesChanged, nil
}

func ListRoutes(appConfig *AppConfig) (RouteStore, error) {
	file, err := loadRouteStoreFile()
	if err != nil {
		return RouteStore{}, err
	}
	profiles, err := ListProfiles(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	store, migratedProfiles, routesChanged, profilesChanged, err := migrateRouteData(file, profiles)
	if err != nil {
		return RouteStore{}, err
	}
	if routesChanged || profilesChanged {
		if err := saveRouteStoreFile(store); err != nil {
			return RouteStore{}, err
		}
	}
	if profilesChanged {
		if err := saveProfileStore(migratedProfiles); err != nil {
			return RouteStore{}, fmt.Errorf("清理Profile旧路由字段失败: %w", err)
		}
	}
	return store, nil
}

func UpsertRouteGroup(group RouteGroup, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	profiles, err := ListProfiles(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	creating := strings.TrimSpace(group.ID) == ""
	if creating {
		group.ID, err = newRouteGroupID()
		if err != nil {
			return RouteStore{}, err
		}
		group.Rules = []RouteRule{}
	} else {
		found := false
		for _, existing := range store.Groups {
			if existing.ID == strings.TrimSpace(group.ID) {
				group.Rules = existing.Rules
				found = true
				break
			}
		}
		if !found {
			return RouteStore{}, fmt.Errorf("规则组不存在: %s", group.ID)
		}
	}
	normalized, err := normalizeAndValidateGroup(group, profiles.Profiles)
	if err != nil {
		return RouteStore{}, err
	}
	if creating {
		store.Groups = append(store.Groups, normalized)
	} else {
		for i := range store.Groups {
			if store.Groups[i].ID == normalized.ID {
				store.Groups[i] = normalized
				break
			}
		}
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

func ToggleRouteGroup(groupID string, enabled bool, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	groupID = strings.TrimSpace(groupID)
	for i := range store.Groups {
		if store.Groups[i].ID == groupID {
			store.Groups[i].Enabled = enabled
			if err := saveRouteStoreFile(store); err != nil {
				return RouteStore{}, err
			}
			return store, nil
		}
	}
	return RouteStore{}, fmt.Errorf("规则组不存在: %s", groupID)
}

func DeleteRouteGroup(groupID string, cascade bool, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	groupID = strings.TrimSpace(groupID)
	out := make([]RouteGroup, 0, len(store.Groups))
	found := false
	for _, group := range store.Groups {
		if group.ID != groupID {
			out = append(out, group)
			continue
		}
		found = true
		if len(group.Rules) > 0 && !cascade {
			return RouteStore{}, &RouteGroupNotEmptyError{GroupID: group.ID, GroupName: group.Name, RuleCount: len(group.Rules)}
		}
	}
	if !found {
		return RouteStore{}, fmt.Errorf("规则组不存在: %s", groupID)
	}
	store.Groups = out
	if err := saveRouteStoreFile(store); err != nil {
		return RouteStore{}, err
	}
	return store, nil
}

func UpsertRoute(groupID string, rule RouteRule, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	profiles, err := ListProfiles(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	groupID = strings.TrimSpace(groupID)
	destination := -1
	for i := range store.Groups {
		if store.Groups[i].ID == groupID {
			destination = i
			break
		}
	}
	if destination < 0 {
		return RouteStore{}, fmt.Errorf("规则组不存在: %s", groupID)
	}
	creating := strings.TrimSpace(rule.ID) == ""
	sourceGroup := -1
	sourceRule := -1
	if creating {
		rule.ID, err = newRouteID()
		if err != nil {
			return RouteStore{}, err
		}
	} else {
		for groupIndex, group := range store.Groups {
			for ruleIndex, existing := range group.Rules {
				if existing.ID == strings.TrimSpace(rule.ID) {
					sourceGroup = groupIndex
					sourceRule = ruleIndex
					break
				}
			}
		}
		if sourceGroup < 0 {
			return RouteStore{}, fmt.Errorf("路由不存在: %s", rule.ID)
		}
	}
	normalized, err := NormalizeAndValidateRoute(rule, profiles.Profiles)
	if err != nil {
		return RouteStore{}, err
	}
	if creating {
		store.Groups[destination].Rules = append(store.Groups[destination].Rules, normalized)
	} else if sourceGroup == destination {
		// Editing in place preserves the stable order used for equal-specificity matches.
		store.Groups[sourceGroup].Rules[sourceRule] = normalized
	} else {
		sourceRules := store.Groups[sourceGroup].Rules
		store.Groups[sourceGroup].Rules = append(sourceRules[:sourceRule], sourceRules[sourceRule+1:]...)
		store.Groups[destination].Rules = append(store.Groups[destination].Rules, normalized)
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

type selectedBatchRoute struct {
	groupIndex        int
	rule              RouteRule
	effectiveStrategy string
	effectiveTargets  []string
}

func routeRulesEqual(left, right RouteRule) bool {
	return left.ID == right.ID &&
		left.Pattern == right.Pattern &&
		left.Type == right.Type &&
		left.Enabled == right.Enabled &&
		left.Strategy == right.Strategy &&
		slices.Equal(left.TargetProfileIDs, right.TargetProfileIDs)
}

// BatchUpdateRoutes applies a batch patch in memory, validates the complete
// route store, and persists it with a single atomic replacement.
func BatchUpdateRoutes(update RouteBatchUpdate, appConfig *AppConfig) (RouteBatchResult, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteBatchResult{}, err
	}
	profiles, err := ListProfiles(appConfig)
	if err != nil {
		return RouteBatchResult{}, err
	}

	if len(update.RouteIDs) == 0 {
		return RouteBatchResult{}, fmt.Errorf("至少选择一条路由规则")
	}
	selectedIDs := make(map[string]bool, len(update.RouteIDs))
	for _, rawID := range update.RouteIDs {
		routeID := strings.TrimSpace(rawID)
		if routeID == "" {
			return RouteBatchResult{}, fmt.Errorf("route id不能为空")
		}
		if selectedIDs[routeID] {
			return RouteBatchResult{}, fmt.Errorf("route id重复: %s", routeID)
		}
		selectedIDs[routeID] = true
	}

	mode := strings.ToLower(strings.TrimSpace(update.InheritanceMode))
	switch mode {
	case "", RouteInheritanceInherit, RouteInheritanceCurrent, RouteInheritanceOverride:
	default:
		return RouteBatchResult{}, fmt.Errorf("不支持的继承模式: %s", update.InheritanceMode)
	}
	if mode == RouteInheritanceOverride {
		if update.OverridePolicy == nil {
			return RouteBatchResult{}, fmt.Errorf("统一独立出口必须提供overridePolicy")
		}
	} else if update.OverridePolicy != nil {
		return RouteBatchResult{}, fmt.Errorf("overridePolicy仅可用于override继承模式")
	}

	var overrideStrategy string
	var overrideTargets []string
	if update.OverridePolicy != nil {
		overrideStrategy, overrideTargets, err = normalizeAndValidatePolicy(update.OverridePolicy.Strategy, update.OverridePolicy.TargetProfileIDs, profiles.Profiles)
		if err != nil {
			return RouteBatchResult{}, fmt.Errorf("统一独立出口无效: %w", err)
		}
	}

	hasDestination := update.Destination != nil
	if !hasDestination && mode == "" && update.Enabled == nil {
		return RouteBatchResult{}, fmt.Errorf("至少指定一个批量修改项")
	}

	selected := make([]selectedBatchRoute, 0, len(selectedIDs))
	for groupIndex, group := range store.Groups {
		for _, rule := range group.Rules {
			if !selectedIDs[rule.ID] {
				continue
			}
			strategy := group.Strategy
			targets := group.TargetProfileIDs
			if explicitRoute(rule) {
				strategy = rule.Strategy
				targets = rule.TargetProfileIDs
			}
			selected = append(selected, selectedBatchRoute{
				groupIndex:        groupIndex,
				rule:              rule,
				effectiveStrategy: strategy,
				effectiveTargets:  append([]string(nil), targets...),
			})
		}
	}
	if len(selected) != len(selectedIDs) {
		found := make(map[string]bool, len(selected))
		for _, item := range selected {
			found[item.rule.ID] = true
		}
		missing := make([]string, 0)
		for routeID := range selectedIDs {
			if !found[routeID] {
				missing = append(missing, routeID)
			}
		}
		sort.Strings(missing)
		return RouteBatchResult{}, fmt.Errorf("路由不存在: %s", strings.Join(missing, ", "))
	}

	destinationGroupID := ""
	createdGroupID := ""
	if hasDestination {
		destinationGroupID = strings.TrimSpace(update.Destination.GroupID)
		if destinationGroupID != "" && update.Destination.NewGroup != nil {
			return RouteBatchResult{}, fmt.Errorf("现有目标组和新规则组不能同时配置")
		}
		if destinationGroupID == "" && update.Destination.NewGroup == nil {
			return RouteBatchResult{}, fmt.Errorf("destination必须指定groupId或newGroup")
		}
		if destinationGroupID != "" {
			found := false
			for _, group := range store.Groups {
				if group.ID == destinationGroupID {
					found = true
					break
				}
			}
			if !found {
				return RouteBatchResult{}, fmt.Errorf("规则组不存在: %s", destinationGroupID)
			}
		} else {
			group := *update.Destination.NewGroup
			if strings.TrimSpace(group.ID) != "" || len(group.Rules) > 0 {
				return RouteBatchResult{}, fmt.Errorf("批量新建规则组不能指定id或rules")
			}
			group.ID, err = newRouteGroupID()
			if err != nil {
				return RouteBatchResult{}, err
			}
			group.Rules = []RouteRule{}
			group, err = normalizeAndValidateGroup(group, profiles.Profiles)
			if err != nil {
				return RouteBatchResult{}, err
			}
			store.Groups = append(store.Groups, group)
			destinationGroupID = group.ID
			createdGroupID = group.ID
		}
	}

	updatedByID := make(map[string]RouteRule, len(selected))
	changedCount := 0
	for _, item := range selected {
		rule := item.rule
		switch mode {
		case RouteInheritanceInherit:
			rule.Strategy = ""
			rule.TargetProfileIDs = nil
		case RouteInheritanceCurrent:
			rule.Strategy = item.effectiveStrategy
			rule.TargetProfileIDs = append([]string(nil), item.effectiveTargets...)
		case RouteInheritanceOverride:
			rule.Strategy = overrideStrategy
			rule.TargetProfileIDs = append([]string(nil), overrideTargets...)
		}
		if update.Enabled != nil {
			rule.Enabled = *update.Enabled
		}
		updatedByID[rule.ID] = rule
		if !routeRulesEqual(rule, item.rule) || (hasDestination && store.Groups[item.groupIndex].ID != destinationGroupID) {
			changedCount++
		}
	}

	movedCount := 0
	if hasDestination {
		incoming := make([]RouteRule, 0, len(selected))
		for groupIndex := range store.Groups {
			group := &store.Groups[groupIndex]
			kept := make([]RouteRule, 0, len(group.Rules))
			for _, rule := range group.Rules {
				updated, selectedRule := updatedByID[rule.ID]
				if !selectedRule {
					kept = append(kept, rule)
					continue
				}
				if group.ID == destinationGroupID {
					kept = append(kept, updated)
				} else {
					incoming = append(incoming, updated)
					movedCount++
				}
			}
			group.Rules = kept
		}
		for groupIndex := range store.Groups {
			if store.Groups[groupIndex].ID == destinationGroupID {
				store.Groups[groupIndex].Rules = append(store.Groups[groupIndex].Rules, incoming...)
				break
			}
		}
	} else {
		for groupIndex := range store.Groups {
			for ruleIndex, rule := range store.Groups[groupIndex].Rules {
				if updated, ok := updatedByID[rule.ID]; ok {
					store.Groups[groupIndex].Rules[ruleIndex] = updated
				}
			}
		}
	}
	store, err = validateRouteStore(store, profiles.Profiles)
	if err != nil {
		return RouteBatchResult{}, err
	}
	if err := saveRouteStoreFile(store); err != nil {
		return RouteBatchResult{}, err
	}
	return RouteBatchResult{Store: store, ChangedCount: changedCount, MovedCount: movedCount, CreatedGroupID: createdGroupID}, nil
}

func ToggleRoute(routeID string, enabled bool, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	routeID = strings.TrimSpace(routeID)
	for i := range store.Groups {
		for j := range store.Groups[i].Rules {
			if store.Groups[i].Rules[j].ID == routeID {
				store.Groups[i].Rules[j].Enabled = enabled
				if err := saveRouteStoreFile(store); err != nil {
					return RouteStore{}, err
				}
				return store, nil
			}
		}
	}
	return RouteStore{}, fmt.Errorf("路由不存在: %s", routeID)
}

func DeleteRoute(routeID string, appConfig *AppConfig) (RouteStore, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteStore{}, err
	}
	routeID = strings.TrimSpace(routeID)
	found := false
	for i := range store.Groups {
		out := make([]RouteRule, 0, len(store.Groups[i].Rules))
		for _, rule := range store.Groups[i].Rules {
			if rule.ID == routeID {
				found = true
				continue
			}
			out = append(out, rule)
		}
		store.Groups[i].Rules = out
	}
	if !found {
		return RouteStore{}, fmt.Errorf("路由不存在: %s", routeID)
	}
	if err := saveRouteStoreFile(store); err != nil {
		return RouteStore{}, err
	}
	return store, nil
}

func FindRouteReferences(profileID string, appConfig *AppConfig) (RouteReferences, error) {
	store, err := ListRoutes(appConfig)
	if err != nil {
		return RouteReferences{}, err
	}
	profileID = strings.TrimSpace(profileID)
	refs := RouteReferences{}
	for _, group := range store.Groups {
		for _, target := range group.TargetProfileIDs {
			if target == profileID {
				refs.GroupIDs = append(refs.GroupIDs, group.ID)
				break
			}
		}
		for _, rule := range group.Rules {
			for _, target := range rule.TargetProfileIDs {
				if target == profileID {
					refs.RouteIDs = append(refs.RouteIDs, rule.ID)
					break
				}
			}
		}
	}
	return refs, nil
}

func ReferencingRouteIDs(profileID string, appConfig *AppConfig) ([]string, error) {
	refs, err := FindRouteReferences(profileID, appConfig)
	if err != nil {
		return nil, err
	}
	return append(append([]string(nil), refs.GroupIDs...), refs.RouteIDs...), nil
}
