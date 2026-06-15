package domain

import "testing"

func TestParseListSupportsCommentsAndNormalization(t *testing.T) {
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
	if entries[0].Host != "api.example.test" || entries[0].Scheme != "" || entries[0].Port != "" {
		t.Fatalf("hostname shorthand entry = %#v", entries[0])
	}
	if entries[1].Host != "api.example.test" {
		t.Fatalf("uppercase hostname should be normalized, got %q", entries[1].Host)
	}
	if !entries[2].Wildcard || entries[2].Host != "*.qa.example.test" {
		t.Fatalf("wildcard entry = %#v", entries[2])
	}
	if entries[3].Scheme != "https" || entries[3].Host != "localhost" || entries[3].Port != "9443" {
		t.Fatalf("full origin entry = %#v", entries[3])
	}
}

func TestIPv6RequiresFullOrigin(t *testing.T) {
	if _, err := ParseEntry("::1"); err == nil {
		t.Fatal("expected IPv6 shorthand to fail")
	}
	if entry, err := ParseEntry("http://[::1]:3000"); err != nil {
		t.Fatalf("full IPv6 origin failed: %v", err)
	} else if entry.Scheme != "http" || entry.Host != "::1" || entry.Port != "3000" {
		t.Fatalf("full IPv6 origin entry = %#v", entry)
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
