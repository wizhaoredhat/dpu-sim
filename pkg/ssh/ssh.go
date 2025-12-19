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

// Client represents an SSH client for executing commands on remote hosts
type Client struct {
	config *config.SSHConfig
}

// NewClient creates a new SSH client
func NewClient(cfg *config.SSHConfig) *Client {
	return &Client{
		config: cfg,
	}
}

// Execute executes a command on a remote host via SSH
func (c *Client) Execute(ip, command string) (stdout, stderr string, err error) {
	return c.ExecuteWithTimeout(ip, command, 10*time.Second)
}

// ExecuteWithTimeout executes a command with a specific timeout
func (c *Client) ExecuteWithTimeout(ip, command string, timeout time.Duration) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return c.ExecuteWithContext(ctx, ip, command)
}

// ExecuteWithContext executes a command with a context for cancellation
func (c *Client) ExecuteWithContext(ctx context.Context, ip, command string) (stdout, stderr string, err error) {
	// Read private key
	key, err := os.ReadFile(c.config.KeyPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to read SSH key: %w", err)
	}

	// Parse private key
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse SSH key: %w", err)
	}

	// Create SSH client config
	sshConfig := &ssh.ClientConfig{
		User: c.config.User,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
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
func (c *Client) WaitForSSH(ip string, timeout time.Duration) error {
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
		"-i", cfg.KeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=5",
		fmt.Sprintf("%s@%s", cfg.User, ip),
	}

	if command != "" {
		cmd = append(cmd, command)
	}

	return cmd
}
