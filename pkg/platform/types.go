package platform

import (
	"github.com/wizhao/dpu-sim/pkg/config"
)

// Architecture represents a CPU architecture
type Architecture string

// Known architectures
const (
	X86_64  Architecture = "x86_64"
	AARCH64 Architecture = "aarch64"
)

// GoArch returns the Go-style architecture string (e.g., "amd64", "arm64")
// suitable for use with docker --platform=linux/<arch>.
func (a Architecture) GoArch() string {
	switch a {
	case X86_64:
		return "amd64"
	case AARCH64:
		return "arm64"
	default:
		return string(a)
	}
}

// PackageManager names
const (
	DNF = "dnf"
	APT = "apt"
	APK = "apk"
)

// Distro represents information about a Linux distribution
type Distro struct {
	ID             string       // e.g., "fedora", "ubuntu", "debian", "centos", "rhel"
	VersionID      string       // e.g., "43", "22.04", "12"
	IDLike         string       // e.g., "rhel fedora", "debian"
	Architecture   Architecture // e.g., X86_64, AARCH64
	PackageManager string       // e.g., "dnf", "apt", "yum"
}

// InstallFunc is a function that installs a package dependency
// It receives the executor, detected distro and config for platform-specific installation
type InstallFunc func(exec CommandExecutor, distro *Distro, cfg *config.Config, dep *Dependency) error

// CheckFunc is a function that checks if a package dependency is installed
// Used for packages without executables (e.g., development libraries)
type CheckFunc func(exec CommandExecutor, distro *Distro, cfg *config.Config, dep *Dependency) error

// Dependency represents a tool dependency
type Dependency struct {
	Name        string      // Name of the dependency
	Reason      string      // Reason for the dependency
	CheckCmd    []string    // Command to check if dependency is installed (for packages with executables)
	CheckFunc   CheckFunc   // Function to check if dependency is installed (for libraries without executables)
	InstallFunc InstallFunc // Function to install the dependency
}

// DependencyResult holds the result of checking a dependency
type DependencyResult struct {
	Name      string // Name of the dependency
	Installed bool   // True if the dependency is installed
	Output    string // Output of the check command
	Error     error  // Output error of the check command
}
