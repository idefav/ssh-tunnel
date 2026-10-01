package cfg

import (
	"fmt"
	"net/netip"
	"strings"
)

const (
	ENABLE_DNS_KEY        = "dns.enable"
	DNS_LOCAL_ADDRESS_KEY = "dns.local.address"
	DNS_UPSTREAMS_KEY     = "dns.upstreams"
	DefaultDNSUpstreams   = "1.1.1.1:53,8.8.8.8:53"
)

// NormalizeDNSUpstreams accepts literal endpoints only, avoiding resolver loops.
// An empty per-profile list inherits the global list.
func NormalizeDNSUpstreams(servers []string, inherit bool) ([]string, error) {
	if len(servers) == 0 && inherit {
		return nil, nil
	}
	if len(servers) < 1 || len(servers) > 2 {
		return nil, fmt.Errorf("DNS上游必须有1至2个IP:端口")
	}
	result := make([]string, 0, len(servers))
	seen := make(map[string]bool)
	for _, server := range servers {
		endpoint, err := netip.ParseAddrPort(strings.TrimSpace(server))
		if err != nil || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" || endpoint.Addr().IsUnspecified() || endpoint.Addr().IsMulticast() {
			return nil, fmt.Errorf("无效的DNS上游 %q：需要明确的IP:端口", server)
		}
		normalized := endpoint.String()
		if seen[normalized] {
			return nil, fmt.Errorf("重复的DNS上游 %q", server)
		}
		seen[normalized] = true
		result = append(result, normalized)
	}
	return result, nil
}

func ParseDNSUpstreams(raw string) ([]string, error) {
	return NormalizeDNSUpstreams(strings.Split(raw, ","), false)
}

func ValidateDNSListenAddress(address string) error {
	endpoint, err := netip.ParseAddrPort(strings.TrimSpace(address))
	if err != nil || endpoint.Port() == 0 || endpoint.Addr().Zone() != "" || endpoint.Addr().IsMulticast() {
		return fmt.Errorf("DNS监听地址必须是IP:端口")
	}
	return nil
}

// ValidateDNSConfigValue is also used before the config API persists a value.
func ValidateDNSConfigValue(key string, value interface{}) error {
	switch key {
	case ENABLE_DNS_KEY:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("dns.enable必须是布尔值")
		}
	case DNS_LOCAL_ADDRESS_KEY:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("DNS监听地址必须是字符串")
		}
		return ValidateDNSListenAddress(s)
	case DNS_UPSTREAMS_KEY:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("DNS上游必须是字符串")
		}
		_, err := ParseDNSUpstreams(s)
		return err
	}
	return nil
}
