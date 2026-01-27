// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/ssh"
)

// CommandExecutor is an interface for executing commands on different platforms.
// Implementations allow the same installation logic to work across:
// - Local execution (host machine)
// - SSH execution (remote VMs/baremetal)
// - Docker execution (Kind containers)
type CommandExecutor interface {
	// WaitUntilReady waits until the executor is ready to execute commands
	WaitUntilReady(timeout time.Duration) error

	// Execute runs a command and returns stdout, stderr, and error
	Execute(command string) (stdout, stderr string, err error)

	// ExecuteWithTimeout runs a command with a specific timeout
	ExecuteWithTimeout(command string, timeout time.Duration) (stdout, stderr string, err error)

	// RunCmd executes a command with arguments, streaming output to stdout/stderr
	// This is suitable for interactive commands or commands that need visible output
	RunCmd(name string, args ...string) error

	// WriteFile writes content to a file on the target system
	WriteFile(path string, content []byte, mode os.FileMode) error

	// GetDistro returns the Linux distribution information for this executor's target
	GetDistro() (*Distro, error)

	// GetArchitecture returns the architecture of the target system
	GetArchitecture() (Architecture, error)

	// String returns a description of this executor (for logging)
	String() string
}

// LocalExecutor executes commands on the local machine
type LocalExecutor struct {
	cachedDistro *Distro
}

// NewLocalExecutor creates a new LocalExecutor
func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

// WaitUntilReady does nothing for local execution
func (e *LocalExecutor) WaitUntilReady(timeout time.Duration) error {
	return nil
}

// Execute runs a command locally
func (e *LocalExecutor) Execute(command string) (stdout, stderr string, err error) {
	return e.ExecuteWithTimeout(command, 30*time.Second)
}

// ExecuteWithTimeout runs a command with a specific timeout
func (e *LocalExecutor) ExecuteWithTimeout(command string, timeout time.Duration) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// RunCmd executes a command with arguments, streaming output to stdout/stderr
func (e *LocalExecutor) RunCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// WriteFile writes content to a file on the local system
func (e *LocalExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	return os.WriteFile(path, content, mode)
}

// GetDistro returns the Linux distribution information for the local machine
func (e *LocalExecutor) GetDistro() (*Distro, error) {
	if e.cachedDistro != nil {
		return e.cachedDistro, nil
	}
	distro, err := DetectDistro(e)
	if err != nil {
		return nil, err
	}
	e.cachedDistro = distro
	return distro, nil
}

// GetArchitecture returns the architecture of the local system
func (e *LocalExecutor) GetArchitecture() (Architecture, error) {
	return DetectArchitecture(e)
}

// String returns a description of this executor
func (e *LocalExecutor) String() string {
	return "local"
}

// SSHExecutor executes commands on a remote machine via SSH
type SSHExecutor struct {
	client       *ssh.SSHClient
	config       *config.SSHConfig
	ip           string
	cachedDistro *Distro
}

// NewSSHExecutor creates a new SSHExecutor for a specific remote host
func NewSSHExecutor(cfg *config.SSHConfig, ip string) *SSHExecutor {
	return &SSHExecutor{
		client: ssh.NewSSHClient(cfg),
		config: cfg,
		ip:     ip,
	}
}

// WaitUntilReady waits until the SSH executor is ready to execute commands
func (e *SSHExecutor) WaitUntilReady(timeout time.Duration) error {
	return e.client.WaitForSSH(e.ip, timeout)
}

// Execute runs a command on the remote host
func (e *SSHExecutor) Execute(command string) (stdout, stderr string, err error) {
	return e.ExecuteWithTimeout(command, 30*time.Second)
}

// ExecuteWithTimeout runs a command with a specific timeout
func (e *SSHExecutor) ExecuteWithTimeout(command string, timeout time.Duration) (stdout, stderr string, err error) {
	return e.client.ExecuteWithTimeout(e.ip, command, timeout)
}

// RunCmd executes a command with arguments, streaming output to stdout/stderr
func (e *SSHExecutor) RunCmd(name string, args ...string) error {
	// Build the command string
	command := name
	for _, arg := range args {
		// Escape single quotes in arguments
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		command += " '" + escaped + "'"
	}

	stdout, stderr, err := e.ExecuteWithTimeout(command, 5*time.Minute)
	if stdout != "" {
		fmt.Print(stdout)
	}
	if stderr != "" {
		fmt.Fprint(os.Stderr, stderr)
	}
	return err
}

// WriteFile writes content to a file on the remote system
func (e *SSHExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	// Use heredoc to write file content
	// This handles binary and multiline content safely
	encodedContent := strings.ReplaceAll(string(content), "'", "'\"'\"'")
	command := fmt.Sprintf("cat > '%s' << 'EOF'\n%s\nEOF\nchmod %o '%s'",
		path, encodedContent, mode, path)

	_, _, err := e.ExecuteWithTimeout(command, 30*time.Second)
	if err != nil {
		return fmt.Errorf("failed to write file via SSH: %w", err)
	}

	return nil
}

// GetDistro returns the Linux distribution information for the remote machine
func (e *SSHExecutor) GetDistro() (*Distro, error) {
	if e.cachedDistro != nil {
		return e.cachedDistro, nil
	}
	distro, err := DetectDistro(e)
	if err != nil {
		return nil, err
	}
	e.cachedDistro = distro
	return distro, nil
}

// GetArchitecture returns the architecture of the remote system
func (e *SSHExecutor) GetArchitecture() (Architecture, error) {
	return DetectArchitecture(e)
}

// String returns a description of this executor
func (e *SSHExecutor) String() string {
	return fmt.Sprintf("ssh://%s@%s", e.config.User, e.ip)
}

// DockerExecutor executes commands inside a Docker container
type DockerExecutor struct {
	containerID  string
	cachedDistro *Distro
}

// NewDockerExecutor creates a new DockerExecutor for a specific container
func NewDockerExecutor(containerID string) *DockerExecutor {
	return &DockerExecutor{
		containerID: containerID,
	}
}

// WaitUntilReady waits until the Docker executor is ready to execute commands
func (e *DockerExecutor) WaitUntilReady(timeout time.Duration) error {
	return nil
}

// Execute runs a command inside the container
func (e *DockerExecutor) Execute(command string) (stdout, stderr string, err error) {
	return e.ExecuteWithTimeout(command, 30*time.Second)
}

// ExecuteWithTimeout runs a command with a specific timeout
func (e *DockerExecutor) ExecuteWithTimeout(command string, timeout time.Duration) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", e.containerID, "sh", "-c", command)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err = cmd.Run()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// RunCmd executes a command with arguments inside the container, streaming output
func (e *DockerExecutor) RunCmd(name string, args ...string) error {
	dockerArgs := []string{"exec", e.containerID, name}
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// WriteFile writes content to a file inside the container
func (e *DockerExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	// Use docker cp via stdin
	cmd := exec.Command("docker", "exec", "-i", e.containerID, "sh", "-c",
		fmt.Sprintf("cat > '%s' && chmod %o '%s'", path, mode, path))

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker exec: %w", err)
	}

	if _, err := io.Copy(stdin, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}
	stdin.Close()

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("docker exec failed: %w", err)
	}

	return nil
}

// GetDistro returns the Linux distribution information for the container
func (e *DockerExecutor) GetDistro() (*Distro, error) {
	if e.cachedDistro != nil {
		return e.cachedDistro, nil
	}
	distro, err := DetectDistro(e)
	if err != nil {
		return nil, err
	}
	e.cachedDistro = distro
	return distro, nil
}

// GetArchitecture returns the architecture of the container
func (e *DockerExecutor) GetArchitecture() (Architecture, error) {
	return DetectArchitecture(e)
}

// String returns a description of this executor
func (e *DockerExecutor) String() string {
	return fmt.Sprintf("docker://%s", e.containerID)
}
