package routing

import (
	"fmt"
	"strings"
)

func NormalizePolicy(strategy string, targetProfileIDs []string, profiles map[string]struct{}) (string, []string, error) {
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

func NormalizeRule(rule RouteRule, profiles map[string]struct{}) (RouteRule, error) {
	rule.ID = strings.TrimSpace(rule.ID)
	rule.Type = strings.ToLower(strings.TrimSpace(rule.Type))
	if rule.Type == "" {
		rule.Type = GuessConfigRouteType(strings.TrimSpace(rule.Pattern))
	}
	pattern, err := NormalizePattern(rule.Type, rule.Pattern)
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
	rule.Strategy, rule.TargetProfileIDs, err = NormalizePolicy(rule.Strategy, rule.TargetProfileIDs, profiles)
	if err != nil {
		return RouteRule{}, err
	}
	return rule, nil
}

func NormalizeGroup(group RouteGroup, profiles map[string]struct{}) (RouteGroup, error) {
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
	group.Strategy, group.TargetProfileIDs, err = NormalizePolicy(group.Strategy, group.TargetProfileIDs, profiles)
	if err != nil {
		return RouteGroup{}, fmt.Errorf("规则组默认出口无效: %w", err)
	}
	group.Rules = append([]RouteRule(nil), group.Rules...)
	if group.Rules == nil {
		group.Rules = []RouteRule{}
	}
	for i, rule := range group.Rules {
		normalized, err := NormalizeRule(rule, profiles)
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

func UniquenessKey(rule RouteRule) string {
	return strings.ToLower(rule.Type) + "\x00" + strings.ToLower(rule.Pattern)
}

func ValidateStore(store RouteStore, profiles map[string]struct{}) (RouteStore, error) {
	seenGroupIDs := make(map[string]bool)
	seenGroupNames := make(map[string]bool)
	seenRouteIDs := make(map[string]bool)
	seenPatterns := make(map[string]bool)
	out := RouteStore{Version: RouteStoreVersion, Groups: make([]RouteGroup, 0, len(store.Groups))}
	for _, group := range store.Groups {
		normalized, err := NormalizeGroup(group, profiles)
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
			key := UniquenessKey(rule)
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
