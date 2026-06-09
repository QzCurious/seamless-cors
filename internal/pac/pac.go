package pac

import (
	"encoding/json"
	"fmt"
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
	return fmt.Sprintf(
		pacTemplate,
		pacJSONLiteral("PROXY "+opts.ProxyListen),
		pacJSONLiteral(buckets.exactHosts),
		pacJSONLiteral(buckets.wildcardParents),
		pacJSONLiteral(buckets.origins),
	)
}

const pacTemplate = `var proxy = %s;
var exactHosts = %s;
var wildcardParents = %s;
var origins = %s;

function FindProxyForURL(url, host) {
  host = host.toLowerCase();
  var scheme = url.substring(0, url.indexOf(':')).toLowerCase();
  var urlPort = portForURL(url, scheme);
  if (matchesOrigin(scheme, host, urlPort)) return proxy;
  if (matchesHostRoute(exactHosts, scheme, host)) return proxy;
  if (matchesWildcardRoute(wildcardParents, scheme, host)) return proxy;
  return 'DIRECT';
}

function portForURL(url, scheme) {
  var parsedPort = url.match(/^[a-zA-Z]+:\/\/\[[^\]]+\]:(\d+)/) || url.match(/^[a-zA-Z]+:\/\/[^\/:]+:(\d+)/);
  return parsedPort ? parsedPort[1] : (scheme == 'https' ? '443' : '80');
}

function routeAllows(route, scheme) {
  return (scheme == 'http' && route.http) || (scheme == 'https' && route.https);
}

function matchesHostRoute(routes, scheme, host) {
  for (var i = 0; i < routes.length; i++) {
    if (host == routes[i].host && routeAllows(routes[i], scheme)) return true;
  }
  return false;
}

function matchesWildcardRoute(routes, scheme, host) {
  for (var i = 0; i < routes.length; i++) {
    var suffix = '.' + routes[i].host;
    if (!routeAllows(routes[i], scheme) || !dnsDomainIs(host, suffix)) continue;
    var prefix = host.substring(0, host.length - suffix.length);
    if (prefix != '' && prefix.indexOf('.') == -1) return true;
  }
  return false;
}

function matchesOrigin(scheme, host, port) {
  for (var i = 0; i < origins.length; i++) {
    var route = origins[i];
    if (scheme == route.scheme && host == route.host && port == route.port) return true;
  }
  return false;
}
`

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

type pacLiteral interface {
	string | []hostRoute | []originRoute
}

func pacJSONLiteral[T pacLiteral](value T) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
