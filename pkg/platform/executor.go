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
	"github.com/wizhao/dpu-sim/pkg/log"
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

	// RunCmd executes a command with arguments
	// The level parameter controls output visibility:
	// - If level <= global log level: output streams to stdout/stderr
	// - If level > global log level: output is captured silently (included in error on failure)
	RunCmd(level log.Level, name string, args ...string) error

	// RunCmdInDir executes a command with arguments in a specific working directory.
	// Behaves like RunCmd but sets the working directory before execution.
	RunCmdInDir(level log.Level, dir string, name string, args ...string) error

	// FileExists checks if a file or directory exists on the target system
	FileExists(path string) (bool, error)

	// ReadFile reads the contents of a file on the target system
	ReadFile(path string) ([]byte, error)

	// WriteFile writes content to a file on the target system
	WriteFile(path string, content []byte, mode os.FileMode) error

	// RemoveAll removes a path and any children it contains on the target system
	RemoveAll(path string) error

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

// RunCmd executes a command with arguments
func (e *LocalExecutor) RunCmd(level log.Level, name string, args ...string) error {
	cmd := exec.Command(name, args...)

	// If the requested level is visible, stream to stdout/stderr
	if level <= log.GetLevel() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Otherwise capture output silently
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w\nstdout: %s\nstderr: %s", err, stdoutBuf.String(), stderrBuf.String())
	}
	return nil
}

// RunCmdInDir executes a command with arguments in a specific working directory
func (e *LocalExecutor) RunCmdInDir(level log.Level, dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir

	// If the requested level is visible, stream to stdout/stderr
	if level <= log.GetLevel() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Otherwise capture output silently
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w\nstdout: %s\nstderr: %s", err, stdoutBuf.String(), stderrBuf.String())
	}
	return nil
}

// FileExists checks if a file or directory exists on the local system
func (e *LocalExecutor) FileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ReadFile reads the contents of a file on the local system
func (e *LocalExecutor) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// WriteFile writes content to a file on the local system
func (e *LocalExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	return os.WriteFile(path, content, mode)
}

// RemoveAll removes a path and any children it contains on the local system
func (e *LocalExecutor) RemoveAll(path string) error {
	return os.RemoveAll(path)
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

// RunCmd executes a command with arguments
func (e *SSHExecutor) RunCmd(level log.Level, name string, args ...string) error {
	// Build the command string
	command := name
	for _, arg := range args {
		// Escape single quotes in arguments
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		command += " '" + escaped + "'"
	}

	stdout, stderr, err := e.ExecuteWithTimeout(command, 5*time.Minute)

	// If the requested level is visible, print output
	if level <= log.GetLevel() {
		if stdout != "" {
			fmt.Print(stdout)
		}
		if stderr != "" {
			fmt.Fprint(os.Stderr, stderr)
		}
		return err
	}

	// Otherwise only include output in error
	if err != nil {
		return fmt.Errorf("command failed: %w\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	return nil
}

// RunCmdInDir executes a command with arguments in a specific working directory via SSH
func (e *SSHExecutor) RunCmdInDir(level log.Level, dir string, name string, args ...string) error {
	// Build the command string with cd prefix
	command := fmt.Sprintf("cd '%s' && '%s'", dir, name)
	for _, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\"'\"'")
		command += " '" + escaped + "'"
	}

	stdout, stderr, err := e.ExecuteWithTimeout(command, 5*time.Minute)

	// If the requested level is visible, print output
	if level <= log.GetLevel() {
		if stdout != "" {
			fmt.Print(stdout)
		}
		if stderr != "" {
			fmt.Fprint(os.Stderr, stderr)
		}
		return err
	}

	// Otherwise only include output in error
	if err != nil {
		return fmt.Errorf("command failed: %w\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	return nil
}

// FileExists checks if a file or directory exists on the remote system
func (e *SSHExecutor) FileExists(path string) (bool, error) {
	_, _, err := e.Execute(fmt.Sprintf("test -e '%s'", path))
	if err != nil {
		return false, nil
	}
	return true, nil
}

// ReadFile reads the contents of a file on the remote system
func (e *SSHExecutor) ReadFile(path string) ([]byte, error) {
	stdout, _, err := e.Execute(fmt.Sprintf("cat '%s'", path))
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s via SSH: %w", path, err)
	}
	return []byte(stdout), nil
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

// RemoveAll removes a path and any children it contains on the remote system
func (e *SSHExecutor) RemoveAll(path string) error {
	_, _, err := e.Execute(fmt.Sprintf("rm -rf '%s'", path))
	return err
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

// RunCmd executes a command with arguments inside the container
func (e *DockerExecutor) RunCmd(level log.Level, name string, args ...string) error {
	dockerArgs := []string{"exec", e.containerID, name}
	dockerArgs = append(dockerArgs, args...)

	cmd := exec.Command("docker", dockerArgs...)

	// If the requested level is visible, stream to stdout/stderr
	if level <= log.GetLevel() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Otherwise capture output silently
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w\nstdout: %s\nstderr: %s", err, stdoutBuf.String(), stderrBuf.String())
	}
	return nil
}

// RunCmdInDir executes a command with arguments in a specific working directory inside the container
func (e *DockerExecutor) RunCmdInDir(level log.Level, dir string, name string, args ...string) error {
	// Build the command string with cd prefix
	command := fmt.Sprintf("cd '%s' && '%s'", dir, name)
	for _, arg := range args {
		escaped := strings.ReplaceAll(arg, "'", "'\\''")
		command += " '" + escaped + "'"
	}

	dockerArgs := []string{"exec", e.containerID, "sh", "-c", command}
	cmd := exec.Command("docker", dockerArgs...)

	// If the requested level is visible, stream to stdout/stderr
	if level <= log.GetLevel() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Otherwise capture output silently
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("command failed: %w\nstdout: %s\nstderr: %s", err, stdoutBuf.String(), stderrBuf.String())
	}
	return nil
}

// FileExists checks if a file or directory exists inside the container
func (e *DockerExecutor) FileExists(path string) (bool, error) {
	_, _, err := e.Execute(fmt.Sprintf("test -e '%s'", path))
	if err != nil {
		return false, nil
	}
	return true, nil
}

// ReadFile reads the contents of a file inside the container
func (e *DockerExecutor) ReadFile(path string) ([]byte, error) {
	stdout, _, err := e.Execute(fmt.Sprintf("cat '%s'", path))
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s in container: %w", path, err)
	}
	return []byte(stdout), nil
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

// RemoveAll removes a path and any children it contains inside the container
func (e *DockerExecutor) RemoveAll(path string) error {
	_, _, err := e.Execute(fmt.Sprintf("rm -rf '%s'", path))
	return err
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
