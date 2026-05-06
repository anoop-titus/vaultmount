package mount

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// MacFUSEStatus is the result of diagnosing the macFUSE driver.
type MacFUSEStatus int

const (
	MacFUSEOK       MacFUSEStatus = iota // loaded, version match
	MacFUSEMismatch                       // loaded, version mismatch — can still mount
	MacFUSEMissing                        // driver not loaded — fatal
)

func (s MacFUSEStatus) String() string {
	switch s {
	case MacFUSEOK:
		return "ok"
	case MacFUSEMismatch:
		return "mismatch"
	default:
		return "missing"
	}
}

// VPSStatus is the result of diagnosing SSH connectivity to the VPS.
type VPSStatus int

const (
	VPSOk          VPSStatus = iota
	VPSUnreachable           // transient — retry next cycle
	VPSAuthFail              // permanent until fixed manually
	VPSHostChanged           // MITM risk — permanent until fixed manually
)

func (s VPSStatus) String() string {
	switch s {
	case VPSOk:
		return "ok"
	case VPSUnreachable:
		return "unreachable"
	case VPSAuthFail:
		return "auth_fail"
	default:
		return "host_changed"
	}
}

// Runner abstracts exec.Command for testing.
type Runner func(name string, args ...string) ([]byte, error)

// DefaultRunner executes real commands.
var DefaultRunner Runner = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// DiagMacFUSE checks whether the macFUSE driver is loaded.
// Uses system_profiler NOT kextstat — kextstat is blind to dexts on macOS arm64.
func DiagMacFUSE() (MacFUSEStatus, string) {
	return diagMacFUSEWith(DefaultRunner)
}

func diagMacFUSEWith(run Runner) (MacFUSEStatus, string) {
	out, err := run("system_profiler", "SPExtensionsDataType")
	if err != nil {
		return MacFUSEMissing, fmt.Sprintf("system_profiler failed: %v", err)
	}
	return parseMacFUSEOutput(string(out))
}

// parseMacFUSEOutput parses system_profiler SPExtensionsDataType output.
// It looks for the macFUSE block and extracts Loaded + Version fields.
func parseMacFUSEOutput(output string) (MacFUSEStatus, string) {
	lines := strings.Split(output, "\n")
	inBlock := false
	loaded := ""
	driverVer := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "macFUSE:") {
			inBlock = true
			continue
		}
		if inBlock {
			if strings.HasPrefix(trimmed, "Version:") {
				driverVer = strings.TrimSpace(strings.TrimPrefix(trimmed, "Version:"))
			}
			if strings.HasPrefix(trimmed, "Loaded:") {
				loaded = strings.TrimSpace(strings.TrimPrefix(trimmed, "Loaded:"))
			}
			// Another top-level key ends the block
			if !strings.HasPrefix(line, " ") && trimmed != "" && !strings.Contains(trimmed, ":") {
				break
			}
			if strings.Contains(trimmed, "Bundle ID:") && loaded != "" {
				break
			}
		}
	}

	if loaded != "Yes" {
		return MacFUSEMissing, fmt.Sprintf("macFUSE driver not loaded (system_profiler: loaded=%q)", loaded)
	}

	binVer := macFUSEBinaryVersion()
	if binVer != "" && driverVer != "" && binVer != driverVer {
		return MacFUSEMismatch, fmt.Sprintf("driver=%s binary=%s — version mismatch, continuing", driverVer, binVer)
	}
	return MacFUSEOK, fmt.Sprintf("driver=%s binary=%s — OK", driverVer, binVer)
}

func macFUSEBinaryVersion() string {
	out, err := exec.Command(
		"/Library/Filesystems/macfuse.fs/Contents/Resources/mount_macfuse",
	).CombinedOutput()
	if err == nil || len(out) > 0 {
		for _, line := range strings.Split(string(out), "\n") {
			for _, part := range strings.Fields(line) {
				if len(part) > 2 && part[0] >= '0' && part[0] <= '9' {
					return part
				}
			}
		}
	}
	return ""
}

// DiagSSHFS returns true if the sshfs binary exists and is executable.
func DiagSSHFS(bin string) bool {
	info, err := os.Stat(bin)
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}

// DiagVPS checks SSH connectivity to the VPS.
func DiagVPS(host, user, keyPath string, port, timeoutSec int) (VPSStatus, string) {
	return diagVPSWith(DefaultRunner, host, user, keyPath, port, timeoutSec)
}

func diagVPSWith(run Runner, host, user, keyPath string, port, timeoutSec int) (VPSStatus, string) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", fmt.Sprintf("ConnectTimeout=%d", timeoutSec),
		"-o", "StrictHostKeyChecking=yes",
		"-p", strconv.Itoa(port),
	}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	args = append(args, fmt.Sprintf("%s@%s", user, host), "exit")

	out, err := run("ssh", args...)
	if err == nil {
		return VPSOk, "SSH OK"
	}

	combined := string(out)
	if strings.Contains(combined, "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED") ||
		strings.Contains(combined, "Host key verification failed") {
		return VPSHostChanged, "host key changed — possible MITM. Fix: ssh-keygen -R " + host
	}
	if strings.Contains(combined, "Permission denied") ||
		strings.Contains(combined, "publickey") ||
		strings.Contains(combined, "authentication") {
		return VPSAuthFail, "SSH auth rejected — check key and authorized_keys on server"
	}
	return VPSUnreachable, fmt.Sprintf("unreachable: %s", strings.TrimSpace(combined))
}

// DiagMount returns true if mountPoint appears in the mount table.
func DiagMount(mountPoint string) bool {
	return diagMountWith(DefaultRunner, mountPoint)
}

func diagMountWith(run Runner, mountPoint string) bool {
	out, err := run("mount")
	if err != nil {
		return false
	}
	return strings.Contains(string(out), mountPoint)
}

// ProbeMountLive does a non-blocking ls on the mount point within a timeout.
// Returns true if the mount is alive and responsive.
func ProbeMountLive(mountPoint string, timeoutSec int) bool {
	type result struct{ ok bool }
	ch := make(chan result, 1)
	go func() {
		_, err := os.ReadDir(mountPoint)
		ch <- result{err == nil}
	}()
	select {
	case r := <-ch:
		return r.ok
	case <-time.After(time.Duration(timeoutSec) * time.Second):
		return false
	}
}
