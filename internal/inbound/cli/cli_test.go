package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/QzCurious/seamless-cors/internal/version"
)

func TestCommandPrintsVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			command := NewCommand()
			command.SetArgs(args)
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), version.Current()) || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCommandDiscoveryDoesNotRunGatewayCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "no command", args: []string{}, want: "Available Commands:"},
		{name: "root help", args: []string{"--help"}, want: "Available Commands:"},
		{name: "help command", args: []string{"help", "start"}, want: "Edit these files to configure upstreams"},
		{name: "command help", args: []string{"install", "--help"}, want: "Install, repair, or renew"},
		{name: "completion", args: []string{"completion", "zsh"}, want: "#compdef seamless-cors"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			command := NewCommand()
			for _, child := range command.Commands() {
				child.RunE = func(*cobra.Command, []string) error {
					t.Fatal("discovery must not run a Gateway command")
					return nil
				}
			}
			command.SetArgs(tc.args)
			command.SetOut(&stdout)
			command.SetErr(&stderr)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), tc.want) || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestCommandsRejectPositionalArgumentsBeforeRunning(t *testing.T) {
	for _, name := range []string{"start", "serve", "stop", "status", "install", "uninstall", "version"} {
		t.Run(name, func(t *testing.T) {
			command := NewCommand()
			child, _, err := command.Find([]string{name})
			if err != nil {
				t.Fatal(err)
			}
			child.RunE = func(*cobra.Command, []string) error {
				t.Fatal("invalid arguments must not run the command")
				return nil
			}
			command.SetArgs([]string{name, "unexpected"})
			command.SetOut(io.Discard)
			var stderr bytes.Buffer
			command.SetErr(&stderr)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unexpected") {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(stderr.String(), "Error:") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestCommandRejectsUnknownInput(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"unknown"}, want: "unknown command"},
		{args: []string{"start", "--unknown"}, want: "unknown flag"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			command := NewCommand()
			for _, child := range command.Commands() {
				child.RunE = func(*cobra.Command, []string) error {
					t.Fatal("invalid input must not run a Gateway command")
					return nil
				}
			}
			command.SetArgs(tc.args)
			command.SetOut(io.Discard)
			var stderr bytes.Buffer
			command.SetErr(&stderr)
			if err := command.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v", err)
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestCommandFailureIsPrintedOnceWithoutUsage(t *testing.T) {
	wantErr := errors.New("start failed")
	command := NewCommand()
	start, _, err := command.Find([]string{"start"})
	if err != nil {
		t.Fatal(err)
	}
	start.RunE = func(*cobra.Command, []string) error {
		return wantErr
	}
	var stdout, stderr bytes.Buffer
	command.SetArgs([]string{"start"})
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := command.Execute(); !errors.Is(err, wantErr) {
		t.Fatalf("error = %v", err)
	}
	if got := stderr.String(); got != "Error: start failed\n" {
		t.Fatalf("stderr = %q", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCommandContextCancellationReachesGateway(t *testing.T) {
	publishTestGateway(t, func(http.ResponseWriter, *http.Request) {
		t.Error("canceled commands must not reach the Gateway Router")
	})
	for _, name := range []string{"start", "install"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			command := NewCommand()
			command.SetArgs([]string{name})
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			if err := command.ExecuteContext(ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context cancellation", err)
			}
		})
	}
}

func executeTestCommand(args []string, stdin io.Reader, stdout io.Writer) error {
	command := NewCommand()
	command.SetArgs(args)
	command.SetIn(stdin)
	command.SetOut(stdout)
	command.SetErr(io.Discard)
	return command.Execute()
}
