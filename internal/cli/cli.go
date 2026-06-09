package cli

import (
	"fmt"
	"io"

	"github.com/spf13/pflag"

	"seamless-cors/internal/app"
	"seamless-cors/internal/config"
)

const usage = `Usage:
  seamless-cors start [flags]
  seamless-cors stop [flags]
  seamless-cors status [flags]
`

// Run dispatches the v1 Minimal Command Surface.
func Run(args []string, stdout, stderr io.Writer) error {
	return run(args, stdout, stderr, commandHandlers{
		start:  app.Start,
		stop:   app.Stop,
		status: app.Status,
	})
}

type commandHandlers struct {
	start  func(io.Writer, io.Writer, config.Overrides) error
	stop   func(io.Writer, io.Writer) error
	status func(io.Writer, io.Writer) error
}

func run(args []string, stdout, stderr io.Writer, commands commandHandlers) error {
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
		return reportCommandError(stderr, commands.start(stdout, stderr, overrides))
	case "stop":
		return reportCommandError(stderr, commands.stop(stdout, stderr))
	case "status":
		return reportCommandError(stderr, commands.status(stdout, stderr))
	default:
		err := fmt.Errorf("unknown command: %s", args[0])
		fmt.Fprintln(stderr, err)
		fmt.Fprint(stderr, usage)
		return err
	}
}

func reportCommandError(stderr io.Writer, err error) error {
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
	return err
}

func parseOverrides(args []string) (config.Overrides, error) {
	flags := pflag.NewFlagSet("start", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var overrides config.Overrides
	flags.BoolVar(&overrides.CATrusted, "ca-trusted", false, "trust ephemeral development CA for this run")
	flags.StringVar(&overrides.DomainList, "domain-list", "", "domain list path")

	if err := flags.Parse(args); err != nil {
		return config.Overrides{}, err
	}

	overrides.CATrustedSet = flags.Changed("ca-trusted")
	return overrides, nil
}
