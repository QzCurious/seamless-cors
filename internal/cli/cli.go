package cli

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"

	"cors-vpn/internal/app"
	"cors-vpn/internal/config"
)

const usage = `Usage:
  cors-gateway start [flags]
  cors-gateway stop [flags]
  cors-gateway status [flags]
`

// Run dispatches the v1 Minimal Command Surface.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("missing command")
	}

	switch args[0] {
	case "start":
		overrides, err := parseOverrides(args[1:])
		if err != nil {
			fmt.Fprintln(stderr, err)
			return err
		}
		return app.Start(stdout, stderr, overrides)
	case "stop":
		return app.Stop(stdout, stderr)
	case "status":
		return app.Status(stdout, stderr)
	default:
		err := fmt.Errorf("unknown command: %s", args[0])
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, usage)
		return err
	}
}

func parseOverrides(args []string) (config.Overrides, error) {
	flags := pflag.NewFlagSet("start", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var overrides config.Overrides
	flags.StringVar(&overrides.ProxyListen, "proxy-listen", "", "proxy listener address")
	flags.StringVar(&overrides.PACListen, "pac-listen", "", "PAC listener address")
	flags.StringVar(&overrides.ControlListen, "control-listen", "", "control listener address")
	flags.BoolVar(&overrides.ManagedSystemProxy, "managed-system-proxy", false, "manage system proxy settings")
	flags.BoolVar(&overrides.CATrusted, "ca-trusted", false, "trust ephemeral development CA for this run")
	flags.StringVar(&overrides.DomainList, "domain-list", "", "domain list path")
	flags.StringVar(&overrides.LogLevel, "log-level", "", "log level")

	if err := flags.Parse(args); err != nil {
		return config.Overrides{}, err
	}

	overrides.ManagedSystemProxySet = flags.Changed("managed-system-proxy")
	overrides.CATrustedSet = flags.Changed("ca-trusted")
	return overrides, nil
}
