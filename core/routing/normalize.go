package routing

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func GuessConfigRouteType(pattern string) string {
	if strings.Contains(pattern, "/") {
		return RouteTypeCIDR
	}
	if strings.HasPrefix(pattern, "*.") {
		return RouteTypeDomain
	}
	if LooksLikeIPv4Pattern(pattern) {
		return RouteTypeIP
	}
	return RouteTypeDomain
}

func NormalizePattern(routeType, pattern string) (string, error) {
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

func LooksLikeIPv4Pattern(pattern string) bool {
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
