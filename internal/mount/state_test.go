package mount

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/titusmd/vaultmount/internal/config"
)

// collect drains all events from the state machine into a slice.
func collect(t *testing.T, cfg MachineConfig) []Event {
	t.Helper()
	ch := make(chan Event, 32)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	Run(ctx, cfg, ch)
	close(ch)
	var events []Event
	for e := range ch {
		events = append(events, e)
	}
	return events
}

func lastState(events []Event) State {
	if len(events) == 0 {
		return StateIdle
	}
	return events[len(events)-1].State
}

func baseConn() config.Connection {
	return config.Connection{
		ID:         "test",
		Name:       "Test",
		Host:       "1.2.3.4",
		Port:       22,
		User:       "user",
		RemotePath: "/home/user",
		MountPoint: "/tmp/testmount",
	}
}

// ── Happy path ────────────────────────────────────────────────────────────────

func TestStateMachine_HappyPath(t *testing.T) {
	cfg := MachineConfig{
		Connection: baseConn(),
		SSHFSBin:   "/bin/ls", // exists + executable
		MaxRetries: 3,
		RetryDelay: 0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return true },
	}
	events := collect(t, cfg)
	if lastState(events) != StateMounted {
		t.Errorf("want StateMounted, got %s\nevents: %v", lastState(events), events)
	}
}

// ── macFUSE missing ───────────────────────────────────────────────────────────

func TestStateMachine_MacFUSEMissing(t *testing.T) {
	mounted := false
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEMissing, "not loaded" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount:       func() error { mounted = true; return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := collect(t, cfg)
	if lastState(events) != StateFatalError {
		t.Errorf("want StateFatalError, got %s", lastState(events))
	}
	if mounted {
		t.Error("mount should not be attempted when macFUSE is missing")
	}
}

// ── macFUSE mismatch still mounts ────────────────────────────────────────────

func TestStateMachine_MacFUSEMismatch_StillMounts(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEMismatch, "5.1.3 vs 5.2.0" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return true },
	}
	events := collect(t, cfg)
	if lastState(events) != StateMounted {
		t.Errorf("want StateMounted despite mismatch, got %s", lastState(events))
	}
}

// ── sshfs binary missing ──────────────────────────────────────────────────────

func TestStateMachine_SSHFSMissing(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/nonexistent/sshfs",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := collect(t, cfg)
	if lastState(events) != StateFatalError {
		t.Errorf("want StateFatalError, got %s", lastState(events))
	}
}

// ── VPS unreachable ───────────────────────────────────────────────────────────

func TestStateMachine_VPSUnreachable(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSUnreachable, "connection refused" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := collect(t, cfg)
	if lastState(events) != StateError {
		t.Errorf("want StateError for unreachable, got %s", lastState(events))
	}
}

// ── VPS auth fail → fatal ─────────────────────────────────────────────────────

func TestStateMachine_VPSAuthFail(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSAuthFail, "permission denied" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := collect(t, cfg)
	if lastState(events) != StateFatalError {
		t.Errorf("want StateFatalError for auth fail, got %s", lastState(events))
	}
}

// ── VPS host changed → fatal ──────────────────────────────────────────────────

func TestStateMachine_VPSHostChanged(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSHostChanged, "key changed" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := collect(t, cfg)
	if lastState(events) != StateFatalError {
		t.Errorf("want StateFatalError for host changed, got %s", lastState(events))
	}
}

// ── sshfs fails then succeeds (retry) ────────────────────────────────────────

func TestStateMachine_RetrySuccess(t *testing.T) {
	call := 0
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount: func() error {
			call++
			if call < 3 {
				return errors.New("connection reset")
			}
			return nil
		},
		CheckMounted: func(mp string) bool { return call >= 3 },
	}
	events := collect(t, cfg)
	if lastState(events) != StateMounted {
		t.Errorf("want StateMounted after retry, got %s\nevents: %v", lastState(events), events)
	}
}

// ── all retries exhausted ─────────────────────────────────────────────────────

func TestStateMachine_AllRetriesExhausted(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount:       func() error { return errors.New("permanent failure") },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := collect(t, cfg)
	if lastState(events) != StateError {
		t.Errorf("want StateError when all retries fail, got %s", lastState(events))
	}
}

// ── macFUSE driver failure (sshfs exits 0 but not in mount table) ─────────────

func TestStateMachine_FuseDriverFailure(t *testing.T) {
	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount:       func() error { return nil }, // sshfs exits 0
		CheckMounted:   func(mp string) bool { return false }, // but not mounted
	}
	events := collect(t, cfg)
	if lastState(events) != StateFatalError {
		t.Errorf("want StateFatalError for FUSE driver failure, got %s", lastState(events))
	}
}

// ── context cancellation ──────────────────────────────────────────────────────

func TestStateMachine_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Event, 32)

	cfg := MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     5,
		RetryDelay:     100 * time.Millisecond,
		RunDiagMacFUSE: func() (MacFUSEStatus, string) { return MacFUSEOK, "ok" },
		RunDiagVPS:     func() (VPSStatus, string) { return VPSOk, "ok" },
		RunMount: func() error {
			cancel() // cancel during first mount attempt
			return errors.New("fail")
		},
		CheckMounted: func(mp string) bool { return false },
	}

	done := make(chan struct{})
	go func() {
		Run(ctx, cfg, ch)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("state machine did not exit after context cancel")
	}
}

// ── state string representations ─────────────────────────────────────────────

func TestStateString(t *testing.T) {
	cases := []struct {
		s    State
		want string
	}{
		{StateIdle, "idle"},
		{StateDiagnosing, "diagnosing"},
		{StateMounting, "mounting"},
		{StateMounted, "mounted"},
		{StateRetrying, "retrying"},
		{StateError, "error"},
		{StateFatalError, "fatal"},
	}
	for _, tc := range cases {
		if tc.s.String() != tc.want {
			t.Errorf("State(%d).String() = %q, want %q", tc.s, tc.s.String(), tc.want)
		}
	}
}
