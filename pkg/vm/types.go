package vm

import (
	"fmt"

	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// VMManager manages libvirt virtual machines and networks
type VMManager struct {
	conn       *libvirt.Connect
	config     *config.Config
	hostDistro *platform.Distro
}

// NewVMManager creates a new VMManager with the given config, connecting to libvirt.
// cfg can be nil for operations that don't require configuration.
func NewVMManager(cfg *config.Config) (*VMManager, error) {
	distro, err := platform.GetHostDistro()
	if err != nil {
		return nil, fmt.Errorf("failed to detect host distro: %w", err)
	}
	conn, err := libvirt.NewConnect("qemu:///system")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %w", err)
	}

	hostname, err := conn.GetHostname()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to get hostname: %w", err)
	}
	log.Debug("✓ Connected to libvirt: %s", hostname)

	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	return &VMManager{
		conn:       conn,
		config:     cfg,
		hostDistro: distro,
	}, nil
}

// Close closes the libvirt connection
func (m *VMManager) Close() error {
	if m.conn != nil {
		_, err := m.conn.Close()
		return err
	}
	return nil
}

// VMState represents the state of a virtual machine
type VMState int

const (
	VMStateUnknown VMState = iota
	VMStateRunning
	VMStateBlocked
	VMStatePaused
	VMStateShutdown
	VMStateShutoff
	VMStateCrashed
)

// String returns string representation of VM state
func (s VMState) String() string {
	switch s {
	case VMStateRunning:
		return "Running"
	case VMStateBlocked:
		return "Blocked"
	case VMStatePaused:
		return "Paused"
	case VMStateShutdown:
		return "Shutdown"
	case VMStateShutoff:
		return "Shut off"
	case VMStateCrashed:
		return "Crashed"
	default:
		return "Unknown"
	}
}

// InterfaceInfo represents VM interface information
type InterfaceInfo struct {
	Name   string
	Hwaddr string
	Addrs  []string
}

// VMInfo represents comprehensive VM information
type VMInfo struct {
	Name      string
	State     VMState
	IP        string
	VCPUs     uint
	MemoryMB  uint64
	MaxMemory uint64
}
