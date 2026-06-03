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
