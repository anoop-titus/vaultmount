package e2e_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/titusmd/vaultmount/internal/config"
	"github.com/titusmd/vaultmount/internal/launchd"
	"github.com/titusmd/vaultmount/internal/mount"
	"github.com/titusmd/vaultmount/internal/tui/components"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newStore(t *testing.T) *config.Store {
	t.Helper()
	s, err := config.NewStoreAt(filepath.Join(t.TempDir(), "connections.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func runMachine(t *testing.T, cfg mount.MachineConfig) []mount.Event {
	t.Helper()
	ch := make(chan mount.Event, 64)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	mount.Run(ctx, cfg, ch)
	close(ch)
	var events []mount.Event
	for e := range ch {
		events = append(events, e)
	}
	return events
}

func lastEvent(events []mount.Event) mount.Event {
	if len(events) == 0 {
		return mount.Event{State: mount.StateIdle}
	}
	return events[len(events)-1]
}

func baseConn() config.Connection {
	return config.Connection{
		ID:         "e2e-test",
		Name:       "E2E Test",
		Host:       "1.2.3.4",
		Port:       22,
		User:       "testuser",
		RemotePath: "/home/testuser/data",
		MountPoint: "/tmp/e2e-testmount",
	}
}

// ── E2E Test 1: Config round-trip ─────────────────────────────────────────────

func TestConfigRoundtrip(t *testing.T) {
	s := newStore(t)

	conns := []config.Connection{
		{ID: "e1", Name: "Server Alpha", Host: "10.0.0.1", Port: 22, User: "admin"},
		{ID: "e2", Name: "Server Beta", Host: "10.0.0.2", Port: 2222, User: "ubuntu"},
		{ID: "e3", Name: "Server Gamma", Host: "10.0.0.3", Port: 22, User: "root"},
	}
	for _, c := range conns {
		if err := s.Save(c); err != nil {
			t.Fatalf("Save %s: %v", c.ID, err)
		}
	}

	list := s.List()
	if len(list) != 3 {
		t.Fatalf("want 3 connections, got %d", len(list))
	}

	// Delete one
	if err := s.Delete("e2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list = s.List()
	if len(list) != 2 {
		t.Fatalf("want 2 after delete, got %d", len(list))
	}
	for _, c := range list {
		if c.ID == "e2" {
			t.Error("deleted connection still present")
		}
	}

	// Get remaining
	got, ok := s.Get("e1")
	if !ok || got.Host != "10.0.0.1" {
		t.Errorf("Get e1: ok=%v host=%s", ok, got.Host)
	}
}

// ── E2E Test 2: State machine happy path ─────────────────────────────────────

func TestStateMachine_HappyPath_E2E(t *testing.T) {
	cfg := mount.MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (mount.MacFUSEStatus, string) { return mount.MacFUSEOK, "ok" },
		RunDiagVPS:     func() (mount.VPSStatus, string) { return mount.VPSOk, "ok" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return true },
	}
	events := runMachine(t, cfg)
	if lastEvent(events).State != mount.StateMounted {
		t.Errorf("want StateMounted, got %s\nevents: %v", lastEvent(events).State, events)
	}
}

// ── E2E Test 3: macFUSE missing ───────────────────────────────────────────────

func TestStateMachine_MacFUSEMissing_E2E(t *testing.T) {
	mountCalled := false
	cfg := mount.MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (mount.MacFUSEStatus, string) { return mount.MacFUSEMissing, "not loaded" },
		RunDiagVPS:     func() (mount.VPSStatus, string) { return mount.VPSOk, "ok" },
		RunMount:       func() error { mountCalled = true; return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := runMachine(t, cfg)
	if lastEvent(events).State != mount.StateFatalError {
		t.Errorf("want StateFatalError, got %s", lastEvent(events).State)
	}
	if mountCalled {
		t.Error("mount must not be called when macFUSE is missing")
	}
}

// ── E2E Test 4: VPS unreachable ───────────────────────────────────────────────

func TestStateMachine_VPSUnreachable_E2E(t *testing.T) {
	cfg := mount.MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (mount.MacFUSEStatus, string) { return mount.MacFUSEOK, "ok" },
		RunDiagVPS:     func() (mount.VPSStatus, string) { return mount.VPSUnreachable, "timeout" },
		RunMount:       func() error { return nil },
		CheckMounted:   func(mp string) bool { return false },
	}
	events := runMachine(t, cfg)
	final := lastEvent(events)
	if final.State != mount.StateError {
		t.Errorf("want StateError for unreachable, got %s", final.State)
	}
}

// ── E2E Test 5: Retry then succeed ────────────────────────────────────────────

func TestStateMachine_RetrySuccess_E2E(t *testing.T) {
	attempt := 0
	cfg := mount.MachineConfig{
		Connection:     baseConn(),
		SSHFSBin:       "/bin/ls",
		MaxRetries:     3,
		RetryDelay:     0,
		RunDiagMacFUSE: func() (mount.MacFUSEStatus, string) { return mount.MacFUSEOK, "ok" },
		RunDiagVPS:     func() (mount.VPSStatus, string) { return mount.VPSOk, "ok" },
		RunMount: func() error {
			attempt++
			if attempt < 3 {
				return errors.New("connection reset")
			}
			return nil
		},
		CheckMounted: func(mp string) bool { return attempt >= 3 },
	}
	events := runMachine(t, cfg)
	if lastEvent(events).State != mount.StateMounted {
		t.Errorf("want StateMounted after retry, got %s", lastEvent(events).State)
	}
	if attempt != 3 {
		t.Errorf("want 3 attempts, got %d", attempt)
	}
}

// ── E2E Test 6: Plist generation ─────────────────────────────────────────────

func TestPlistGeneration_E2E(t *testing.T) {
	cfg := launchd.PlistConfig{
		Label:      "com.vaultmount.e2e-test",
		ScriptPath: "/Users/test/.local/bin/vaultmount-e2e-test.sh",
		Interval:   60,
		LogPath:    "/tmp/vaultmount-e2e.log",
	}
	content, err := launchd.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	checks := []struct {
		name    string
		contains string
	}{
		{"xml declaration", "<?xml"},
		{"plist doctype", "<!DOCTYPE plist"},
		{"label", "com.vaultmount.e2e-test"},
		{"script path", "/Users/test/.local/bin/vaultmount-e2e-test.sh"},
		{"interval", "<integer>60</integer>"},
		{"log path", "/tmp/vaultmount-e2e.log"},
		{"run at load", "<true/>"},
		{"bash shell", "/bin/bash"},
	}
	for _, c := range checks {
		if !strings.Contains(content, c.contains) {
			t.Errorf("plist missing %s (%q)", c.name, c.contains)
		}
	}
}

// ── E2E Test 7: Script generation ────────────────────────────────────────────

func TestScriptGeneration_E2E(t *testing.T) {
	c := config.Connection{
		ID:         "e2e-script",
		Name:       "E2E Script Test",
		Host:       "192.168.1.100",
		Port:       22,
		User:       "ubuntu",
		RemotePath: "/home/ubuntu/data",
		MountPoint: "/Users/test/Vaults/e2e",
		SSHKeyPath: "/Users/test/.ssh/id_ed25519",
	}
	script, err := launchd.GenerateMountScript(c, "/opt/homebrew/bin/sshfs", "/tmp/e2e.log")
	if err != nil {
		t.Fatalf("GenerateMountScript: %v", err)
	}

	checks := []struct {
		name     string
		contains string
	}{
		{"shebang", "#!/usr/bin/env bash"},
		{"host", "192.168.1.100"},
		{"remote path", "/home/ubuntu/data"},
		{"mount point", "/Users/test/Vaults/e2e"},
		{"ssh key", "id_ed25519"},
		{"sshfs binary", "/opt/homebrew/bin/sshfs"},
		{"system_profiler check", "system_profiler"},
		{"no kextstat", "system_profiler"},
		{"reconnect flag", "reconnect"},
		{"mount table check", "mount"},
	}
	for _, c := range checks {
		if !strings.Contains(script, c.contains) {
			t.Errorf("script missing %s (%q)", c.name, c.contains)
		}
	}
	// kextstat must not be called as a command (comments mentioning it are ok)
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue // skip comments
		}
		if strings.Contains(trimmed, "kextstat") {
			t.Errorf("script line uses kextstat as a command (must use system_profiler instead): %q", trimmed)
		}
	}
}

// ── E2E Test 8: SpeedBar rendering ───────────────────────────────────────────

func TestSpeedBarRender_E2E(t *testing.T) {
	cases := []struct {
		bps      float64
		contains string
	}{
		{0, "0 B/s"},
		{1024, "1 KB/s"},
		{1024 * 1024, "1.0 MB/s"},
		{512 * 1024, "512 KB/s"},
		{10 * 1024 * 1024 * 1024, "10.0 GB/s"},
	}
	for _, tc := range cases {
		got := components.FormatSpeed(tc.bps)
		if got != tc.contains {
			t.Errorf("FormatSpeed(%.0f) = %q, want %q", tc.bps, got, tc.contains)
		}
	}

	// Render a bar
	bar := components.SpeedBar{
		Label:    "↑ Upload",
		Current:  1024 * 1024,
		Width:    60,
		IsUpload: true,
	}
	rendered := bar.Render()
	if !strings.Contains(rendered, "Upload") {
		t.Error("rendered bar missing label")
	}
	if !strings.Contains(rendered, "MB/s") {
		t.Error("rendered bar missing speed unit")
	}
}

// ── E2E Test 9: Form validation ───────────────────────────────────────────────

func TestFormValidation_E2E(t *testing.T) {
	fields := []components.FieldDef{
		{Key: "name", Label: "Name", Required: true},
		{Key: "host", Label: "Host", Required: true},
		{Key: "port", Label: "Port", Required: false},
	}
	form := components.NewForm(fields)
	// All required fields empty
	errs := form.Validate()
	if len(errs) < 2 {
		t.Errorf("want at least 2 validation errors for empty required fields, got %d: %v", len(errs), errs)
	}
	// Check error messages reference the field labels
	errStr := strings.Join(errs, " ")
	if !strings.Contains(errStr, "Name") {
		t.Error("validation error should mention Name field")
	}
	if !strings.Contains(errStr, "Host") {
		t.Error("validation error should mention Host field")
	}
}

// ── E2E Test 10: FormatSpeed edge cases ──────────────────────────────────────

func TestFormatSpeed_EdgeCases(t *testing.T) {
	cases := []struct {
		bps  float64
		want string
	}{
		{-1, "0 B/s"},
		{0, "0 B/s"},
		{500, "500 B/s"},
		{999, "999 B/s"},
		{1000, "1000 B/s"},
		{1023, "1023 B/s"},
		{1024, "1 KB/s"},
		{1536, "2 KB/s"},
		{1048576, "1.0 MB/s"},
		{10485760, "10.0 MB/s"},
	}
	for _, tc := range cases {
		got := components.FormatSpeed(tc.bps)
		if got != tc.want {
			t.Errorf("FormatSpeed(%v) = %q, want %q", tc.bps, got, tc.want)
		}
	}
}
