package kind

import (
	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

type KindManager struct {
	config       *config.Config
	provider     *cluster.Provider
	containerBin string // "podman" or "docker"
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

func detectContainerBin() (string, []cluster.ProviderOption) {
	engine, err := containerengine.NewProjectEngine(platform.NewLocalExecutor())
	if err != nil {
		log.Warn("Container engine detection failed for kind provider selection: %v; using kind default provider autodetect", err)
		return "docker", nil
	}

	switch engine.Name() {
	case containerengine.EnginePodman:
		return "podman", []cluster.ProviderOption{cluster.ProviderWithPodman()}
	case containerengine.EngineDocker:
		return "docker", []cluster.ProviderOption{cluster.ProviderWithDocker()}
	default:
		return "docker", nil
	}
}

// ContainerBin returns the container runtime binary ("docker" or "podman")
// selected at startup for this manager.
func (m *KindManager) ContainerBin() string {
	return m.containerBin
}

// NewKindManager creates a new Kind cluster manager
func NewKindManager(cfg *config.Config) *KindManager {
	bin, opts := detectContainerBin()
	return &KindManager{
		config:       cfg,
		provider:     cluster.NewProvider(opts...),
		containerBin: bin,
	}
}
