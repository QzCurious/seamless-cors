package gatewayrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"seamless-cors/internal/gatewayfacade"
)

type Facade interface {
	PlanStart() (gatewayfacade.StartPlan, error)
	ExecuteStart(context.Context, gatewayfacade.StartRequest) (gatewayfacade.StartResult, error)
	Stop() (gatewayfacade.StopResult, error)
	Status(bool) (gatewayfacade.StatusResult, error)
	Install() (gatewayfacade.InstallResult, error)
	Uninstall() (gatewayfacade.UninstallResult, error)
}

type Server struct {
	server     *http.Server
	token      string
	facade     Facade
	shutdownCh chan struct{}
}

func New(token string, facade Facade) *Server {
	s := &Server{
		token:      token,
		facade:     facade,
		shutdownCh: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/start/plan", s.handleStartPlan)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/stop", s.handleStop)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/install", s.handleInstall)
	mux.HandleFunc("/uninstall", s.handleUninstall)
	s.server = &http.Server{Handler: mux}
	return s
}

func (s *Server) Serve(listener net.Listener) error {
	return s.server.Serve(listener)
}

func (s *Server) Close(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) ShutdownRequested() <-chan struct{} {
	return s.shutdownCh
}

func (s *Server) handleStartPlan(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(w, req) || !s.method(w, req, http.MethodGet) {
		return
	}
	result, err := s.facade.PlanStart()
	writeResult(w, result, err)
}

func (s *Server) handleStart(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(w, req) || !s.method(w, req, http.MethodPost) {
		return
	}
	var input gatewayfacade.StartRequest
	if req.Body != nil && req.ContentLength != 0 {
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	result, err := s.facade.ExecuteStart(context.Background(), input)
	writeResult(w, result, err)
}

func (s *Server) handleStop(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(w, req) || !s.method(w, req, http.MethodPost) {
		return
	}
	result, err := s.facade.Stop()
	if !writeResult(w, result, err) {
		return
	}
	if result.Kind == gatewayfacade.StopResultStopped {
		go func() {
			time.Sleep(25 * time.Millisecond)
			s.requestShutdown()
			_ = s.server.Close()
		}()
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(w, req) || !s.method(w, req, http.MethodGet) {
		return
	}
	result, err := s.facade.Status(false)
	writeResult(w, result, err)
}

func (s *Server) handleInstall(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(w, req) || !s.method(w, req, http.MethodPost) {
		return
	}
	result, err := s.facade.Install()
	writeResult(w, result, err)
}

func (s *Server) handleUninstall(w http.ResponseWriter, req *http.Request) {
	if !s.authorized(w, req) || !s.method(w, req, http.MethodPost) {
		return
	}
	result, err := s.facade.Uninstall()
	writeResult(w, result, err)
}

func (s *Server) authorized(w http.ResponseWriter, req *http.Request) bool {
	if s.token != "" && req.Header.Get("X-Seamless-CORS-Token") == s.token {
		return true
	}
	w.WriteHeader(http.StatusUnauthorized)
	return false
}

func (s *Server) method(w http.ResponseWriter, req *http.Request, method string) bool {
	if req.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	http.Error(w, fmt.Sprintf("%s requires %s", req.URL.Path, method), http.StatusMethodNotAllowed)
	return false
}

func (s *Server) requestShutdown() {
	select {
	case <-s.shutdownCh:
	default:
		close(s.shutdownCh)
	}
}

func writeResult(w http.ResponseWriter, result any, err error) bool {
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil && !strings.Contains(err.Error(), "broken pipe") {
		return false
	}
	return true
}
