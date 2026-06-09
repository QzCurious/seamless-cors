package cli

import (
	"fmt"
	"io"

	"seamless-cors/internal/app"
)

const usage = `Usage:
  seamless-cors start
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
	start  func(io.Writer, io.Writer) error
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
		if len(args[1:]) > 0 {
			err := fmt.Errorf("start does not accept configuration flags; edit config.yaml instead")
			fmt.Fprintln(stderr, err)
			return err
		}
		return reportCommandError(stderr, commands.start(stdout, stderr))
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
