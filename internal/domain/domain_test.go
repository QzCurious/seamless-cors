package domain

import "testing"

func TestParseListSupportsCommentsAndMatching(t *testing.T) {
	entries, errs := ParseList(`
# staging
api.example.test # exact
API.EXAMPLE.TEST
*.qa.example.test
https://localhost:9443
`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(entries) != 4 {
		t.Fatalf("entries = %d", len(entries))
	}
	if !entries[0].Matches("https", "api.example.test", "443") {
		t.Fatal("hostname shorthand should match any scheme and port")
	}
	if entries[1].Host != "api.example.test" {
		t.Fatalf("uppercase hostname should be normalized, got %q", entries[1].Host)
	}
	if !entries[1].Matches("http", "API.EXAMPLE.TEST", "3333") {
		t.Fatal("matching should ignore host case")
	}
	if !entries[2].Matches("https", "one.qa.example.test", "443") {
		t.Fatal("wildcard should match one label")
	}
	if entries[2].Matches("https", "two.one.qa.example.test", "443") {
		t.Fatal("wildcard should not match two labels")
	}
	if !entries[3].Matches("https", "localhost", "9443") {
		t.Fatal("full origin should match exact scheme host port")
	}
}

func TestFullOriginWithoutExplicitPortMatchesOnlyDefaultPort(t *testing.T) {
	entry, err := ParseEntry("https://api.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Matches("https", "api.example.test", "443") {
		t.Fatal("https origin should match the default HTTPS port")
	}
	if entry.Matches("https", "api.example.test", "8443") {
		t.Fatal("https origin without explicit port matched a non-default port")
	}
	if entry.Matches("http", "api.example.test", "80") {
		t.Fatal("https origin matched a different scheme")
	}
}

func TestIPv6RequiresFullOrigin(t *testing.T) {
	if _, err := ParseEntry("::1"); err == nil {
		t.Fatal("expected IPv6 shorthand to fail")
	}
	if entry, err := ParseEntry("http://[::1]:3000"); err != nil {
		t.Fatalf("full IPv6 origin failed: %v", err)
	} else if !entry.Matches("http", "::1", "3000") {
		t.Fatal("full IPv6 origin did not match")
	}
}

func TestInlineCommentsRequireWhitespaceBeforeHash(t *testing.T) {
	entries, errs := ParseList("api#bad.example.test\napi.example.test # staging\n")
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	if len(entries) != 1 || entries[0].Host != "api.example.test" {
		t.Fatalf("entries = %#v", entries)
	}
}
