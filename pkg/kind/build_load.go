package kind

import (
	"fmt"

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
			for _, cl := range cfg.Kubernetes.Clusters {
				if !cfg.ClusterNeedsOVNKubernetesImage(cl.Name) {
					continue
				}
				if err := m.LoadImage(cl.Name, localImage); err != nil {
					return fmt.Errorf("kind load %q into cluster %q: %w", localImage, cl.Name, err)
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
			if err := m.LoadImage(hostCluster, deviceplugin.DevicePluginImage); err != nil {
				return fmt.Errorf("kind load device plugin into cluster %q: %w", hostCluster, err)
			}
			log.Info("✓ Device plugin image loaded into cluster %s", hostCluster)
		}
	}

	log.Info("✓ Registry image builds complete; images loaded into Kind")
	return nil
}
