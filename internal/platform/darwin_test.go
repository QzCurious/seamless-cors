//go:build darwin

package platform

import (
	"strings"
	"testing"
)

type fakeRunner struct {
	calls []string
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch args[0] {
	case "-listallnetworkservices":
		return []byte("An asterisk (*) denotes that a network service is disabled.\nWi-Fi\nThunderbolt Bridge\n"), nil
	case "-getautoproxyurl":
		return []byte("URL: http://old.example/proxy.pac\nEnabled: Yes\n"), nil
	default:
		return []byte{}, nil
	}
}

func TestDarwinAdapterInstallsPACAndRestoresPreviousAutoProxy(t *testing.T) {
	runner := &fakeRunner{}
	adapter := &DarwinAdapter{runner: runner}

	if err := adapter.InstallPAC("http://127.0.0.1:8079/proxy.pac"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RestoreProxy(); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"networksetup -setautoproxyurl Wi-Fi http://127.0.0.1:8079/proxy.pac",
		"networksetup -setautoproxystate Wi-Fi on",
		"networksetup -setautoproxyurl Wi-Fi http://old.example/proxy.pac",
		"networksetup -setautoproxystate Wi-Fi on",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
}

func TestDarwinAdapterTrustsAndRemovesEphemeralCAInUserKeychain(t *testing.T) {
	runner := &fakeRunner{}
	adapter := &DarwinAdapter{runner: runner, keychainPath: "/tmp/login.keychain-db"}
	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIBATAKBggqhkjOPQQDAjAUMRIwEAYDVQQDEwlkZXYtdGVz
dDAeFw0yNjAxMDEwMDAwMDBaFw0yNjAxMDIwMDAwMDBaMBQxEjAQBgNVBAMTCWRl
di10ZXN0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEVm7vdnHl4ppQq91cHjbB
BiYUDhmY3ar6+P0gq0LrFs5vKiRbP+RlYoDx6K6P4iW22JwdAC3PeEGGhIOZLaNN
MEswDgYDVR0PAQH/BAQDAgEGMA8GA1UdEwEB/wQFMAMBAf8wKAYDVR0RBCEwH4IJ
ZGV2LXRlc3SHBH8AAAGHEAAAAAAAAAAAAAAAAAAAAAEwCgYIKoZIzj0EAwIDSAAw
RQIhAOoa4X7HjCOTEOEdPAQRxIhH3WETktsEOl3ZK9otm64jAiBEfd+WY1KcU6RC
3EpP1QovunMjInSJ/ksZrQPrLEpe7g==
-----END CERTIFICATE-----
`)

	if err := adapter.TrustCA(certPEM); err != nil {
		t.Fatal(err)
	}
	if err := adapter.RemoveCA(); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.calls, "\n")
	for _, want := range []string{
		"security add-trusted-cert -d -r trustRoot -k /tmp/login.keychain-db",
		"security delete-certificate -Z",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing call %q in:\n%s", want, joined)
		}
	}
}
