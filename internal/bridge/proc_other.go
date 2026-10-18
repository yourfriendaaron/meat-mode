//go:build !unix

package bridge

import "syscall"

// The bridge ships for Linux and macOS. Elsewhere it builds, but cannot end
// an agent's process group, so it refuses to start agents (see AgentRunner.start).

func agentProcAttr() *syscall.SysProcAttr { return nil }
func processStart(int) string             { return "" }
func terminateGroup(int) error            { return errNoGroups }
func killGroup(int) error                 { return errNoGroups }
func groupAlive(int) bool                 { return false }
func processAlive(int) bool               { return false }
