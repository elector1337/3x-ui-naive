package service

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/goccy/go-json"

	"github.com/mhsanaei/3x-ui/v3/database/model"
)

// Per-user IP limiting for naive servers.
//
// Caddy can't enforce a per-credential IP cap itself, so the panel does it:
// when IPLimit>0 the generated Caddyfile writes a JSON access log (client_ip +
// user_id per CONNECT), the naive IP job reads it, and any IP beyond a user's
// cap is dropped in the kernel via an `inet 3xui_naive_ban` nftables table.
//
// Bans carry a timeout: the job re-adds still-excess IPs every run, so an IP
// that stops exceeding the limit simply expires out of the set — no unban
// bookkeeping, no permanent-ban loops.

const (
	nftBanTable     = "3xui_naive_ban"
	nftBanChain     = "naive_ban"
	naiveBanTTLSecs = 300 // how long an excess IP stays dropped before it must be re-observed
)

func nftBan4Set(id int) string { return fmt.Sprintf("ban4_%d", id) }
func nftBan6Set(id int) string { return fmt.Sprintf("ban6_%d", id) }

// naiveLogEntry is the subset of a Caddy access-log line the IP job needs.
type naiveLogEntry struct {
	Ts      float64 `json:"ts"`
	UserID  string  `json:"user_id"`
	Status  int     `json:"status"`
	Request struct {
		ClientIP string `json:"client_ip"`
		Method   string `json:"method"`
	} `json:"request"`
}

// parseNaiveAccessIPs reads newline-delimited Caddy JSON access-log bytes and
// returns, per user, the distinct client IPs with their last-seen timestamp.
// Only successfully authenticated CONNECTs (method CONNECT, status 200,
// non-empty user) count. Malformed lines are skipped. Pure function — unit
// tested without Caddy.
func parseNaiveAccessIPs(data []byte) map[string]map[string]int64 {
	out := make(map[string]map[string]int64)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e naiveLogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		if e.Request.Method != "CONNECT" || e.Status != 200 || e.UserID == "" || e.Request.ClientIP == "" {
			continue
		}
		ts := int64(e.Ts)
		ips := out[e.UserID]
		if ips == nil {
			ips = make(map[string]int64)
			out[e.UserID] = ips
		}
		if ts > ips[e.Request.ClientIP] {
			ips[e.Request.ClientIP] = ts
		}
	}
	return out
}

// naiveExcessIPs returns the IPs to ban given per-user IP/last-seen maps and a
// per-user limit. For each user over the cap, the most-recently-seen `limit`
// IPs are kept and the rest returned. limit<=0 means unlimited (no bans).
func naiveExcessIPs(perUser map[string]map[string]int64, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var excess []string
	for _, ips := range perUser {
		if len(ips) <= limit {
			continue
		}
		type ipSeen struct {
			ip   string
			seen int64
		}
		list := make([]ipSeen, 0, len(ips))
		for ip, seen := range ips {
			list = append(list, ipSeen{ip, seen})
		}
		// newest first; keep the first `limit`, ban the remainder
		sort.Slice(list, func(i, j int) bool {
			if list[i].seen != list[j].seen {
				return list[i].seen > list[j].seen
			}
			return list[i].ip < list[j].ip // stable tiebreak
		})
		for _, e := range list[limit:] {
			excess = append(excess, e.ip)
		}
	}
	return excess
}

// splitIPFamilies partitions IPs into v4 and v6 (IPv6 contains a colon).
func splitIPFamilies(ips []string) (v4, v6 []string) {
	for _, ip := range ips {
		if strings.Contains(ip, ":") {
			v6 = append(v6, ip)
		} else {
			v4 = append(v4, ip)
		}
	}
	return v4, v6
}

// buildNaiveBanTable renders an `nft -f` script that (re)creates the ban table:
// one chain plus per-server v4/v6 timeout sets and drop rules, for every server
// with IPLimit>0. Wiped and rebuilt atomically so structure can't accumulate;
// ban elements are added separately by the job and re-observed each run.
func buildNaiveBanTable(servers []*model.NaiveServer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", nftBanTable)
	fmt.Fprintf(&b, "delete table inet %s\n", nftBanTable)
	fmt.Fprintf(&b, "add table inet %s\n", nftBanTable)
	fmt.Fprintf(&b, "add chain inet %s %s { type filter hook input priority -160 ; policy accept ; }\n", nftBanTable, nftBanChain)
	for _, srv := range servers {
		if srv == nil || srv.IPLimit <= 0 || srv.UseRawConfig || srv.Port <= 0 || srv.Port > 65535 {
			continue
		}
		s4, s6 := nftBan4Set(srv.Id), nftBan6Set(srv.Id)
		fmt.Fprintf(&b, "add set inet %s %s { type ipv4_addr ; flags timeout ; }\n", nftBanTable, s4)
		fmt.Fprintf(&b, "add set inet %s %s { type ipv6_addr ; flags timeout ; }\n", nftBanTable, s6)
		fmt.Fprintf(&b, "add rule inet %s %s tcp dport %d ip saddr @%s drop\n", nftBanTable, nftBanChain, srv.Port, s4)
		fmt.Fprintf(&b, "add rule inet %s %s tcp dport %d ip6 saddr @%s drop\n", nftBanTable, nftBanChain, srv.Port, s6)
	}
	return b.String()
}

// syncNaiveBanTable applies buildNaiveBanTable. No-op when nft is unavailable.
func syncNaiveBanTable(servers []*model.NaiveServer) error {
	if !nftAvailable() {
		return nil
	}
	cmd := exec.Command(nftBin, "-f", "-")
	cmd.Stdin = strings.NewReader(buildNaiveBanTable(servers))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft ban sync: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// banNaiveIPs adds the given IPs to a server's ban sets with a timeout. Safe to
// call repeatedly — re-adding refreshes the timeout. No-op when nft missing.
func banNaiveIPs(id int, ips []string) error {
	if !nftAvailable() || len(ips) == 0 {
		return nil
	}
	v4, v6 := splitIPFamilies(ips)
	var script strings.Builder
	if len(v4) > 0 {
		fmt.Fprintf(&script, "add element inet %s %s { %s }\n", nftBanTable, nftBan4Set(id), joinBanElems(v4))
	}
	if len(v6) > 0 {
		fmt.Fprintf(&script, "add element inet %s %s { %s }\n", nftBanTable, nftBan6Set(id), joinBanElems(v6))
	}
	if script.Len() == 0 {
		return nil
	}
	cmd := exec.Command(nftBin, "-f", "-")
	cmd.Stdin = strings.NewReader(script.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft ban add: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// joinBanElems formats IPs as nft set elements each with the ban timeout.
func joinBanElems(ips []string) string {
	parts := make([]string, len(ips))
	for i, ip := range ips {
		parts[i] = fmt.Sprintf("%s timeout %ds", ip, naiveBanTTLSecs)
	}
	return strings.Join(parts, ", ")
}
