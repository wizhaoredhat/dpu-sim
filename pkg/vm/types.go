package vm

import (
	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// Manager manages libvirt virtual machines and networks
type Manager struct {
	conn   *libvirt.Connect
	config *config.Config
	vms    []vmInfo
}

// vmInfo tracks created VM information
type vmInfo struct {
	name   string
	domain *libvirt.Domain
}

// NetworkManager manages libvirt networks
type NetworkManager struct {
	conn     *libvirt.Connect
	config   *config.Config
	networks []networkInfo
}

// networkInfo tracks created network information
type networkInfo struct {
	name    string
	network *libvirt.Network
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
