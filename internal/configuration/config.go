package configuration

type RuntimeEnv string

const (
	Development   RuntimeEnv = "dev"
	Preproduction RuntimeEnv = "pre"
	Production    RuntimeEnv = "pro"
)

type Config struct {
	DestDir      string               `yaml:"dest_dir"`
	SrcDir       string               `yaml:"src_dir"`
	AppName      string               `yaml:"app_name"`
	Environments map[string]EnvConfig `yaml:"environments"`
}

type EnvConfig struct {
	Runtime RuntimeEnv `yaml:"name"`
	ApiUrl  string     `yaml:"api_url"`
	Debug   bool       `yaml:"debug"`

	PrivateKeyFile        string         `yaml:"privateKeyFile,omitempty"`
	MachineCredentialFile string         `yaml:"machineCredentialFile,omitempty"`
	MyDidkey              string         `yaml:"mydidkey,omitempty"`
	Verifier              VerifierConfig `yaml:"verifier"`
	Issuer                IssuerConfig   `yaml:"issuer"`
	Mail                  MailConfig     `yaml:"mail"`
}

type VerifierConfig struct {
	URL           string `yaml:"url,omitempty"`
	TokenEndpoint string `yaml:"token_endpoint,omitempty"`
}

type IssuerConfig struct {
	CredentialIssuancePath string `yaml:"credentialIssuancePath,omitempty"`
}

type MailConfig struct {
	OnboardTeamEmail []string `yaml:"onboard_team_email"`
	IssuerTeamEmail  []string `yaml:"issuer_team_email"`
	CCTeamEmail      []string `yaml:"cc_list_email"`
	SMTP             SMTPConfig
}

type SMTPConfig struct {
	Enabled      bool   `json:"enabled,omitempty" yaml:"enabled"`
	Host         string `json:"host,omitempty" yaml:"host"`
	Port         int    `json:"port,omitempty" yaml:"port"`
	TLS          bool   `json:"tls,omitempty" yaml:"tls"`
	Username     string `json:"username,omitempty" yaml:"username"`
	PasswordFile string `json:"passwordFile,omitempty" yaml:"passwordFile"`
}
