package platform

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"strings"
)

var ErrTrustApprovalDenied = errors.New("certificate trust approval denied")

const PACFootprintFileName = "seamless-cors.pac"

type PACServiceState struct {
	Name    string
	URL     string
	Enabled bool
}

type Adapter interface {
	InstallPAC(url string) error
	CurrentPACState() ([]PACServiceState, error)
	ClearOwnedPAC() error
	HasCAFootprint() (bool, error)
	TrustCA(certPEM []byte) error
	CleanupCAFootprint() error
}

type NoopAdapter struct{}

func (NoopAdapter) InstallPAC(string) error {
	return fmt.Errorf("managed PAC routing is unsupported on this platform")
}
func (NoopAdapter) CurrentPACState() ([]PACServiceState, error) { return nil, nil }
func (NoopAdapter) ClearOwnedPAC() error                        { return nil }
func (NoopAdapter) HasCAFootprint() (bool, error)               { return false, nil }
func (NoopAdapter) TrustCA([]byte) error                        { return nil }
func (NoopAdapter) CleanupCAFootprint() error                   { return nil }

var CurrentAdapter Adapter = NoopAdapter{}

func IsManagedPACFootprint(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return path.Base(u.EscapedPath()) == PACFootprintFileName
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && path.Base(u.EscapedPath()) == PACFootprintFileName
}

func HasOwnedPACState(states []PACServiceState) bool {
	for _, state := range states {
		if state.Enabled && IsManagedPACFootprint(state.URL) {
			return true
		}
	}
	return false
}

func HasForeignEnabledPACState(states []PACServiceState) bool {
	for _, state := range states {
		if state.Enabled && state.URL != "" && state.URL != "(null)" && !IsManagedPACFootprint(state.URL) {
			return true
		}
	}
	return false
}
