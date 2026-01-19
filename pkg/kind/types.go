package kind

import (
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/wizhao/dpu-sim/pkg/config"
)

type KindManager struct {
	config   *config.Config
	provider *cluster.Provider
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

// NewKindManager creates a new Kind cluster manager
func NewKindManager(cfg *config.Config) *KindManager {
	return &KindManager{
		config:   cfg,
		provider: cluster.NewProvider(),
	}
}
