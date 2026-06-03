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
		ProxyListen:        "127.0.0.1:8080",
		PACListen:          "127.0.0.1:8079",
		ControlListen:      "127.0.0.1:0",
		ManagedSystemProxy: true,
		CATrusted:          false,
		DomainCount:        2,
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
	if !strings.Contains(out.String(), "Transparent CORS Gateway status: running") {
		t.Fatalf("status output = %q", out.String())
	}
	resp, err := http.Post(base+"/stop", "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated stop status = %d", resp.StatusCode)
	}

	resp, err = postWithToken(base+"/stop", "secret-token")
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
