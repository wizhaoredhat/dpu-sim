// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
)

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
	distro, err := cmdExec.GetDistro()
	if err != nil {
		return fmt.Errorf("failed to detect distribution on %s: %w", cmdExec.String(), err)
	}
	return EnsureDependenciesWithExecutorAndDistro(cmdExec, distro, deps, cfg)
}

// EnsureDependenciesWithExecutorAndDistro behaves like EnsureDependenciesWithExecutor
// but uses the provided distro so callers can share one detection source.
func EnsureDependenciesWithExecutorAndDistro(cmdExec CommandExecutor, distro *Distro, deps []Dependency, cfg *config.Config) error {
	log.Debug("Checking dependencies on %s...", cmdExec.String())

	// Ensure ~/.local/bin is in PATH for pip user installs (only affects local executor)
	ensureLocalBinInPath()

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
