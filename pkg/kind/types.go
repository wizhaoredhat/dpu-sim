package kind

import (
	"github.com/wizhao/dpu-sim/pkg/config"
)

// Manager manages Kind clusters
type Manager struct {
	config *config.Config
}

// ClusterInfo represents information about a Kind cluster
type ClusterInfo struct {
	Name   string
	Nodes  []NodeInfo
	Status string
}

// NodeInfo represents information about a Kind node
type NodeInfo struct {
	Name   string
	Role   string
	Status string
}

// NewManager creates a new Kind cluster manager
func NewManager(cfg *config.Config) *Manager {
	return &Manager{
		config: cfg,
	}
}
