package kind

import (
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
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

func kindProviderOptions() []cluster.ProviderOption {
	engine, err := containerengine.NewProjectEngine(platform.NewLocalExecutor())
	if err != nil {
		log.Warn("Container engine detection failed for kind provider selection: %v; using kind default provider autodetect", err)
		return nil
	}

	switch engine.Name() {
	case containerengine.EnginePodman:
		return []cluster.ProviderOption{cluster.ProviderWithPodman()}
	case containerengine.EngineDocker:
		return []cluster.ProviderOption{cluster.ProviderWithDocker()}
	default:
		return nil
	}
}

// NewKindManager creates a new Kind cluster manager
func NewKindManager(cfg *config.Config) *KindManager {
	return &KindManager{
		config:   cfg,
		provider: cluster.NewProvider(kindProviderOptions()...),
	}
}
