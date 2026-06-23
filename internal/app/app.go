package app

import (
	"fmt"
	"io"

	"seamless-cors/internal/managedgateway"
	"seamless-cors/internal/platform"
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
	return managedgateway.InstallCA(stdout, adapter)
}

func Uninstall(stdout, _ io.Writer) error {
	adapter := platform.CurrentAdapter
	report := adapter.Capabilities()
	if report.CATrustManagement != platform.CapabilitySupported {
		return fmt.Errorf("CA trust management is unsupported on this platform")
	}
	return managedgateway.UninstallCA(stdout, adapter)
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
