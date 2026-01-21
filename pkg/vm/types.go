package vm

import (
	"libvirt.org/go/libvirt"

	"github.com/wizhao/dpu-sim/pkg/config"
)

// Manager manages libvirt virtual machines and networks
type VMManager struct {
	conn   *libvirt.Connect
	config *config.Config
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
