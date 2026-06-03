package platform

type Adapter interface {
	InstallPAC(url string) error
	RestoreProxy() error
	TrustCA(certPEM []byte) error
	RemoveCA() error
}
