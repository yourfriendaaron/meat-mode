//go:build linux

package bridge

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// agentProcAttr puts the agent in a process group of its own, so the bridge
// can end it with everything it started, and has the kernel kill it if the
// bridge dies first.
func agentProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// processStart returns when pid started, in clock ticks since boot, to tell
// the agent from a later process that reuses its pid.
func processStart(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return ""
	}
	// The command name may hold spaces and parentheses; the fields after
	// it start at the last ')', with field 3 (state) first.
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(f) < 20 {
		return ""
	}
	return f[19] // field 22, starttime
}
