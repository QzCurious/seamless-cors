package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func foregroundSignalContext(parent context.Context) (context.Context, func()) {
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go superviseForegroundSignals(signals, done, cancel, os.Exit)
	return ctx, func() {
		signal.Stop(signals)
		close(done)
		cancel()
	}
}

func superviseForegroundSignals(signals <-chan os.Signal, done <-chan struct{}, cancel context.CancelFunc, force func(int)) {
	select {
	case <-signals:
		cancel()
	case <-done:
		return
	}
	select {
	case <-signals:
		force(130)
	case <-done:
	}
}
