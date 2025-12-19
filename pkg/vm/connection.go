// Package vm provides libvirt-based VM management for the DPU simulator.
//
// This package handles creation, configuration, and lifecycle management
// of VMs used in DPU simulation environments.
package vm

import (
	"fmt"

	"libvirt.org/go/libvirt"
)

// Connect establishes a connection to libvirt
func Connect() (*libvirt.Connect, error) {
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %w", err)
	}

	hostname, err := conn.GetHostname()
	if err != nil {
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	fmt.Printf("✓ Connected to libvirt: %s\n", hostname)

	return conn, nil
}
