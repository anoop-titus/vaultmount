package mount

import (
	"context"
	"fmt"
	"time"

	"github.com/titusmd/vaultmount/internal/config"
)

// State represents the current state of a mount operation.
type State int

const (
	StateIdle       State = iota
	StateDiagnosing       // running prerequisite checks
	StateMounting         // attempting sshfs mount
	StateMounted          // successfully mounted
	StateRetrying         // transient failure, will retry
	StateError            // retriable error (e.g. VPS unreachable)
	StateFatalError       // permanent error, needs manual intervention
)

func (s State) String() string {
	return [...]string{
		"idle", "diagnosing", "mounting", "mounted",
		"retrying", "error", "fatal",
	}[s]
}

// Event is emitted by the state machine on each transition.
type Event struct {
	State   State
	Message string
	Attempt int
}

// MachineConfig holds injectable dependencies for testing.
type MachineConfig struct {
	Connection config.Connection
	SSHFSBin   string
	MaxRetries int
	RetryDelay time.Duration

	// Injected runners (use real implementations when nil)
	RunDiagMacFUSE func() (MacFUSEStatus, string)
	RunDiagVPS     func() (VPSStatus, string)
	RunMount       func() error
	CheckMounted   func(mp string) bool
}

func (c *MachineConfig) defaults() {
	if c.MaxRetries == 0 {
		c.MaxRetries = 3
	}
	// Only apply default delay when no test runners are injected (production mode).
	// Test callers set runners explicitly and control RetryDelay themselves.
	if c.RetryDelay == 0 && c.RunDiagMacFUSE == nil {
		c.RetryDelay = 5 * time.Second
	}
	if c.SSHFSBin == "" {
		c.SSHFSBin = "/opt/homebrew/bin/sshfs"
	}
	if c.RunDiagMacFUSE == nil {
		c.RunDiagMacFUSE = DiagMacFUSE
	}
	if c.RunDiagVPS == nil {
		conn := c.Connection
		c.RunDiagVPS = func() (VPSStatus, string) {
			return DiagVPS(conn.Host, conn.User, conn.SSHKeyPath, conn.Port, 5)
		}
	}
	if c.CheckMounted == nil {
		c.CheckMounted = DiagMount
	}
	if c.RunMount == nil {
		conn := c.Connection
		bin := c.SSHFSBin
		c.RunMount = func() error {
			return runSSHFS(bin, conn)
		}
	}
}

// Run executes the deterministic mount state machine.
// Events are sent to the events channel. Blocks until mounted, fatal error, or ctx done.
func Run(ctx context.Context, cfg MachineConfig, events chan<- Event) {
	cfg.defaults()
	emit := func(s State, msg string, attempt int) {
		select {
		case events <- Event{State: s, Message: msg, Attempt: attempt}:
		case <-ctx.Done():
		}
	}

	emit(StateDiagnosing, "checking macFUSE driver", 0)

	// Step 1: macFUSE check
	mfStatus, mfMsg := cfg.RunDiagMacFUSE()
	switch mfStatus {
	case MacFUSEMissing:
		emit(StateFatalError, fmt.Sprintf("macFUSE not loaded: %s", mfMsg), 0)
		return
	case MacFUSEMismatch:
		emit(StateDiagnosing, fmt.Sprintf("macFUSE version mismatch (%s) — continuing", mfMsg), 0)
	case MacFUSEOK:
		emit(StateDiagnosing, fmt.Sprintf("macFUSE OK (%s)", mfMsg), 0)
	}

	// Step 2: sshfs binary check
	if !DiagSSHFS(cfg.SSHFSBin) {
		emit(StateFatalError, fmt.Sprintf("sshfs binary missing at %s", cfg.SSHFSBin), 0)
		return
	}

	// Step 3: VPS connectivity
	emit(StateDiagnosing, "checking VPS connectivity", 0)
	vpsStatus, vpsMsg := cfg.RunDiagVPS()
	switch vpsStatus {
	case VPSHostChanged, VPSAuthFail:
		emit(StateFatalError, fmt.Sprintf("SSH fatal: %s", vpsMsg), 0)
		return
	case VPSUnreachable:
		emit(StateError, fmt.Sprintf("VPS unreachable: %s — will retry next cycle", vpsMsg), 0)
		return
	case VPSOk:
		emit(StateDiagnosing, "VPS SSH OK", 0)
	}

	// Step 4: mount with retries
	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		emit(StateMounting, fmt.Sprintf("mounting (attempt %d/%d)", attempt, cfg.MaxRetries), attempt)

		if err := cfg.RunMount(); err != nil {
			// sshfs itself errored
			if attempt < cfg.MaxRetries {
				emit(StateRetrying, fmt.Sprintf("sshfs error: %v — retrying in %s", err, cfg.RetryDelay), attempt)
				select {
				case <-time.After(cfg.RetryDelay):
				case <-ctx.Done():
					return
				}
				continue
			}
			emit(StateError, fmt.Sprintf("all %d mount attempts failed: %v", cfg.MaxRetries, err), attempt)
			return
		}

		// sshfs exits 0 — check mount table (sshfs can lie on macFUSE failure)
		time.Sleep(500 * time.Millisecond)
		if cfg.CheckMounted(cfg.Connection.MountPoint) {
			emit(StateMounted, fmt.Sprintf("mounted at %s", cfg.Connection.MountPoint), attempt)
			return
		}

		// sshfs exited 0 but not in mount table → macFUSE driver failure
		emit(StateFatalError, "sshfs exited 0 but mount not in table — macFUSE driver failure (reboot may fix)", attempt)
		return
	}
}

// runSSHFS executes the sshfs command for real.
func runSSHFS(bin string, c config.Connection) error {
	args := []string{
		fmt.Sprintf("%s@%s:%s", c.User, c.Host, c.RemotePath),
		c.MountPoint,
		"-o", "reconnect",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=3",
		"-o", "follow_symlinks",
		"-o", fmt.Sprintf("volname=%s", c.Name),
		"-o", "auto_cache",
		"-p", fmt.Sprintf("%d", c.Port),
	}
	if c.SSHKeyPath != "" {
		args = append(args, "-o", fmt.Sprintf("IdentityFile=%s", c.SSHKeyPath))
	}
	if c.Compression {
		args = append(args, "-o", "Compression=yes")
	} else {
		args = append(args, "-o", "Compression=no")
	}
	out, err := DefaultRunner(bin, args...)
	if err != nil {
		return fmt.Errorf("sshfs: %w — %s", err, string(out))
	}
	return nil
}
