package tracker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// DefaultTimeout bounds one tool call when a Cmd sets none.
const DefaultTimeout = time.Minute

// killGrace is how long Run waits, after it kills a tool, for a child that
// kept the output pipes open before it gives up on them.
const killGrace = 2 * time.Second

// Cmd is one call of a tracker's tool.
type Cmd struct {
	Bin     string
	Dir     string        // the working directory; "" keeps the bridge's own
	Args    []string      // the arguments, with no shell
	Env     []string      // added after the bridge's environment, which ChildEnv cleans; a name already there keeps its first value, so Drop it to set it
	Drop    []string      // variables of the bridge's environment to leave out
	Timeout time.Duration // DefaultTimeout when zero
}

// Run runs the tool and returns what it printed. The bridge's MEAT_MODE_*
// settings and Cmd.Drop never reach it. A call that outlives its timeout is
// killed and returns an error that wraps context.DeadlineExceeded; one cut
// short by the caller's context wraps that context's error instead. A
// nonzero exit returns the exec error, for the adapter to put in its own
// words.
func Run(ctx context.Context, c Cmd) (stdout, stderr []byte, err error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Bin, c.Args...)
	cmd.Dir = c.Dir
	cmd.Env = append(ChildEnv(os.Environ(), c.Drop...), c.Env...)
	cmd.WaitDelay = killGrace
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	switch {
	case err == nil || errors.Is(err, exec.ErrNotFound) || ctx.Err() == nil:
	case parent.Err() != nil:
		err = fmt.Errorf("%s stopped: %w", c.Bin, parent.Err())
	default:
		err = fmt.Errorf("%s timed out after %s: %w", c.Bin, timeout, context.DeadlineExceeded)
	}
	return out.Bytes(), errOut.Bytes(), err
}
