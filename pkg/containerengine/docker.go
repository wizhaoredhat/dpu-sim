package containerengine

import (
	"context"
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/platform"
)

// DockerEngine is the Docker-backed Engine implementation.
type DockerEngine struct {
	*baseEngine
}

func NewDockerEngine(exec platform.CommandExecutor) *DockerEngine {
	return &DockerEngine{
		baseEngine: &baseEngine{
			name: EngineDocker,
			bin:  "docker",
			exec: exec,
		},
	}
}

func (e *DockerEngine) Push(ctx context.Context, imageRef string, opts PushOptions) error {
	if opts.Insecure {
		return fmt.Errorf("docker push to insecure registries requires daemon insecure registry configuration")
	}
	return e.baseEngine.push(ctx, imageRef, opts, "")
}
