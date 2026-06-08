package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

type State struct {
	ProxyListen      string   `json:"proxyListen"`
	PACListen        string   `json:"pacListen"`
	ControlListen    string   `json:"controlListen"`
	CATrusted        bool     `json:"caTrusted"`
	DomainCount      int      `json:"domainCount"`
	PendingLifecycle []string `json:"pendingLifecycle,omitempty"`
}

type RuntimeState struct {
	State
	Token string `json:"token"`
}

type Server struct {
	server *http.Server
	token  string
	state  atomic.Value
}

func New(state State, token string) *Server {
	mux := http.NewServeMux()
	s := &Server{token: token}
	s.SetState(state)
	mux.HandleFunc("/status", func(w http.ResponseWriter, req *http.Request) {
		if !s.authorized(req) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.State())
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, req *http.Request) {
		if !s.authorized(req) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		go func() {
			time.Sleep(25 * time.Millisecond)
			_ = s.server.Close()
		}()
	})
	s.server = &http.Server{Handler: mux}
	return s
}

func (s *Server) SetState(state State) {
	s.state.Store(state)
}

func (s *Server) State() State {
	return s.state.Load().(State)
}

func (s *Server) authorized(req *http.Request) bool {
	return s.token != "" && req.Header.Get("X-Seamless-CORS-Token") == s.token
}

func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

func (s *Server) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func WriteRuntimeState(path string, state RuntimeState) error {
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

func ReadRuntimeState(path string) (RuntimeState, error) {
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

func FetchStatus(baseURL, token string) (State, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/status", nil)
	if err != nil {
		return State{}, err
	}
	req.Header.Set("X-Seamless-CORS-Token", token)
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return State{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return State{}, fmt.Errorf("status endpoint returned %s", resp.Status)
	}
	var state State
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return State{}, err
	}
	return state, nil
}

func CallStatus(baseURL, token string, stdout io.Writer) error {
	state, err := FetchStatus(baseURL, token)
	if err != nil {
		fmt.Fprintln(stdout, "seamless-cors status: not running")
		return nil
	}
	fmt.Fprintln(stdout, "seamless-cors status: running")
	fmt.Fprintf(stdout, "runtime-proxy-endpoint: %s\n", state.ProxyListen)
	fmt.Fprintf(stdout, "runtime-pac-endpoint: %s\n", state.PACListen)
	fmt.Fprintf(stdout, "runtime-control-endpoint: %s\n", state.ControlListen)
	fmt.Fprintf(stdout, "domains: %d\n", state.DomainCount)
	fmt.Fprintln(stdout, "managed-pac: active")
	fmt.Fprintf(stdout, "ca-trusted: %t\n", state.CATrusted)
	if len(state.PendingLifecycle) > 0 {
		fmt.Fprintf(stdout, "pending lifecycle changes: %s\n", strings.Join(state.PendingLifecycle, ", "))
	}
	return nil
}

func CallStop(baseURL, token string, stdout io.Writer) (bool, error) {
	client := http.Client{Timeout: 500 * time.Millisecond}
	resp, err := postWithToken(client, baseURL+"/stop", token)
	if err != nil {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return false, nil
	}
	fmt.Fprintln(stdout, "seamless-cors stop requested")
	return true, nil
}

func postWithToken(client http.Client, url, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Seamless-CORS-Token", token)
	return client.Do(req)
}
