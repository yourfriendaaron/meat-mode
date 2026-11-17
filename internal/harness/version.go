package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// versionTimeout bounds one `<program> --version`.
const versionTimeout = 30 * time.Second

// CheckVersion looks program up with look (exec.LookPath when nil), runs `<program> --version` and
// returns the first match of pattern in what it prints, or all of it
// trimmed when pattern is nil. The program runs without the bridge's own
// settings or any harness's session markers, and is killed at versionTimeout.
func CheckVersion(ctx context.Context, look func(string) (string, error), program string, pattern *regexp.Regexp) (string, error) {
	if look == nil {
		look = exec.LookPath
	}
	path, err := look(program)
	if err != nil {
		return "", fmt.Errorf("%s not found: %w", program, err)
	}
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Env = StripEnv(os.Environ())
	// A shell wrapper's child can hold the output pipe after the kill.
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", program, err)
	}
	v := strings.TrimSpace(string(out))
	if pattern != nil {
		v = pattern.FindString(v)
	}
	if v == "" {
		return "", fmt.Errorf("%s --version printed no version: %q", program, strings.TrimSpace(string(out)))
	}
	return v, nil
}
