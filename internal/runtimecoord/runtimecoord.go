package runtimecoord

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"seamless-cors/internal/config"
	"seamless-cors/internal/control"
)

const StateFileName = "control-state.json"

type StateStatus string

const (
	Missing StateStatus = "missing"
	Active  StateStatus = "active"
	Stale   StateStatus = "stale"
)

type RuntimeState struct {
	control.State
	Token string `json:"token"`
}

type Verification struct {
	Status StateStatus
	State  RuntimeState
}

func Default() (*Coordinator, error) {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return nil, err
	}
	return New(runtimeDir), nil
}

type Coordinator struct {
	runtimeDir string
	statePath  string
}

func New(runtimeDir string) *Coordinator {
	return &Coordinator{
		runtimeDir: runtimeDir,
		statePath:  filepath.Join(runtimeDir, StateFileName),
	}
}

func (c *Coordinator) runtimeDirPath() string {
	return c.runtimeDir
}

func (c *Coordinator) stateFilePath() string {
	return c.statePath
}

func (c *Coordinator) Verify() Verification {
	state, err := c.read()
	if err != nil {
		if os.IsNotExist(err) {
			return Verification{Status: Missing}
		}
		return Verification{Status: Stale}
	}
	if isActive(state) {
		return Verification{Status: Active, State: state}
	}
	return Verification{Status: Stale, State: state}
}

func (c *Coordinator) EnsureStartAllowed() (bool, error) {
	if err := os.MkdirAll(c.runtimeDir, 0o700); err != nil {
		return false, err
	}
	switch c.Verify().Status {
	case Active:
		return false, fmt.Errorf("seamless-cors is already running")
	case Stale:
		return true, nil
	default:
		return false, nil
	}
}

func (c *Coordinator) Write(state RuntimeState) error {
	return write(c.statePath, state)
}

func (c *Coordinator) read() (RuntimeState, error) {
	return read(c.statePath)
}

func read(path string) (RuntimeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RuntimeState{}, err
	}
	var state RuntimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return RuntimeState{}, err
	}
	return state, nil
}

func write(path string, state RuntimeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(data)
	return err
}

func WaitForStop(state RuntimeState) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !isActive(state) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func isActive(state RuntimeState) bool {
	if state.ControlListen == "" || state.Token == "" {
		return false
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		if _, err := control.FetchStatus("http://"+state.ControlListen, state.Token); err == nil {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
