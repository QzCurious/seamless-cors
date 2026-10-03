package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/QzCurious/seamless-cors/internal/gateway"
	"github.com/QzCurious/seamless-cors/internal/version"
)

// NewCommand builds the terminal-facing command tree. Callers can configure
// arguments, streams, and context through Cobra before executing it.
func NewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "seamless-cors",
		Short:   "Test browser cross-origin behavior against configured upstreams",
		Version: version.Current(),
		PersistentPreRun: func(command *cobra.Command, _ []string) {
			// Usage helps with invalid input, but not with Gateway failures.
			command.SilenceUsage = true
		},
	}
	command.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start browser traffic handling",
			Long: `Start the gateway using the Global Upstream List and upstreams.txt in the
working directory. Edit these files to configure upstreams.

With no existing owner, start keeps the gateway in the foreground. With an
existing owner, start activates its runtime or retries System PAC delivery.`,
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, stop := foregroundSignalContext(cmd.Context())
				defer stop()
				stdout := cmd.OutOrStdout()
				stdin := cmd.InOrStdin()
				hooks := gateway.StartHooks{
					ConfirmUpstreamListCreation: func(ctx context.Context, detail gateway.UpstreamListCreationConsent) (bool, error) {
						return confirmUpstreamListCreation(ctx, stdin, stdout, detail)
					},
					Started: func(result gateway.StartResult) { renderStartResult(stdout, result) },
				}
				result, err := gateway.Start(ctx, hooks)
				if err != nil {
					return err
				}
				if result == nil {
					return errors.New("gateway start returned no result")
				}
				if result.Fulfillment() == gateway.CommandFulfilled {
					return nil
				}
				if result.Kind() == gateway.StartResultOwnerTransition {
					return fmt.Errorf("Gateway Ownership is transitioning; retry start")
				}
				if cleanup, ok := result.(gateway.StartCleanupFailed); ok {
					return fmt.Errorf("gateway start cleanup failed: %s", cleanupFailureText(cleanup.Failures))
				}
				if result.Kind() == gateway.StartResultStartAlreadyMutating {
					return fmt.Errorf("CA operation in progress; retry start")
				}
				return fmt.Errorf("gateway start was not fulfilled: %s", result.Kind())
			},
		},
		&cobra.Command{
			Use:   "serve",
			Short: "Run the gateway control owner in the foreground",
			Long: `Run the gateway control owner without starting browser traffic handling.
Use a separate start command to activate its runtime.`,
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, stop := foregroundSignalContext(cmd.Context())
				defer stop()
				stdout := cmd.OutOrStdout()
				ready := func() {
					fmt.Fprintln(stdout, "gateway owner running")
				}
				return gateway.Serve(ctx, ready)
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the gateway and clean up its owned state",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, stopSignals := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stopSignals()
				stdout := cmd.OutOrStdout()
				result, err := gateway.Stop(ctx)
				if err != nil {
					return err
				}
				renderStopResult(stdout, result)
				return nil
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report gateway, routing, and User CA state",
			Long: `Report current gateway, routing, and User CA state without changing it.
Output is intended for people rather than a stable scripting interface.`,
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				stdout := cmd.OutOrStdout()
				result, err := gateway.Status(ctx)
				if err != nil {
					return err
				}
				if result.Fulfillment() == gateway.CommandUnfulfilled {
					return fmt.Errorf("Gateway Ownership is transitioning; retry status")
				}
				renderStatus(stdout, result)
				if result.State == gateway.GatewayStatusStaleCache {
					fmt.Fprintln(stdout, "stale Gateway State Cache detected; run start or stop to clean up")
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "install",
			Short: "Install, repair, or renew the current-user development CA",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				stdout := cmd.OutOrStdout()
				result, err := gateway.InstallCA(ctx)
				if err != nil {
					return err
				}
				renderInstallResult(stdout, result)
				if result.Fulfillment() == gateway.CommandFulfilled {
					return nil
				}
				switch result.Kind {
				case gateway.InstallResultAlreadyMutating:
					return fmt.Errorf("certificate operation in progress; retry install")
				case gateway.InstallResultOwnerEnding:
					return fmt.Errorf("Gateway owner is ending; retry install")
				case gateway.InstallResultOwnerTransition:
					return fmt.Errorf("Gateway Ownership is transitioning; retry install")
				default:
					return fmt.Errorf("gateway install was not fulfilled: %s", result.Kind)
				}
			},
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Remove seamless-cors User CAs and local CA material",
			Long: `Remove all seamless-cors User CAs and local CA material.
Ask for confirmation when HTTPS interception is active.`,
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				stdout := cmd.OutOrStdout()
				stdin := cmd.InOrStdin()
				result, err := gateway.UninstallCA(ctx, gateway.UninstallRequest{})
				if err != nil {
					return err
				}
				if result.Kind == gateway.UninstallResultConsentRequired {
					fmt.Fprintln(stdout, "HTTPS interception is active. Uninstalling will disable HTTPS interception and remove every seamless-cors UserCA.")
					fmt.Fprint(stdout, "Proceed? [y/N] ")
					confirmed, err := readYes(ctx, stdin, false)
					if err != nil {
						return err
					}
					if !confirmed {
						fmt.Fprintln(stdout, "Installed User CA uninstall canceled.")
						return nil
					}
					result, err = gateway.UninstallCA(ctx, gateway.UninstallRequest{ConsentFingerprint: result.ConsentFingerprint})
					if err != nil {
						return err
					}
				}
				renderUninstallResult(stdout, result)
				if result.Fulfillment() == gateway.CommandFulfilled {
					return nil
				}
				switch result.Kind {
				case gateway.UninstallResultIncomplete:
					return fmt.Errorf("Installed User CA removal is incomplete")
				case gateway.UninstallResultAlreadyMutating:
					return fmt.Errorf("certificate operation in progress; retry uninstall")
				case gateway.UninstallResultOwnerEnding:
					return fmt.Errorf("Gateway owner is ending; retry uninstall")
				case gateway.UninstallResultOwnerTransition:
					return fmt.Errorf("Gateway Ownership is transitioning; retry uninstall")
				default:
					return fmt.Errorf("gateway uninstall was not fulfilled: %s", result.Kind)
				}
			},
		},
		&cobra.Command{
			Use:   "version",
			Short: "Print the installed version",
			Args:  cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				_, err := fmt.Fprintln(command.OutOrStdout(), command.Root().Version)
				return err
			},
		},
	)
	return command
}
