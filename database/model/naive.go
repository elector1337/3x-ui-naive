package model

import (
	"github.com/mhsanaei/3x-ui/v3/util/crypto"

	"gorm.io/gorm"
)

// naive proxy server (runs as external process, not an xray protocol)
type NaiveServer struct {
	Id           int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Remark       string `json:"remark" form:"remark"`
	Enable       bool   `json:"enable" form:"enable" gorm:"default:false"`
	Listen       string `json:"listen" form:"listen"`
	Port         int    `json:"port" form:"port"`
	Domain       string `json:"domain" form:"domain"`
	UseACME      bool   `json:"useAcme" form:"useAcme" gorm:"default:false"`
	AcmeEmail    string `json:"acmeEmail" form:"acmeEmail"`
	CertFile     string `json:"certFile" form:"certFile"`
	KeyFile      string `json:"keyFile" form:"keyFile"`
	AuthUser     string `json:"authUser" form:"authUser"`
	AuthPass     string `json:"authPass" form:"authPass"`
	Padding      bool   `json:"padding" form:"padding" gorm:"default:true"`
	LogLevel     string `json:"logLevel" form:"logLevel" gorm:"default:WARN"`
	ExtraArgs    string `json:"extraArgs" form:"extraArgs"`
	UseRawConfig bool   `json:"useRawConfig" form:"useRawConfig" gorm:"default:false"`
	RawConfig    string `json:"rawConfig" form:"rawConfig"`
	// Up / Down are cumulative byte counters sampled from the kernel (nftables)
	// by the naive traffic job. They keep accumulating across restarts until an
	// explicit reset. Caddy's forward_proxy does not report tunnel bytes itself,
	// so the count comes from a per-port nftables counter (Linux + root only).
	Up        int64 `json:"up" form:"up" gorm:"default:0"`
	Down      int64 `json:"down" form:"down" gorm:"default:0"`
	CreatedAt int64 `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt int64 `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (NaiveServer) TableName() string { return "naive_servers" }

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
