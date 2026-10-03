package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QzCurious/seamless-cors/internal/gateway"
)

func confirmUpstreamListCreation(ctx context.Context, stdin io.Reader, stdout io.Writer, detail gateway.UpstreamListCreationConsent) (bool, error) {
	fmt.Fprintf(stdout, "upstreams.txt is missing: %s\n", detail.Path)
	if len(detail.MissingParentDirectories) > 0 {
		fmt.Fprintf(stdout, "Missing parent directories that will also be created:\n  %s\n", strings.Join(detail.MissingParentDirectories, "\n  "))
	}
	fmt.Fprint(stdout, "Create it? [Y/n] ")
	return readYes(ctx, stdin, true)
}

func readYes(ctx context.Context, stdin io.Reader, defaultYes bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}

	type readResult struct {
		answer string
		err    error
	}
	result := make(chan readResult, 1)
	go func() {
		answer, err := bufio.NewReader(stdin).ReadString('\n')
		result <- readResult{answer: answer, err: err}
	}()

	var answer string
	var err error
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case read := <-result:
		answer, err = read.answer, read.err
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes, nil
	}
	return answer == "y" || answer == "yes", nil
}
