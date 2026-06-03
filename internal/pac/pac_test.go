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
	if strings.Contains(js, "scheme == 'https'") {
		t.Fatal("HTTPS route should be omitted when CA is not trusted")
	}
	if !strings.Contains(js, "scheme == 'http'") {
		t.Fatal("HTTP route should be present")
	}
	if !strings.Contains(js, "DIRECT") {
		t.Fatal("PAC should return DIRECT for unmatched traffic")
	}
}
