package cni

import (
	"fmt"

	"github.com/wizhao/dpu-sim/pkg/log"
)

// InstallCNI installs the specified CNI on a cluster using the Kubernetes API
// If kubeconfigPath is provided, it will configure the client from that file
func (m *CNIManager) InstallCNI(cniType CNIType, clusterName string, k8sIP string) error {
	log.Info("\n=== Installing %s CNI on cluster %s ===", cniType, clusterName)

	switch cniType {
	case CNIFlannel:
		return m.installFlannel(clusterName)
	case CNIOVNKubernetes:
		return m.installOVNKubernetes(clusterName, k8sIP, m.config.IsKindMode())
	case CNIKindnet:
		if m.config.IsKindMode() {
			log.Info("Kindnet is the default CNI for Kind clusters, no installation needed")
			return nil
		}
		return fmt.Errorf("Kindnet is not supported for cluster %s", clusterName)
	default:
		return fmt.Errorf("unsupported CNI type: %s", cniType)
	}
}

// RebuildCNIImage rebuilds the CNI container image from source for the given
// CNI type. Currently only OVN-Kubernetes supports rebuilding; for other CNIs
// a message is logged and no action is taken. This method does not require
// Kubernetes API access.
func (m *CNIManager) RebuildCNIImage(cniType CNIType) error {
	switch cniType {
	case CNIOVNKubernetes:
		return m.rebuildOVNKubernetesImage()
	default:
		log.Info("CNI %q does not support image rebuilding, skipping", cniType)
		return nil
	}
}

// RedeployCNI triggers a rolling restart of the CNI components on the specified
// cluster so that pods pick up the newly built image. Requires a Kubernetes
// client (use NewCNIManagerWithKubeconfig or NewCNIManagerWithKubeconfigFile).
func (m *CNIManager) RedeployCNI(clusterName string) error {
	cniType := CNIType(m.config.GetCNIType(clusterName))

	switch cniType {
	case CNIOVNKubernetes:
		return m.redeployOVNKubernetes(clusterName)
	default:
		log.Info("CNI %q does not support redeployment, skipping cluster %s", cniType, clusterName)
		return nil
	}
}
