package containerengine

import "context"

// Name identifies the selected container runtime implementation.
type Name string

const (
	EngineAuto   Name = "auto"
	EngineDocker Name = "docker"
	EnginePodman Name = "podman"
)

// BuildOptions defines arguments for building an image.
type BuildOptions struct {
	ContextDir string
	Dockerfile string
	Image      string
	Platform   string
	BuildArgs  map[string]string
	ExtraArgs  []string
}

// PushOptions defines arguments for pushing an image.
type PushOptions struct {
	Insecure  bool
	ExtraArgs []string
}

// RunContainerOptions defines arguments for running a container.
type RunContainerOptions struct {
	Name      string
	Image     string
	Detach    bool
	Restart   string
	Network   string
	Publish   []string
	ExtraArgs []string
}

// ContainerState is a runtime-agnostic view of container status.
type ContainerState struct {
	Exists  bool
	Running bool
}

// NetworkEndpoint holds per-network addressing details.
type NetworkEndpoint struct {
	IPAddress string
}

// Engine is the runtime abstraction shared by Docker and Podman implementations.
type Engine interface {
	Name() Name

	Build(ctx context.Context, opts BuildOptions) error
	Tag(ctx context.Context, sourceRef, targetRef string) error
	Push(ctx context.Context, imageRef string, opts PushOptions) error
	ImageExists(ctx context.Context, imageRef string) bool

	RunContainer(ctx context.Context, opts RunContainerOptions) error
	RemoveContainer(ctx context.Context, name string, force bool) error
	InspectContainerState(ctx context.Context, name string) (ContainerState, error)
	InspectContainerNetworks(ctx context.Context, name string) (map[string]NetworkEndpoint, error)
	ConnectNetwork(ctx context.Context, network, container string) error
}
