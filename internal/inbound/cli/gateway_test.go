package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/adrg/xdg"

	"github.com/QzCurious/seamless-cors/internal/gateway"
)

func TestSecondForegroundSignalForcesExit(t *testing.T) {
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	forced := make(chan int, 1)
	go superviseForegroundSignals(signals, done, cancel, func(code int) { forced <- code })
	signals <- os.Interrupt
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("first signal did not request graceful stop")
	}
	signals <- os.Interrupt
	select {
	case code := <-forced:
		if code != 130 {
			t.Fatalf("code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("second signal did not force exit")
	}
}

func TestStartRendersEverySystemPACServiceAndDeliveryIssue(t *testing.T) {
	var out bytes.Buffer
	renderStartResult(&out, gateway.Started{UpstreamListCreationWarning: &gateway.UpstreamListCreationWarningDetail{Cause: "directory write denied"}, Guidance: gateway.StartGuidance{SystemPAC: gateway.SystemPACReport{
		Services: []gateway.SystemPACServiceState{
			{Name: "Ethernet", Manageable: true, Owned: true, Enabled: new(true)},
			{Name: "Wi-Fi", Manageable: false, Enabled: new(true), URL: "http://corp/pac"},
			{Name: "VPN"},
		},
		Issues: []gateway.SystemPACIssue{{Kind: gateway.SystemPACIssueMutation, ServiceName: "Ethernet", Cause: "write denied"}},
	}}})
	for _, want := range []string{"upstream-list creation failed: directory write denied", "Ethernet: manageable: enabled", "Wi-Fi: not manageable: enabled: http://corp/pac", "VPN: not manageable: unobserved", "mutation: Ethernet: write denied"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStopCleanupUncertaintyIsProminentButSuccessful(t *testing.T) {
	publishTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stop" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"changed":            true,
			"cleanupFulfillment": gateway.CommandUnfulfilled,
			"systemPacCleanup": gateway.SystemPACReport{Issues: []gateway.SystemPACIssue{
				{Kind: gateway.SystemPACIssueVerification, ServiceName: "VPN", Cause: "verification uncertain"},
			}},
			"cleanupFailures": []gateway.CleanupFailure{{Subject: gateway.CleanupSubjectSystemPAC, Diagnostic: "verification uncertain"}},
		})
	})
	var out bytes.Buffer
	if err := executeTestCommand([]string{"stop"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"cleanup is incomplete", "owned PAC or Gateway state may remain", "verification uncertain", "system-pac-cleanup-issue: verification: VPN", "rerun `seamless-cors stop`"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestStatusLabelsFreshAndHistoricalSystemPACReports(t *testing.T) {
	var out bytes.Buffer
	historical := gateway.SystemPACReport{Issues: []gateway.SystemPACIssue{{Kind: gateway.SystemPACIssueMutation, Cause: "old failure"}}}
	renderStatus(&out, gateway.StatusResult{StatusReport: gateway.StatusReport{State: gateway.GatewayStatusRunning, Runtime: &gateway.RuntimeStatusDetail{
		LatestSystemPACDelivery: &historical,
	}, SystemPAC: gateway.SystemPACReport{Services: []gateway.SystemPACServiceState{{Name: "Wi-Fi", Manageable: true, Owned: true, Enabled: new(true)}}}, InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthUsable}}})
	for _, want := range []string{"system-pac-current-service", "system-pac-historical-delivery-issue", "old failure"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRecoverableFileSyncIssueRendersRepairAction(t *testing.T) {
	var out bytes.Buffer
	renderFileSyncIssue(&out, gateway.UpstreamListSourceGlobal, "/config/upstreams.txt", &gateway.FileSyncIssue{Kind: gateway.FileSyncIssueFileUnreadable, Cause: "file missing"})
	for _, want := range []string{"unreadable: file missing", "observation will resume automatically"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %s", want, out.String())
		}
	}
}

func TestStatusRendersBlockedHTTPSCORSWithInstallGuidance(t *testing.T) {
	var out bytes.Buffer
	renderStatus(&out, gateway.StatusResult{StatusReport: gateway.StatusReport{
		State:       gateway.GatewayStatusRunning,
		InstalledCA: gateway.InstalledCAStatusDetail{Health: gateway.CAHealthNotUsable},
		Runtime:     &gateway.RuntimeStatusDetail{Traffic: gateway.TrafficStatusDetail{HTTPSCORS: gateway.TrafficFeatureBlocked}},
	}})
	for _, want := range []string{"https-cors: blocked", "Run `seamless-cors install`"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %s", want, out.String())
		}
	}
}

func TestInstallAndUninstallRenderResults(t *testing.T) {
	var out bytes.Buffer
	renderInstallResult(&out, gateway.InstallResult{Kind: gateway.InstallResultInstalled})
	if !strings.Contains(out.String(), "User CA is installed.") {
		t.Fatalf("install output = %q", out.String())
	}
	out.Reset()
	renderUninstallResult(&out, gateway.UninstallResult{Kind: gateway.UninstallResultUninstalled})
	if !strings.Contains(out.String(), "User CA is uninstalled.") {
		t.Fatalf("uninstall output = %q", out.String())
	}
}

func TestUninstallRequiresConsentAndRetriesWithFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		calls int
		want  string
	}{
		{name: "accepted", input: "yes\n", calls: 2, want: "User CA is uninstalled."},
		{name: "declined by default", input: "", calls: 1, want: "uninstall canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			publishTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/uninstall" || r.Method != http.MethodPost {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				calls++
				var request gateway.UninstallRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if calls == 1 {
					if request.ConsentFingerprint != "" {
						t.Errorf("first request already consented: %#v", request)
					}
					w.WriteHeader(http.StatusConflict)
					io.WriteString(w, `{"error":{"code":"consent-required","message":"confirmation required","details":{"consentFingerprint":"current-state"}}}`)
					return
				}
				if request.ConsentFingerprint != "current-state" {
					t.Errorf("retry fingerprint = %q", request.ConsentFingerprint)
				}
				io.WriteString(w, `{}`)
			})
			var out bytes.Buffer
			if err := executeTestCommand([]string{"uninstall"}, strings.NewReader(tc.input), &out); err != nil {
				t.Fatal(err)
			}
			if calls != tc.calls || !strings.Contains(out.String(), tc.want) || !strings.Contains(out.String(), "HTTPS interception is active") {
				t.Fatalf("calls = %d, output = %q", calls, out.String())
			}
		})
	}
}

func TestStartUsesCommandStreamsForConsentAndGuidance(t *testing.T) {
	calls := 0
	publishTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/start" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		var request gateway.StartRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if calls == 1 {
			if request.UpstreamListCreationConsent != nil {
				t.Errorf("first request already consented: %#v", request)
			}
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"error":{"code":"upstream-list-creation-consent-required","message":"confirmation required","details":{"upstreamListCreationConsent":{"path":"/config/upstreams.txt","fingerprint":"current-state"}}}}`)
			return
		}
		if consent := request.UpstreamListCreationConsent; consent == nil || consent.Decision != gateway.UpstreamListCreationAccepted || consent.Fingerprint != "current-state" {
			t.Errorf("retry consent = %#v", consent)
		}
		json.NewEncoder(w).Encode(map[string]any{"changed": true, "guidance": gateway.StartGuidance{}})
	})
	var out bytes.Buffer
	if err := executeTestCommand([]string{"start"}, strings.NewReader("yes\n"), &out); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("start requests = %d", calls)
	}
	for _, want := range []string{"/config/upstreams.txt", "Create it?", "seamless-cors is running."} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output missing %q: %s", want, out.String())
		}
	}
}

func TestStartDoesNotConsentWhenPromptInputFails(t *testing.T) {
	calls := 0
	publishTestGateway(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/start" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			io.WriteString(w, `{"error":{"code":"upstream-list-creation-consent-required","message":"confirmation required","details":{"upstreamListCreationConsent":{"path":"/config/upstreams.txt","fingerprint":"current-state"}}}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"changed": true, "guidance": gateway.StartGuidance{}})
	})
	readErr := errors.New("terminal input failed")
	stdin, writer := io.Pipe()
	defer stdin.Close()
	if err := writer.CloseWithError(readErr); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := executeTestCommand([]string{"start"}, stdin, &out)
	if !errors.Is(err, readErr) || calls != 1 {
		t.Fatalf("start error = %v, requests = %d; want input error and no consent retry", err, calls)
	}
}

// Publish an HTTP test owner so CLI tests use the real Gateway discovery and
// client path without changing OS-managed state.
func publishTestGateway(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	t.Cleanup(xdg.Reload)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	xdg.Reload()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Seamless-CORS-Token") != "test-owner-token" {
			t.Error("missing Gateway Owner token")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	path, err := xdg.RuntimeFile("seamless-cors/gateway-state-cache.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]string{
		"httpRouterListen": strings.TrimPrefix(server.URL, "http://"),
		"token":            "test-owner-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
