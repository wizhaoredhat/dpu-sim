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
