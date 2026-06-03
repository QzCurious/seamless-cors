package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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
