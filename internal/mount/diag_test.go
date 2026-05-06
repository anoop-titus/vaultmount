package mount

import (
	"fmt"
	"testing"
)

// ── parseMacFUSEOutput tests ──────────────────────────────────────────────────

func TestParseMacFUSEOutput_OK(t *testing.T) {
	input := `
Software:

    macFUSE:

      Version: 5.1.3
      Last Modified: 12/23/25, 4:12 PM
      Bundle ID: io.macfuse.filesystems.macfuse.25
      Loaded: Yes
      Architectures: arm64e
`
	status, msg := parseMacFUSEOutput(input)
	// mismatch or ok depending on binary version; at minimum not missing
	if status == MacFUSEMissing {
		t.Errorf("want OK or Mismatch, got Missing; msg=%s", msg)
	}
}

func TestParseMacFUSEOutput_NotLoaded(t *testing.T) {
	input := `
    macFUSE:

      Version: 5.1.3
      Loaded: No
      Bundle ID: io.macfuse.123
`
	status, _ := parseMacFUSEOutput(input)
	if status != MacFUSEMissing {
		t.Errorf("want MacFUSEMissing, got %s", status)
	}
}

func TestParseMacFUSEOutput_Absent(t *testing.T) {
	input := `
    SomeOtherExtension:
      Loaded: Yes
`
	status, _ := parseMacFUSEOutput(input)
	if status != MacFUSEMissing {
		t.Errorf("want MacFUSEMissing when block absent, got %s", status)
	}
}

func TestParseMacFUSEOutput_LoadedYesNoVersion(t *testing.T) {
	input := `
    macFUSE:
      Loaded: Yes
`
	status, _ := parseMacFUSEOutput(input)
	// No version info → ok (can't detect mismatch)
	if status == MacFUSEMissing {
		t.Errorf("want OK when loaded=Yes, got Missing")
	}
}

// ── diagMacFUSEWith tests ─────────────────────────────────────────────────────

func TestDiagMacFUSEWith_Loaded(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte(`
    macFUSE:
      Version: 5.2.0
      Loaded: Yes
      Bundle ID: io.macfuse.25
`), nil
	}
	status, _ := diagMacFUSEWith(mockRun)
	if status == MacFUSEMissing {
		t.Error("want non-missing with loaded=Yes")
	}
}

func TestDiagMacFUSEWith_NotLoaded(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte(`
    macFUSE:
      Loaded: No
`), nil
	}
	status, _ := diagMacFUSEWith(mockRun)
	if status != MacFUSEMissing {
		t.Errorf("want MacFUSEMissing, got %s", status)
	}
}

func TestDiagMacFUSEWith_CommandFails(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("command not found")
	}
	status, _ := diagMacFUSEWith(mockRun)
	if status != MacFUSEMissing {
		t.Errorf("want MacFUSEMissing when command fails, got %s", status)
	}
}

// ── diagVPSWith tests ─────────────────────────────────────────────────────────

func TestDiagVPSWith_OK(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte(""), nil // exit 0
	}
	status, msg := diagVPSWith(mockRun, "host", "user", "", 22, 5)
	if status != VPSOk {
		t.Errorf("want VPSOk, got %s: %s", status, msg)
	}
}

func TestDiagVPSWith_AuthFail(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte("Permission denied (publickey)"), fmt.Errorf("exit 255")
	}
	status, _ := diagVPSWith(mockRun, "host", "user", "", 22, 5)
	if status != VPSAuthFail {
		t.Errorf("want VPSAuthFail, got %s", status)
	}
}

func TestDiagVPSWith_HostChanged(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte("WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED"), fmt.Errorf("exit 255")
	}
	status, _ := diagVPSWith(mockRun, "host", "user", "", 22, 5)
	if status != VPSHostChanged {
		t.Errorf("want VPSHostChanged, got %s", status)
	}
}

func TestDiagVPSWith_Unreachable(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte("ssh: connect to host host port 22: Connection refused"), fmt.Errorf("exit 255")
	}
	status, _ := diagVPSWith(mockRun, "host", "user", "", 22, 5)
	if status != VPSUnreachable {
		t.Errorf("want VPSUnreachable, got %s", status)
	}
}

// ── diagMountWith tests ───────────────────────────────────────────────────────

func TestDiagMountWith_Present(t *testing.T) {
	mp := "/Users/titus/Vaults/testmount"
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte("sshfs@host:/path on " + mp + " (macfuse)\n"), nil
	}
	if !diagMountWith(mockRun, mp) {
		t.Error("want true for present mount")
	}
}

func TestDiagMountWith_Absent(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return []byte("devfs on /dev (devfs)\n"), nil
	}
	if diagMountWith(mockRun, "/not/mounted") {
		t.Error("want false for absent mount")
	}
}

func TestDiagMountWith_CommandFails(t *testing.T) {
	mockRun := func(name string, args ...string) ([]byte, error) {
		return nil, fmt.Errorf("mount failed")
	}
	if diagMountWith(mockRun, "/any") {
		t.Error("want false when command fails")
	}
}

// ── DiagSSHFS tests ───────────────────────────────────────────────────────────

func TestDiagSSHFS_Missing(t *testing.T) {
	if DiagSSHFS("/nonexistent/bin/sshfs") {
		t.Error("want false for missing binary")
	}
}

func TestDiagSSHFS_Present(t *testing.T) {
	// /bin/ls is always present and executable
	if !DiagSSHFS("/bin/ls") {
		t.Error("want true for /bin/ls")
	}
}
