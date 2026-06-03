package pac

import (
	"strings"
	"testing"

	"cors-vpn/internal/domain"
)

func TestGenerateUsesTrustAwareHTTPSRouting(t *testing.T) {
	httpsEntry, err := domain.ParseEntry("https://api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	httpEntry, err := domain.ParseEntry("http://api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   false,
		Entries:     []domain.Entry{httpsEntry, httpEntry},
	})
	if strings.Contains(js, "scheme == 'https' && host == 'api.example.test'") {
		t.Fatal("HTTPS route should be omitted when CA is not trusted")
	}
	if !strings.Contains(js, "scheme == 'http'") {
		t.Fatal("HTTP route should be present")
	}
	if !strings.Contains(js, "DIRECT") {
		t.Fatal("PAC should return DIRECT for unmatched traffic")
	}
}

func TestGenerateRoutesHostnameShorthandHTTPSOnlyWhenCATrusted(t *testing.T) {
	entry, err := domain.ParseEntry("api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   false,
		Entries:     []domain.Entry{entry},
	})
	if !strings.Contains(js, "scheme == 'http' && host == 'api.example.test'") {
		t.Fatalf("hostname shorthand should route HTTP when CA is not trusted, got:\n%s", js)
	}
	if strings.Contains(js, "scheme == 'https' && host == 'api.example.test'") {
		t.Fatalf("hostname shorthand should not route HTTPS when CA is not trusted, got:\n%s", js)
	}
}

func TestGenerateUsesExactPortsForFullOrigins(t *testing.T) {
	entry, err := domain.ParseEntry("http://api.example.test:8081")
	if err != nil {
		t.Fatal(err)
	}
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   false,
		Entries:     []domain.Entry{entry},
	})
	if !strings.Contains(js, "urlPort == '8081'") {
		t.Fatalf("PAC should check full-origin port, got:\n%s", js)
	}
}
