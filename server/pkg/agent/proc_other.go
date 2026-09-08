//go:build !windows

package agent

import (
	"errors"
	"log/slog"
	"os/exec"
	"syscall"
	"time"
)

// hideAgentWindow is a no-op on non-Windows platforms.
func hideAgentWindow(cmd *exec.Cmd) {}

// configureProcessGroup puts the child into its own process group (it becomes
// the group leader, so the group id equals the child pid). This lets the
// daemon signal the entire tree — the agent CLI plus any tool subprocess it
// spawns — in one call, instead of killing only the direct child and leaking
// grandchildren that keep running (and, for opencode, spinning on EPIPE) after
// a task is cancelled or the daemon restarts. See signalProcessGroup.
//
// Called by newRuntimeCmd in launch.go, which is the single point where a
// runtime process is constructed. No backend calls it directly: the group has
// to exist for every runtime process, and per-backend opt-in did not deliver
// that (GH #7522).
func configureProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// startOwnedProcessTree is a plain Start on non-Windows platforms:
// newRuntimeCmd already put the child in its own process group before it
// existed, so there is nothing left to claim once it is running. The logger is
// unused here; Windows needs it to report degraded ownership.
//
// It is still the only way this package starts a long-lived runtime process,
// so the two platforms share one call site per backend.
func startOwnedProcessTree(cmd *exec.Cmd, _ *slog.Logger) error { return cmd.Start() }

// releaseProcessGroup drops ownership of the tree once its leader has been
// reaped. On Windows that closes the Job Object, which kills anything still
// inside it. On Unix a process group needs no handle, but it is NOT gone once
// the leader is: descendants that inherited the group keep running, and no
// caller signals the group after a normal exit — cancellation is the only path
// that does. A headless Chrome the agent opened for a site check and never
// closed survived its task this way and burned 15 cores for 15 hours
// (9 tabs rendered through SwiftShader). Every caller reaches this after
// cmd.Wait (or from a defer that follows it), so SIGKILL the group here to give
// Unix the same "nothing outlives the leader" guarantee the Job Object gives
// Windows. An already-empty group yields ESRCH, which signalProcessGroup
// absorbs; a cmd that never started has no Process and returns early.
//
// A descendant that left the group with setsid is still out of reach (#7615).
func releaseProcessGroup(cmd *exec.Cmd) {
	signalProcessGroup(cmd, syscall.SIGKILL)
}

func codexInitializeRetrySupported() bool { return true }

// signalProcessGroup sends sig to the whole process group led by the command
// (when it was started with configureProcessGroup), falling back to the single
// process if the group send fails. Targeting the group (negative pid) reaches
// the descendants the agent spawned, not just the leader.
func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
		_ = cmd.Process.Signal(sig)
	}
}

func waitProcessGroupGone(cmd *exec.Cmd, timeout time.Duration) bool {
	if cmd == nil || cmd.Process == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Kill(-cmd.Process.Pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
