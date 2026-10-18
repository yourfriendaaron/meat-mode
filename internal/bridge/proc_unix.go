//go:build unix

package bridge

import (
	"errors"
	"syscall"
)

// signalGroup sends sig to the process group pgid. A group that is already
// gone is not an error.
func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 1 {
		return errors.New("no process group")
	}
	err := syscall.Kill(-pgid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

// terminateGroup asks the group to end (SIGTERM).
func terminateGroup(pgid int) error { return signalGroup(pgid, syscall.SIGTERM) }

// killGroup ends the group at once (SIGKILL).
func killGroup(pgid int) error { return signalGroup(pgid, syscall.SIGKILL) }

// groupAlive reports whether any process is left in the group pgid.
func groupAlive(pgid int) bool {
	if pgid <= 1 {
		return false
	}
	err := syscall.Kill(-pgid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// processAlive reports whether the process pid exists.
func processAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
