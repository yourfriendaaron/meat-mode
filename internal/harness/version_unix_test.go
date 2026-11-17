//go:build unix

package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/yourfriendaaron/meat-mode/internal/harness"
)

func script(t *testing.T, body string) func(string) (string, error) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return func(string) (string, error) { return bin, nil }
}

func TestCheckVersion(t *testing.T) {
	re := regexp.MustCompile(`\d+\.\d+\.\d+\S*`)
	if v, err := harness.CheckVersion(context.Background(), script(t, "echo 'tool-cli 0.4.2-beta'"), "tool", re); err != nil || v != "0.4.2-beta" {
		t.Errorf("with a pattern: %q, %v", v, err)
	}
	if v, err := harness.CheckVersion(context.Background(), script(t, "echo ' 1.2 '"), "tool", nil); err != nil || v != "1.2" {
		t.Errorf("with no pattern: %q, %v", v, err)
	}
	if _, err := harness.CheckVersion(context.Background(), script(t, "echo hello"), "tool", re); err == nil {
		t.Error("output with no version passed")
	}
}

// A program that hangs is killed when the context ends, a child that holds
// its output open included.
func TestCheckVersionTimesOutAHungTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := harness.CheckVersion(ctx, script(t, "sleep 30 & wait"), "tool", nil)
	if err == nil {
		t.Fatal("a hung tool passed")
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("CheckVersion took %s; the hung tool was not stopped", took)
	}
}

func TestCheckVersionToolMissing(t *testing.T) {
	look := func(string) (string, error) { return "", os.ErrNotExist }
	if _, err := harness.CheckVersion(context.Background(), look, "tool", nil); err == nil {
		t.Error("a missing tool passed")
	}
}
