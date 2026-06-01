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
