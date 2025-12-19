// Package vm provides libvirt-based VM management for the DPU simulator.
//
// This package handles creation, configuration, and lifecycle management
// of VMs used in DPU simulation environments.
package vm

import (
	"fmt"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
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

// NewManager creates a new VM manager
func NewManager(cfg *config.Config) (*Manager, error) {
	conn, err := Connect()
	if err != nil {
		return nil, err
	}

	return &Manager{
		conn:   conn,
		config: cfg,
		vms:    make([]vmInfo, 0),
	}, nil
}

// Close closes the libvirt connection
func (m *Manager) Close() error {
	if m.conn != nil {
		_, err := m.conn.Close()
		return err
	}
	return nil
}

// GetConnection returns the underlying libvirt connection
func (m *Manager) GetConnection() *libvirt.Connect {
	return m.conn
}

// NewNetworkManager creates a new network manager
func NewNetworkManager(cfg *config.Config, conn *libvirt.Connect) *NetworkManager {
	return &NetworkManager{
		conn:     conn,
		config:   cfg,
		networks: make([]networkInfo, 0),
	}
}
