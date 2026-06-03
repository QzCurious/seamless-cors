package app

import (
	"fmt"
	"io"

	"cors-vpn/internal/config"
)

// Start is the foreground runtime entry point. The proxy runtime is added after
// the foundation modules are implemented and covered.
func Start(stdout, _ io.Writer, overrides config.Overrides) error {
	cfg := config.Default()
	cfg = config.ApplyOverrides(cfg, overrides)
	fmt.Fprintf(stdout, "Transparent CORS Gateway start requested\n")
	fmt.Fprintf(stdout, "proxy-listen: %s\n", cfg.ProxyListen)
	fmt.Fprintf(stdout, "pac-listen: %s\n", cfg.PACListen)
	fmt.Fprintf(stdout, "control-listen: %s\n", cfg.ControlListen)
	return nil
}

// Stop will call the token-protected Control Endpoint once runtime state exists.
func Stop(stdout, _ io.Writer) error {
	fmt.Fprintln(stdout, "Transparent CORS Gateway stop requested")
	return nil
}

// Status is intentionally read-only.
func Status(stdout, _ io.Writer) error {
	fmt.Fprintln(stdout, "Transparent CORS Gateway status: not running")
	return nil
}
