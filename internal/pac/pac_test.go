package pac

import (
	"reflect"
	"strings"
	"testing"

	"seamless-cors/internal/domain"
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
	if !strings.Contains(js, `var origins = [{"scheme":"http","host":"api.example.test","port":"80"}]`) {
		t.Fatalf("HTTP origin route should be present, got:\n%s", js)
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
	if !strings.Contains(js, `var exactHosts = [{"host":"api.example.test","http":true,"https":false}]`) {
		t.Fatalf("hostname shorthand should route HTTP when CA is not trusted, got:\n%s", js)
	}
	if strings.Contains(js, `"https":true`) {
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
	if !strings.Contains(js, `{"scheme":"http","host":"api.example.test","port":"8081"}`) {
		t.Fatalf("PAC should check full-origin port, got:\n%s", js)
	}
}

func TestDeriveRouteBucketsAppliesTrustAwarePACRouting(t *testing.T) {
	entries := mustParseEntries(t,
		"api.example.test",
		"*.dev.example.test",
		"http://origin.example.test:8081",
		"https://secure.example.test",
	)

	buckets := deriveRouteBuckets(entries, false)

	wantExact := []hostRoute{{Host: "api.example.test", AllowHTTP: true, AllowHTTPS: false}}
	if !reflect.DeepEqual(buckets.exactHosts, wantExact) {
		t.Fatalf("exact hosts = %#v", buckets.exactHosts)
	}
	wantWildcards := []hostRoute{{Host: "dev.example.test", AllowHTTP: true, AllowHTTPS: false}}
	if !reflect.DeepEqual(buckets.wildcardParents, wantWildcards) {
		t.Fatalf("wildcard parents = %#v", buckets.wildcardParents)
	}
	wantOrigins := []originRoute{{Scheme: "http", Host: "origin.example.test", Port: "8081"}}
	if !reflect.DeepEqual(buckets.origins, wantOrigins) {
		t.Fatalf("origins = %#v", buckets.origins)
	}
}

func TestDeriveRouteBucketsIncludesHTTPSWhenCATrusted(t *testing.T) {
	entries := mustParseEntries(t, "api.example.test", "https://secure.example.test")

	buckets := deriveRouteBuckets(entries, true)

	wantExact := []hostRoute{{Host: "api.example.test", AllowHTTP: true, AllowHTTPS: true}}
	if !reflect.DeepEqual(buckets.exactHosts, wantExact) {
		t.Fatalf("exact hosts = %#v", buckets.exactHosts)
	}
	wantOrigins := []originRoute{{Scheme: "https", Host: "secure.example.test", Port: "443"}}
	if !reflect.DeepEqual(buckets.origins, wantOrigins) {
		t.Fatalf("origins = %#v", buckets.origins)
	}
}

func TestGenerateRendersMostlyStaticPACWithRouteArrays(t *testing.T) {
	entry, err := domain.ParseEntry("*.dev.example.test")
	if err != nil {
		t.Fatal(err)
	}
	js := Generate(Options{
		ProxyListen: "127.0.0.1:8080",
		CATrusted:   true,
		Entries:     []domain.Entry{entry},
	})

	for _, want := range []string{
		`var proxy = "PROXY 127.0.0.1:8080";`,
		`var exactHosts = [];`,
		`var wildcardParents = [{"host":"dev.example.test","http":true,"https":true}];`,
		`function matchesWildcardRoute(routes, scheme, host)`,
		`function matchesOrigin(scheme, host, port)`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Generated PAC missing %q:\n%s", want, js)
		}
	}
}

func mustParseEntries(t *testing.T, texts ...string) []domain.Entry {
	t.Helper()
	entries := make([]domain.Entry, 0, len(texts))
	for _, text := range texts {
		entry, err := domain.ParseEntry(text)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}
