package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wizhao/dpu-sim/pkg/log"
)

func TestLocalExecutor_Execute(t *testing.T) {
	cmdExec := NewLocalExecutor()

	tests := []struct {
		name       string
		command    string
		wantStdout string
		wantErr    bool
	}{
		{
			name:       "echo command",
			command:    "echo hello",
			wantStdout: "hello\n",
			wantErr:    false,
		},
		{
			name:       "command with pipe",
			command:    "echo hello world | tr ' ' '-'",
			wantStdout: "hello-world\n",
			wantErr:    false,
		},
		{
			name:    "failing command",
			command: "false",
			wantErr: true,
		},
		{
			name:    "non-existent command",
			command: "nonexistentcommand12345",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, _, err := cmdExec.Execute(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && stdout != tt.wantStdout {
				t.Errorf("Execute() stdout = %q, want %q", stdout, tt.wantStdout)
			}
		})
	}
}

func TestLocalExecutor_ExecuteWithTimeout(t *testing.T) {
	cmdExec := NewLocalExecutor()

	t.Run("command completes within timeout", func(t *testing.T) {
		stdout, _, err := cmdExec.ExecuteWithTimeout("echo test", 5*time.Second)
		if err != nil {
			t.Errorf("ExecuteWithTimeout() unexpected error: %v", err)
		}
		if strings.TrimSpace(stdout) != "test" {
			t.Errorf("ExecuteWithTimeout() stdout = %q, want %q", stdout, "test")
		}
	})

	t.Run("command times out", func(t *testing.T) {
		_, _, err := cmdExec.ExecuteWithTimeout("sleep 10", 100*time.Millisecond)
		if err == nil {
			t.Error("ExecuteWithTimeout() expected timeout error, got nil")
		}
	})
}

func TestLocalExecutor_RunCmd(t *testing.T) {
	cmdExec := NewLocalExecutor()

	t.Run("successful command", func(t *testing.T) {
		err := cmdExec.RunCmd(log.LevelInfo, "true")
		if err != nil {
			t.Errorf("RunCmd() unexpected error: %v", err)
		}
	})

	t.Run("failing command", func(t *testing.T) {
		err := cmdExec.RunCmd(log.LevelInfo, "false")
		if err == nil {
			t.Error("RunCmd() expected error, got nil")
		}
	})

	t.Run("command with arguments", func(t *testing.T) {
		err := cmdExec.RunCmd(log.LevelInfo, "test", "-d", "/tmp")
		if err != nil {
			t.Errorf("RunCmd() unexpected error: %v", err)
		}
	})

	t.Run("debug level command silent at info level", func(t *testing.T) {
		// Ensure log level is Info (default)
		log.SetLevel(log.LevelInfo)
		err := cmdExec.RunCmd(log.LevelDebug, "echo", "this should be silent")
		if err != nil {
			t.Errorf("RunCmd() unexpected error: %v", err)
		}
	})
}

func TestLocalExecutor_WriteFile(t *testing.T) {
	cmdExec := NewLocalExecutor()

	t.Run("write and verify file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		content := []byte("hello world")

		err := cmdExec.WriteFile(filePath, content, 0644)
		if err != nil {
			t.Fatalf("WriteFile() unexpected error: %v", err)
		}

		// Verify file content
		got, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(got) != string(content) {
			t.Errorf("WriteFile() content = %q, want %q", string(got), string(content))
		}

		// Verify file permissions
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Failed to stat file: %v", err)
		}
		// Note: on some systems, umask may affect permissions
		if info.Mode().Perm()&0600 != 0600 {
			t.Errorf("WriteFile() permissions = %o, want at least 0600", info.Mode().Perm())
		}
	})
}

func TestLocalExecutor_GetDistro(t *testing.T) {
	cmdExec := NewLocalExecutor()

	distro, err := cmdExec.GetDistro()
	if err != nil {
		t.Fatalf("GetDistro() unexpected error: %v", err)
	}

	// Basic validation - should have an ID
	if distro.ID == "" {
		t.Error("GetDistro() returned empty ID")
	}

	// Should have a package manager
	if distro.PackageManager == "" {
		t.Error("GetDistro() returned empty PackageManager")
	}

	// Should have an architecture
	if distro.Architecture == "" {
		t.Error("GetDistro() returned empty Architecture")
	}

	// Verify caching works
	distro2, err := cmdExec.GetDistro()
	if err != nil {
		t.Fatalf("GetDistro() second call unexpected error: %v", err)
	}
	if distro != distro2 {
		t.Error("GetDistro() should return cached distro")
	}
}

func TestLocalExecutor_GetArchitecture(t *testing.T) {
	cmdExec := NewLocalExecutor()

	arch, err := cmdExec.GetArchitecture()
	if err != nil {
		t.Fatalf("GetArchitecture() unexpected error: %v", err)
	}

	// Should be a known architecture or at least not empty
	if arch == "" {
		t.Error("GetArchitecture() returned empty architecture")
	}

	// Common architectures
	knownArchs := []Architecture{X86_64, AARCH64}
	found := false
	for _, known := range knownArchs {
		if arch == known {
			found = true
			break
		}
	}
	if !found {
		t.Logf("GetArchitecture() returned unknown architecture: %s", arch)
	}
}

func TestLocalExecutor_String(t *testing.T) {
	cmdExec := NewLocalExecutor()
	if cmdExec.String() != "local" {
		t.Errorf("String() = %q, want %q", cmdExec.String(), "local")
	}
}

func TestDockerExecutor_String(t *testing.T) {
	cmdExec := NewDockerExecutor("test-container")
	want := "docker://test-container"
	if cmdExec.String() != want {
		t.Errorf("String() = %q, want %q", cmdExec.String(), want)
	}
}

// MockExecutor implements CommandExecutor for testing dependency installation
type MockExecutor struct {
	Commands       []string // Records all commands executed
	Files          map[string][]byte
	distro         *Distro
	architecture   Architecture
	ShouldFail     bool
	FailOnCommands map[string]bool
}

func NewMockExecutor(distro *Distro) *MockExecutor {
	return &MockExecutor{
		Commands:       make([]string, 0),
		Files:          make(map[string][]byte),
		distro:         distro,
		architecture:   X86_64,
		FailOnCommands: make(map[string]bool),
	}
}

func (e *MockExecutor) Execute(command string) (stdout, stderr string, err error) {
	return e.ExecuteWithTimeout(command, 30*time.Second)
}

func (e *MockExecutor) ExecuteWithTimeout(command string, timeout time.Duration) (stdout, stderr string, err error) {
	e.Commands = append(e.Commands, command)
	if e.ShouldFail || e.FailOnCommands[command] {
		return "", "mock error", os.ErrNotExist
	}
	return "mock output", "", nil
}

func (e *MockExecutor) RunCmd(level log.Level, name string, args ...string) error {
	cmd := name
	for _, arg := range args {
		cmd += " " + arg
	}
	e.Commands = append(e.Commands, cmd)
	if e.ShouldFail || e.FailOnCommands[cmd] {
		return os.ErrNotExist
	}
	return nil
}

func (e *MockExecutor) WriteFile(path string, content []byte, mode os.FileMode) error {
	if e.ShouldFail {
		return os.ErrPermission
	}
	e.Files[path] = content
	return nil
}

func (e *MockExecutor) GetDistro() (*Distro, error) {
	return e.distro, nil
}

func (e *MockExecutor) GetArchitecture() (Architecture, error) {
	return e.architecture, nil
}

func (e *MockExecutor) String() string {
	return "mock"
}

func TestMockExecutor_BasicOperations(t *testing.T) {
	distro := &Distro{
		ID:             "fedora",
		VersionID:      "43",
		PackageManager: DNF,
		Architecture:   X86_64,
	}
	cmdExec := NewMockExecutor(distro)

	t.Run("execute records commands", func(t *testing.T) {
		_, _, err := cmdExec.Execute("test command")
		if err != nil {
			t.Errorf("Execute() unexpected error: %v", err)
		}
		if len(cmdExec.Commands) != 1 || cmdExec.Commands[0] != "test command" {
			t.Errorf("Execute() did not record command correctly")
		}
	})

	t.Run("run cmd records commands", func(t *testing.T) {
		err := cmdExec.RunCmd(log.LevelInfo, "sudo", "dnf", "install", "-y", "package")
		if err != nil {
			t.Errorf("RunCmd() unexpected error: %v", err)
		}
		found := false
		for _, cmd := range cmdExec.Commands {
			if cmd == "sudo dnf install -y package" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("RunCmd() did not record command correctly, got: %v", cmdExec.Commands)
		}
	})

	t.Run("write file stores content", func(t *testing.T) {
		err := cmdExec.WriteFile("/test/path", []byte("content"), 0644)
		if err != nil {
			t.Errorf("WriteFile() unexpected error: %v", err)
		}
		if string(cmdExec.Files["/test/path"]) != "content" {
			t.Errorf("WriteFile() did not store content correctly")
		}
	})

	t.Run("get distro returns configured distro", func(t *testing.T) {
		got, err := cmdExec.GetDistro()
		if err != nil {
			t.Errorf("GetDistro() unexpected error: %v", err)
		}
		if got.ID != "fedora" {
			t.Errorf("GetDistro() ID = %q, want %q", got.ID, "fedora")
		}
	})
}

func TestMockExecutor_FailureScenarios(t *testing.T) {
	distro := &Distro{
		ID:             "fedora",
		VersionID:      "43",
		PackageManager: DNF,
		Architecture:   X86_64,
	}
	cmdExec := NewMockExecutor(distro)
	cmdExec.ShouldFail = true

	t.Run("execute fails when ShouldFail is true", func(t *testing.T) {
		_, _, err := cmdExec.Execute("test")
		if err == nil {
			t.Error("Execute() expected error, got nil")
		}
	})

	t.Run("run cmd fails when ShouldFail is true", func(t *testing.T) {
		err := cmdExec.RunCmd(log.LevelInfo, "test")
		if err == nil {
			t.Error("RunCmd() expected error, got nil")
		}
	})

	t.Run("write file fails when ShouldFail is true", func(t *testing.T) {
		err := cmdExec.WriteFile("/test", []byte("content"), 0644)
		if err == nil {
			t.Error("WriteFile() expected error, got nil")
		}
	})
}
