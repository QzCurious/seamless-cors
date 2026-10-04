package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIncompleteCleanupStopStillSucceedsAndTerminatesRouter(t *testing.T) {
	server := newRouter("token", &fakeCommandHandler{})
	output, err := server.stop(context.Background(), nil)
	if err != nil || len(output.Body.CleanupFailures) != 1 {
		t.Fatalf("Stop output/error = %#v / %v", output, err)
	}
	select {
	case <-server.ShutdownRequested():
	case <-time.After(time.Second):
		t.Fatal("cleanup-failed Stop did not terminate the Router")
	}
}

func TestDocsAndOpenAPIDoNotRequireToken(t *testing.T) {
	server := newRouter("token", &fakeCommandHandler{})

	for _, path := range []string{"/docs", "/openapi.json"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()

		server.server.Handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d, want %d: %s", path, rec.Code, http.StatusOK, rec.Body.String())
		}
	}
}

func TestCommandRoutesRequireToken(t *testing.T) {
	handler := &fakeCommandHandler{}
	server := newRouter("token", handler)

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status returned %d, want %d: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if handler.statusCalled {
		t.Fatal("command handler Status was called without token")
	}
	var body struct {
		Error gatewayErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != errorCodeUnauthorized || body.Error.Message == "" {
		t.Fatalf("unauthorized body = %#v", body)
	}
}

func TestHealthRequiresTokenAndDoesNotCallCommandHandler(t *testing.T) {
	handler := &fakeCommandHandler{}
	server := newRouter("token", handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("health without token returned %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(tokenHeader, "token")
	rec = httptest.NewRecorder()
	server.server.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("health returned %d, want %d: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if handler.statusCalled {
		t.Fatal("health should not call command handler Status")
	}
}

func TestOpenAPIDocumentsGatewayOwnerToken(t *testing.T) {
	server := newRouter("token", &fakeCommandHandler{})
	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()

	server.server.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("openapi returned %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatal(err)
	}
	components := spec["components"].(map[string]any)
	securitySchemes := components["securitySchemes"].(map[string]any)
	scheme := securitySchemes["gatewayOwnerToken"].(map[string]any)
	if scheme["name"] != tokenHeader {
		t.Fatalf("security scheme name = %v, want %s", scheme["name"], tokenHeader)
	}
	if scheme["in"] != "header" {
		t.Fatalf("security scheme in = %v, want header", scheme["in"])
	}

	paths := spec["paths"].(map[string]any)
	statusPath := paths["/status"].(map[string]any)
	getStatus := statusPath["get"].(map[string]any)
	security := getStatus["security"].([]any)
	if len(security) != 1 {
		t.Fatalf("status security length = %d, want 1", len(security))
	}
	requirement := security[0].(map[string]any)
	if _, ok := requirement["gatewayOwnerToken"]; !ok {
		t.Fatalf("status security = %#v, want gatewayOwnerToken", requirement)
	}
}

type fakeCommandHandler struct {
	statusCalled bool
}

func (f *fakeCommandHandler) Stop(context.Context) (StopResult, error) {
	return StopResult{Kind: StopResultStopped, CleanupFailures: []CleanupFailure{{Subject: CleanupSubjectSystemPAC, Diagnostic: "uncertain"}}}, nil
}

func (f *fakeCommandHandler) Status(context.Context, bool) (StatusResult, error) {
	f.statusCalled = true
	return StatusResult{Kind: StatusResultReported, StatusReport: StatusReport{State: GatewayStatusRunning}}, nil
}

func (f *fakeCommandHandler) Install(context.Context) (InstallResult, error) {
	return InstallResult{Kind: InstallResultInstalled}, nil
}

func (f *fakeCommandHandler) UninstallWithConsent(context.Context, string) (UninstallResult, error) {
	return UninstallResult{Kind: UninstallResultUninstalled}, nil
}
