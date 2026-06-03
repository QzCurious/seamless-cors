package domain

import "testing"

func TestParseListSupportsCommentsAndMatching(t *testing.T) {
	entries, errs := ParseList(`
# staging
api.example.test # exact
*.qa.example.test
https://localhost:9443
`)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d", len(entries))
	}
	if !entries[0].Matches("https", "api.example.test", "443") {
		t.Fatal("hostname shorthand should match any scheme and port")
	}
	if !entries[1].Matches("https", "one.qa.example.test", "443") {
		t.Fatal("wildcard should match one label")
	}
	if entries[1].Matches("https", "two.one.qa.example.test", "443") {
		t.Fatal("wildcard should not match two labels")
	}
	if !entries[2].Matches("https", "localhost", "9443") {
		t.Fatal("full origin should match exact scheme host port")
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
