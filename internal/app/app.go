package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"cors-vpn/internal/ca"
	"cors-vpn/internal/config"
	"cors-vpn/internal/control"
	"cors-vpn/internal/domain"
	"cors-vpn/internal/pac"
	"cors-vpn/internal/platform"
	"cors-vpn/internal/proxy"
)

func Start(stdout, _ io.Writer, overrides config.Overrides) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return StartWithContext(ctx, stdout, overrides, platform.CurrentAdapter)
}

func StartWithContext(ctx context.Context, stdout io.Writer, overrides config.Overrides, adapter platform.Adapter) error {
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
	if len(entries) == 0 {
		return fmt.Errorf("Domain List has no active entries; add at least one domain to %s", loaded.DomainPath)
	}

	runtime, err := NewRuntime(loaded.Config, entries, adapter, stdout)
	if err != nil {
		return err
	}
	writeStartGuidance(stdout, loaded)
	return runtime.Serve(ctx)
}

func writeStartGuidance(stdout io.Writer, loaded config.LoadResult) {
	fmt.Fprintf(stdout, "Transparent CORS Gateway running\n")
	fmt.Fprintf(stdout, "config: %s\n", loaded.ConfigPath)
	fmt.Fprintf(stdout, "domain-list: %s\n", loaded.DomainPath)
	if !loaded.Config.ManagedSystemProxy {
		fmt.Fprintf(stdout, "proxy-listen: %s\n", loaded.Config.ProxyListen)
	}
	if len(loaded.OverrideNames) > 0 {
		fmt.Fprintf(stdout, "one-run overrides: %s\n", strings.Join(loaded.OverrideNames, ", "))
	}
}

type Runtime struct {
	cfg       config.Config
	entries   []domain.Entry
	adapter   platform.Adapter
	stdout    io.Writer
	authority *ca.EphemeralAuthority
	proxyCore *proxy.Core
	proxy     *http.Server
	pac       *http.Server
	control   *control.Server
	listeners []net.Listener
}

func NewRuntime(cfg config.Config, entries []domain.Entry, adapter platform.Adapter, stdout io.Writer) (*Runtime, error) {
	if err := config.Validate(cfg); err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("Domain List has no active entries")
	}
	proxyListener, err := net.Listen("tcp", cfg.ProxyListen)
	if err != nil {
		return nil, fmt.Errorf("proxy-listen %s unavailable: %w", cfg.ProxyListen, err)
	}
	pacListener, err := net.Listen("tcp", cfg.PACListen)
	if err != nil {
		proxyListener.Close()
		return nil, fmt.Errorf("pac-listen %s unavailable: %w", cfg.PACListen, err)
	}
	controlListener, err := net.Listen("tcp", cfg.ControlListen)
	if err != nil {
		proxyListener.Close()
		pacListener.Close()
		return nil, fmt.Errorf("control-listen %s unavailable: %w", cfg.ControlListen, err)
	}

	proxyCore := proxy.New(proxy.Options{Entries: entries, CATrusted: cfg.CATrusted, Logger: stdout})
	pacBody := pac.Generate(pac.Options{ProxyListen: cfg.ProxyListen, CATrusted: cfg.CATrusted, Entries: entries})
	controlServer := control.New(control.State{
		ProxyListen:        cfg.ProxyListen,
		PACListen:          cfg.PACListen,
		ControlListen:      cfg.ControlListen,
		ManagedSystemProxy: cfg.ManagedSystemProxy,
		CATrusted:          cfg.CATrusted,
		DomainCount:        len(entries),
	})
	return &Runtime{
		cfg:       cfg,
		entries:   entries,
		adapter:   adapter,
		stdout:    stdout,
		proxyCore: proxyCore,
		proxy:     &http.Server{Handler: proxyCore},
		pac:       &http.Server{Handler: pac.Handler(pacBody)},
		control:   controlServer,
		listeners: []net.Listener{proxyListener, pacListener, controlListener},
	}, nil
}

func (r *Runtime) Serve(ctx context.Context) error {
	if r.cfg.ManagedSystemProxy {
		if err := r.adapter.InstallPAC("http://" + r.cfg.PACListen + "/proxy.pac"); err != nil {
			r.Close()
			return err
		}
	}
	if r.cfg.CATrusted {
		runtimeDir, err := config.RuntimeDir()
		if err != nil {
			r.Close()
			return err
		}
		authority, err := ca.Create(runtimeDir, r.adapter)
		if err != nil {
			r.Close()
			return err
		}
		r.authority = authority
		r.proxyCore.SetAuthority(authority)
	}

	errs := make(chan error, 3)
	go func() { errs <- r.proxy.Serve(r.listeners[0]) }()
	go func() { errs <- r.pac.Serve(r.listeners[1]) }()
	go func() { errs <- r.control.Serve(r.listeners[2]) }()

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
	if r.cfg.ManagedSystemProxy {
		_ = r.adapter.RestoreProxy()
	}
	_ = ca.Remove(r.authority, r.adapter)
	return nil
}

func Stop(stdout, _ io.Writer) error {
	cfg, err := config.DefaultConfigPath()
	if err != nil {
		return err
	}
	loaded, err := config.LoadOrBootstrap(cfg, config.Overrides{}, nil)
	if err != nil {
		fmt.Fprintln(stdout, "Transparent CORS Gateway status: not running")
		return nil
	}
	return control.CallStop("http://"+loaded.Config.ControlListen, stdout)
}

func Status(stdout, _ io.Writer) error {
	cfg, err := config.DefaultConfigPath()
	if err != nil {
		return err
	}
	loaded, err := config.LoadOrBootstrap(cfg, config.Overrides{}, nil)
	if err != nil {
		fmt.Fprintln(stdout, "Transparent CORS Gateway status: not running")
		return nil
	}
	return control.CallStatus("http://"+loaded.Config.ControlListen, stdout)
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
