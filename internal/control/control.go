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
	ProxyListen        string   `json:"proxyListen"`
	PACListen          string   `json:"pacListen"`
	ControlListen      string   `json:"controlListen"`
	ManagedSystemProxy bool     `json:"managedSystemProxy"`
	CATrusted          bool     `json:"caTrusted"`
	DomainCount        int      `json:"domainCount"`
	PendingLifecycle   []string `json:"pendingLifecycle,omitempty"`
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
	return s.token != "" && req.Header.Get("X-CORS-Gateway-Token") == s.token
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
	return os.WriteFile(path, data, 0o600)
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

func CallStatus(baseURL, token string, stdout io.Writer) error {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/status", nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-CORS-Gateway-Token", token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(stdout, "Transparent CORS Gateway status: not running")
		return nil
	}
	defer resp.Body.Close()
	var state State
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Transparent CORS Gateway status: running")
	fmt.Fprintf(stdout, "proxy-listen: %s\n", state.ProxyListen)
	fmt.Fprintf(stdout, "pac-listen: %s\n", state.PACListen)
	fmt.Fprintf(stdout, "control-listen: %s\n", state.ControlListen)
	fmt.Fprintf(stdout, "domains: %d\n", state.DomainCount)
	fmt.Fprintf(stdout, "managed-system-proxy: %t\n", state.ManagedSystemProxy)
	fmt.Fprintf(stdout, "ca-trusted: %t\n", state.CATrusted)
	if len(state.PendingLifecycle) > 0 {
		fmt.Fprintf(stdout, "pending lifecycle changes: %s\n", strings.Join(state.PendingLifecycle, ", "))
	}
	return nil
}

func CallStop(baseURL, token string, stdout io.Writer) error {
	resp, err := postWithToken(baseURL+"/stop", token)
	if err != nil {
		fmt.Fprintln(stdout, "Transparent CORS Gateway stop requested; no running gateway found")
		return nil
	}
	defer resp.Body.Close()
	fmt.Fprintln(stdout, "Transparent CORS Gateway stop requested")
	return nil
}

func postWithToken(url, token string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-CORS-Gateway-Token", token)
	return http.DefaultClient.Do(req)
}
