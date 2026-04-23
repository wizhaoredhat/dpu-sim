package kind

import (
	"context"
	"fmt"
	"os"

	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/deviceplugin"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// BuildAndLoadImagesFromRegistryConfig builds images declared under registry.containers
// when the registry is disabled (Kind mode). Images are loaded into Kind so nodes can run
// them; pod specs must use the matching localhost/… references from config.KindNodeLocalImageRef.
func (m *KindManager) BuildAndLoadImagesFromRegistryConfig(cmdExec platform.CommandExecutor, engine containerengine.Engine) error {
	cfg := m.config
	if !cfg.IsRegistryImageBuildOnly() || !cfg.IsKindMode() {
		return nil
	}

	log.Info("\n=== Building registry container images (registry disabled; loading into Kind) ===")
	build := cni.BuildCNIImageWithRuntime(cmdExec, engine)

	for _, container := range cfg.Registry.Containers {
		localImage, err := build(container)
		if err != nil {
			return fmt.Errorf("build registry container %q: %w", container.Name, err)
		}

		switch config.CNIType(container.CNI) {
		case config.CNIOVNKubernetes:
			kindRef, err := m.prepareBuiltImageForKindLoad(context.Background(), cmdExec, engine, localImage)
			if err != nil {
				return fmt.Errorf("prepare registry container %q image for Kind: %w", container.Name, err)
			}
			for _, cl := range cfg.Kubernetes.Clusters {
				if !cfg.ClusterNeedsOVNKubernetesImage(cl.Name) {
					continue
				}
				if err := m.LoadImage(cl.Name, kindRef); err != nil {
					return fmt.Errorf("kind load %q into cluster %q: %w", kindRef, cl.Name, err)
				}
				log.Info("✓ OVN-Kubernetes image loaded into cluster %s", cl.Name)
			}
		default:
			return fmt.Errorf("unsupported CNI type for registry container build: %q", container.CNI)
		}
	}

	if cfg.IsOffloadDPU() {
		if err := deviceplugin.BuildDevicePluginImage(cmdExec, engine); err != nil {
			return fmt.Errorf("build device plugin image: %w", err)
		}
		hostCluster := cfg.GetDPUHostClusterName()
		if hostCluster != "" {
			kindRef, err := m.prepareBuiltImageForKindLoad(context.Background(), cmdExec, engine, deviceplugin.DevicePluginImage)
			if err != nil {
				return fmt.Errorf("prepare device plugin image for Kind: %w", err)
			}
			if err := m.LoadImage(hostCluster, kindRef); err != nil {
				return fmt.Errorf("kind load device plugin into cluster %q: %w", hostCluster, err)
			}
			log.Info("✓ Device plugin image loaded into cluster %s", hostCluster)
		}
	}

	log.Info("✓ Registry image builds complete; images loaded into Kind")
	return nil
}

func engineRuntimeBin(n containerengine.Name) (string, error) {
	switch n {
	case containerengine.EngineDocker:
		return "docker", nil
	case containerengine.EnginePodman:
		return "podman", nil
	default:
		return "", fmt.Errorf("unsupported container engine %q for Kind image load (need docker or podman)", n)
	}
}

func (m *KindManager) assertImageInKindRuntime(cmdExec platform.CommandExecutor, imageRef string) error {
	_, _, err := cmdExec.Execute(fmt.Sprintf("%s image inspect %q >/dev/null 2>&1", m.containerBin, imageRef))
	if err != nil {
		return fmt.Errorf("image %q is not present in local %s (required before kind load image-archive)", imageRef, m.containerBin)
	}
	return nil
}

// transferImageToKindRuntime copies a tagged image from one local runtime to another via save/load.
func transferImageToKindRuntime(cmdExec platform.CommandExecutor, fromBin, toBin, imageRef string) error {
	tmpFile, err := os.CreateTemp("", "dpu-sim-kind-runtime-transfer-*.tar")
	if err != nil {
		return fmt.Errorf("create temp archive for %s→%s image transfer: %w", fromBin, toBin, err)
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

// prepareBuiltImageForKindLoad ensures builtRef exists under config.KindNodeLocalImageRef(builtRef) in
// m.containerBin's image store so LoadImage can save it for kind load image-archive.
func (m *KindManager) prepareBuiltImageForKindLoad(
	ctx context.Context,
	cmdExec platform.CommandExecutor,
	engine containerengine.Engine,
	builtRef string,
) (string, error) {
	nodeLocal := config.KindNodeLocalImageRef(builtRef)
	if nodeLocal == "" {
		return "", fmt.Errorf("empty Kind node-local image ref derived from %q", builtRef)
	}

	engineBin, err := engineRuntimeBin(engine.Name())
	if err != nil {
		return "", err
	}

	if engineBin != m.containerBin {
		if nodeLocal != builtRef {
			if err := engine.Tag(ctx, builtRef, nodeLocal); err != nil {
				return "", fmt.Errorf(
					"retag built image %q to Kind node-local ref %q with build engine %s (Kind uses %s): %w",
					builtRef, nodeLocal, engineBin, m.containerBin, err,
				)
			}
		}
		if err := transferImageToKindRuntime(cmdExec, engineBin, m.containerBin, nodeLocal); err != nil {
			return "", fmt.Errorf(
				"copy image into %s after build with %s (engine/runtime mismatch): %w",
				m.containerBin, engineBin, err,
			)
		}
		if err := m.assertImageInKindRuntime(cmdExec, nodeLocal); err != nil {
			return "", err
		}
		return nodeLocal, nil
	}

	if nodeLocal != builtRef {
		if err := engine.Tag(ctx, builtRef, nodeLocal); err != nil {
			return "", fmt.Errorf(
				"retag built image %q to Kind node-local ref %q with %s: %w",
				builtRef, nodeLocal, engineBin, err,
			)
		}
	}
	if err := m.assertImageInKindRuntime(cmdExec, nodeLocal); err != nil {
		return "", err
	}
	return nodeLocal, nil
}
