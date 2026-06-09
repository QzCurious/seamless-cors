package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DomainList string `yaml:"domain-list"`
	CATrusted  bool   `yaml:"ca-trusted"`
	SourcePath string `yaml:"-"`
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
	DomainList   string
	CATrusted    bool
	CATrustedSet bool
}

func Default() Config {
	return Config{
		DomainList: "~/.seamless-cors/domains.txt",
		CATrusted:  false,
	}
}

func HomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".seamless-cors"), nil
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
			fmt.Fprintf(stdout, "Created:\n  %s\n  %s\n\n", configPath, filepath.Join(home, DefaultDomainListFileName))
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
	cfg.SourcePath = configPath
	return LoadResult{
		Config:        cfg,
		ConfigPath:    configPath,
		DomainPath:    cfg.DomainList,
		Bootstrapped:  bootstrapped,
		OverrideNames: overrides.Names(),
	}, nil
}

func LoadExisting(configPath string, overrides Overrides) (LoadResult, error) {
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return LoadResult{}, err
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
	cfg.SourcePath = configPath
	return LoadResult{
		Config:        cfg,
		ConfigPath:    configPath,
		DomainPath:    cfg.DomainList,
		OverrideNames: overrides.Names(),
	}, nil
}

func ApplyOverrides(cfg Config, overrides Overrides) Config {
	if overrides.DomainList != "" {
		cfg.DomainList = overrides.DomainList
	}
	if overrides.CATrustedSet {
		cfg.CATrusted = overrides.CATrusted
	}
	return cfg
}

func (o Overrides) Names() []string {
	var names []string
	if o.DomainList != "" {
		names = append(names, "domain-list")
	}
	if o.CATrustedSet {
		names = append(names, "ca-trusted")
	}
	return names
}

func Validate(cfg Config) error {
	if cfg.DomainList == "" {
		return fmt.Errorf("domain-list is required")
	}
	return nil
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
	return `# One domain or origin per line.
domain-list: ~/.seamless-cors/domains.txt

# Opt into HTTPS interception. Generated CA is removed on stop.
ca-trusted: false
`
}
