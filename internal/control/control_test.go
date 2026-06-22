package control

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestControlEndpointReportsHumanStatusAndAcceptsStop(t *testing.T) {
	server := New(State{
		RuntimeActive: true,
		ProxyListen:   "127.0.0.1:49152",
		PACListen:     "127.0.0.1:49153",
		ControlListen: "127.0.0.1:0",
		DomainList:    "/Users/example/.seamless-cors/domains.txt",
		CATrusted:     false,
		DomainCount:   2,
	}, "secret-token")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()

	base := "http://" + listener.Addr().String()
	var out bytes.Buffer
	if err := CallStatus(base, "secret-token", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "seamless-cors status: running") {
		t.Fatalf("status output = %q", out.String())
	}
	for _, want := range []string{
		"domain-list: /Users/example/.seamless-cors/domains.txt",
		"domains: 2",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("status output missing %q: %q", want, out.String())
		}
	}
	resp, err := http.Post(base+"/stop", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stop status = %d", resp.StatusCode)
	}

	resp, err = postWithToken(http.Client{}, base+"/stop", "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("stop status = %d", resp.StatusCode)
	}
}

func TestControlEndpointRejectsStatusWithoutToken(t *testing.T) {
	server := New(State{DomainCount: 1}, "secret-token")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() { _ = server.Serve(listener) }()

	resp, err := http.Get("http://" + listener.Addr().String() + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestControlStopReportsCleanupFailureAndCanRetry(t *testing.T) {
	stopErr := errors.New("cleanup denied")
	server := NewWithStop(State{RuntimeActive: true}, "secret-token", func() error {
		err := stopErr
		stopErr = nil
		return err
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	base := "http://" + listener.Addr().String()

	var out bytes.Buffer
	stopped, err := CallStop(base, "secret-token", &out)
	if err == nil || !strings.Contains(err.Error(), "cleanup denied") {
		t.Fatalf("stop error = %v", err)
	}
	if stopped {
		t.Fatal("stop reported success after cleanup failure")
	}

	stopped, err = CallStop(base, "secret-token", &out)
	if err != nil {
		t.Fatal(err)
	}
	if !stopped {
		t.Fatal("stop did not report success after cleanup retry")
	}
	if err := <-done; err != http.ErrServerClosed {
		t.Fatalf("server exit = %v", err)
	}
}
