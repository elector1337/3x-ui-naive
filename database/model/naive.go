package model

// naive proxy server (runs as external process, not an xray protocol)
type NaiveServer struct {
	Id        int    `json:"id" form:"id" gorm:"primaryKey;autoIncrement"`
	Remark    string `json:"remark" form:"remark"`
	Enable    bool   `json:"enable" form:"enable" gorm:"default:false"`
	Listen    string `json:"listen" form:"listen"`
	Port      int    `json:"port" form:"port"`
	Domain    string `json:"domain" form:"domain"`
	UseACME   bool   `json:"useAcme" form:"useAcme" gorm:"default:false"`
	AcmeEmail string `json:"acmeEmail" form:"acmeEmail"`
	CertFile  string `json:"certFile" form:"certFile"`
	KeyFile   string `json:"keyFile" form:"keyFile"`
	AuthUser  string `json:"authUser" form:"authUser"`
	AuthPass  string `json:"authPass" form:"authPass"`
	Padding   bool   `json:"padding" form:"padding" gorm:"default:true"`
	LogLevel  string `json:"logLevel" form:"logLevel" gorm:"default:WARN"`
	ExtraArgs    string `json:"extraArgs" form:"extraArgs"`
	UseRawConfig bool   `json:"useRawConfig" form:"useRawConfig" gorm:"default:false"`
	RawConfig    string `json:"rawConfig" form:"rawConfig"`
	CreatedAt int64  `json:"createdAt" gorm:"autoCreateTime"`
	UpdatedAt int64  `json:"updatedAt" gorm:"autoUpdateTime"`
}

func (NaiveServer) TableName() string { return "naive_servers" }
