package cors

import (
	"net/http"
	"testing"
)

func TestRepairResponseHeadersReplacesCORSAndPreservesVary(t *testing.T) {
	headers := http.Header{}
	headers.Set("Access-Control-Allow-Origin", "*")
	headers.Set("X-Trace", "abc")
	headers.Set("Vary", "Accept-Encoding")

	RepairResponseHeaders(headers, "null")

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
