package corsproxy

import (
	"net/http"
	"sort"
	"strings"
)

var corsHeaderNames = map[string]struct{}{
	"Access-Control-Allow-Credentials":     {},
	"Access-Control-Allow-Headers":         {},
	"Access-Control-Allow-Methods":         {},
	"Access-Control-Allow-Origin":          {},
	"Access-Control-Expose-Headers":        {},
	"Access-Control-Max-Age":               {},
	"Access-Control-Allow-Private-Network": {},
}

func isPreflight(req *http.Request) bool {
	return req.Method == http.MethodOptions &&
		req.Header.Get("Origin") != "" &&
		req.Header.Get("Access-Control-Request-Method") != ""
}

func preflightResponse(req *http.Request) *http.Response {
	headers := http.Header{}
	applyReflectivePolicy(headers, req.Header.Get("Origin"))
	headers.Set("Access-Control-Allow-Methods", req.Header.Get("Access-Control-Request-Method"))
	if requestedHeaders := req.Header.Get("Access-Control-Request-Headers"); requestedHeaders != "" {
		headers.Set("Access-Control-Allow-Headers", requestedHeaders)
	}
	if req.Header.Get("Access-Control-Request-Private-Network") == "true" {
		headers.Set("Access-Control-Allow-Private-Network", "true")
	}
	headers.Set("Access-Control-Max-Age", "600")
	return &http.Response{
		StatusCode:    http.StatusNoContent,
		Status:        http.StatusText(http.StatusNoContent),
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        headers,
		Body:          http.NoBody,
		ContentLength: 0,
		Request:       req,
	}
}

func repairResponseHeaders(headers http.Header, origin string) {
	if origin == "" {
		return
	}
	for name := range corsHeaderNames {
		headers.Del(name)
	}
	applyReflectivePolicy(headers, origin)
	headers.Set("Access-Control-Expose-Headers", concreteExposeHeaders(headers))
}

func applyReflectivePolicy(headers http.Header, origin string) {
	if origin == "" {
		return
	}
	headers.Set("Access-Control-Allow-Origin", origin)
	headers.Set("Access-Control-Allow-Credentials", "true")
	preserveVary(headers, "Origin")
}

func concreteExposeHeaders(headers http.Header) string {
	var names []string
	for name := range headers {
		if _, isCORS := corsHeaderNames[http.CanonicalHeaderKey(name)]; isCORS {
			continue
		}
		names = append(names, http.CanonicalHeaderKey(name))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func preserveVary(headers http.Header, value string) {
	current := headers.Values("Vary")
	for _, item := range current {
		for _, part := range strings.Split(item, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	headers.Add("Vary", value)
}
