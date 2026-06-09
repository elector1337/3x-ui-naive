package service

import (
	"sort"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/database/model"
)

// realistic Caddy forward_proxy access-log lines (shape captured from a live
// caddy+forwardproxy build): CONNECT with user_id + request.client_ip.
const sampleAccessLog = `
{"level":"info","ts":1000.0,"logger":"http.log.access.log0","msg":"handled request","request":{"client_ip":"1.1.1.1","method":"CONNECT","host":"x:8090"},"user_id":"alice","status":200}
{"level":"info","ts":1001.0,"request":{"client_ip":"2.2.2.2","method":"CONNECT"},"user_id":"alice","status":200}
{"level":"info","ts":1002.0,"request":{"client_ip":"3.3.3.3","method":"CONNECT"},"user_id":"alice","status":200}
{"level":"info","ts":1003.0,"request":{"client_ip":"9.9.9.9","method":"CONNECT"},"user_id":"alice","status":407}
{"level":"info","ts":1004.0,"request":{"client_ip":"5.5.5.5","method":"CONNECT"},"user_id":"bob","status":200}
{"level":"info","ts":1005.0,"request":{"client_ip":"2001:db8::1","method":"CONNECT"},"user_id":"bob","status":200}
not json at all
`

func TestParseNaiveAccessIPs(t *testing.T) {
	got := parseNaiveAccessIPs([]byte(sampleAccessLog))
	if len(got["alice"]) != 3 {
		t.Errorf("alice: want 3 distinct IPs (407 excluded), got %d: %v", len(got["alice"]), got["alice"])
	}
	if got["alice"]["3.3.3.3"] != 1002 {
		t.Errorf("alice 3.3.3.3 last-seen want 1002, got %d", got["alice"]["3.3.3.3"])
	}
	if _, banned := got["alice"]["9.9.9.9"]; banned {
		t.Error("407 (failed auth) IP must not be counted")
	}
	if len(got["bob"]) != 2 {
		t.Errorf("bob: want 2 IPs, got %d", len(got["bob"]))
	}
}

func TestNaiveExcessIPs(t *testing.T) {
	per := parseNaiveAccessIPs([]byte(sampleAccessLog))

	// limit 0 = unlimited
	if got := naiveExcessIPs(per, 0); got != nil {
		t.Errorf("limit 0 should ban nothing, got %v", got)
	}
	// limit 2: alice has 3 IPs -> oldest (1.1.1.1) banned; bob has 2 -> none
	excess := naiveExcessIPs(per, 2)
	if len(excess) != 1 || excess[0] != "1.1.1.1" {
		t.Errorf("want [1.1.1.1] banned (oldest beyond limit), got %v", excess)
	}
	// limit 1: alice keeps newest (3.3.3.3), bans 2.2.2.2 + 1.1.1.1; bob keeps
	// newest, bans the other -> 3 total
	excess = naiveExcessIPs(per, 1)
	sort.Strings(excess)
	if len(excess) != 3 {
		t.Fatalf("limit 1: want 3 banned, got %d: %v", len(excess), excess)
	}
	if !contains(excess, "1.1.1.1") || !contains(excess, "2.2.2.2") {
		t.Errorf("alice should keep only newest 3.3.3.3, got banned %v", excess)
	}
}

func TestSplitIPFamilies(t *testing.T) {
	v4, v6 := splitIPFamilies([]string{"1.2.3.4", "2001:db8::1", "5.6.7.8"})
	if len(v4) != 2 || len(v6) != 1 || v6[0] != "2001:db8::1" {
		t.Errorf("split wrong: v4=%v v6=%v", v4, v6)
	}
}

func TestBuildNaiveBanTable(t *testing.T) {
	servers := []*model.NaiveServer{
		{Id: 1, Port: 8443, IPLimit: 2},
		{Id: 2, Port: 9443, IPLimit: 0},                     // no limit -> skipped
		{Id: 3, Port: 9543, IPLimit: 3, UseRawConfig: true}, // raw -> skipped
	}
	out := buildNaiveBanTable(servers)
	if strings.Count(out, "add table inet 3xui_naive_ban") != 2 || !strings.Contains(out, "delete table inet 3xui_naive_ban") {
		t.Errorf("expected wipe+rebuild pattern:\n%s", out)
	}
	for _, want := range []string{
		"add set inet 3xui_naive_ban ban4_1 { type ipv4_addr ; flags timeout ; }",
		"add set inet 3xui_naive_ban ban6_1 { type ipv6_addr ; flags timeout ; }",
		"tcp dport 8443 ip saddr @ban4_1 drop",
		"tcp dport 8443 ip6 saddr @ban6_1 drop",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	for _, no := range []string{"ban4_2", "ban4_3", "9443", "9543"} {
		if strings.Contains(out, no) {
			t.Errorf("should not contain %q (no-limit/raw server)\n%s", no, out)
		}
	}
}

func TestJoinBanElems(t *testing.T) {
	got := joinBanElems([]string{"1.1.1.1", "2.2.2.2"})
	if !strings.Contains(got, "1.1.1.1 timeout 300s") || !strings.Contains(got, "2.2.2.2 timeout 300s") {
		t.Errorf("elements need per-IP timeout: %s", got)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
