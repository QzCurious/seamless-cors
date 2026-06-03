//go:build darwin

package platform

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func init() {
	CurrentAdapter = NewDarwinAdapter()
}

type DarwinAdapter struct {
	runner   commandRunner
	services []proxyServiceState
}

type commandRunner interface {
	run(name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

type proxyServiceState struct {
	Name    string
	URL     string
	Enabled bool
}

func NewDarwinAdapter() *DarwinAdapter {
	return &DarwinAdapter{runner: execRunner{}}
}

func (a *DarwinAdapter) InstallPAC(url string) error {
	services, err := a.listServices()
	if err != nil {
		return err
	}
	a.services = nil
	for _, service := range services {
		state, err := a.getAutoProxy(service)
		if err != nil {
			return err
		}
		a.services = append(a.services, state)
		if _, err := a.networksetup("-setautoproxyurl", service, url); err != nil {
			return err
		}
		if _, err := a.networksetup("-setautoproxystate", service, "on"); err != nil {
			return err
		}
	}
	return nil
}

func (a *DarwinAdapter) RestoreProxy() error {
	var firstErr error
	for _, state := range a.services {
		if state.URL != "" && state.URL != "(null)" {
			if _, err := a.networksetup("-setautoproxyurl", state.Name, state.URL); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		next := "off"
		if state.Enabled {
			next = "on"
		}
		if _, err := a.networksetup("-setautoproxystate", state.Name, next); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (a *DarwinAdapter) TrustCA([]byte) error {
	return nil
}

func (a *DarwinAdapter) RemoveCA() error {
	return nil
}

func (a *DarwinAdapter) listServices() ([]string, error) {
	out, err := a.networksetup("-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	var services []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

func (a *DarwinAdapter) getAutoProxy(service string) (proxyServiceState, error) {
	out, err := a.networksetup("-getautoproxyurl", service)
	if err != nil {
		return proxyServiceState{}, err
	}
	state := proxyServiceState{Name: service}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "URL":
			state.URL = value
		case "Enabled":
			state.Enabled = strings.EqualFold(value, "Yes")
		}
	}
	return state, nil
}

func (a *DarwinAdapter) networksetup(args ...string) ([]byte, error) {
	out, err := a.runner.run("networksetup", args...)
	if err != nil {
		return out, fmt.Errorf("networksetup %s failed: %s: %w", strings.Join(args, " "), bytes.TrimSpace(out), err)
	}
	return out, nil
}
