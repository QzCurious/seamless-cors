package cli

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestStartRejectsConfigurationFlags(t *testing.T) {
	var stderr bytes.Buffer

	err := run([]string{"start", "--ca-trusted"}, io.Discard, &stderr, commandHandlers{
		start: func(io.Writer, io.Writer) error {
			t.Fatal("start handler should not run")
			return nil
		},
	})
	if err != nil {
		if !strings.Contains(err.Error(), "does not accept configuration flags") {
			t.Fatalf("error = %v", err)
		}
	} else {
		t.Fatal("expected start flag error")
	}
	if !strings.Contains(stderr.String(), "edit config.yaml") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunPrintsStartCommandErrors(t *testing.T) {
	wantErr := errors.New("start failed")
	var stderr bytes.Buffer

	err := run([]string{"start"}, io.Discard, &stderr, commandHandlers{
		start: func(io.Writer, io.Writer) error {
			return wantErr
		},
	})

	if !errors.Is(err, wantErr) {
		t.Fatalf("run error = %v", err)
	}
	if got := stderr.String(); !strings.Contains(got, wantErr.Error()) {
		t.Fatalf("stderr = %q", got)
	}
}
