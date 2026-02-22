package credissuance

type Environment string

const Production Environment = "production"
const Preproduction Environment = "preproduction"
const Development Environment = "development"

type SMTPConfig struct {
	Enabled      bool   `json:"enabled,omitempty" yaml:"enabled"`
	Host         string `json:"host,omitempty" yaml:"host"`
	Port         int    `json:"port,omitempty" yaml:"port"`
	TLS          bool   `json:"tls,omitempty" yaml:"tls"`
	Username     string `json:"username,omitempty" yaml:"username"`
	password     string
	PasswordFile string `json:"passwordFile,omitempty" yaml:"passwordFile"`
}
