// Package network provides network utilities for the DPU simulator.
//
// This package handles network naming conventions and bridge management
// for host-to-DPU network connections.
package network

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/ssh"
)

// InterfaceInfo represents detailed information about a network interface
type InterfaceInfo struct {
	Name       string   `json:"ifname"`    // Interface name (e.g., "eth0", "enp1s0")
	Index      int      `json:"ifindex"`   // Interface index
	MTU        int      `json:"mtu"`       // Maximum Transmission Unit
	State      string   `json:"operstate"` // Operational state (UP, DOWN, UNKNOWN)
	MAC        string   `json:"address"`   // Hardware/MAC address
	Broadcast  string   `json:"broadcast"` // Broadcast address
	Flags      []string `json:"flags"`     // Interface flags (e.g., BROADCAST, MULTICAST)
	LinkType   string   `json:"link_type"` // Link type (e.g., "ether", "loopback")
	Group      string   `json:"group"`     // Interface group
	TxQueueLen int      `json:"txqlen"`    // Transmit queue length
	Addresses  []IPAddr `json:"addr_info"` // IP addresses assigned to interface
	TargetIP   string   `json:"-"`         // The IP address used to find this interface
}

// IPAddr represents an IP address assigned to an interface
type IPAddr struct {
	Family    string `json:"family"`              // Address family (inet, inet6)
	Local     string `json:"local"`               // Local IP address
	Prefixlen int    `json:"prefixlen"`           // Prefix length (CIDR notation)
	Scope     string `json:"scope"`               // Address scope (global, link, host)
	Label     string `json:"label,omitempty"`     // Address label (optional)
	Broadcast string `json:"broadcast,omitempty"` // Broadcast address for IPv4
}

// String returns a human-readable representation of the interface info
func (i *InterfaceInfo) String() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Interface: %s (index: %d)\n", i.Name, i.Index))
	sb.WriteString(fmt.Sprintf("  State: %s\n", i.State))
	sb.WriteString(fmt.Sprintf("  MAC: %s\n", i.MAC))
	sb.WriteString(fmt.Sprintf("  MTU: %d\n", i.MTU))
	sb.WriteString(fmt.Sprintf("  Link Type: %s\n", i.LinkType))
	sb.WriteString(fmt.Sprintf("  Flags: %v\n", i.Flags))
	if len(i.Addresses) > 0 {
		sb.WriteString("  Addresses:\n")
		for _, addr := range i.Addresses {
			sb.WriteString(fmt.Sprintf("    - %s/%d (%s, scope: %s)\n", addr.Local, addr.Prefixlen, addr.Family, addr.Scope))
		}
	}
	return sb.String()
}

// GetInterfaceByIP retrieves interface information for the interface that has the specified IP address.
// It SSHs into the machine at targetMachineIP and finds the interface with searchIP.
func GetInterfaceByIP(sshClient *ssh.Client, targetMachineIP, searchIP string) (*InterfaceInfo, error) {
	return GetInterfaceByIPWithTimeout(sshClient, targetMachineIP, searchIP, 30*time.Second)
}

// GetInterfaceByIPWithTimeout retrieves interface information with a custom timeout.
func GetInterfaceByIPWithTimeout(sshClient *ssh.Client, targetMachineIP, searchIP string, timeout time.Duration) (*InterfaceInfo, error) {
	// Use 'ip -j addr show' to get JSON output of all interfaces
	cmd := "ip -j addr show"

	stdout, stderr, err := sshClient.ExecuteWithTimeout(targetMachineIP, cmd, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to execute ip command: %w, stderr: %s", err, stderr)
	}

	// Parse JSON output
	var interfaces []InterfaceInfo
	if err := json.Unmarshal([]byte(stdout), &interfaces); err != nil {
		return nil, fmt.Errorf("failed to parse ip command output: %w", err)
	}

	// Find the interface with the matching IP address
	for _, iface := range interfaces {
		for _, addr := range iface.Addresses {
			if addr.Local == searchIP {
				iface.TargetIP = searchIP
				return &iface, nil
			}
		}
	}

	return nil, fmt.Errorf("no interface found with IP address %s", searchIP)
}

// GetAllInterfaces retrieves information about all network interfaces on a remote machine.
func GetAllInterfaces(sshClient *ssh.Client, targetMachineIP string) ([]InterfaceInfo, error) {
	return GetAllInterfacesWithTimeout(sshClient, targetMachineIP, 30*time.Second)
}

// GetAllInterfacesWithTimeout retrieves all interface information with a custom timeout.
func GetAllInterfacesWithTimeout(sshClient *ssh.Client, targetMachineIP string, timeout time.Duration) ([]InterfaceInfo, error) {
	cmd := "ip -j addr show"

	stdout, stderr, err := sshClient.ExecuteWithTimeout(targetMachineIP, cmd, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to execute ip command: %w, stderr: %s", err, stderr)
	}

	var interfaces []InterfaceInfo
	if err := json.Unmarshal([]byte(stdout), &interfaces); err != nil {
		return nil, fmt.Errorf("failed to parse ip command output: %w", err)
	}

	return interfaces, nil
}

// GetInterfaceByName retrieves interface information by name from a remote machine.
func GetInterfaceByName(sshClient *ssh.Client, targetMachineIP, ifaceName string) (*InterfaceInfo, error) {
	return GetInterfaceByNameWithTimeout(sshClient, targetMachineIP, ifaceName, 30*time.Second)
}

// GetInterfaceByNameWithTimeout retrieves interface information by name with a custom timeout.
func GetInterfaceByNameWithTimeout(sshClient *ssh.Client, targetMachineIP, ifaceName string, timeout time.Duration) (*InterfaceInfo, error) {
	cmd := fmt.Sprintf("ip -j addr show dev %s", ifaceName)

	stdout, stderr, err := sshClient.ExecuteWithTimeout(targetMachineIP, cmd, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to execute ip command: %w, stderr: %s", err, stderr)
	}

	var interfaces []InterfaceInfo
	if err := json.Unmarshal([]byte(stdout), &interfaces); err != nil {
		return nil, fmt.Errorf("failed to parse ip command output: %w", err)
	}

	if len(interfaces) == 0 {
		return nil, fmt.Errorf("interface %s not found", ifaceName)
	}

	return &interfaces[0], nil
}

// GenerateBridgeName generates a bridge name for a host-DPU pair
// Format: h2d-<short-hash> where hash is from "hostName-dpuName"
func GenerateBridgeName(hostName, dpuName string) string {
	// Create deterministic hash from host and DPU names
	input := fmt.Sprintf("%s-%s", hostName, dpuName)
	hash := sha256.Sum256([]byte(input))

	// Take first 8 characters of hex hash for short identifier
	shortHash := fmt.Sprintf("%x", hash[:8])

	bridgeName := fmt.Sprintf("h2d-%s", shortHash)
	return SanitizeBridgeName(bridgeName)
}

// GetHostToDPUNetworkName generates the libvirt network name for a host-DPU pair
func GetHostToDPUNetworkName(hostName, dpuName string) string {
	return fmt.Sprintf("h2d-%s-%s", hostName, dpuName)
}

// SanitizeBridgeName ensures bridge name meets Linux requirements
// Bridge names must be <= 15 characters and contain only alphanumeric and -_
func SanitizeBridgeName(name string) string {
	// Replace invalid characters with hyphens
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, name)

	// Truncate to 15 characters if needed
	if len(name) > 15 {
		name = name[:15]
	}

	// Remove trailing hyphens
	name = strings.TrimRight(name, "-")

	return name
}

// ValidateBridgeName checks if a bridge name is valid
func ValidateBridgeName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("bridge name cannot be empty")
	}

	if len(name) > 15 {
		return fmt.Errorf("bridge name %s is too long (%d characters, max 15)", name, len(name))
	}

	for i, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return fmt.Errorf("bridge name %s contains invalid character at position %d: %c", name, i, r)
		}
	}

	return nil
}
