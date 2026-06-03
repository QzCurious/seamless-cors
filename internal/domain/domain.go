package domain

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

type Entry struct {
	Raw      string
	Scheme   string
	Host     string
	Port     string
	Wildcard bool
}

type LineError struct {
	Line int
	Text string
	Err  error
}

func (e LineError) Error() string {
	return fmt.Sprintf("line %d: %s: %v", e.Line, e.Text, e.Err)
}

func ParseList(contents string) ([]Entry, []LineError) {
	var entries []Entry
	var errs []LineError

	for idx, line := range strings.Split(contents, "\n") {
		lineNo := idx + 1
		text := stripComment(line)
		if text == "" {
			continue
		}
		entry, err := ParseEntry(text)
		if err != nil {
			errs = append(errs, LineError{Line: lineNo, Text: text, Err: err})
			continue
		}
		entries = append(entries, entry)
	}
	return entries, errs
}

func ParseEntry(text string) (Entry, error) {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "://") {
		return parseOrigin(text)
	}
	return parseHostname(text)
}

func (e Entry) Matches(scheme, host, port string) bool {
	if e.Scheme != "" && !strings.EqualFold(e.Scheme, scheme) {
		return false
	}
	if e.Port != "" && e.Port != port {
		return false
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	want := strings.ToLower(e.Host)
	if !e.Wildcard {
		return host == want
	}
	suffix := strings.TrimPrefix(want, "*.")
	if !strings.HasSuffix(host, "."+suffix) {
		return false
	}
	prefix := strings.TrimSuffix(host, "."+suffix)
	return prefix != "" && !strings.Contains(prefix, ".")
}

func parseOrigin(text string) (Entry, error) {
	u, err := url.Parse(text)
	if err != nil {
		return Entry{}, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return Entry{}, fmt.Errorf("origin scheme must be http or https")
	}
	if u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return Entry{}, fmt.Errorf("entry must be an origin without path, query, or fragment")
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "*") {
		return Entry{}, fmt.Errorf("wildcards require hostname shorthand")
	}
	return Entry{Raw: text, Scheme: u.Scheme, Host: host, Port: u.Port()}, nil
}

func parseHostname(text string) (Entry, error) {
	if strings.Contains(text, "/") || strings.Contains(text, ":") {
		return Entry{}, fmt.Errorf("host shorthand must not include scheme, port, path, or IPv6")
	}
	host := strings.TrimSuffix(strings.ToLower(text), ".")
	if host == "" {
		return Entry{}, fmt.Errorf("host is required")
	}
	wildcard := strings.HasPrefix(host, "*.")
	if strings.Contains(strings.TrimPrefix(host, "*."), "*") {
		return Entry{}, fmt.Errorf("wildcard must be a single leading label")
	}
	if wildcard && strings.Count(host, ".") < 2 {
		return Entry{}, fmt.Errorf("wildcard must include a concrete parent domain")
	}
	if !wildcard && net.ParseIP(host) != nil && strings.Contains(host, ":") {
		return Entry{}, fmt.Errorf("IPv6 entries require full origin syntax")
	}
	return Entry{Raw: text, Host: host, Wildcard: wildcard}, nil
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		line = line[:idx]
	}
	return strings.TrimSpace(line)
}
