package cfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDNSUpstreamValidation(t *testing.T) {
	for _, input := range []string{"", "dns.example:53", "1.1.1.1", "1.1.1.1:0", "0.0.0.0:53", "224.0.0.1:53", "[ff02::1]:53", "[fe80::1%en0]:53", "1.1.1.1:53,", "1.1.1.1:53,1.1.1.1:53", "1.1.1.1:53,8.8.8.8:53,9.9.9.9:53"} {
		if _, err := ParseDNSUpstreams(input); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	want := []string{"1.1.1.1:53", "[2001:db8::53]:53"}
	got, err := ParseDNSUpstreams(" 1.1.1.1:53, [2001:db8::53]:53 ")
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized=%v err=%v", got, err)
	}
	if got, err := NormalizeDNSUpstreams(nil, true); err != nil || got != nil {
		t.Fatal("empty override must inherit")
	}
	for _, v := range []struct {
		key   string
		value interface{}
	}{{ENABLE_DNS_KEY, "true"}, {DNS_UPSTREAMS_KEY, 1}, {DNS_LOCAL_ADDRESS_KEY, "localhost:53"}, {DNS_LOCAL_ADDRESS_KEY, "127.0.0.1:0"}} {
		if err := ValidateDNSConfigValue(v.key, v.value); err == nil {
			t.Errorf("accepted %+v", v)
		}
	}
}

func TestProfileDNSRoundTripAndInvalidSave(t *testing.T) {
	dir := setupBatchRouteTest(t, batchTestStore())
	profile := testProfiles("jp")["jp"]
	profile.DNSUpstreams = []string{" 10.0.0.53:53 ", "[2001:db8::53]:53"}
	if _, err := UpsertProfile("jp", profile, nil); err != nil {
		t.Fatal(err)
	}
	store, err := ListProfiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(store.Profiles["jp"].DNSUpstreams, []string{"10.0.0.53:53", "[2001:db8::53]:53"}) {
		t.Fatal("override lost")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "profiles.json"))
	profile.DNSUpstreams = []string{"bad"}
	if _, err := UpsertProfile("jp", profile, nil); err == nil {
		t.Fatal("invalid override saved")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "profiles.json"))
	if string(before) != string(after) {
		t.Fatal("invalid save modified profile store")
	}
	var legacy SSHProfile
	if json.Unmarshal([]byte(`{"serverIp":"127.0.0.1"}`), &legacy) != nil || len(legacy.DNSUpstreams) != 0 {
		t.Fatal("legacy compatibility")
	}
	profile.DNSUpstreams = nil
	if _, err := UpsertProfile("jp", profile, nil); err != nil {
		t.Fatal(err)
	}
	store, _ = ListProfiles(nil)
	if len(store.Profiles["jp"].DNSUpstreams) != 0 {
		t.Fatal("cannot clear override")
	}
}
