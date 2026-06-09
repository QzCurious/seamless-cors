package app

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"seamless-cors/internal/ca"
	"seamless-cors/internal/config"
	"seamless-cors/internal/control"
	"seamless-cors/internal/domain"
	"seamless-cors/internal/pac"
	"seamless-cors/internal/platform"
	"seamless-cors/internal/proxy"
)

var ErrLifecycleConsentDeclined = errors.New("explicit lifecycle consent declined")

func Start(stdout, _ io.Writer, overrides config.Overrides) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartWithContextAndInput(ctx, os.Stdin, stdout, overrides, platform.CurrentAdapter)
}

func StartWithContext(ctx context.Context, stdout io.Writer, overrides config.Overrides, adapter platform.Adapter) error {
	return StartWithContextAndInput(ctx, nil, stdout, overrides, adapter)
}

func StartWithContextAndInput(ctx context.Context, stdin io.Reader, stdout io.Writer, overrides config.Overrides, adapter platform.Adapter) error {
	if active, err := activeRuntimeState(); err != nil {
		return err
	} else if active {
		fmt.Fprintln(stdout, "seamless-cors is already running")
		return nil
	}
	if err := RuntimeCleanup(stdout, adapter); err != nil {
		return err
	}
	loaded, err := config.LoadOrBootstrap("", overrides, stdout)
	if err != nil {
		return err
	}
	entries, errs, err := loadDomainList(loaded.DomainPath)
	if err != nil {
		return err
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid Domain List:\n%s", formatDomainErrors(errs))
	}
	pacStates, err := adapter.CurrentPACState()
	if err != nil {
		return err
	}
	consent := lifecycleConsentRequest{
		ManagedPAC:      platform.HasForeignEnabledPACState(pacStates),
		CATrust:         loaded.Config.CATrusted,
		CurrentPACState: pacStates,
	}
	if err := promptForLifecycleConsent(stdin, stdout, consent); err != nil {
		return err
	}

	runtime, err := NewRuntime(loaded.Config, entries, adapter, stdout)
	if err != nil {
		return err
	}
	runtime.overrides = overrides
	writeStartGuidance(stdout, loaded)
	return runtime.Serve(ctx)
}

func writeStartGuidance(stdout io.Writer, loaded config.LoadResult) {
	fmt.Fprintf(stdout, "seamless-cors running\n")
	fmt.Fprintf(stdout, "config: %s\n", loaded.ConfigPath)
	fmt.Fprintf(stdout, "domain-list: %s\n", loaded.DomainPath)
	fmt.Fprintln(stdout, "managed-pac: active")
	if len(loaded.OverrideNames) > 0 {
		fmt.Fprintf(stdout, "one-run overrides: %s\n", strings.Join(loaded.OverrideNames, ", "))
	}
}

type lifecycleConsentRequest struct {
	ManagedPAC      bool
	CATrust         bool
	CurrentPACState []platform.PACServiceState
}

func (r lifecycleConsentRequest) needed() bool {
	return r.ManagedPAC || r.CATrust
}

func promptForLifecycleConsent(stdin io.Reader, stdout io.Writer, req lifecycleConsentRequest) error {
	if !req.needed() {
		return nil
	}
	fmt.Fprintln(stdout, "Explicit Lifecycle Consent required before seamless-cors changes current-user OS-managed state.")
	if req.ManagedPAC {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "Managed PAC Consent:")
		fmt.Fprintln(stdout, "seamless-cors will replace existing managed PAC state for this run.")
		fmt.Fprintln(stdout, "Runtime Cleanup removes seamless-cors PAC settings without restoring previous PAC state.")
		fmt.Fprintln(stdout, "Current managed PAC state:")
		for _, state := range req.CurrentPACState {
			if state.URL == "" {
				state.URL = "(empty)"
			}
			fmt.Fprintf(stdout, "  %s: enabled=%t url=%s\n", state.Name, state.Enabled, state.URL)
		}
	}
	if req.CATrust {
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, "CA Trust Consent:")
		fmt.Fprintln(stdout, "ca-trusted: true adds an ephemeral seamless-cors development CA to the current user's OS trust store for this run.")
	}
	fmt.Fprintln(stdout)
	fmt.Fprint(stdout, "Proceed? [y/N] ")
	ok, err := readYes(stdin)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(stdout, "Startup canceled; no lifecycle changes were applied.")
		return ErrLifecycleConsentDeclined
	}
	fmt.Fprintln(stdout)
	return nil
}

func readYes(stdin io.Reader) (bool, error) {
	if stdin == nil {
		return true, nil
	}
	answer, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	return answer == "y" || answer == "yes", nil
}

type Runtime struct {
	cfg              config.Config
	entries          []domain.Entry
	mu               sync.RWMutex
	adapter          platform.Adapter
	stdout           io.Writer
	authority        *ca.EphemeralAuthority
	proxyCore        *proxy.Core
	proxy            *http.Server
	pacHandler       *pac.DynamicHandler
	pac              *http.Server
	control          *control.Server
	listeners        []net.Listener
	token            string
	runtimeDir       string
	statePath        string
	overrides        config.Overrides
	pendingLifecycle []string
}

func NewRuntime(cfg config.Config, entries []domain.Entry, adapter platform.Adapter, stdout io.Writer) (*Runtime, error) {
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("proxy listener unavailable: %w", err)
	}
	pacListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		return nil, fmt.Errorf("PAC listener unavailable: %w", err)
	}
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		return nil, fmt.Errorf("control listener unavailable: %w", err)
	}

	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		controlListener.Close()
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		controlListener.Close()
		return nil, err
	}
	proxyListen := proxyListener.Addr().String()
	pacListen := pacListener.Addr().String()
	controlListen := controlListener.Addr().String()

	proxyCore := proxy.New(proxy.Options{Entries: entries, CATrusted: cfg.CATrusted, Logger: stdout})
	pacBody := pac.Generate(pac.Options{ProxyListen: proxyListen, CATrusted: cfg.CATrusted, Entries: entries})
	pacHandler := pac.NewDynamicHandler(pacBody)
	controlServer := control.New(control.State{
		ProxyListen:   proxyListen,
		PACListen:     pacListen,
		ControlListen: controlListen,
		DomainList:    cfg.DomainList,
		LogLevel:      cfg.LogLevel,
		CATrusted:     cfg.CATrusted,
		DomainCount:   len(entries),
	}, token)
	return &Runtime{
		cfg:        cfg,
		entries:    entries,
		adapter:    adapter,
		stdout:     stdout,
		proxyCore:  proxyCore,
		pacHandler: pacHandler,
		proxy:      &http.Server{Handler: proxyCore},
		pac:        &http.Server{Handler: pacHandler},
		control:    controlServer,
		listeners:  []net.Listener{proxyListener, pacListener, controlListener},
		token:      token,
		runtimeDir: runtimeDir,
		statePath:  filepath.Join(runtimeDir, "control-state.json"),
	}, nil
}

func (r *Runtime) Serve(ctx context.Context) error {
	if err := r.ensureSingleInstance(); err != nil {
		r.Close()
		return err
	}
	state := control.RuntimeState{
		State: r.controlState(),
		Token: r.token,
	}
	if err := r.writeRuntimeState(state); err != nil {
		r.Close()
		return err
	}

	errs := make(chan error, 4)
	go func() { errs <- r.control.Serve(r.listeners[2]) }()

	if err := r.adapter.InstallPAC("http://" + r.listeners[1].Addr().String() + "/" + platform.PACFootprintFileName); err != nil {
		r.Close()
		return err
	}
	if r.cfg.CATrusted {
		authority, err := ca.Create(r.runtimeDir, r.adapter)
		if err != nil {
			cleanupErr := r.Close()
			if errors.Is(err, platform.ErrTrustApprovalDenied) {
				fmt.Fprintln(r.stdout, "Certificate trust was not approved.")
				fmt.Fprintln(r.stdout, "Run the command again and approve the system prompt, or set ca-trusted: false.")
			}
			if cleanupErr != nil {
				return fmt.Errorf("%w; %v", err, cleanupErr)
			}
			return err
		}
		r.authority = authority
		r.proxyCore.SetAuthority(authority)
		fmt.Fprintln(r.stdout, "Local CA certificate added to the system trust settings.")
	}

	go r.watchLiveDomainList(ctx, errs)
	go r.watchLiveConfig(ctx, errs)
	go func() { errs <- r.proxy.Serve(r.listeners[0]) }()
	go func() { errs <- r.pac.Serve(r.listeners[1]) }()

	select {
	case <-ctx.Done():
		return r.Close()
	case err := <-errs:
		if err == http.ErrServerClosed {
			return r.Close()
		}
		r.Close()
		return err
	}
}

func (r *Runtime) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	_ = r.proxy.Close()
	_ = r.pac.Close()
	_ = r.control.Close(ctx)
	return RuntimeCleanup(r.stdout, r.adapter)
}

func Stop(stdout, _ io.Writer) error {
	state, err := readRuntimeState()
	if err != nil {
		fmt.Fprintln(stdout, "seamless-cors stop requested; no running seamless-cors found")
		return RuntimeCleanup(stdout, platform.CurrentAdapter)
	}
	stopped, err := control.CallStop("http://"+state.ControlListen, state.Token, stdout)
	if err != nil {
		return err
	}
	if stopped {
		waitForRuntimeToStop(state)
	}
	return RuntimeCleanup(stdout, platform.CurrentAdapter)
}

func Status(stdout, _ io.Writer) error {
	state, err := readRuntimeState()
	if err != nil {
		fmt.Fprintln(stdout, "seamless-cors status: not running")
		reportCleanupNeeded(stdout, platform.CurrentAdapter, false)
		return nil
	}
	if err := control.CallStatus("http://"+state.ControlListen, state.Token, stdout); err != nil {
		return err
	}
	if _, err := control.FetchStatus("http://"+state.ControlListen, state.Token); err != nil {
		fmt.Fprintln(stdout, "stale runtime state detected; run start or stop to clean up")
		reportCleanupNeeded(stdout, platform.CurrentAdapter, true)
	}
	return nil
}

func loadDomainList(path string) ([]domain.Entry, []domain.LineError, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	entries, errs := domain.ParseList(string(data))
	return entries, errs, nil
}

func formatDomainErrors(errs []domain.LineError) string {
	var lines []string
	for _, err := range errs {
		lines = append(lines, err.Error())
	}
	return strings.Join(lines, "\n")
}

func (r *Runtime) ensureSingleInstance() error {
	if err := os.MkdirAll(r.runtimeDir, 0o700); err != nil {
		return err
	}
	state, err := control.ReadRuntimeState(r.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		_ = os.Remove(r.statePath)
		return nil
	}
	if runtimeStateIsActive(state) {
		return fmt.Errorf("seamless-cors is already running")
	}
	return RuntimeCleanup(r.stdout, r.adapter)
}

func (r *Runtime) writeRuntimeState(state control.RuntimeState) error {
	if err := control.WriteRuntimeState(r.statePath, state); err == nil {
		return nil
	} else if !os.IsExist(err) {
		return err
	}
	existing, err := control.ReadRuntimeState(r.statePath)
	if err != nil {
		return err
	}
	if runtimeStateIsActive(existing) {
		return fmt.Errorf("seamless-cors is already running")
	}
	if err := RuntimeCleanup(r.stdout, r.adapter); err != nil {
		return err
	}
	return control.WriteRuntimeState(r.statePath, state)
}

func (r *Runtime) watchLiveDomainList(ctx context.Context, errs chan<- error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last string
	var lastPath string
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			path := r.domainListPath()
			if path != lastPath {
				last = ""
				lastPath = path
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			text := string(data)
			if text == last {
				continue
			}
			entries, parseErrs := domain.ParseList(text)
			if len(parseErrs) > 0 {
				select {
				case errs <- fmt.Errorf("Fatal Domain List Error: invalid Domain List:\n%s", formatDomainErrors(parseErrs)):
				case <-ctx.Done():
				}
				return
			}
			r.applyEntries(entries)
			last = text
		}
	}
}

func (r *Runtime) watchLiveConfig(ctx context.Context, errs chan<- error) {
	if r.cfg.SourcePath == "" {
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	data, _ := os.ReadFile(r.cfg.SourcePath)
	last := string(data)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			data, err := os.ReadFile(r.cfg.SourcePath)
			if err != nil {
				continue
			}
			text := string(data)
			if text == last {
				continue
			}
			loaded, err := config.LoadExisting(r.cfg.SourcePath, r.overrides)
			if err != nil {
				select {
				case errs <- fmt.Errorf("Fatal Config Error: %w", err):
				case <-ctx.Done():
				}
				return
			}
			r.mu.Lock()
			r.pendingLifecycle = r.lifecycleChangesLocked(loaded.Config)
			r.cfg.DomainList = loaded.Config.DomainList
			r.cfg.LogLevel = loaded.Config.LogLevel
			state := r.controlStateLocked()
			r.mu.Unlock()
			r.control.SetState(state)
			last = text
		}
	}
}

func (r *Runtime) applyEntries(entries []domain.Entry) {
	r.mu.Lock()
	r.entries = entries
	state := r.controlStateLocked()
	r.mu.Unlock()
	r.proxyCore.SetEntries(entries)
	r.pacHandler.Set(pac.Generate(pac.Options{
		ProxyListen: r.listeners[0].Addr().String(),
		CATrusted:   r.cfg.CATrusted,
		Entries:     entries,
	}))
	r.control.SetState(state)
}

func (r *Runtime) controlState() control.State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.controlStateLocked()
}

func (r *Runtime) controlStateLocked() control.State {
	return control.State{
		ProxyListen:      r.listeners[0].Addr().String(),
		PACListen:        r.listeners[1].Addr().String(),
		ControlListen:    r.listeners[2].Addr().String(),
		DomainList:       r.cfg.DomainList,
		LogLevel:         r.cfg.LogLevel,
		CATrusted:        r.cfg.CATrusted,
		DomainCount:      len(r.entries),
		PendingLifecycle: append([]string(nil), r.pendingLifecycle...),
	}
}

func (r *Runtime) lifecycleChangesLocked(next config.Config) []string {
	var pending []string
	if next.CATrusted != r.cfg.CATrusted {
		pending = append(pending, "ca-trusted")
	}
	return pending
}

func (r *Runtime) domainListPath() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg.DomainList
}

func readRuntimeState() (control.RuntimeState, error) {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return control.RuntimeState{}, err
	}
	return control.ReadRuntimeState(filepath.Join(runtimeDir, "control-state.json"))
}

func randomToken() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func RuntimeCleanup(stdout io.Writer, adapter platform.Adapter) error {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return err
	}
	var errs []error
	if err := adapter.ClearOwnedPAC(); err != nil {
		errs = append(errs, fmt.Errorf("managed PAC cleanup failed: %w", err))
	}
	if err := adapter.CleanupCAFootprint(); err != nil {
		errs = append(errs, fmt.Errorf("CA trust cleanup failed: %w", err))
	}
	for _, name := range []string{"ephemeral-ca.pem", "ephemeral-ca-key.pem", "control-state.json"} {
		err := os.Remove(filepath.Join(runtimeDir, name))
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("runtime file cleanup failed for %s: %w", name, err))
		}
	}
	if len(errs) > 0 {
		return cleanupError{errs: errs}
	}
	return nil
}

type cleanupError struct {
	errs []error
}

func (e cleanupError) Error() string {
	var parts []string
	for _, err := range e.errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ") + "\nCleanup failed; resolve the OS or permission problem, then run `seamless-cors stop` again."
}

func activeRuntimeState() (bool, error) {
	state, err := readRuntimeState()
	if err != nil {
		return false, nil
	}
	return runtimeStateIsActive(state), nil
}

func waitForRuntimeToStop(state control.RuntimeState) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !runtimeStateIsActive(state) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func reportCleanupNeeded(stdout io.Writer, adapter platform.Adapter, staleState bool) {
	needed := staleState || runtimeFilesNeedCleanup()
	if states, err := adapter.CurrentPACState(); err == nil && platform.HasOwnedPACState(states) {
		needed = true
	}
	if hasCA, err := adapter.HasCAFootprint(); err == nil && hasCA {
		needed = true
	}
	if needed {
		fmt.Fprintln(stdout, "cleanup-needed: run `seamless-cors stop` to clean seamless-cors-owned runtime state")
	}
}

func runtimeFilesNeedCleanup() bool {
	runtimeDir, err := config.RuntimeDir()
	if err != nil {
		return false
	}
	for _, name := range []string{"ephemeral-ca.pem", "ephemeral-ca-key.pem", "control-state.json"} {
		if _, err := os.Stat(filepath.Join(runtimeDir, name)); err == nil {
			return true
		}
	}
	return false
}

func runtimeStateIsActive(state control.RuntimeState) bool {
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
