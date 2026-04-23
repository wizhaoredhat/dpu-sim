package containerengine

import (
	"fmt"
	"os"

	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// ImagePresentInRuntime returns an error if imageRef is not in the local image store
// for the given container runtime CLI.
func ImagePresentInRuntime(cmdExec platform.CommandExecutor, runtimeBin, imageRef string) error {
	shCmd := fmt.Sprintf(
		"%s image inspect %s >/dev/null 2>&1",
		platform.ShQuote(runtimeBin),
		platform.ShQuote(imageRef),
	)
	_, _, err := cmdExec.Execute(shCmd)
	if err != nil {
		return fmt.Errorf("image %s is not present in container runtime %s: %w", imageRef, runtimeBin, err)
	}
	return nil
}

// TransferImageBetweenRuntimes copies imageRef from one local OCI runtime image store
// to another using save to a temporary tar archive and load (Docker- and Podman-compatible flags).
func TransferImageBetweenRuntimes(cmdExec platform.CommandExecutor, fromBin, toBin, imageRef string) error {
	tmpFile, err := os.CreateTemp("", "dpu-sim-runtime-transfer-*.tar")
	if err != nil {
		return fmt.Errorf("create temp archive for container runtime %s→%s image transfer: %w", fromBin, toBin, err)
	}

	tmpPath := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp archive for image transfer: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	if err := cmdExec.RunCmd(log.LevelInfo, fromBin, "save", "-o", tmpPath, imageRef); err != nil {
		return fmt.Errorf("%s save %q for transfer to %s: %w", fromBin, imageRef, toBin, err)
	}
	if err := cmdExec.RunCmd(log.LevelInfo, toBin, "load", "-i", tmpPath); err != nil {
		return fmt.Errorf("%s load archive produced by %s for image %q: %w", toBin, fromBin, imageRef, err)
	}
	return nil
}
