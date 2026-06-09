package control

import (
	"bytes"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestControlEndpointReportsHumanStatusAndAcceptsStop(t *testing.T) {
	server := New(State{
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
	if strings.Contains(out.String(), "log-level") {
		t.Fatalf("status output included obsolete log-level: %q", out.String())
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
