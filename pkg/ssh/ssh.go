// Package ssh provides SSH client utilities for the DPU simulator.
//
// This package handles SSH connections and command execution on VMs,
// matching the functionality of Python's paramiko library used in the
// original implementation.
package ssh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// buildAuthMethods builds SSH authentication methods from configuration.
//
// It supports both private-key and password authentication, in that order.
// This allows key-based login as the default while preserving password
// fallback for environments that do not yet have the shared key installed.
func (c *SSHClient) buildAuthMethods() ([]ssh.AuthMethod, error) {
	auth := []ssh.AuthMethod{}
	var keyErr error

	if c.config.KeyPath != "" {
		key, err := os.ReadFile(c.config.KeyPath)
		if err != nil {
			keyErr = fmt.Errorf("failed to read SSH key: %w", err)
		} else {
			signer, err := ssh.ParsePrivateKey(key)
			if err != nil {
				keyErr = fmt.Errorf("failed to parse SSH key: %w", err)
			} else {
				auth = append(auth, ssh.PublicKeys(signer))
			}
		}
	}

	if c.config.Password != "" {
		auth = append(auth, ssh.Password(c.config.Password))
	}

	if len(auth) == 0 {
		if keyErr != nil {
			return nil, keyErr
		}
		return nil, fmt.Errorf("no usable SSH auth method configured")
	}

	return auth, nil
}

// SSHClient represents an SSH client for executing commands on remote hosts
type SSHClient struct {
	config *config.SSHConfig
}

// NewSSHClient creates a new SSH client
func NewSSHClient(cfg *config.SSHConfig) *SSHClient {
	return &SSHClient{
		config: cfg,
	}
}

// Execute executes a command on a remote host via SSH
func (c *SSHClient) Execute(ip, command string) (stdout, stderr string, err error) {
	return c.ExecuteWithTimeout(ip, command, 10*time.Second)
}

// ExecuteWithTimeout executes a command with a specific timeout
func (c *SSHClient) ExecuteWithTimeout(ip, command string, timeout time.Duration) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return c.ExecuteWithContext(ctx, ip, command)
}

// ExecuteWithContext executes a command with a context for cancellation
func (c *SSHClient) ExecuteWithContext(ctx context.Context, ip, command string) (stdout, stderr string, err error) {
	authMethods, err := c.buildAuthMethods()
	if err != nil {
		return "", "", err
	}

	// Create SSH client config
	sshConfig := &ssh.ClientConfig{
		User:            c.config.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}

	// Connect to SSH server
	addr := fmt.Sprintf("%s:22", ip)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return "", "", fmt.Errorf("failed to dial SSH: %w", err)
	}
	defer client.Close()

	// Create session
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	// Capture stdout and stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	// Execute command with context
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		session.Signal(ssh.SIGKILL)
		return "", "", fmt.Errorf("command timed out: %w", ctx.Err())
	case err := <-done:
		if err != nil {
			return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("command failed: %w", err)
		}
		return stdoutBuf.String(), stderrBuf.String(), nil
	}
}

// WaitForSSH waits for SSH to become available on a host
func (c *SSHClient) WaitForSSH(ip string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for SSH on %s: %w", ip, ctx.Err())
		case <-ticker.C:
			_, _, err := c.ExecuteWithTimeout(ip, "echo test", 10*time.Second)
			if err == nil {
				return nil
			}
			// Continue waiting
		}
	}
}

// BuildSSHCommand builds an SSH command array for subprocess execution
// This is useful for interactive SSH sessions
func BuildSSHCommand(cfg *config.SSHConfig, ip, command string) []string {
	cmd := []string{
		"ssh",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", cfg.User, ip),
	}

	if cfg.KeyPath != "" {
		cmd = append([]string{"ssh", "-i", cfg.KeyPath}, cmd[1:]...)
	}

	if command != "" {
		cmd = append(cmd, command)
	}

	return cmd
}
