// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
)

// getProjectRoot returns the root directory of the dpu-sim project
func getProjectRoot() (string, error) {
	// Get the directory of the current source file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get current file path")
	}

	// Navigate from pkg/linux/deps.go to project root (2 levels up)
	projectRoot := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return projectRoot, nil
}

// ensureLocalBinInPath adds ~/.local/bin to PATH if not already present
func ensureLocalBinInPath() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	localBin := filepath.Join(homeDir, ".local", "bin")
	currentPath := os.Getenv("PATH")

	if !strings.Contains(currentPath, localBin) {
		os.Setenv("PATH", localBin+":"+currentPath)
	}
}

// UnsupportedPackageManager returns an error for an unsupported package manager
func UnsupportedPackageManager(distro *Distro) error {
	return fmt.Errorf("unsupported package manager: %s", distro.PackageManager)
}

// UnsupportedArchitecture returns an error for an unsupported architecture
func UnsupportedArchitecture(distro *Distro) error {
	return fmt.Errorf("unsupported architecture: %s", distro.Architecture)
}

// checkDependency checks if a single dependency is installed
func checkDependency(cmdExec CommandExecutor, dep Dependency, distro *Distro, cfg *config.Config) DependencyResult {
	result := DependencyResult{
		Name:      dep.Name,
		Installed: false,
	}

	// If CheckCmd is empty, use CheckFunc (for libraries without executables)
	if len(dep.CheckCmd) == 0 {
		if dep.CheckFunc == nil {
			result.Error = fmt.Errorf("no check command or check function defined for dependency %s", dep.Name)
			return result
		}
		if err := dep.CheckFunc(cmdExec, distro, cfg, &dep); err != nil {
			result.Error = err
			return result
		}
		result.Installed = true
		return result
	}

	// Build command string from CheckCmd
	checkCmd := strings.Join(dep.CheckCmd, " ")
	stdout, stderr, err := cmdExec.Execute(checkCmd)
	result.Output = stdout + stderr
	if err != nil {
		result.Error = err
		return result
	}

	result.Installed = true
	return result
}

// installDependency attempts to install a dependency using its install function
func installDependency(cmdExec CommandExecutor, dep Dependency, distro *Distro, cfg *config.Config) error {
	if dep.InstallFunc == nil {
		return fmt.Errorf("no install function defined for %s", dep.Name)
	}

	log.Info("Installing %s for %s on %s...", dep.Name, distro.ID, cmdExec.String())

	if err := dep.InstallFunc(cmdExec, distro, cfg, &dep); err != nil {
		return fmt.Errorf("failed to install %s: %w. Needed for %s", dep.Name, err, dep.Reason)
	}

	log.Info("✓ %s installed", dep.Name)
	return nil
}

// EnsureDependenciesWithExecutor checks and installs dependencies using the provided executor
// This allows installing dependencies on remote machines (via SSH) or in containers (via Docker)
// exec is the CommandExecutor that determines where commands are run
// cfg provides configuration including version information for dependencies
// Returns an error if any dependency cannot be installed
func EnsureDependenciesWithExecutor(cmdExec CommandExecutor, deps []Dependency, cfg *config.Config) error {
	log.Debug("Checking dependencies on %s...", cmdExec.String())

	// Ensure ~/.local/bin is in PATH for pip user installs (only affects local executor)
	ensureLocalBinInPath()

	// Detect distro using the executor
	distro, err := cmdExec.GetDistro()
	if err != nil {
		return fmt.Errorf("failed to detect distribution on %s: %w", cmdExec.String(), err)
	}
	log.Info("✓ Detected Linux distribution: %s %s (package manager: %s, architecture: %s)", distro.ID, distro.VersionID, distro.PackageManager, distro.Architecture)

	var missing []Dependency
	for _, dep := range deps {
		result := checkDependency(cmdExec, dep, distro, cfg)
		if result.Installed {
			log.Info("✓ %s is installed", dep.Name)
		} else {
			log.Debug("✗ %s is not installed", dep.Name)
			missing = append(missing, dep)
		}
	}

	if len(missing) > 0 {
		var names []string
		for _, dep := range missing {
			names = append(names, dep.Name)
		}
		log.Info("Installing missing dependencies: %s", strings.Join(names, ", "))
		for _, dep := range missing {
			if err := installDependency(cmdExec, dep, distro, cfg); err != nil {
				return fmt.Errorf("failed to install dependency %s: %w", dep.Name, err)
			}

			result := checkDependency(cmdExec, dep, distro, cfg)
			if !result.Installed {
				return fmt.Errorf("dependency %s was installed but verification failed", dep.Name)
			}
		}
	}

	log.Info("✓ All dependencies are available")
	return nil
}

// getOVNKubernetesPath returns the path to the ovn-kubernetes directory
func getOVNKubernetesPath() (string, error) {
	projectRoot, err := getProjectRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(projectRoot, "ovn-kubernetes"), nil
}

// isOVNKubernetesPopulated checks if the ovn-kubernetes directory contains actual content
// An uninitialized submodule directory exists but is empty
func isOVNKubernetesPopulated(ovnPath string) bool {
	// Daemonset.sh is a dependency, check for its existence
	daemonsetScript := filepath.Join(ovnPath, "dist", "images", "daemonset.sh")
	if _, err := os.Stat(daemonsetScript); err == nil {
		return true
	}
	return false
}

// initOVNKubernetesSubmodule initializes and updates the ovn-kubernetes git submodule
func initOVNKubernetesSubmodule(projectRoot string) error {
	log.Debug("Initializing ovn-kubernetes git submodule...")

	initCmd := exec.Command("git", "submodule", "init", "ovn-kubernetes")
	initCmd.Dir = projectRoot
	initCmd.Stdout = os.Stdout
	initCmd.Stderr = os.Stderr
	if err := initCmd.Run(); err != nil {
		return fmt.Errorf("failed to initialize submodule: %w", err)
	}

	updateCmd := exec.Command("git", "submodule", "update", "--init", "ovn-kubernetes")
	updateCmd.Dir = projectRoot
	updateCmd.Stdout = os.Stdout
	updateCmd.Stderr = os.Stderr
	if err := updateCmd.Run(); err != nil {
		return fmt.Errorf("failed to update submodule: %w", err)
	}

	log.Info("✓ ovn-kubernetes submodule is initialized")
	return nil
}

// EnsureOVNKubernetesSource ensures the ovn-kubernetes source code is available.
// It first tries to initialize the git submodule if it exists but is empty.
// If submodule initialization fails or the directory doesn't exist, it clones the repository.
func EnsureOVNKubernetesSource() (string, error) {
	ovnPath, err := getOVNKubernetesPath()
	if err != nil {
		return "", fmt.Errorf("failed to get OVN-Kubernetes path: %w", err)
	}

	projectRoot, err := getProjectRoot()
	if err != nil {
		return "", fmt.Errorf("failed to get project root: %w", err)
	}

	// Check if directory exists and is populated
	if _, err := os.Stat(ovnPath); err == nil {
		if isOVNKubernetesPopulated(ovnPath) {
			log.Debug("OVN-Kubernetes source found at %s", ovnPath)
			return ovnPath, nil
		}

		// Directory exists but is empty (uninitialized submodule)
		log.Info("OVN-Kubernetes directory exists but appears empty (uninitialized submodule)")
		if err := initOVNKubernetesSubmodule(projectRoot); err != nil {
			log.Warn("Warning: Failed to initialize submodule: %v", err)
			log.Info("Attempting to clone repository directly...")

			// Remove the empty directory and clone fresh
			if err := os.RemoveAll(ovnPath); err != nil {
				return "", fmt.Errorf("failed to remove empty ovn-kubernetes directory: %w", err)
			}
		} else {
			// Submodule initialized successfully
			if isOVNKubernetesPopulated(ovnPath) {
				return ovnPath, nil
			}
			return "", fmt.Errorf("submodule initialized but content still missing")
		}
	}

	// Directory doesn't exist or was removed - try submodule init first, then clone as fallback
	gitDir := filepath.Join(projectRoot, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		// We're in a git repository, try submodule init
		if err := initOVNKubernetesSubmodule(projectRoot); err == nil {
			if isOVNKubernetesPopulated(ovnPath) {
				return ovnPath, nil
			}
		}
		log.Info("Submodule initialization failed, falling back to clone...")
	}

	log.Info("OVN-Kubernetes not found, cloning from %s:master...", DefaultOVNRepoURL)
	cmd := exec.Command("git", "clone", "--branch", "master", DefaultOVNRepoURL, ovnPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to clone OVN-Kubernetes repository: %w", err)
	}

	log.Info("✓ OVN-Kubernetes is cloned to %s", ovnPath)
	return ovnPath, nil
}
