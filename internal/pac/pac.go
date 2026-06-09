package pac

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"

	"seamless-cors/internal/domain"
)

type Options struct {
	ProxyListen string
	CATrusted   bool
	Entries     []domain.Entry
}

func Handler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}

type DynamicHandler struct {
	body atomic.Value
}

func NewDynamicHandler(body string) *DynamicHandler {
	h := &DynamicHandler{}
	h.Set(body)
	return h
}

func (h *DynamicHandler) Set(body string) {
	h.body.Store(body)
}

func (h *DynamicHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.body.Load().(string)))
}

func Generate(opts Options) string {
	buckets := deriveRouteBuckets(opts.Entries, opts.CATrusted)
	var b strings.Builder
	b.WriteString("var proxy = ")
	writeJSON(&b, "PROXY "+opts.ProxyListen)
	b.WriteString(";\n")
	b.WriteString("var exactHosts = ")
	writeJSON(&b, buckets.exactHosts)
	b.WriteString(";\n")
	b.WriteString("var wildcardParents = ")
	writeJSON(&b, buckets.wildcardParents)
	b.WriteString(";\n")
	b.WriteString("var origins = ")
	writeJSON(&b, buckets.origins)
	b.WriteString(";\n")
	b.WriteString("\n")
	b.WriteString("function FindProxyForURL(url, host) {\n")
	b.WriteString("  host = host.toLowerCase();\n")
	b.WriteString("  var scheme = url.substring(0, url.indexOf(':')).toLowerCase();\n")
	b.WriteString("  var urlPort = portForURL(url, scheme);\n")
	b.WriteString("  if (matchesOrigin(scheme, host, urlPort)) return proxy;\n")
	b.WriteString("  if (matchesHostRoute(exactHosts, scheme, host)) return proxy;\n")
	b.WriteString("  if (matchesWildcardRoute(wildcardParents, scheme, host)) return proxy;\n")
	b.WriteString("  return 'DIRECT';\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("function portForURL(url, scheme) {\n")
	b.WriteString("  var parsedPort = url.match(/^[a-zA-Z]+:\\/\\/\\[[^\\]]+\\]:(\\d+)/) || url.match(/^[a-zA-Z]+:\\/\\/[^\\/:]+:(\\d+)/);\n")
	b.WriteString("  return parsedPort ? parsedPort[1] : (scheme == 'https' ? '443' : '80');\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("function routeAllows(route, scheme) {\n")
	b.WriteString("  return (scheme == 'http' && route.http) || (scheme == 'https' && route.https);\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("function matchesHostRoute(routes, scheme, host) {\n")
	b.WriteString("  for (var i = 0; i < routes.length; i++) {\n")
	b.WriteString("    if (host == routes[i].host && routeAllows(routes[i], scheme)) return true;\n")
	b.WriteString("  }\n")
	b.WriteString("  return false;\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("function matchesWildcardRoute(routes, scheme, host) {\n")
	b.WriteString("  for (var i = 0; i < routes.length; i++) {\n")
	b.WriteString("    var suffix = '.' + routes[i].host;\n")
	b.WriteString("    if (!routeAllows(routes[i], scheme) || !dnsDomainIs(host, suffix)) continue;\n")
	b.WriteString("    var prefix = host.substring(0, host.length - suffix.length);\n")
	b.WriteString("    if (prefix != '' && prefix.indexOf('.') == -1) return true;\n")
	b.WriteString("  }\n")
	b.WriteString("  return false;\n")
	b.WriteString("}\n")
	b.WriteString("\n")
	b.WriteString("function matchesOrigin(scheme, host, port) {\n")
	b.WriteString("  for (var i = 0; i < origins.length; i++) {\n")
	b.WriteString("    var route = origins[i];\n")
	b.WriteString("    if (scheme == route.scheme && host == route.host && port == route.port) return true;\n")
	b.WriteString("  }\n")
	b.WriteString("  return false;\n")
	b.WriteString("}\n")
	return b.String()
}

type routeBuckets struct {
	exactHosts      []hostRoute
	wildcardParents []hostRoute
	origins         []originRoute
}

type hostRoute struct {
	Host       string `json:"host"`
	AllowHTTP  bool   `json:"http"`
	AllowHTTPS bool   `json:"https"`
}

type originRoute struct {
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
	Port   string `json:"port"`
}

func deriveRouteBuckets(entries []domain.Entry, caTrusted bool) routeBuckets {
	buckets := routeBuckets{
		exactHosts:      []hostRoute{},
		wildcardParents: []hostRoute{},
		origins:         []originRoute{},
	}
	for _, entry := range entries {
		if entry.Scheme != "" {
			if entry.Scheme == "https" && !caTrusted {
				continue
			}
			buckets.origins = append(buckets.origins, originRoute{
				Scheme: entry.Scheme,
				Host:   entry.Host,
				Port:   entry.Port,
			})
			continue
		}
		route := hostRoute{
			Host:       entry.Host,
			AllowHTTP:  true,
			AllowHTTPS: caTrusted,
		}
		if entry.Wildcard {
			route.Host = strings.TrimPrefix(entry.Host, "*.")
			buckets.wildcardParents = append(buckets.wildcardParents, route)
			continue
		}
		buckets.exactHosts = append(buckets.exactHosts, route)
	}
	return buckets
}

func writeJSON(b *strings.Builder, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	b.Write(data)
}
