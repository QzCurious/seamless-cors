package cors

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

func IsPreflight(req *http.Request) bool {
	return req.Method == http.MethodOptions &&
		req.Header.Get("Origin") != "" &&
		req.Header.Get("Access-Control-Request-Method") != ""
}

func WritePreflight(w http.ResponseWriter, req *http.Request) {
	h := w.Header()
	ApplyReflectivePolicy(h, req.Header.Get("Origin"))
	h.Set("Access-Control-Allow-Methods", req.Header.Get("Access-Control-Request-Method"))
	if requestedHeaders := req.Header.Get("Access-Control-Request-Headers"); requestedHeaders != "" {
		h.Set("Access-Control-Allow-Headers", requestedHeaders)
	}
	if req.Header.Get("Access-Control-Request-Private-Network") == "true" {
		h.Set("Access-Control-Allow-Private-Network", "true")
	}
	h.Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

func RepairResponseHeaders(headers http.Header, origin string) {
	for name := range corsHeaderNames {
		headers.Del(name)
	}
	ApplyReflectivePolicy(headers, origin)
	headers.Set("Access-Control-Expose-Headers", concreteExposeHeaders(headers))
}

func ApplyReflectivePolicy(headers http.Header, origin string) {
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
