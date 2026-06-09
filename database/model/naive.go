package model

import (
	"github.com/mhsanaei/3x-ui/v3/util/crypto"

	"gorm.io/gorm"
)

// naive proxy server (runs as external process, not an xray protocol)
type NaiveServer struct {
	Id     int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Remark string `json:"remark" form:"remark"`
	Enable bool   `json:"enable" form:"enable" gorm:"default:false"`
	// SubId ties this server (and its users) into a subscription. When it
	// matches a requested subscription id, the server's naive+https client
	// URLs are appended to that subscription's output. Empty = not included.
	SubId     string `json:"subId" form:"subId" gorm:"index"`
	Listen    string `json:"listen" form:"listen"`
	Port      int    `json:"port" form:"port"`
	Domain    string `json:"domain" form:"domain"`
	UseACME   bool   `json:"useAcme" form:"useAcme" gorm:"default:false"`
	AcmeEmail string `json:"acmeEmail" form:"acmeEmail"`
	CertFile  string `json:"certFile" form:"certFile"`
	KeyFile   string `json:"keyFile" form:"keyFile"`
	AuthUser  string `json:"authUser" form:"authUser"`
	AuthPass  string `json:"authPass" form:"authPass"`
	// IPLimit is the maximum number of distinct client IPs allowed per user
	// (primary + each extra user). 0 = unlimited. When >0 the panel enables a
	// per-site JSON access log and the naive IP job bans excess IPs via
	// nftables (Linux + root). Excess bans carry a timeout, so an IP that
	// stops exceeding the limit is unbanned automatically.
	IPLimit int  `json:"ipLimit" form:"ipLimit" gorm:"default:0"`
	Padding bool `json:"padding" form:"padding" gorm:"default:true"`
	// EnableH3 advertises and serves HTTP/3 (QUIC). On by default (Caddy's
	// default); disabling it pins the server to h1+h2.
	EnableH3     bool   `json:"enableH3" form:"enableH3" gorm:"default:true"`
	LogLevel     string `json:"logLevel" form:"logLevel" gorm:"default:WARN"`
	ExtraArgs    string `json:"extraArgs" form:"extraArgs"`
	UseRawConfig bool   `json:"useRawConfig" form:"useRawConfig" gorm:"default:false"`
	RawConfig    string `json:"rawConfig" form:"rawConfig"`
	// Up / Down are cumulative byte counters sampled from the kernel (nftables)
	// by the naive traffic job. They keep accumulating across restarts until an
	// explicit reset. Caddy's forward_proxy does not report tunnel bytes itself,
	// so the count comes from a per-port nftables counter (Linux + root only).
	Up   int64 `json:"up" form:"up" gorm:"default:0"`
	Down int64 `json:"down" form:"down" gorm:"default:0"`
	// Total is the traffic quota in bytes (Up+Down); 0 = unlimited. When the
	// quota is reached the naive traffic job stops the process.
	Total int64 `json:"total" form:"total" gorm:"default:0"`
	// ExpiryTime is an absolute expiry timestamp in milliseconds; 0 = never.
	// Past expiry the naive traffic job stops the process.
	ExpiryTime int64 `json:"expiryTime" form:"expiryTime" gorm:"default:0"`
	// TrafficReset is the periodic counter-reset schedule: never|day|week|month.
	TrafficReset string `json:"trafficReset" form:"trafficReset" gorm:"default:never"`
	// LastTrafficResetTime records when the periodic reset last ran (ms).
	LastTrafficResetTime int64 `json:"lastTrafficResetTime" form:"lastTrafficResetTime" gorm:"default:0"`
	// Users are additional basic_auth credentials beyond AuthUser/AuthPass.
	// Each enabled user becomes its own `basic_auth` line in the Caddyfile and
	// gets its own client URL / QR. Loaded via Preload; cascade-deleted.
	Users     []*NaiveUser `json:"users" form:"users" gorm:"foreignKey:NaiveId;constraint:OnDelete:CASCADE"`
	CreatedAt int64        `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt int64        `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (NaiveServer) TableName() string { return "naive_servers" }

// NaiveUser is an extra basic_auth credential attached to a NaiveServer.
type NaiveUser struct {
	Id       int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	NaiveId  int    `json:"naiveId" form:"naiveId" gorm:"index"`
	Username string `json:"username" form:"username"`
	Password string `json:"password" form:"password"`
	// No gorm default: a bool default:true would force-enable rows saved with
	// Enable=false (gorm can't tell a false zero-value from "unset"). The form
	// always sends an explicit value.
	Enable bool `json:"enable" form:"enable"`
}

func (NaiveUser) TableName() string { return "naive_users" }

// BeforeSave / AfterFind transparently encrypt the user password at rest,
// matching the NaiveServer.AuthPass scheme.
func (u *NaiveUser) BeforeSave(_ *gorm.DB) error {
	enc, err := crypto.EncryptString(u.Password)
	if err != nil {
		return err
	}
	u.Password = enc
	return nil
}

func (u *NaiveUser) AfterFind(_ *gorm.DB) error {
	dec, err := crypto.DecryptString(u.Password)
	if err != nil {
		return err
	}
	u.Password = dec
	return nil
}

// BeforeSave encrypts the AuthPass field before write so the on-disk
// SQLite row never contains the plain credential. Idempotent: skips
// already-encrypted values.
func (s *NaiveServer) BeforeSave(_ *gorm.DB) error {
	enc, err := crypto.EncryptString(s.AuthPass)
	if err != nil {
		return err
	}
	s.AuthPass = enc
	return nil
}

// AfterFind decrypts AuthPass back to plaintext for downstream consumers
// (Caddyfile generator, REST API responses, etc.).
func (s *NaiveServer) AfterFind(_ *gorm.DB) error {
	dec, err := crypto.DecryptString(s.AuthPass)
	if err != nil {
		return err
	}
	s.AuthPass = dec
	return nil
}
