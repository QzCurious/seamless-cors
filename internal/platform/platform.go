package platform

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"runtime"
	"strings"
	"time"
)

var ErrTrustApprovalDenied = errors.New("certificate trust approval denied")

const PACFootprintFileName = "seamless-cors.pac"

type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityLimited     CapabilityStatus = "limited"
	CapabilityUnknown     CapabilityStatus = "unknown"
)

type CapabilityReport struct {
	Platform          string
	Supported         bool
	PACManagement     CapabilityStatus
	CATrustManagement CapabilityStatus
	RuntimeCleanup    CapabilityStatus
}

type CARecord struct {
	SHA1     string
	CertPEM  []byte
	NotAfter time.Time
}

type PACServiceState struct {
	Name    string
	URL     string
	Enabled bool
}

type Adapter interface {
	Capabilities() CapabilityReport
	InstallPAC(url string, services []string) ([]string, error)
	RefreshPAC(url string, services []string) error
	CurrentPACState() ([]PACServiceState, error)
	ClearOwnedPAC() error
	ClearPACForServices(url string, services []string) error
	TrustedCAs() ([]CARecord, error)
	TrustCA(certPEM []byte) error
	RemoveCAs(fingerprints []string) error
}

type NoopAdapter struct{}

func (NoopAdapter) Capabilities() CapabilityReport {
	return CapabilityReport{
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
		Supported:         false,
		PACManagement:     CapabilityUnsupported,
		CATrustManagement: CapabilityUnsupported,
		RuntimeCleanup:    CapabilityLimited,
	}
}
func (NoopAdapter) InstallPAC(string, []string) ([]string, error) {
	return nil, fmt.Errorf("managed PAC routing is unsupported on this platform")
}
func (NoopAdapter) RefreshPAC(string, []string) error {
	return fmt.Errorf("managed PAC routing is unsupported on this platform")
}
func (NoopAdapter) CurrentPACState() ([]PACServiceState, error) { return nil, nil }
func (NoopAdapter) ClearOwnedPAC() error                        { return nil }
func (NoopAdapter) ClearPACForServices(string, []string) error  { return nil }
func (NoopAdapter) TrustedCAs() ([]CARecord, error)             { return nil, nil }
func (NoopAdapter) TrustCA([]byte) error                        { return nil }
func (NoopAdapter) RemoveCAs([]string) error                    { return nil }

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
