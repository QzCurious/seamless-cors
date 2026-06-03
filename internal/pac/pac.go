package pac

import (
	"fmt"
	"strings"

	"cors-vpn/internal/domain"
)

type Options struct {
	ProxyListen string
	CATrusted   bool
	Entries     []domain.Entry
}

func Generate(opts Options) string {
	var b strings.Builder
	b.WriteString("function FindProxyForURL(url, host) {\n")
	b.WriteString("  var scheme = url.substring(0, url.indexOf(':')).toLowerCase();\n")
	for _, entry := range opts.Entries {
		if entry.Scheme == "https" && !opts.CATrusted {
			continue
		}
		writeRule(&b, entry, opts.ProxyListen)
	}
	b.WriteString("  return 'DIRECT';\n")
	b.WriteString("}\n")
	return b.String()
}

func writeRule(b *strings.Builder, entry domain.Entry, proxyListen string) {
	var checks []string
	if entry.Scheme != "" {
		checks = append(checks, fmt.Sprintf("scheme == '%s'", entry.Scheme))
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
