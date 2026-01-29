// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"fmt"
	"strings"
	"time"
)

// IsFedoraLike returns true if the distro is Fedora-based (Fedora, RHEL, CentOS, etc.)
func (d *Distro) IsFedoraLike() bool {
	switch d.ID {
	case "fedora", "rhel", "centos", "rocky", "almalinux":
		return true
	}
	return strings.Contains(d.IDLike, "fedora") || strings.Contains(d.IDLike, "rhel")
}

// IsRHEL returns true if the distro is RHEL
func (d *Distro) IsRHEL() bool {
	return d.ID == "rhel"
}

// IsDebianLike returns true if the distro is Debian-based (Debian, Ubuntu, etc.)
func (d *Distro) IsDebianLike() bool {
	switch d.ID {
	case "debian", "ubuntu", "linuxmint", "pop":
		return true
	}
	return strings.Contains(d.IDLike, "debian") || strings.Contains(d.IDLike, "ubuntu")
}

// DetectPackageManager determines the package manager based on the Linux distribution
func DetectPackageManager(distro *Distro) string {
	if distro.IsFedoraLike() {
		// Fedora 22+ and RHEL 8+ use dnf, older versions use yum
		return DNF
	}
	if distro.IsDebianLike() {
		return APT
	}

	// Fallback based on ID
	switch distro.ID {
	case "alpine":
		return APK
	default:
		return "unknown"
	}
}

// DetectDistro detects the Linux distribution using the provided executor.
// This works uniformly across local, SSH, and Docker executors.
func DetectDistro(cmdExec CommandExecutor) (*Distro, error) {
	// Read /etc/os-release which is standard on most modern Linux distributions
	stdout, stderr, err := cmdExec.ExecuteWithTimeout("cat /etc/os-release", 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to read /etc/os-release: %w, stderr: %s", err, stderr)
	}

	distro := ParseOSRelease(stdout)

	// Detect architecture using uname -m
	arch, err := DetectArchitecture(cmdExec)
	if err != nil {
		return nil, fmt.Errorf("failed to detect architecture: %w", err)
	}
	distro.Architecture = arch

	return distro, nil
}

// DetectArchitecture detects the CPU architecture using the provided executor.
func DetectArchitecture(cmdExec CommandExecutor) (Architecture, error) {
	stdout, _, err := cmdExec.ExecuteWithTimeout("uname -m", 10*time.Second)
	if err != nil {
		return "", fmt.Errorf("failed to detect architecture: %w", err)
	}

	arch := strings.TrimSpace(stdout)
	switch arch {
	case "x86_64":
		return X86_64, nil
	case "aarch64":
		return AARCH64, nil
	default:
		return Architecture(arch), nil
	}
}

// ParseOSRelease parses the contents of /etc/os-release and returns a Distro
func ParseOSRelease(osReleaseContent string) *Distro {
	distro := &Distro{}

	// Parse the os-release file (KEY=VALUE or KEY="VALUE" format)
	for _, line := range strings.Split(osReleaseContent, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := strings.Trim(parts[1], `"'`)

		switch key {
		case "ID":
			distro.ID = value
		case "VERSION_ID":
			distro.VersionID = value
		case "ID_LIKE":
			distro.IDLike = value
		}
	}

	// Determine package manager based on distro
	distro.PackageManager = DetectPackageManager(distro)

	return distro
}
