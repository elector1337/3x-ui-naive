package sub

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-json"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/crypto"
)

// seedNaiveSub creates one enabled naive server tagged with subId, carrying a
// primary credential, one enabled extra user and one disabled extra user.
func seedNaiveSub(t *testing.T, subId string) {
	t.Helper()
	initSubDB(t)
	crypto.SetEncryptionKeyPath(filepath.Join(t.TempDir(), "encryption.key"))
	srv := &model.NaiveServer{
		Remark: "naive-de", Enable: true, SubId: subId,
		Port: 8443, Domain: "naive.example.com",
		CertFile: "/c", KeyFile: "/k",
		AuthUser: "alice", AuthPass: "s3cret",
		Users: []*model.NaiveUser{
			{Username: "bob", Password: "pw2", Enable: true},
			{Username: "carol", Password: "pw3", Enable: false},
		},
	}
	if err := database.GetDB().Create(srv).Error; err != nil {
		t.Fatalf("seed naive server: %v", err)
	}
}

// A subscription carrying only naive servers must render in every format, not
// just the plain one: /json and /clash are what a modern client asks for.
func TestNaiveOnlySub_AllFormats(t *testing.T) {
	seedNaiveSub(t, "naive-sub")
	base := NewSubService("")

	t.Run("clash", func(t *testing.T) {
		out, _, err := NewSubClashService(false, "", base).GetClash("naive-sub", "sub.example.com")
		if err != nil {
			t.Fatalf("GetClash err = %v", err)
		}
		for _, want := range []string{
			"name: naive-de", "name: naive-de-bob", "type: http",
			"server: naive.example.com", "port: 8443",
			"username: alice", "password: s3cret", "tls: true", "sni: naive.example.com",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("clash output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "carol") {
			t.Errorf("disabled naive user leaked into clash output:\n%s", out)
		}
	})

	t.Run("json", func(t *testing.T) {
		out, _, err := NewSubJsonService("", "", "", "", base).GetJson("naive-sub", "sub.example.com", true)
		if err != nil {
			t.Fatalf("GetJson err = %v", err)
		}
		var configs []map[string]any
		if err := json.Unmarshal([]byte(out), &configs); err != nil {
			t.Fatalf("GetJson is not a config array: %v; body=%s", err, out)
		}
		if len(configs) != 2 {
			t.Fatalf("want a config per enabled credential, got %d:\n%s", len(configs), out)
		}
		if remark, _ := configs[0]["remarks"].(string); remark != "naive-de" {
			t.Errorf("remarks = %q, want naive-de", remark)
		}

		outbound, ok := configs[0]["outbounds"].([]any)
		if !ok || len(outbound) == 0 {
			t.Fatalf("no outbounds in naive config:\n%s", out)
		}
		proxy, _ := outbound[0].(map[string]any)
		if got, _ := proxy["protocol"].(string); got != "http" {
			t.Errorf("protocol = %q, want http", got)
		}
		stream, _ := proxy["streamSettings"].(map[string]any)
		if got, _ := stream["security"].(string); got != "tls" {
			t.Errorf("security = %q, want tls (naive is HTTPS)", got)
		}
		tlsSettings, _ := stream["tlsSettings"].(map[string]any)
		if got, _ := tlsSettings["serverName"].(string); got != "naive.example.com" {
			t.Errorf("serverName = %q, want the naive domain", got)
		}
		settings, _ := proxy["settings"].(map[string]any)
		servers, _ := settings["servers"].([]any)
		if len(servers) != 1 {
			t.Fatalf("want one server entry, got %d", len(servers))
		}
		server, _ := servers[0].(map[string]any)
		if got, _ := server["address"].(string); got != "naive.example.com" {
			t.Errorf("address = %q", got)
		}
		users, _ := server["users"].([]any)
		if len(users) != 1 {
			t.Fatalf("want one basic-auth user, got %d", len(users))
		}
		user, _ := users[0].(map[string]any)
		if user["user"] != "alice" || user["pass"] != "s3cret" {
			t.Errorf("credentials = %v, want alice/s3cret", user)
		}
	})

	t.Run("raw links still work", func(t *testing.T) {
		links, _, _, _, err := base.GetSubs("naive-sub", "sub.example.com")
		if err != nil {
			t.Fatalf("GetSubs err = %v", err)
		}
		if len(links) != 2 {
			t.Fatalf("want 2 naive links, got %d: %v", len(links), links)
		}
		if !strings.HasPrefix(links[0], "naive+https://alice:s3cret@naive.example.com:8443") {
			t.Errorf("unexpected link: %s", links[0])
		}
	})
}

// A naive server tagged with a different subId must not appear.
func TestNaiveSub_OtherSubIdExcluded(t *testing.T) {
	seedNaiveSub(t, "naive-sub")
	base := NewSubService("")

	out, _, err := NewSubClashService(false, "", base).GetClash("someone-else", "sub.example.com")
	if err != nil {
		t.Fatalf("GetClash err = %v", err)
	}
	if out != "" {
		t.Fatalf("foreign subId returned naive proxies:\n%s", out)
	}
	jsonOut, _, err := NewSubJsonService("", "", "", "", base).GetJson("someone-else", "sub.example.com", false)
	if err != nil {
		t.Fatalf("GetJson err = %v", err)
	}
	if jsonOut != "" {
		t.Fatalf("foreign subId returned naive configs:\n%s", jsonOut)
	}
}

// Legacy Clash has no http-with-TLS proxy, so the legacy output must drop the
// naive entries rather than emit a proxy the old core cannot parse.
func TestNaiveSub_LegacyClashDropsNaive(t *testing.T) {
	seedNaiveSub(t, "naive-sub")
	base := NewSubService("")

	out, _, err := NewSubClashService(false, "", base).GetClashLegacy("naive-sub", "sub.example.com")
	if err == nil && strings.Contains(out, "naive.example.com") {
		t.Fatalf("legacy clash must not carry naive proxies:\n%s", out)
	}
}

// Guard the encoding the plain format uses, so a naive-only subscription keeps
// decoding to the links a client imports.
func TestNaiveSub_PlainOutputIsBase64Links(t *testing.T) {
	seedNaiveSub(t, "naive-sub")
	links, _, _, _, err := NewSubService("").GetSubs("naive-sub", "sub.example.com")
	if err != nil {
		t.Fatalf("GetSubs err = %v", err)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(string(decoded), "naive+https://bob:pw2@naive.example.com:8443") {
		t.Errorf("extra user missing from plain output: %s", decoded)
	}
}
