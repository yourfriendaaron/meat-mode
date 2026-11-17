//go:build unix

package tracker

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// A hung tool is killed at its timeout, along with a child that holds its
// output open.
func TestRunTimesOutAHungTool(t *testing.T) {
	start := time.Now()
	_, _, err := Run(context.Background(), Cmd{Bin: "sh", Args: []string{"-c", "sleep 30 & wait"}, Timeout: 100 * time.Millisecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v; want a deadline error", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("Run took %s; the hung tool was not stopped", took)
	}
}

// The caller's own deadline, when it comes first, is named as the caller's,
// not as the tool's timeout, and a missing tool still reads as missing.
func TestRunParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := Run(ctx, Cmd{Bin: "sh", Args: []string{"-c", "sleep 30 & wait"}})
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), DefaultTimeout.String()) {
		t.Fatalf("err = %v; want the caller's deadline, not the tool's timeout", err)
	}
	_, _, err = Run(ctx, Cmd{Bin: "meat-mode-no-such-tool"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing tool after the deadline: err = %v; want not found", err)
	}
}

func TestRunEnvAndOutput(t *testing.T) {
	t.Setenv("MEAT_MODE_TOKEN", "secret")
	t.Setenv("DROPPED", "x")
	out, errOut, err := Run(context.Background(), Cmd{
		Bin:  "sh",
		Args: []string{"-c", `echo "$MEAT_MODE_TOKEN|$DROPPED|$KEPT"; echo oops >&2; exit 3`},
		Drop: []string{"DROPPED"},
		Env:  []string{"KEPT=yes"},
	})
	if err == nil {
		t.Fatal("a nonzero exit returned no error")
	}
	if got := strings.TrimSpace(string(out)); got != "||yes" {
		t.Errorf("stdout = %q; want only the added variable", got)
	}
	if strings.TrimSpace(string(errOut)) != "oops" {
		t.Errorf("stderr = %q", errOut)
	}
}
