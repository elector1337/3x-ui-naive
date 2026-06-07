package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/database/model"
	"github.com/mhsanaei/3x-ui/v3/util/crypto"
)

func TestParseNftCounters(t *testing.T) {
	// Sample shaped like real `nft -j list counters table inet 3xui_naive`
	// output: a metainfo entry plus one object per counter.
	data := []byte(`{
		"nftables": [
			{"metainfo": {"version": "1.0.9", "release_name": "x", "json_schema_version": 1}},
			{"counter": {"family": "inet", "table": "3xui_naive", "name": "n_in_1", "handle": 3, "packets": 12, "bytes": 1048576}},
			{"counter": {"family": "inet", "table": "3xui_naive", "name": "n_out_1", "handle": 4, "packets": 20, "bytes": 5242880}},
			{"counter": {"family": "inet", "table": "3xui_naive", "name": "n_in_7", "handle": 5, "packets": 1, "bytes": 100}},
			{"counter": {"family": "inet", "table": "3xui_naive", "name": "unrelated", "handle": 6, "packets": 0, "bytes": 999}}
		]
	}`)

	got, err := parseNftCounters(data)
	if err != nil {
		t.Fatalf("parseNftCounters: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 server ids, got %d: %+v", len(got), got)
	}
	if got[1].Up != 1048576 || got[1].Down != 5242880 {
		t.Errorf("id 1: want up=1048576 down=5242880, got %+v", got[1])
	}
	if got[7].Up != 100 || got[7].Down != 0 {
		t.Errorf("id 7: want up=100 down=0, got %+v", got[7])
	}
}

func TestParseNftCounters_Empty(t *testing.T) {
	got, err := parseNftCounters([]byte(`{"nftables": [{"metainfo": {}}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no counters, got %+v", got)
	}
}

func TestParseNftCounters_BadJSON(t *testing.T) {
	if _, err := parseNftCounters([]byte("not json")); err == nil {
		t.Error("expected error on malformed json")
	}
}

func TestCounterID(t *testing.T) {
	cases := []struct {
		name   string
		wantID int
		wantUp bool
		wantOK bool
	}{
		{"n_in_1", 1, true, true},
		{"n_out_42", 42, false, true},
		{"n_in_", 0, true, false},
		{"n_out_abc", 0, false, false},
		{"random", 0, false, false},
	}
	for _, c := range cases {
		id, up, ok := counterID(c.name)
		if ok != c.wantOK || id != c.wantID || (ok && up != c.wantUp) {
			t.Errorf("counterID(%q) = (%d,%v,%v), want (%d,%v,%v)",
				c.name, id, up, ok, c.wantID, c.wantUp, c.wantOK)
		}
	}
}

func TestBuildNftRuleset(t *testing.T) {
	servers := []*model.NaiveServer{
		{Id: 1, Port: 8443},
		{Id: 2, Port: 9443, UseRawConfig: true}, // skipped: raw config
		{Id: 3, Port: 0},                        // skipped: invalid port
		{Id: 4, Port: 443},
	}
	out := buildNftRuleset(servers)

	// Table is wiped and rebuilt (add → delete → add) so rules can't accumulate.
	if strings.Count(out, "add table inet 3xui_naive") != 2 {
		t.Errorf("expected add table twice (rebuild pattern):\n%s", out)
	}
	if !strings.Contains(out, "delete table inet 3xui_naive") {
		t.Errorf("expected delete table for clean rebuild:\n%s", out)
	}

	// Server 1 and 4 get counters + rules; 2 and 3 do not.
	for _, want := range []string{
		"add counter inet 3xui_naive n_in_1",
		"add counter inet 3xui_naive n_out_1",
		"tcp dport 8443 counter name n_in_1",
		"tcp sport 8443 counter name n_out_1",
		"tcp dport 443 counter name n_in_4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("ruleset missing %q\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"n_in_2", "n_out_2", "n_in_3", "9443"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("ruleset should not contain %q (raw/invalid server)\n%s", unwanted, out)
		}
	}
}

// TestApplyTrafficDeltas_AccumulatesAndResets drives the DB write path with a
// real sqlite db (no nft needed): deltas accumulate cumulatively, and
// ResetTraffic zeroes a single server without touching others.
func TestApplyTrafficDeltas_AccumulatesAndResets(t *testing.T) {
	setupConflictDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))
	StartTrafficWriter()
	t.Cleanup(StopTrafficWriter)

	svc := NewNaiveService()
	a := &model.NaiveServer{Remark: "a", Port: 8443, Domain: "a.example.com", CertFile: "/x", KeyFile: "/y", AuthUser: "u", AuthPass: "p"}
	b := &model.NaiveServer{Remark: "b", Port: 9443, Domain: "b.example.com", CertFile: "/x", KeyFile: "/y", AuthUser: "u", AuthPass: "p"}
	if err := svc.Add(a); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := svc.Add(b); err != nil {
		t.Fatalf("add b: %v", err)
	}

	// First sample.
	if err := svc.applyTrafficDeltas(map[int]naiveTrafficDelta{
		a.Id: {Up: 100, Down: 200},
		b.Id: {Up: 5, Down: 7},
	}); err != nil {
		t.Fatalf("apply 1: %v", err)
	}
	// Second sample accumulates on top.
	if err := svc.applyTrafficDeltas(map[int]naiveTrafficDelta{
		a.Id: {Up: 50, Down: 25},
	}); err != nil {
		t.Fatalf("apply 2: %v", err)
	}

	gotA, _ := svc.Get(a.Id)
	if gotA.Up != 150 || gotA.Down != 225 {
		t.Errorf("server a: want up=150 down=225, got up=%d down=%d", gotA.Up, gotA.Down)
	}
	gotB, _ := svc.Get(b.Id)
	if gotB.Up != 5 || gotB.Down != 7 {
		t.Errorf("server b: want up=5 down=7, got up=%d down=%d", gotB.Up, gotB.Down)
	}

	// Reset only a; b is untouched.
	if err := svc.ResetTraffic(a.Id); err != nil {
		t.Fatalf("reset a: %v", err)
	}
	gotA, _ = svc.Get(a.Id)
	if gotA.Up != 0 || gotA.Down != 0 {
		t.Errorf("server a after reset: want 0/0, got %d/%d", gotA.Up, gotA.Down)
	}
	gotB, _ = svc.Get(b.Id)
	if gotB.Up != 5 || gotB.Down != 7 {
		t.Errorf("server b after a-reset: want 5/7, got %d/%d", gotB.Up, gotB.Down)
	}
}

func TestNaiveDepleted(t *testing.T) {
	cases := []struct {
		name string
		srv  *model.NaiveServer
		want bool
	}{
		{"unlimited", &model.NaiveServer{Total: 0, Up: 1 << 40, Down: 1 << 40}, false},
		{"under quota", &model.NaiveServer{Total: 1000, Up: 400, Down: 400}, false},
		{"exactly at quota", &model.NaiveServer{Total: 1000, Up: 600, Down: 400}, true},
		{"over quota", &model.NaiveServer{Total: 1000, Up: 900, Down: 900}, true},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := naiveDepleted(c.srv); got != c.want {
			t.Errorf("%s: naiveDepleted = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNaiveExpired(t *testing.T) {
	now := int64(1_000_000)
	cases := []struct {
		name string
		srv  *model.NaiveServer
		want bool
	}{
		{"never", &model.NaiveServer{ExpiryTime: 0}, false},
		{"future", &model.NaiveServer{ExpiryTime: now + 1}, false},
		{"exactly now", &model.NaiveServer{ExpiryTime: now}, true},
		{"past", &model.NaiveServer{ExpiryTime: now - 1}, true},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := naiveExpired(c.srv, now); got != c.want {
			t.Errorf("%s: naiveExpired = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestResetTrafficBySchedule_OnlyMatchingPeriod verifies that only servers with
// the matching TrafficReset schedule are zeroed.
func TestResetTrafficBySchedule_OnlyMatchingPeriod(t *testing.T) {
	setupConflictDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))
	StartTrafficWriter()
	t.Cleanup(StopTrafficWriter)

	svc := NewNaiveService()
	daily := &model.NaiveServer{Remark: "d", Port: 8443, Domain: "d.example.com", CertFile: "/x", KeyFile: "/y", AuthUser: "u", AuthPass: "p", TrafficReset: "day"}
	weekly := &model.NaiveServer{Remark: "w", Port: 9443, Domain: "w.example.com", CertFile: "/x", KeyFile: "/y", AuthUser: "u", AuthPass: "p", TrafficReset: "week"}
	if err := svc.Add(daily); err != nil {
		t.Fatalf("add daily: %v", err)
	}
	if err := svc.Add(weekly); err != nil {
		t.Fatalf("add weekly: %v", err)
	}
	if err := svc.applyTrafficDeltas(map[int]naiveTrafficDelta{
		daily.Id:  {Up: 100, Down: 100},
		weekly.Id: {Up: 100, Down: 100},
	}); err != nil {
		t.Fatalf("seed traffic: %v", err)
	}

	n, err := svc.ResetTrafficBySchedule("day")
	if err != nil {
		t.Fatalf("reset day: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 server reset, got %d", n)
	}

	gotDaily, _ := svc.Get(daily.Id)
	if gotDaily.Up != 0 || gotDaily.Down != 0 {
		t.Errorf("daily should be reset, got %d/%d", gotDaily.Up, gotDaily.Down)
	}
	if gotDaily.LastTrafficResetTime == 0 {
		t.Error("daily LastTrafficResetTime should be stamped")
	}
	gotWeekly, _ := svc.Get(weekly.Id)
	if gotWeekly.Up != 100 || gotWeekly.Down != 100 {
		t.Errorf("weekly should be untouched, got %d/%d", gotWeekly.Up, gotWeekly.Down)
	}
}

// TestNaiveUsers_PersistAndReplace verifies users round-trip in plaintext,
// survive reload (decrypted), and that Update replaces the set.
func TestNaiveUsers_PersistAndReplace(t *testing.T) {
	setupConflictDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))
	StartTrafficWriter()
	t.Cleanup(StopTrafficWriter)

	svc := NewNaiveService()
	srv := &model.NaiveServer{
		Remark: "m", Port: 8443, Domain: "m.example.com", CertFile: "/x", KeyFile: "/y",
		AuthUser: "alice", AuthPass: "p1",
		Users: []*model.NaiveUser{
			{Username: "bob", Password: "p2", Enable: true},
			{Username: "carol", Password: "p3", Enable: true},
		},
	}
	if err := svc.Add(srv); err != nil {
		t.Fatalf("add: %v", err)
	}
	// passwords still plaintext in the returned struct
	for _, u := range srv.Users {
		if u.Password == "" || len(u.Password) > 20 {
			t.Errorf("user %s password not plaintext after Add: %q", u.Username, u.Password)
		}
	}

	got, err := svc.Get(srv.Id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Users) != 2 {
		t.Fatalf("expected 2 users after reload, got %d", len(got.Users))
	}
	byName := map[string]string{}
	for _, u := range got.Users {
		byName[u.Username] = u.Password
	}
	if byName["bob"] != "p2" || byName["carol"] != "p3" {
		t.Errorf("passwords not decrypted on reload: %+v", byName)
	}

	// Update replaces the user set: drop carol, add dave.
	got.Users = []*model.NaiveUser{
		{Username: "bob", Password: "p2new", Enable: true},
		{Username: "dave", Password: "p4", Enable: true},
	}
	if err := svc.Update(got); err != nil {
		t.Fatalf("update: %v", err)
	}
	reloaded, _ := svc.Get(srv.Id)
	if len(reloaded.Users) != 2 {
		t.Fatalf("expected 2 users after replace, got %d", len(reloaded.Users))
	}
	names := map[string]string{}
	for _, u := range reloaded.Users {
		names[u.Username] = u.Password
	}
	if _, ok := names["carol"]; ok {
		t.Error("carol should have been removed by replace")
	}
	if names["bob"] != "p2new" || names["dave"] != "p4" {
		t.Errorf("unexpected users after replace: %+v", names)
	}
}

func TestNaiveClientURL(t *testing.T) {
	got := naiveClientURL("ex.com", 443, "al ice", "p@s:s", "my remark")
	// userinfo + remark must be escaped; host carries the port.
	if !strings.HasPrefix(got, "naive+https://") {
		t.Fatalf("bad scheme: %s", got)
	}
	if !strings.Contains(got, "@ex.com:443") {
		t.Errorf("missing host:port: %s", got)
	}
	if strings.Contains(got, " ") {
		t.Errorf("url must not contain raw spaces: %s", got)
	}
	if !strings.Contains(got, "#") {
		t.Errorf("expected remark fragment: %s", got)
	}
}

// TestNaiveSubLinks selects only enabled, non-raw servers tagged with the subId
// and emits the primary credential plus each enabled user.
func TestNaiveSubLinks(t *testing.T) {
	setupConflictDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))
	StartTrafficWriter()
	t.Cleanup(StopTrafficWriter)

	svc := NewNaiveService()
	add := func(s *model.NaiveServer) {
		if err := svc.Add(s); err != nil {
			t.Fatalf("add %s: %v", s.Remark, err)
		}
	}
	add(&model.NaiveServer{Remark: "match", Enable: true, SubId: "sub1", Port: 8443, Domain: "a.com", CertFile: "/x", KeyFile: "/y", AuthUser: "alice", AuthPass: "p1",
		Users: []*model.NaiveUser{{Username: "bob", Password: "p2", Enable: true}, {Username: "carol", Password: "p3", Enable: false}}})
	add(&model.NaiveServer{Remark: "othersub", Enable: true, SubId: "sub2", Port: 9443, Domain: "b.com", CertFile: "/x", KeyFile: "/y", AuthUser: "u", AuthPass: "p"})
	add(&model.NaiveServer{Remark: "disabled", Enable: false, SubId: "sub1", Port: 9543, Domain: "c.com", CertFile: "/x", KeyFile: "/y", AuthUser: "u", AuthPass: "p"})

	links := svc.SubLinks("sub1")
	// primary (alice) + enabled user (bob); carol disabled, othersub/disabled excluded
	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d: %v", len(links), links)
	}
	joined := strings.Join(links, "\n")
	for _, want := range []string{"alice:p1@a.com:8443", "bob:p2@a.com:8443"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	for _, no := range []string{"carol", "b.com", "c.com"} {
		if strings.Contains(joined, no) {
			t.Errorf("unexpected %q in:\n%s", no, joined)
		}
	}
	if svc.SubLinks("") != nil {
		t.Error("empty subId must return nil")
	}
}

// nftAvailable must be a safe no-op gate on non-Linux dev machines.
func TestNftUnavailableIsNoOp(t *testing.T) {
	if nftAvailable() {
		t.Skip("nft available (root linux) — no-op test not applicable")
	}
	if err := rebuildNftCounters([]*model.NaiveServer{{Id: 1, Port: 8443}}); err != nil {
		t.Errorf("rebuild should be a no-op when nft unavailable, got %v", err)
	}
	deltas, err := sampleNftCounters()
	if err != nil || deltas != nil {
		t.Errorf("sample should be (nil,nil) when nft unavailable, got (%+v,%v)", deltas, err)
	}
}
