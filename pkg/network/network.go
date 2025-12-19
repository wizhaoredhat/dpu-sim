// Package network provides network utilities for the DPU simulator.
//
// This package handles network naming conventions and bridge management
// for host-to-DPU network connections.
package network

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

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
