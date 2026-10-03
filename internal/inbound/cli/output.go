package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/QzCurious/seamless-cors/internal/gateway"
)

func renderStartResult(stdout io.Writer, result gateway.StartResult) {
	switch typed := result.(type) {
	case gateway.Started:
		renderUpstreamListCreationWarning(stdout, typed.UpstreamListCreationWarning)
		guidance := typed.Guidance
		fmt.Fprintln(stdout, "seamless-cors is running.")
		renderStartUpstreamListSources(stdout, guidance.UpstreamLists)
		renderTrafficStatus(stdout, guidance.Traffic)
		renderUserCAAssessmentIssue(stdout, guidance.UserCAIssue)
		renderUserCAInstallGuidance(stdout, guidance.InstalledCA, guidance.UserCAIssue)
		if guidance.InstalledCA.RenewalDue {
			fmt.Fprintln(stdout, "installed-ca-renewal: due")
			fmt.Fprintln(stdout, "action: Run `seamless-cors install` to renew it.")
		}
		renderSystemPACReport(stdout, "system-pac-delivery", guidance.SystemPAC)
		renderStartUpstreamListIssues(stdout, guidance.UpstreamLists)
	case gateway.AlreadyRunning:
		fmt.Fprintln(stdout, "seamless-cors already running")
	case gateway.StartCleanupFailed:
		renderUpstreamListCreationWarning(stdout, typed.UpstreamListCreationWarning)
		fmt.Fprintln(stdout, "seamless-cors start cleanup failed")
	case gateway.StartAlreadyMutating:
		renderUpstreamListCreationWarning(stdout, typed.UpstreamListCreationWarning)
	case gateway.StartStopCancelled:
		renderUpstreamListCreationWarning(stdout, typed.UpstreamListCreationWarning)
	}
}

func renderSystemPACReport(stdout io.Writer, label string, report gateway.SystemPACReport) {
	for _, service := range report.Services {
		manageability := "not manageable"
		if service.Manageable {
			manageability = "manageable"
		}
		fmt.Fprintf(stdout, "%s-service: %s: %s", label, service.Name, manageability)
		if service.Enabled == nil {
			fmt.Fprint(stdout, ": unobserved")
		} else if *service.Enabled {
			fmt.Fprint(stdout, ": enabled")
		} else {
			fmt.Fprint(stdout, ": disabled")
		}
		if service.URL != "" {
			fmt.Fprintf(stdout, ": %s", service.URL)
		}
		fmt.Fprintln(stdout)
	}
	for _, issue := range report.Issues {
		if issue.ServiceName == "" {
			fmt.Fprintf(stdout, "%s-issue: %s: %s\n", label, issue.Kind, issue.Cause)
		} else {
			fmt.Fprintf(stdout, "%s-issue: %s: %s: %s\n", label, issue.Kind, issue.ServiceName, issue.Cause)
		}
	}
}

func renderStopResult(stdout io.Writer, result gateway.StopResult) {
	switch result.Kind {
	case gateway.StopResultStopped:
		fmt.Fprintln(stdout, "seamless-cors stop requested")
	case gateway.StopResultNotRunning:
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
	}
	if len(result.CleanupFailures) > 0 {
		fmt.Fprintln(stdout, "warning: cleanup is incomplete; owned PAC or Gateway state may remain")
		for _, failure := range result.CleanupFailures {
			fmt.Fprintf(stdout, "cleanup-issue: %s: %s\n", failure.Subject, failure.Diagnostic)
		}
		fmt.Fprintln(stdout, "action: Check System PAC settings and rerun `seamless-cors stop`.")
	}
	renderSystemPACReport(stdout, "system-pac-cleanup", result.SystemPACCleanup)
}

func renderInstallResult(stdout io.Writer, result gateway.InstallResult) {
	switch result.Kind {
	case gateway.InstallResultInstalled:
		fmt.Fprintln(stdout, "User CA is installed.")
	}
	if !result.InstalledCAExpires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCAExpires.Format("2006-01-02"))
	}
}

func renderUninstallResult(stdout io.Writer, result gateway.UninstallResult) {
	switch result.Kind {
	case gateway.UninstallResultUninstalled:
		fmt.Fprintln(stdout, "User CA is uninstalled.")
	case gateway.UninstallResultConsentRequired:
		fmt.Fprintln(stdout, "Installed User CA uninstall requires confirmation.")
	case gateway.UninstallResultIncomplete:
		fmt.Fprintln(stdout, "Installed User CA uninstall is incomplete.")
		if result.CleanupIssue != nil {
			fmt.Fprintf(stdout, "installed-ca-cleanup-issue: %s\n", result.CleanupIssue.Cause)
			if result.CleanupIssue.Action != "" {
				fmt.Fprintf(stdout, "action: %s\n", result.CleanupIssue.Action)
			}
		}
	}
}

func renderStatus(stdout io.Writer, result gateway.StatusResult) {
	switch result.State {
	case gateway.GatewayStatusRunning:
		fmt.Fprintln(stdout, "seamless-cors status: running")
	case gateway.GatewayStatusStarting:
		fmt.Fprintln(stdout, "seamless-cors status: starting")
	case gateway.GatewayStatusRouterOnly:
		fmt.Fprintln(stdout, "seamless-cors status: owner running")
		fmt.Fprintln(stdout, "gateway-runtime: inactive")
	case gateway.GatewayStatusEnding:
		fmt.Fprintln(stdout, "seamless-cors status: owner ending")
		fmt.Fprintln(stdout, "gateway-runtime: inactive")
		fmt.Fprintln(stdout, "retry-stop: run `seamless-cors stop` to finish gateway cleanup")
	default:
		fmt.Fprintln(stdout, "seamless-cors status: not running")
	}
	if result.Runtime != nil {
		fmt.Fprintf(stdout, "runtime-proxy-endpoint: %s\n", result.Runtime.ProxyListen)
		fmt.Fprintf(stdout, "runtime-pac-endpoint: %s\n", result.Runtime.PACListen)
		if result.Owner != nil {
			fmt.Fprintf(stdout, "gateway-router-endpoint: %s\n", result.Owner.RouterListen)
		}
		renderUpstreamListSources(stdout, result.Runtime.UpstreamLists)
		fmt.Fprintf(stdout, "upstreams: %d\n", result.Runtime.UpstreamCount)
		if result.Runtime.LatestSystemPACDelivery != nil {
			renderSystemPACReport(stdout, "system-pac-historical-delivery", *result.Runtime.LatestSystemPACDelivery)
		}
		renderTrafficStatus(stdout, result.Runtime.Traffic)
	}
	renderSystemPACReport(stdout, "system-pac-current", result.SystemPAC)
	fmt.Fprintf(stdout, "installed-ca: %s\n", result.InstalledCA.Health)
	if !result.InstalledCA.Expires.IsZero() {
		fmt.Fprintf(stdout, "installed-ca-expires: %s\n", result.InstalledCA.Expires.Format("2006-01-02"))
	}
	if result.InstalledCA.RenewalDue {
		fmt.Fprintln(stdout, "installed-ca-renewal: due")
	}
	if result.InstalledCA.CleanupIssue != nil {
		fmt.Fprintf(stdout, "installed-ca-cleanup-issue: %s\n", result.InstalledCA.CleanupIssue.Cause)
		if result.InstalledCA.CleanupIssue.Action != "" {
			fmt.Fprintf(stdout, "action: %s\n", result.InstalledCA.CleanupIssue.Action)
		}
	}
	renderUserCAAssessmentIssue(stdout, result.UserCAAssessmentIssue)
	renderUserCAInstallGuidance(stdout, result.InstalledCA, result.UserCAAssessmentIssue)
	if result.Cleanup.State == gateway.CleanupStatusNeeded {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned gateway footprint")
	} else if result.Cleanup.State == gateway.CleanupStatusUnknown {
		fmt.Fprintln(stdout, "cleanup-status: unknown")
		for _, subject := range result.Cleanup.Subjects {
			if subject.State == gateway.CleanupStatusUnknown && subject.Diagnostic != "" {
				fmt.Fprintf(stdout, "cleanup-%s: unknown: %s\n", subject.Subject, subject.Diagnostic)
			}
		}
	}
}

func renderTrafficStatus(stdout io.Writer, status gateway.TrafficStatusDetail) {
	fmt.Fprintf(stdout, "traffic-routing-ready: %t\n", status.RoutingReady)
	fmt.Fprintf(stdout, "http-cors: %s\n", status.HTTPCORS)
	fmt.Fprintf(stdout, "https-cors: %s\n", status.HTTPSCORS)
	fmt.Fprintf(stdout, "https-facade: %s\n", status.HTTPSFacade)
}

func renderUserCAAssessmentIssue(stdout io.Writer, issue *gateway.UserCAAssessmentIssue) {
	if issue != nil {
		fmt.Fprintf(stdout, "userca-assessment-issue: %s\n", issue.Cause)
	}
}

func renderUserCAInstallGuidance(stdout io.Writer, installed gateway.InstalledCAStatusDetail, issue *gateway.UserCAAssessmentIssue) {
	if issue == nil && installed.Health == gateway.CAHealthNotUsable {
		fmt.Fprintln(stdout, "action: Run `seamless-cors install` to install or repair the User CA.")
	}
}

func renderUpstreamListWarnings(stdout io.Writer, warnings []gateway.UpstreamListWarningDetail) {
	for _, warning := range warnings {
		fmt.Fprintf(
			stdout,
			"warning: %s %s:%d: %s: %s\n",
			warning.Source,
			warning.Path,
			warning.Line,
			warning.Text,
			warning.Diagnostic,
		)
	}
}

func renderUpstreamListSources(stdout io.Writer, sources []gateway.UpstreamListSourceDetail) {
	for _, source := range sources {
		fmt.Fprintf(stdout, "upstream-list-%s: %s\n", source.Kind, source.Path)
		renderFileSyncIssue(stdout, source.Kind, source.Path, source.FileSyncIssue)
		renderUpstreamListProjectionIssue(stdout, source.Kind, source.Path, source.ProjectionIssue)
		renderUpstreamListWarnings(stdout, source.Warnings)
	}
}

func renderStartUpstreamListSources(stdout io.Writer, sources []gateway.UpstreamListSourceDetail) {
	if len(sources) == 0 {
		return
	}
	fmt.Fprintln(stdout, "Upstream lists:")
	for _, source := range sources {
		label := "Directory"
		if source.Kind == gateway.UpstreamListSourceGlobal {
			label = "Global"
		}
		fmt.Fprintf(stdout, "  %s: %s\n", label, source.Path)
	}
}

func renderStartUpstreamListIssues(stdout io.Writer, sources []gateway.UpstreamListSourceDetail) {
	for _, source := range sources {
		renderFileSyncIssue(stdout, source.Kind, source.Path, source.FileSyncIssue)
		renderUpstreamListProjectionIssue(stdout, source.Kind, source.Path, source.ProjectionIssue)
		renderUpstreamListWarnings(stdout, source.Warnings)
	}
}

func renderUpstreamListCreationWarning(stdout io.Writer, warning *gateway.UpstreamListCreationWarningDetail) {
	if warning == nil {
		return
	}
	fmt.Fprintf(stdout, "warning: upstream-list creation failed: %s\n", warning.Cause)
}

func renderFileSyncIssue(stdout io.Writer, source gateway.UpstreamListSourceKind, path string, issue *gateway.FileSyncIssue) {
	if issue == nil {
		return
	}
	if issue.Kind == gateway.FileSyncIssueObservationStopped {
		fmt.Fprintf(stdout, "warning: %s %s observation stopped: %s\n", source, path, issue.Cause)
		fmt.Fprintln(stdout, "action: repair the cause and restart seamless-cors")
		return
	}
	fmt.Fprintf(stdout, "warning: %s %s unreadable: %s\n", source, path, issue.Cause)
	fmt.Fprintln(stdout, "action: restore the upstream-list file; observation will resume automatically")
}

func renderUpstreamListProjectionIssue(stdout io.Writer, source gateway.UpstreamListSourceKind, path string, issue *gateway.UpstreamListProjectionIssue) {
	if issue == nil {
		return
	}
	fmt.Fprintf(stdout, "warning: %s %s contents rejected: %s\n", source, path, issue.Cause)
}

func cleanupFailureText(failures []gateway.CleanupFailure) string {
	var parts []string
	for _, failure := range failures {
		if failure.Diagnostic != "" {
			parts = append(parts, failure.Diagnostic)
		} else {
			parts = append(parts, string(failure.Subject))
		}
	}
	return strings.Join(parts, "; ")
}
