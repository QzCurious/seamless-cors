package pac

import (
	"fmt"
	"net/http"
	"strings"

	"cors-vpn/internal/domain"
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

func Generate(opts Options) string {
	var b strings.Builder
	b.WriteString("function FindProxyForURL(url, host) {\n")
	b.WriteString("  var scheme = url.substring(0, url.indexOf(':')).toLowerCase();\n")
	b.WriteString("  var parsedPort = url.match(/^[a-zA-Z]+:\\/\\/\\[[^\\]]+\\]:(\\d+)/) || url.match(/^[a-zA-Z]+:\\/\\/[^\\/:]+:(\\d+)/);\n")
	b.WriteString("  var urlPort = parsedPort ? parsedPort[1] : (scheme == 'https' ? '443' : '80');\n")
	for _, entry := range opts.Entries {
		if entry.Scheme == "https" && !opts.CATrusted {
			continue
		}
		writeRule(&b, entry, opts.ProxyListen, opts.CATrusted)
	}
	b.WriteString("  return 'DIRECT';\n")
	b.WriteString("}\n")
	return b.String()
}

func writeRule(b *strings.Builder, entry domain.Entry, proxyListen string, caTrusted bool) {
	var checks []string
	if entry.Scheme != "" {
		checks = append(checks, fmt.Sprintf("scheme == '%s'", entry.Scheme))
	} else if !caTrusted {
		checks = append(checks, "scheme == 'http'")
	}
	if entry.Port != "" {
		checks = append(checks, fmt.Sprintf("urlPort == '%s'", entry.Port))
	}
	if entry.Wildcard {
		parent := strings.TrimPrefix(entry.Host, "*.")
		checks = append(checks, fmt.Sprintf("dnsDomainIs(host, '.%s') && host.substring(0, host.length - %d).indexOf('.') == -1", parent, len(parent)+1))
	} else {
		checks = append(checks, fmt.Sprintf("host == '%s'", entry.Host))
	}
	condition := strings.Join(checks, " && ")
	fmt.Fprintf(b, "  if (%s) return 'PROXY %s';\n", condition, proxyListen)
}
