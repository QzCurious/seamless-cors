package control

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type State struct {
	ProxyListen        string `json:"proxyListen"`
	PACListen          string `json:"pacListen"`
	ControlListen      string `json:"controlListen"`
	ManagedSystemProxy bool   `json:"managedSystemProxy"`
	CATrusted          bool   `json:"caTrusted"`
	DomainCount        int    `json:"domainCount"`
}

type Server struct {
	server *http.Server
}

func New(state State) *Server {
	mux := http.NewServeMux()
	s := &Server{}
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	})
	mux.HandleFunc("/stop", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		go func() {
			time.Sleep(25 * time.Millisecond)
			_ = s.server.Close()
		}()
	})
	s.server = &http.Server{Handler: mux}
	return s
}

func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

func (s *Server) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func CallStatus(baseURL string, stdout io.Writer) error {
	resp, err := http.Get(baseURL + "/status")
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
	return nil
}

func CallStop(baseURL string, stdout io.Writer) error {
	resp, err := http.Post(baseURL+"/stop", "text/plain", nil)
	if err != nil {
		fmt.Fprintln(stdout, "Transparent CORS Gateway stop requested; no running gateway found")
		return nil
	}
	defer resp.Body.Close()
	fmt.Fprintln(stdout, "Transparent CORS Gateway stop requested")
	return nil
}
