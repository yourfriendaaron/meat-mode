//go:build unix && !linux

package bridge

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// agentProcAttr puts the agent in a process group of its own, so the bridge
// can end it with everything it started. There is no parent-death signal
// here; the next bridge start ends a run left behind (see AgentRunner.recover).
func agentProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// processStart returns when pid started, as ps prints it, to tell the agent
// from a later process that reuses its pid.
func processStart(pid int) string {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
