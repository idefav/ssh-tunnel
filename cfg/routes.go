package cfg

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/idefav/ssh-tunnel/core/routing"
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

type RouteRule = routing.RouteRule
type RouteGroup = routing.RouteGroup
type RouteStore = routing.RouteStore
type EffectiveRoute = routing.EffectiveRoute

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

func guessStandaloneRouteType(pattern string) string { return routing.GuessConfigRouteType(pattern) }
func normalizeRoutePattern(routeType, pattern string) (string, error) {
	return routing.NormalizePattern(routeType, pattern)
}
func looksLikeIPv4Pattern(pattern string) bool { return routing.LooksLikeIPv4Pattern(pattern) }

func profileIDSet(profiles map[string]SSHProfile) map[string]struct{} {
	ids := make(map[string]struct{}, len(profiles))
	for id := range profiles {
		ids[id] = struct{}{}
	}
	return ids
}
func normalizeAndValidatePolicy(strategy string, targets []string, profiles map[string]SSHProfile) (string, []string, error) {
	return routing.NormalizePolicy(strategy, targets, profileIDSet(profiles))
}
func NormalizeAndValidateRoute(rule RouteRule, profiles map[string]SSHProfile) (RouteRule, error) {
	return routing.NormalizeRule(rule, profileIDSet(profiles))
}
func normalizeAndValidateGroup(group RouteGroup, profiles map[string]SSHProfile) (RouteGroup, error) {
	return routing.NormalizeGroup(group, profileIDSet(profiles))
}
func routeUniquenessKey(rule RouteRule) string { return routing.UniquenessKey(rule) }
func validateRouteStore(store RouteStore, profiles map[string]SSHProfile) (RouteStore, error) {
	return routing.ValidateStore(store, profileIDSet(profiles))
}

func explicitRoute(rule RouteRule) bool {
	return strings.TrimSpace(rule.Strategy) != "" && len(rule.TargetProfileIDs) > 0
}

func ResolveEffectiveRoutes(store RouteStore) []EffectiveRoute {
	return routing.ResolveEffectiveRoutes(store)
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
 embedded := make(map[string][]routing.LegacyDomainRule, len(profiles.Profiles))
 for id, profile := range profiles.Profiles { for _, rule := range profile.DomainRoutes { embedded[id] = append(embedded[id], routing.LegacyDomainRule{Pattern: rule.Pattern, Type: rule.Type}) } }
 store, changed, cleared, err := routing.Migrate(routing.LegacyStore{Version: file.Version, Groups: file.Groups, Routes: file.Routes}, embedded, profileIDSet(profiles.Profiles))
 if err != nil { return RouteStore{}, profiles, false, false, err }
 for _, id := range cleared { profile := profiles.Profiles[id]; profile.DomainRoutes = nil; profiles.Profiles[id] = profile }
 return store, profiles, changed, len(cleared) != 0, nil
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
