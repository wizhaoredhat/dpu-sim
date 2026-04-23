package kind

import (
	"fmt"

	"sigs.k8s.io/kind/pkg/cluster"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

type KindManager struct {
	config       *config.Config
	provider     *cluster.Provider
	containerBin string // e.g. "podman" or "docker"
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

// detectContainerBin detects the container runtime binary (e.g. "docker" or "podman")
// and the corresponding cluster.ProviderOption for Kind.
func detectContainerBin() (bin string, opts []cluster.ProviderOption, err error) {
	engine, err := containerengine.NewProjectEngine(platform.NewLocalExecutor())
	if err != nil {
		return "", nil, fmt.Errorf("detect container engine for Kind: %w", err)
	}

	switch engine.Name() {
	case containerengine.EnginePodman:
		return "podman", []cluster.ProviderOption{cluster.ProviderWithPodman()}, nil
	case containerengine.EngineDocker:
		return "docker", []cluster.ProviderOption{cluster.ProviderWithDocker()}, nil
	default:
		return "", nil, fmt.Errorf("unsupported container engine %q for Kind", engine.Name())
	}
}

// ContainerBin returns the container runtime binary (e.g. "docker" or "podman")
// selected at startup for this manager.
func (m *KindManager) ContainerBin() string {
	return m.containerBin
}

// NewKindManager creates a new Kind cluster manager.
func NewKindManager(cfg *config.Config) (*KindManager, error) {
	bin, opts, err := detectContainerBin()
	if err != nil {
		return nil, err
	}
	return &KindManager{
		config:       cfg,
		provider:     cluster.NewProvider(opts...),
		containerBin: bin,
	}, nil
}
