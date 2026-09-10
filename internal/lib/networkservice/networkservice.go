package networkservice

import (
	"context"
	"fmt"
	"os/exec"
)

// PACSetting is one freshly observed PAC setting for a Network Service.
type PACSetting struct {
	URL     string
	Enabled bool
}

// Service exposes one current-user Network Service and its PAC operations.
type Service interface {
	Name() string
	PAC(context.Context) (PACSetting, error)
	SetPAC(context.Context, string) error
	DisablePAC(context.Context) error
}

var _ func(context.Context) ([]Service, error) = List

type commandRunner interface {
	run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// ListError reports failure to discover the visible Network Services.
type ListError struct{ Cause error }

func (e ListError) Error() string { return fmt.Sprintf("list network services: %v", e.Cause) }
func (e ListError) Unwrap() error { return e.Cause }

// PACError reports failure to read one Network Service's PAC setting.
type PACError struct {
	ServiceName string
	Cause       error
}

func (e PACError) Error() string {
	return fmt.Sprintf("get PAC setting for network service %q: %v", e.ServiceName, e.Cause)
}
func (e PACError) Unwrap() error { return e.Cause }
