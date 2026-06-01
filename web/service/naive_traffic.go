package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/goccy/go-json"

	"github.com/mhsanaei/3x-ui/v3/database/model"
)

// Naive traffic accounting.
//
// Caddy's forward_proxy does not report the number of bytes it tunnels (for a
// CONNECT request the access log records size:0, bytes_read:0 — verified), so
// the panel cannot ask Caddy how much a naive server moved. Instead we let the
// kernel count: an nftables table `inet 3xui_naive` holds one input counter and
// one output counter per server, attached to the server's TCP port.
//
//	Up   (client upload)   = bytes arriving on the port  -> tcp dport <port>
//	Down (server download) = bytes leaving the port      -> tcp sport <port>
//
// This is Linux + root only. On any other platform, when the `nft` binary is
// missing, or when the panel does not run as root, every function here becomes
// a no-op and traffic simply stays at zero — the rest of the feature degrades
// gracefully.

const (
	nftTable      = "3xui_naive"
	nftInputHook  = "naive_in"
	nftOutputHook = "naive_out"
)

func nftInCounter(id int) string  { return fmt.Sprintf("n_in_%d", id) }
func nftOutCounter(id int) string { return fmt.Sprintf("n_out_%d", id) }

// counterID extracts the server id from a counter name produced by
// nftInCounter / nftOutCounter. Returns (id, isUp, ok).
func counterID(name string) (id int, isUp, ok bool) {
	switch {
	case strings.HasPrefix(name, "n_in_"):
		isUp = true
		id, err := strconv.Atoi(strings.TrimPrefix(name, "n_in_"))
		return id, isUp, err == nil
	case strings.HasPrefix(name, "n_out_"):
		id, err := strconv.Atoi(strings.TrimPrefix(name, "n_out_"))
		return id, false, err == nil
	default:
		return 0, false, false
	}
}

// nftBin is the resolved path to the nft binary, "" if unavailable. Looked up
// lazily and cached for the process lifetime.
var nftBin string

func nftAvailable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if os.Geteuid() != 0 {
		return false
	}
	if nftBin == "" {
		p, err := exec.LookPath("nft")
		if err != nil {
			return false
		}
		nftBin = p
	}
	return true
}

// naiveTrafficDelta is the byte count read (and zeroed) from the kernel for one
// server during a single sample.
type naiveTrafficDelta struct {
	Up   int64
	Down int64
}

// buildNftRuleset renders a complete `nft -f` script that wipes and recreates
// the 3xui_naive table with one input+output counter per server bound to its
// port. Servers in raw-config mode are skipped — the panel doesn't know which
// port(s) such a config listens on. Wiping and rebuilding in a single
// transactional script keeps it idempotent: rules can never accumulate and
// double-count, no matter the prior kernel state.
func buildNftRuleset(servers []*model.NaiveServer) string {
	var b strings.Builder
	// `add` first so the following `delete` can't fail on a missing table;
	// then a clean rebuild. nft applies the whole script atomically.
	fmt.Fprintf(&b, "add table inet %s\n", nftTable)
	fmt.Fprintf(&b, "delete table inet %s\n", nftTable)
	fmt.Fprintf(&b, "add table inet %s\n", nftTable)
	fmt.Fprintf(&b, "add chain inet %s %s { type filter hook input priority -150 ; policy accept ; }\n", nftTable, nftInputHook)
	fmt.Fprintf(&b, "add chain inet %s %s { type filter hook output priority -150 ; policy accept ; }\n", nftTable, nftOutputHook)

	for _, srv := range servers {
		if srv == nil || srv.UseRawConfig || srv.Port <= 0 || srv.Port > 65535 {
			continue
		}
		in := nftInCounter(srv.Id)
		out := nftOutCounter(srv.Id)
		fmt.Fprintf(&b, "add counter inet %s %s\n", nftTable, in)
		fmt.Fprintf(&b, "add counter inet %s %s\n", nftTable, out)
		fmt.Fprintf(&b, "add rule inet %s %s tcp dport %d counter name %s\n", nftTable, nftInputHook, srv.Port, in)
		fmt.Fprintf(&b, "add rule inet %s %s tcp sport %d counter name %s\n", nftTable, nftOutputHook, srv.Port, out)
	}
	return b.String()
}

// rebuildNftCounters applies buildNftRuleset via `nft -f -`. No-op (nil) when
// nft is unavailable.
func rebuildNftCounters(servers []*model.NaiveServer) error {
	if !nftAvailable() {
		return nil
	}
	script := buildNftRuleset(servers)
	cmd := exec.Command(nftBin, "-f", "-")
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nft rebuild: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sampleNftCounters atomically reads and zeroes every counter in the table and
// returns the per-server deltas accumulated since the previous sample. Returns
// (nil, nil) when nft is unavailable.
func sampleNftCounters() (map[int]naiveTrafficDelta, error) {
	if !nftAvailable() {
		return nil, nil
	}
	cmd := exec.Command(nftBin, "-j", "reset", "counters", "table", "inet", nftTable)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Table missing (e.g. before the first rebuild) is not an error worth
		// surfacing — there's simply nothing to sample yet.
		if strings.Contains(string(out), "No such file or directory") ||
			strings.Contains(string(out), "does not exist") {
			return nil, nil
		}
		return nil, fmt.Errorf("nft reset counters: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return parseNftCounters(out)
}

// parseNftCounters turns the JSON from `nft -j ... counters` into per-server
// byte deltas. Kept separate from the exec call so it can be unit-tested
// without nft or root. Unknown counter names are ignored.
func parseNftCounters(data []byte) (map[int]naiveTrafficDelta, error) {
	var doc struct {
		Nftables []struct {
			Counter *struct {
				Name  string `json:"name"`
				Bytes int64  `json:"bytes"`
			} `json:"counter"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse nft json: %w", err)
	}
	result := make(map[int]naiveTrafficDelta)
	for _, entry := range doc.Nftables {
		if entry.Counter == nil {
			continue
		}
		id, isUp, ok := counterID(entry.Counter.Name)
		if !ok {
			continue
		}
		d := result[id]
		if isUp {
			d.Up += entry.Counter.Bytes
		} else {
			d.Down += entry.Counter.Bytes
		}
		result[id] = d
	}
	return result, nil
}

// teardownNftCounters removes the whole table. Best-effort; used when no naive
// servers remain so we don't leave stray kernel state behind.
func teardownNftCounters() error {
	if !nftAvailable() {
		return nil
	}
	cmd := exec.Command(nftBin, "delete", "table", "inet", nftTable)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "No such file or directory") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return errors.New(msg)
	}
	return nil
}
