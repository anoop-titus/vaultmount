package ssh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ListKeys returns private key paths found in the given directory.
// Filters for files matching id_* that have a corresponding .pub file.
func ListKeys(sshDir string) ([]string, error) {
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ssh dir: %w", err)
	}
	var keys []string
	pubSet := map[string]bool{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".pub") {
			pubSet[strings.TrimSuffix(e.Name(), ".pub")] = true
		}
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "id_") && !strings.HasSuffix(e.Name(), ".pub") {
			if pubSet[e.Name()] {
				keys = append(keys, filepath.Join(sshDir, e.Name()))
			}
		}
	}
	return keys, nil
}

// GenerateKey creates a new ed25519 SSH key pair at the given path.
func GenerateKey(path, comment string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create ssh dir: %w", err)
	}
	args := []string{
		"-t", "ed25519",
		"-f", path,
		"-N", "", // no passphrase
	}
	if comment != "" {
		args = append(args, "-C", comment)
	}
	out, err := exec.Command("ssh-keygen", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keygen: %w — %s", err, string(out))
	}
	return nil
}

// CopyKey copies a public key to the remote server's authorized_keys.
// Equivalent to ssh-copy-id.
func CopyKey(pubKeyPath, user, host string, port int) error {
	pubData, err := os.ReadFile(pubKeyPath)
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	// Use ssh to append to authorized_keys
	cmd := fmt.Sprintf(`mkdir -p ~/.ssh && chmod 700 ~/.ssh && echo %q >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys`,
		strings.TrimSpace(string(pubData)))
	args := []string{
		"-o", "BatchMode=no",
		"-o", fmt.Sprintf("ConnectTimeout=%d", 10),
		"-p", fmt.Sprintf("%d", port),
		fmt.Sprintf("%s@%s", user, host),
		cmd,
	}
	out, err := exec.Command("ssh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy key: %w — %s", err, string(out))
	}
	return nil
}

// TestConnection tests SSH connectivity and returns success + diagnostic message.
func TestConnection(user, host, keyPath string, port int) (bool, string) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "StrictHostKeyChecking=yes",
		"-p", fmt.Sprintf("%d", port),
	}
	if keyPath != "" {
		args = append(args, "-i", keyPath)
	}
	args = append(args, fmt.Sprintf("%s@%s", user, host), "echo ok")

	out, err := exec.Command("ssh", args...).CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out))
	}
	return true, strings.TrimSpace(string(out))
}

// AddKnownHost runs ssh-keyscan and appends the result to known_hosts.
func AddKnownHost(host string, port int, knownHostsPath string) error {
	if knownHostsPath == "" {
		home, _ := os.UserHomeDir()
		knownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
	}
	if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0o700); err != nil {
		return fmt.Errorf("create .ssh dir: %w", err)
	}

	args := []string{"-p", fmt.Sprintf("%d", port), host}
	out, err := exec.Command("ssh-keyscan", args...).Output()
	if err != nil {
		return fmt.Errorf("ssh-keyscan: %w", err)
	}

	f, err := os.OpenFile(knownHostsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open known_hosts: %w", err)
	}
	defer f.Close()
	_, err = f.Write(out)
	return err
}

// RemoveKnownHost removes a host entry from known_hosts via ssh-keygen -R.
func RemoveKnownHost(host, knownHostsPath string) error {
	if knownHostsPath == "" {
		home, _ := os.UserHomeDir()
		knownHostsPath = filepath.Join(home, ".ssh", "known_hosts")
	}
	args := []string{"-R", host, "-f", knownHostsPath}
	out, err := exec.Command("ssh-keygen", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh-keygen -R: %w — %s", err, string(out))
	}
	return nil
}

// DefaultSSHDir returns ~/.ssh.
func DefaultSSHDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh")
}
