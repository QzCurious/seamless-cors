package config

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	ProxyListen        string `yaml:"proxy-listen"`
	PACListen          string `yaml:"pac-listen"`
	ControlListen      string `yaml:"control-listen"`
	ManagedSystemProxy bool   `yaml:"managed-system-proxy"`
	DomainList         string `yaml:"domain-list"`
	LogLevel           string `yaml:"log-level"`
	CATrusted          bool   `yaml:"ca-trusted"`
}

const DefaultConfigFileName = "config.yaml"
const DefaultDomainListFileName = "domains.txt"

type LoadResult struct {
	Config        Config
	ConfigPath    string
	DomainPath    string
	Bootstrapped  bool
	OverrideNames []string
}

type Overrides struct {
	ProxyListen           string
	PACListen             string
	ControlListen         string
	ManagedSystemProxy    bool
	ManagedSystemProxySet bool
	DomainList            string
	LogLevel              string
	CATrusted             bool
	CATrustedSet          bool
}

func Default() Config {
	return Config{
		ProxyListen:        "127.0.0.1:8080",
		PACListen:          "127.0.0.1:8079",
		ControlListen:      "127.0.0.1:8078",
		ManagedSystemProxy: true,
		DomainList:         "~/.cors-gateway/domains.txt",
		LogLevel:           "info",
		CATrusted:          false,
	}
}

func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cors-gateway"), nil
}

func DefaultConfigPath() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultConfigFileName), nil
}

func RuntimeDir() (string, error) {
	home, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "runtime"), nil
}

func LoadOrBootstrap(configPath string, overrides Overrides, stdout io.Writer) (LoadResult, error) {
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return LoadResult{}, err
		}
	}

	var bootstrapped bool
	if _, err := os.Stat(configPath); err != nil {
		if !os.IsNotExist(err) {
			return LoadResult{}, err
		}
		if err := bootstrap(configPath); err != nil {
			return LoadResult{}, err
		}
		bootstrapped = true
		if stdout != nil {
			home, _ := HomeDir()
			fmt.Fprintf(stdout, "Created:\n  %s\n  %s\n\nAdd at least one domain to domains.txt, then run:\n  cors-gateway start\n", configPath, filepath.Join(home, DefaultDomainListFileName))
		}
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return LoadResult{}, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return LoadResult{}, fmt.Errorf("invalid config.yaml: %w", err)
	}
	cfg = ApplyOverrides(cfg, overrides)
	cfg.DomainList, err = ExpandPath(cfg.DomainList)
	if err != nil {
		return LoadResult{}, err
	}
	if err := Validate(cfg); err != nil {
		return LoadResult{}, err
	}
	return LoadResult{
		Config:        cfg,
		ConfigPath:    configPath,
		DomainPath:    cfg.DomainList,
		Bootstrapped:  bootstrapped,
		OverrideNames: overrides.Names(),
	}, nil
}

func ApplyOverrides(cfg Config, overrides Overrides) Config {
	if overrides.ProxyListen != "" {
		cfg.ProxyListen = overrides.ProxyListen
	}
	if overrides.PACListen != "" {
		cfg.PACListen = overrides.PACListen
	}
	if overrides.ControlListen != "" {
		cfg.ControlListen = overrides.ControlListen
	}
	if overrides.ManagedSystemProxySet {
		cfg.ManagedSystemProxy = overrides.ManagedSystemProxy
	}
	if overrides.DomainList != "" {
		cfg.DomainList = overrides.DomainList
	}
	if overrides.LogLevel != "" {
		cfg.LogLevel = overrides.LogLevel
	}
	if overrides.CATrustedSet {
		cfg.CATrusted = overrides.CATrusted
	}
	return cfg
}

func (o Overrides) Names() []string {
	var names []string
	if o.ProxyListen != "" {
		names = append(names, "proxy-listen")
	}
	if o.PACListen != "" {
		names = append(names, "pac-listen")
	}
	if o.ControlListen != "" {
		names = append(names, "control-listen")
	}
	if o.ManagedSystemProxySet {
		names = append(names, "managed-system-proxy")
	}
	if o.DomainList != "" {
		names = append(names, "domain-list")
	}
	if o.LogLevel != "" {
		names = append(names, "log-level")
	}
	if o.CATrustedSet {
		names = append(names, "ca-trusted")
	}
	return names
}

func Validate(cfg Config) error {
	for name, address := range map[string]string{
		"proxy-listen":   cfg.ProxyListen,
		"pac-listen":     cfg.PACListen,
		"control-listen": cfg.ControlListen,
	} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("%s must be a host:port listener address: %w", name, err)
		}
	}
	if cfg.DomainList == "" {
		return fmt.Errorf("domain-list is required")
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
		return nil
	default:
		return fmt.Errorf("log-level must be debug, info, warn, or error")
	}
}

func ExpandPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return os.ExpandEnv(path), nil
}

func bootstrap(configPath string) error {
	home := filepath.Dir(configPath)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	domainPath := filepath.Join(home, DefaultDomainListFileName)
	if _, err := os.Stat(domainPath); os.IsNotExist(err) {
		if err := os.WriteFile(domainPath, []byte("# One domain or origin per line.\n# api.dev.example.com\n"), 0o600); err != nil {
			return err
		}
	}
	return os.WriteFile(configPath, []byte(commentedDefaultConfig()), 0o600)
}

func commentedDefaultConfig() string {
	return `# Local proxy endpoint used by PAC and Manual Proxy Mode.
proxy-listen: 127.0.0.1:8080

# Local endpoint that serves the generated PAC file.
pac-listen: 127.0.0.1:8079

# Local endpoint used by status and stop.
control-listen: 127.0.0.1:8078

# When true, macOS/Windows use PAC routing for matched domains.
managed-system-proxy: true

# One domain or origin per line.
domain-list: ~/.cors-gateway/domains.txt

log-level: info

# Opt into HTTPS interception. Generated CA is removed on stop.
ca-trusted: false
`
}
