package platform

type Adapter interface {
	InstallPAC(url string) error
	RestoreProxy() error
	TrustCA(certPEM []byte) error
	RemoveCA() error
}

type NoopAdapter struct{}

func (NoopAdapter) InstallPAC(string) error { return nil }
func (NoopAdapter) RestoreProxy() error     { return nil }
func (NoopAdapter) TrustCA([]byte) error    { return nil }
func (NoopAdapter) RemoveCA() error         { return nil }

var CurrentAdapter Adapter = NoopAdapter{}
