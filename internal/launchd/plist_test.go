package launchd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/titusmd/vaultmount/internal/config"
	"github.com/titusmd/vaultmount/internal/launchd"
)

func TestGenerate_ValidXML(t *testing.T) {
	cfg := launchd.PlistConfig{
		Label:      "com.vaultmount.test",
		ScriptPath: "/Users/test/.local/bin/vaultmount-test.sh",
		Interval:   60,
		LogPath:    "/tmp/vaultmount-test.log",
	}
	content, err := launchd.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(content, "<?xml") {
		t.Error("missing XML declaration")
	}
	if !strings.Contains(content, "com.vaultmount.test") {
		t.Error("label not in plist")
	}
	if !strings.Contains(content, cfg.ScriptPath) {
		t.Error("script path not in plist")
	}
	if !strings.Contains(content, "<integer>60</integer>") {
		t.Error("interval not in plist")
	}
	if !strings.Contains(content, cfg.LogPath) {
		t.Error("log path not in plist")
	}
	if !strings.Contains(content, "<true/>") {
		t.Error("RunAtLoad not set")
	}
}

func TestGenerate_DefaultInterval(t *testing.T) {
	cfg := launchd.PlistConfig{
		Label:      "com.test",
		ScriptPath: "/bin/sh",
		LogPath:    "/tmp/test.log",
	}
	content, err := launchd.Generate(cfg)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.Contains(content, "<integer>60</integer>") {
		t.Error("default interval should be 60")
	}
}

func TestGenerateMountScript_ContainsSSHFS(t *testing.T) {
	c := config.Connection{
		ID:         "test123",
		Name:       "Test Server",
		Host:       "1.2.3.4",
		Port:       22,
		User:       "admin",
		RemotePath: "/home/admin/data",
		MountPoint: "/Users/titus/Vaults/test",
		SSHKeyPath: "/Users/titus/.ssh/id_ed25519",
	}
	script, err := launchd.GenerateMountScript(c, "/opt/homebrew/bin/sshfs", "/tmp/test.log")
	if err != nil {
		t.Fatalf("GenerateMountScript: %v", err)
	}
	if !strings.Contains(script, "sshfs") {
		t.Error("script missing sshfs command")
	}
	if !strings.Contains(script, "1.2.3.4") {
		t.Error("script missing host")
	}
	if !strings.Contains(script, "/home/admin/data") {
		t.Error("script missing remote path")
	}
	if !strings.Contains(script, "/Users/titus/Vaults/test") {
		t.Error("script missing mount point")
	}
	if !strings.Contains(script, "system_profiler") {
		t.Error("script must use system_profiler for macFUSE check")
	}
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "kextstat") {
			t.Errorf("script uses kextstat as command: %q", trimmed)
		}
	}
}

func TestGenerateMountScript_SSHKey(t *testing.T) {
	c := config.Connection{
		ID:         "k1",
		Name:       "Key Test",
		Host:       "host",
		Port:       22,
		User:       "u",
		RemotePath: "/r",
		MountPoint: "/m",
		SSHKeyPath: "/home/u/.ssh/id_ed25519",
	}
	script, err := launchd.GenerateMountScript(c, "/opt/homebrew/bin/sshfs", "/tmp/k.log")
	if err != nil {
		t.Fatalf("GenerateMountScript: %v", err)
	}
	if !strings.Contains(script, "id_ed25519") {
		t.Error("script missing SSH key path")
	}
}

func TestGenerateMountScript_Compression(t *testing.T) {
	c := config.Connection{
		ID:          "c1",
		Name:        "Comp Test",
		Host:        "host",
		Port:        22,
		User:        "u",
		RemotePath:  "/r",
		MountPoint:  "/m",
		Compression: true,
	}
	script, err := launchd.GenerateMountScript(c, "/opt/homebrew/bin/sshfs", "/tmp/c.log")
	if err != nil {
		t.Fatalf("GenerateMountScript: %v", err)
	}
	if !strings.Contains(script, "Compression=yes") {
		t.Error("script missing Compression=yes")
	}
}

func TestInstallTo_WritesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := launchd.PlistConfig{
		Label:      "com.vaultmount.install-test",
		ScriptPath: "/bin/sh",
		Interval:   30,
		LogPath:    filepath.Join(t.TempDir(), "test.log"),
	}
	if err := launchd.InstallTo(dir, cfg); err != nil {
		t.Fatalf("InstallTo: %v", err)
	}
	plistPath := filepath.Join(dir, "com.vaultmount.install-test.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		t.Fatalf("plist not written: %v", err)
	}
	if !strings.Contains(string(data), "com.vaultmount.install-test") {
		t.Error("plist content missing label")
	}
}

func TestUninstallFrom_RemovesFile(t *testing.T) {
	dir := t.TempDir()
	cfg := launchd.PlistConfig{
		Label:      "com.vaultmount.uninstall-test",
		ScriptPath: "/bin/sh",
		Interval:   60,
		LogPath:    filepath.Join(t.TempDir(), "test.log"),
	}
	launchd.InstallTo(dir, cfg)

	if err := launchd.UninstallFrom(dir, "com.vaultmount.uninstall-test"); err != nil {
		t.Fatalf("UninstallFrom: %v", err)
	}
	plistPath := filepath.Join(dir, "com.vaultmount.uninstall-test.plist")
	if _, err := os.Stat(plistPath); !os.IsNotExist(err) {
		t.Error("plist file should be removed")
	}
}

func TestUninstallFrom_MissingIsOK(t *testing.T) {
	dir := t.TempDir()
	if err := launchd.UninstallFrom(dir, "com.vaultmount.doesnotexist"); err != nil {
		t.Errorf("UninstallFrom missing: want nil, got %v", err)
	}
}
