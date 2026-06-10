package corsproxy

import (
	"net/http"
	"testing"
)

func TestRepairResponseHeadersReplacesCORSAndPreservesVary(t *testing.T) {
	headers := http.Header{}
	headers.Set("Access-Control-Allow-Origin", "*")
	headers.Set("X-Trace", "abc")
	headers.Set("Vary", "Accept-Encoding")

	repairResponseHeaders(headers, "null")

	if got := headers.Get("Access-Control-Allow-Origin"); got != "null" {
		t.Fatalf("origin = %q", got)
	}
	if got := headers.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials = %q", got)
	}
	if got := headers.Get("Access-Control-Expose-Headers"); got != "Vary, X-Trace" {
		t.Fatalf("expose = %q", got)
	}
	if got := headers.Values("Vary"); len(got) != 2 {
		t.Fatalf("Vary = %v", got)
	}
}

func TestPreflightResponseReflectsRequestedCapabilities(t *testing.T) {
	req, err := http.NewRequest(http.MethodOptions, "http://api.example.test/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "null")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	req.Header.Set("Access-Control-Request-Headers", "X-Dev")
	req.Header.Set("Access-Control-Request-Private-Network", "true")

	resp := preflightResponse(req)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	for name, want := range map[string]string{
		"Access-Control-Allow-Origin":          "null",
		"Access-Control-Allow-Credentials":     "true",
		"Access-Control-Allow-Methods":         "PATCH",
		"Access-Control-Allow-Headers":         "X-Dev",
		"Access-Control-Allow-Private-Network": "true",
		"Access-Control-Max-Age":               "600",
	} {
		if got := resp.Header.Get(name); got != want {
			t.Fatalf("%s = %q", name, got)
		}
	}
}
