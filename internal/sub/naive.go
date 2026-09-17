package sub

import (
	"maps"

	"github.com/goccy/go-json"

	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// NaiveProxy in the structured subscription formats.
//
// A naive server is Caddy's forward_proxy: an HTTPS proxy with basic auth. Both
// Mihomo and Xray can speak that without a naive-specific type — as an `http`
// proxy with TLS on the Clash side, and as an `http` outbound over a TLS stream
// on the Xray side — so the same credential the plain output renders as a
// naive+https:// link is expressible in /json and /clash too.

// naiveCredentials returns the naive credentials tagged with subId.
func (s *SubService) naiveCredentials(subId string) []service.NaiveSubCredential {
	if s.naiveService == nil {
		return nil
	}
	return s.naiveService.SubCredentials(subId)
}

// naiveClashProxy renders one credential as a Mihomo `http` proxy. Legacy Clash
// output drops it: legacyClashProxy only keeps vmess/trojan/ss.
func naiveClashProxy(c service.NaiveSubCredential) map[string]any {
	return map[string]any{
		"name":             c.Remark,
		"type":             "http",
		"server":           c.Domain,
		"port":             c.Port,
		"username":         c.Username,
		"password":         c.Password,
		"tls":              true,
		"sni":              c.Domain,
		"skip-cert-verify": false,
	}
}

// naiveJSONConfig renders one credential as a full Xray client config: an http
// outbound carrying the basic-auth user over a TLS stream to the naive domain.
func (s *SubJsonService) naiveJSONConfig(c service.NaiveSubCredential) json_util.RawMessage {
	outbound := map[string]any{
		"protocol": "http",
		"tag":      "proxy",
		"settings": map[string]any{
			"servers": []any{
				map[string]any{
					"address": c.Domain,
					"port":    c.Port,
					"users": []any{
						map[string]any{"user": c.Username, "pass": c.Password},
					},
				},
			},
		},
		"streamSettings": map[string]any{
			"network":  "tcp",
			"security": "tls",
			"tlsSettings": map[string]any{
				"serverName": c.Domain,
				"alpn":       []any{"h2", "http/1.1"},
			},
		},
	}
	rawOutbound, _ := json.Marshal(outbound)
	outbounds := append([]json_util.RawMessage{rawOutbound}, s.defaultOutbounds...)

	config := make(map[string]any)
	maps.Copy(config, s.bakedTemplate())
	config["outbounds"] = outbounds
	config["remarks"] = c.Remark

	raw, _ := json.MarshalIndent(config, "", "  ")
	return raw
}
