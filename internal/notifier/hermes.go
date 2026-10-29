package notifier

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Hermes struct {
	path    string
	target  string
	timeout time.Duration
}

func NewHermes(path string, target string, timeout time.Duration) *Hermes {
	return &Hermes{
		path:    path,
		target:  target,
		timeout: timeout,
	}
}

func (h *Hermes) SendText(ctx context.Context, text string) error {
	if strings.TrimSpace(h.path) == "" {
		return fmt.Errorf("hermes path is empty")
	}
	if strings.TrimSpace(h.target) == "" {
		return fmt.Errorf("hermes target is empty")
	}

	sendCtx := ctx
	cancel := func() {}
	if h.timeout > 0 {
		sendCtx, cancel = context.WithTimeout(ctx, h.timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(sendCtx, h.path, "send", "--to", h.target, "--quiet", "--file", "-")
	cmd.Stdin = strings.NewReader(text)
	output, err := cmd.CombinedOutput()
	if sendCtx.Err() != nil {
		return fmt.Errorf("hermes send timed out: %w", sendCtx.Err())
	}
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("hermes send failed: %s", detail)
	}
	return nil
}
