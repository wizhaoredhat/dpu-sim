package cni

import (
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// InstallCNI installs the specified CNI on a cluster using the Kubernetes API
// If kubeconfigPath is provided, it will configure the client from that file
func (m *CNIManager) InstallCNI(cniType config.CNIType, clusterName string, k8sIP string) error {
	log.Info("\n=== Installing %s CNI on cluster %s ===", cniType, clusterName)

	switch cniType {
	case config.CNIFlannel:
		return m.installFlannel(clusterName)
	case config.CNIOVNKubernetes:
		return m.installOVNKubernetes(clusterName, k8sIP, m.config.IsKindMode())
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

// BuildCNIImage builds a container image for the given registry container
// config. This is intended to be used as a registry.BuildFunc. It dispatches
// to the appropriate CNI-specific build logic and returns the local image name.
func BuildCNIImage(container config.RegistryContainerConfig) (string, error) {
	localExec := platform.NewLocalExecutor()

	cniType := config.CNIType(container.CNI)
	switch cniType {
	case config.CNIOVNKubernetes:
		localImage := container.Tag
		if err := BuildOVNKubernetesImage(localExec, localImage, ""); err != nil {
			return "", fmt.Errorf("failed to build OVN-Kubernetes image: %w", err)
		}
		return localImage, nil
	default:
		return "", fmt.Errorf("unsupported CNI type for image build: %s", cniType)
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
