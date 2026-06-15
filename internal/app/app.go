package app

import (
	"errors"
	"fmt"
	"io"

	"seamless-cors/internal/config"
	"seamless-cors/internal/managedgateway"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/userca"
)

func Check(stdout, _ io.Writer) error {
	writeCapabilityReport(stdout, platform.CurrentAdapter.Capabilities())
	return nil
}

func Install(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	if err := requireSupported(adapter.Capabilities()); err != nil {
		return err
	}
	caDir, err := config.CADir()
	if err != nil {
		return err
	}
	_, result, err := userca.Ensure(caDir, adapter)
	if err != nil {
		if errors.Is(err, platform.ErrTrustApprovalDenied) {
			fmt.Fprintln(stdout, "Certificate trust was not approved.")
			fmt.Fprintln(stdout, "Run the command again and approve the system prompt.")
		}
		return err
	}
	if result.Changed {
		fmt.Fprintln(stdout, "Installed User CA installed.")
	} else {
		fmt.Fprintln(stdout, "Installed User CA is already usable.")
	}
	if !result.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.Expires.Format("2006-01-02"))
	}
	if loaded, err := config.LoadExisting(""); err == nil && !loaded.Config.CATrusted {
		fmt.Fprintln(stdout, "HTTPS interception is disabled by config: ca-trusted: false.")
		fmt.Fprintln(stdout, "Set ca-trusted: true to use the Installed User CA.")
	}
	return nil
}

func Uninstall(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	report := adapter.Capabilities()
	if report.CATrustManagement != platform.CapabilitySupported {
		return fmt.Errorf("CA trust management is unsupported on this platform")
	}
	if running, err := managedgateway.Running(); err != nil {
		return err
	} else if running {
		fmt.Fprintln(stdout, "seamless-cors is running; stop it before uninstalling the Installed User CA.")
		return fmt.Errorf("seamless-cors is running")
	}
	caDir, err := config.CADir()
	if err != nil {
		return err
	}
	if err := userca.Uninstall(caDir, adapter); err != nil {
		return err
	}
	if report, err := userca.Inspect(caDir, adapter); err == nil && report.Health != userca.HealthMissing {
		return fmt.Errorf("Installed User CA uninstall incomplete: installed-ca: %s", report.Health)
	}
	fmt.Fprintln(stdout, "Installed User CA uninstalled.")
	return nil
}

func requireSupported(report platform.CapabilityReport) error {
	if report.Supported &&
		report.PACManagement == platform.CapabilitySupported &&
		report.CATrustManagement == platform.CapabilitySupported &&
		report.RuntimeCleanup == platform.CapabilitySupported {
		return nil
	}
	return fmt.Errorf("platform unsupported: run `seamless-cors check` for details")
}

func writeCapabilityReport(stdout io.Writer, report platform.CapabilityReport) {
	fmt.Fprintf(stdout, "platform: %s\n", report.Platform)
	fmt.Fprintf(stdout, "supported: %t\n", report.Supported)
	fmt.Fprintf(stdout, "pac-management: %s\n", report.PACManagement)
	fmt.Fprintf(stdout, "ca-trust-management: %s\n", report.CATrustManagement)
	fmt.Fprintf(stdout, "runtime-cleanup: %s\n", report.RuntimeCleanup)
}
