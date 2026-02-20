package containerengine

import (
	"context"

	"github.com/wizhao/dpu-sim/pkg/platform"
)

// PodmanEngine is the Podman-backed Engine implementation.
type PodmanEngine struct {
	*baseEngine
}

func NewPodmanEngine(exec platform.CommandExecutor) *PodmanEngine {
	return &PodmanEngine{
		baseEngine: &baseEngine{
			name: EnginePodman,
			bin:  "podman",
			exec: exec,
		},
	}
}

func (e *PodmanEngine) Push(ctx context.Context, imageRef string, opts PushOptions) error {
	return e.baseEngine.push(ctx, imageRef, opts, "--tls-verify=false")
}
