package cni

import (
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/containerengine"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// InstallCNI installs the specified CNI on a cluster using the Kubernetes API.
// For OVN-Kubernetes the deployment mode (full / DPU-host / DPU) and network
// interfaces are determined automatically from the config.
func (m *CNIManager) InstallCNI(cniType config.CNIType, clusterName string, k8sIP string) error {
	log.Info("\n=== Installing %s CNI on cluster %s ===", cniType, clusterName)

	switch cniType {
	case config.CNIFlannel:
		return m.installFlannel(clusterName)
	case config.CNIOVNKubernetes:
		return m.installOVNKubernetes(clusterName, k8sIP)
	case config.CNIKindnet:
		if m.config.IsKindMode() {
			log.Info("Kindnet is the default CNI for Kind clusters, no installation needed")
			return nil
		}
		return fmt.Errorf("Kindnet is not supported for cluster %s", clusterName)
	default:
		return fmt.Errorf("unsupported CNI type: %s", cniType)
	}
}

func (m *CNIManager) InstallAddon(addonType config.AddonType, clusterName string) error {
	log.Info("\n=== Installing addon %s on cluster %s ===", addonType, clusterName)

	switch addonType {
	case config.AddonMultus:
		return m.installMultus()
	case config.AddonCertManager:
		return m.installCertManager(clusterName)
	case config.AddonWhereabouts:
		return m.installWhereabouts(clusterName)
	default:
		return fmt.Errorf("unsupported addon type: %s", addonType)
	}
}

func (m *CNIManager) InstallAddons(addons []config.AddonType, clusterName string) error {
	orderedAddons := resolveAddonInstallOrder(addons)
	for _, addon := range orderedAddons {
		if err := m.InstallAddon(addon, clusterName); err != nil {
			return err
		}
	}

	return nil
}

func resolveAddonInstallOrder(addons []config.AddonType) []config.AddonType {
	ordered := make([]config.AddonType, 0, len(addons)+1)
	hasMultus := false
	hasWhereabouts := false

	for _, addon := range addons {
		if addon == config.AddonMultus {
			hasMultus = true
		}
		if addon == config.AddonWhereabouts {
			hasWhereabouts = true
		}
	}

	injectedWhereabouts := false
	for _, addon := range addons {
		if addon == config.AddonMultus && hasMultus && !hasWhereabouts && !injectedWhereabouts {
			log.Info("Whereabouts addon was not configured explicitly; installing it automatically before multus")
			ordered = append(ordered, config.AddonWhereabouts)
			injectedWhereabouts = true
		}
		ordered = append(ordered, addon)
	}

	return ordered
}

// BuildCNIImage builds a container image for the given registry container
// config. This is intended to be used as a registry.BuildFunc. It dispatches
// to the appropriate CNI-specific build logic and returns the local image name.
func BuildCNIImage(container config.RegistryContainerConfig) (string, error) {
	localExec := platform.NewLocalExecutor()
	engine, err := containerengine.NewProjectEngine(localExec)
	if err != nil {
		return "", err
	}
	return BuildCNIImageWithRuntime(localExec, engine)(container)
}

// BuildCNIImageWithRuntime is BuildCNIImage with injected runtime dependencies
// so callers can reuse a previously detected container engine.
func BuildCNIImageWithRuntime(
	cmdExec platform.CommandExecutor,
	engine containerengine.Engine,
) func(container config.RegistryContainerConfig) (string, error) {
	return func(container config.RegistryContainerConfig) (string, error) {
		cniType := config.CNIType(container.CNI)
		switch cniType {
		case config.CNIOVNKubernetes:
			localImage := container.Tag
			if err := BuildOVNKubernetesImageWithEngine(cmdExec, engine, localImage, ""); err != nil {
				return "", fmt.Errorf("failed to build OVN-Kubernetes image: %w", err)
			}
			return localImage, nil
		default:
			return "", fmt.Errorf("unsupported CNI type for image build: %s", cniType)
		}
	}
}

// RedeployCNI triggers a rolling restart of the CNI components on the specified
// cluster so that pods pick up the newly built image. Requires a Kubernetes
// client (use NewCNIManagerWithKubeconfig or NewCNIManagerWithKubeconfigFile).
func (m *CNIManager) RedeployCNI(clusterName string) error {
	cniType := m.config.GetCNIType(clusterName)

	switch cniType {
	case config.CNIOVNKubernetes:
		return m.redeployOVNKubernetes(clusterName)
	default:
		log.Info("CNI %q does not support redeployment, skipping cluster %s", cniType, clusterName)
		return nil
	}
}
