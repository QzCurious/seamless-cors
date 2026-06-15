package runtimecoord

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"seamless-cors/internal/control"
)

func TestVerifyReportsMissingWithoutStateFile(t *testing.T) {
	coord := New(t.TempDir())

	verification := coord.Verify()

	if verification.Status != Missing {
		t.Fatalf("status = %s, want %s", verification.Status, Missing)
	}
}

func TestVerifyCollapsesMalformedStateFileIntoStale(t *testing.T) {
	coord := New(t.TempDir())
	if err := os.MkdirAll(coord.runtimeDirPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(coord.stateFilePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Stale {
		t.Fatalf("status = %s, want %s", verification.Status, Stale)
	}
}

func TestVerifyReportsStaleWhenControlEndpointIsInactive(t *testing.T) {
	coord := New(t.TempDir())
	if err := coord.Write(RuntimeState{
		State: control.State{ControlListen: "127.0.0.1:1"},
		Token: "token",
	}); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Stale {
		t.Fatalf("status = %s, want %s", verification.Status, Stale)
	}
}

func TestVerifyReportsActiveWhenControlEndpointResponds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/status" {
			http.NotFound(w, req)
			return
		}
		if got := req.Header.Get("X-Seamless-CORS-Token"); got != "token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	coord := New(t.TempDir())
	if err := coord.Write(RuntimeState{
		State: control.State{ControlListen: strings.TrimPrefix(server.URL, "http://")},
		Token: "token",
	}); err != nil {
		t.Fatal(err)
	}

	verification := coord.Verify()

	if verification.Status != Active {
		t.Fatalf("status = %s, want %s", verification.Status, Active)
	}
}

func TestWriteUsesExclusiveRuntimeStateFile(t *testing.T) {
	dir := t.TempDir()
	coord := New(dir)
	state := RuntimeState{State: control.State{ControlListen: "127.0.0.1:1"}, Token: "token"}
	if err := coord.Write(state); err != nil {
		t.Fatal(err)
	}

	err := coord.Write(state)

	if !os.IsExist(err) {
		t.Fatalf("second write error = %v, want exists", err)
	}
}
