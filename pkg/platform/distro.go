// Package platform provides utilities for Linux distribution detection and management
package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/ssh"
)

const X86_64 = "x86_64"

const DNF = "dnf"
const APT = "apt"
const APK = "apk"

// Distro represents information about a Linux distribution
type Distro struct {
	ID             string // e.g., "fedora", "ubuntu", "debian", "centos", "rhel"
	VersionID      string // e.g., "43", "22.04", "12"
	IDLike         string // e.g., "rhel fedora", "debian"
	Architecture   string // e.g., "x86_64", "aarch64"
	PackageManager string // e.g., "dnf", "apt", "yum"
}

// IsFedoraLike returns true if the distro is Fedora-based (Fedora, RHEL, CentOS, etc.)
func (d *Distro) IsFedoraLike() bool {
	switch d.ID {
	case "fedora", "rhel", "centos", "rocky", "almalinux":
		return true
	}
	return strings.Contains(d.IDLike, "fedora") || strings.Contains(d.IDLike, "rhel")
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

// Detect detects the Linux distribution of a remote machine via SSH
func Detect(sshClient *ssh.Client, machineIP string) (*Distro, error) {
	// Read /etc/os-release which is standard on most modern Linux distributions
	script := `cat /etc/os-release`

	stdout, stderr, err := sshClient.ExecuteWithTimeout(machineIP, script, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to read /etc/os-release: %w, stderr: %s", err, stderr)
	}

	distro := Parse(stdout)

	// Detect architecture using uname -m
	arch, stderr, err := sshClient.ExecuteWithTimeout(machineIP, "uname -m", 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to detect architecture: %w, stderr: %s", err, stderr)
	}
	distro.Architecture = strings.TrimSpace(arch)

	return distro, nil
}

// DetectLocalDistro detects the Linux distribution of the local machine
func DetectLocalDistro() (*Distro, error) {
	content, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil, fmt.Errorf("failed to read /etc/os-release: %w", err)
	}

	distro := Parse(string(content))

	// Detect architecture using uname -m
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return nil, fmt.Errorf("failed to detect architecture: %w", err)
	}
	distro.Architecture = strings.TrimSpace(string(out))

	return distro, nil
}

// Parse parses the contents of /etc/os-release and returns a Distro
func Parse(osReleaseContent string) *Distro {
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
