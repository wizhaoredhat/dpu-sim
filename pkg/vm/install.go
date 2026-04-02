package vm

import (
	"fmt"
	"net"
	"time"

	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/network"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// InstallKubernetes installs the software components on a VM
func (m *VMManager) InstallKubernetes(vmName string) error {
	log.Info("=== Installing Kubernetes on VM-based deployment ===")

	k8sMgr := k8s.NewK8sMachineManager(m.config)

	// Install on each VM in the config file
	for _, vmCfg := range m.config.VMs {
		// Install only on specific VM if vmName is set
		if vmName != "" && vmCfg.Name != vmName {
			continue
		}

		// Get VM IP
		mgmtIP, err := m.GetVMMgmtIP(vmCfg.Name)
		if err != nil {
			return fmt.Errorf("failed to get IP for %s: %w", vmCfg.Name, err)
		}

		log.Info("--- Installing Kubernetes on %s (%s) ---", vmCfg.Name, mgmtIP)

		// Get Kubernetes version from config
		k8sVersion := m.config.Kubernetes.Version
		if k8sVersion == "" {
			return fmt.Errorf("Kubernetes version is not set")
		}

		cmdExec := platform.NewSSHExecutor(&m.config.SSH, mgmtIP)
		if err := cmdExec.WaitUntilReady(5 * time.Minute); err != nil {
			return fmt.Errorf("failed to wait for SSH on %s: %w", vmCfg.Name, err)
		}

		if err := k8sMgr.InstallKubernetes(cmdExec, vmCfg.Name, k8sVersion); err != nil {
			return fmt.Errorf("failed to install Kubernetes on %s: %w", vmCfg.Name, err)
		}
	}
	return nil
}

// setupOVNBrExForCluster sets up OVN br-ex on all VMs in the cluster
func (m *VMManager) setupOVNBrExForCluster(clusterRoleMapping config.ClusterRoleMapping, k8sMgr *k8s.K8sMachineManager) error {
	for role, vms := range clusterRoleMapping {
		for _, vmCfg := range vms {
			vmMgmtIP, err := m.GetVMMgmtIP(vmCfg.Name)
			if err != nil {
				return fmt.Errorf("failed to get mgmt IP for %s: %w", vmCfg.Name, err)
			}

			vmK8sIP, err := m.GetVMK8sIP(vmCfg.Name)
			if err != nil {
				return fmt.Errorf("failed to get K8s IP for %s: %w", vmCfg.Name, err)
			}

			log.Debug("Setting up OVN br-ex on %s (%s) - Mgmt IP: %s, K8s IP: %s",
				vmCfg.Name, role, vmMgmtIP, vmK8sIP)

			exec := platform.NewSSHExecutor(&m.config.SSH, vmMgmtIP)
			if err := k8sMgr.SetupOVNBrEx(exec, vmMgmtIP, vmK8sIP); err != nil {
				return fmt.Errorf("failed to setup OVN br-ex on %s: %w", vmCfg.Name, err)
			}

			k8sMgr.PrintOVNBrExStatus(exec)
		}
	}
	return nil
}

// setupK8sCluster sets up a single Kubernetes cluster
func (m *VMManager) setupK8sCluster(clusterName string, clusterRoleMapping config.ClusterRoleMapping) error {
	k8sMgr := k8s.NewK8sMachineManager(m.config)

	clusterCfg := m.config.GetClusterConfig(clusterName)
	if clusterCfg == nil {
		return fmt.Errorf("cluster %s not found in configuration", clusterName)
	}

	// Verify cluster has at least one master node
	masterVMs := clusterRoleMapping[config.ClusterRoleMaster]
	if len(masterVMs) == 0 {
		return fmt.Errorf("no master nodes found for cluster %s", clusterName)
	}

	cniType := clusterCfg.CNI
	if cniType == "" {
		return fmt.Errorf("CNI type is not set for cluster %s", clusterName)
	}

	//if cniType == config.CNIOVNKubernetes {
	//	if err := m.setupOVNBrExForCluster(clusterRoleMapping, k8sMgr); err != nil {
	//		return err
	//	}
	//}
	if cniType == config.CNIOVNKubernetes && m.config.IsOffloadDPU() && m.config.IsDPUCluster(clusterCfg.Name) {
		if err := m.setupOVNKubernetesOffloadToDPUOVS(clusterCfg.Name); err != nil {
			return fmt.Errorf("failed to setup OVS on DPU VMs: %w", err)
		}
	}

	// Ensure br-int exists on all nodes (OVN needs it; avoids "ovs-ofctl: br-int is not a bridge or a socket").
	for _, vms := range clusterRoleMapping {
		for _, vmCfg := range vms {
			mgmtIP, err := m.GetVMMgmtIP(vmCfg.Name)
			if err != nil {
				return fmt.Errorf("failed to get mgmt IP for %s: %w", vmCfg.Name, err)
			}
			exec := platform.NewSSHExecutor(&m.config.SSH, mgmtIP)
			if err := k8sMgr.EnsureOVNBrInt(exec); err != nil {
				return fmt.Errorf("failed to ensure br-int on %s: %w", vmCfg.Name, err)
			}
		}
	}

	podCIDR := clusterCfg.PodCIDR
	serviceCIDR := clusterCfg.ServiceCIDR

	// Initialize the first master node with kubeadm init
	firstMaster := masterVMs[0]
	firstMasterMgmtIP, err := m.GetVMMgmtIP(firstMaster.Name)
	if err != nil {
		return fmt.Errorf("failed to get mgmt IP for %s: %w", firstMaster.Name, err)
	}
	firstMasterK8sIP, err := m.GetVMK8sIP(firstMaster.Name)
	if err != nil {
		return fmt.Errorf("failed to get K8s IP for %s: %w", firstMaster.Name, err)
	}

	firstMasterExec := platform.NewSSHExecutor(&m.config.SSH, firstMasterMgmtIP)

	log.Info("\n=== Initializing first control plane node: %s ===", firstMaster.Name)
	clusterInfo, err := k8sMgr.InitializeControlPlane(firstMasterExec, firstMaster.Name, firstMasterK8sIP, podCIDR, serviceCIDR)
	if err != nil {
		return fmt.Errorf("failed to initialize control plane on %s: %w", firstMaster.Name, err)
	}

	if err := k8s.SaveKubeconfigToFile(clusterInfo.Kubeconfig, clusterName, m.config.Kubernetes.KubeconfigDir); err != nil {
		return fmt.Errorf("failed to save kubeconfig for cluster %s: %w", clusterName, err)
	}

	// Join additional master nodes to the control plane
	if len(masterVMs) > 1 {
		log.Info("=== Joining additional control plane nodes ===")
		for _, masterVM := range masterVMs[1:] {
			masterMgmtIP, err := m.GetVMMgmtIP(masterVM.Name)
			if err != nil {
				return fmt.Errorf("failed to get mgmt IP for %s: %w", masterVM.Name, err)
			}

			masterExec := platform.NewSSHExecutor(&m.config.SSH, masterMgmtIP)
			if err := k8sMgr.JoinControlPlane(masterExec, masterVM.Name, clusterInfo); err != nil {
				return fmt.Errorf("failed to join control plane on %s: %w", masterVM.Name, err)
			}
		}
	}

	// Join worker nodes to the cluster
	workerVMs := clusterRoleMapping[config.ClusterRoleWorker]
	if len(workerVMs) > 0 {
		log.Info("=== Joining worker nodes ===")
		for _, workerVM := range workerVMs {
			workerMgmtIP, err := m.GetVMMgmtIP(workerVM.Name)
			if err != nil {
				return fmt.Errorf("failed to get mgmt IP for %s: %w", workerVM.Name, err)
			}

			workerExec := platform.NewSSHExecutor(&m.config.SSH, workerMgmtIP)
			if err := k8sMgr.JoinWorker(workerExec, workerVM.Name, clusterInfo); err != nil {
				return fmt.Errorf("failed to join worker node %s: %w", workerVM.Name, err)
			}
		}
	}

	kubeconfigPath := k8s.GetKubeconfigPath(clusterName, m.config.Kubernetes.KubeconfigDir)
	cniMgr, err := cni.NewCNIManagerWithKubeconfigFile(m.config, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to create CNI manager: %w", err)
	}

	if err := cniMgr.InstallCNI(cniType, clusterName, firstMasterK8sIP); err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	log.Info("✓ Kubernetes cluster %s setup complete", clusterName)
	return nil
}

// setupOVNKubernetesOffloadToDPUOVS configures OVS external_ids on all DPU VMs in the given
// cluster. OVS is already installed on VMs during InstallKubernetes; this
// sets the external_ids that ovnkube-node DPU mode needs.
func (m *VMManager) setupOVNKubernetesOffloadToDPUOVS(dpuClusterName string) error {
	pairs := m.config.GetHostDPUPairs(dpuClusterName)
	if len(pairs) == 0 {
		return nil
	}

	for _, pair := range pairs {
		mgmtIP, err := m.GetVMMgmtIP(pair.DPUNode)
		if err != nil {
			return fmt.Errorf("failed to get mgmt IP for DPU %s: %w", pair.DPUNode, err)
		}

		encapIP, err := m.GetVMK8sIP(pair.DPUNode)
		if err != nil {
			return fmt.Errorf("failed to get K8s IP for DPU %s: %w", pair.DPUNode, err)
		}

		sshExec := platform.NewSSHExecutor(&m.config.SSH, mgmtIP)
		if err := cni.SetupOVNKOffloadToDPUNodeOVS(sshExec, pair.DPUNode, pair.HostNode, encapIP); err != nil {
			return err
		}
	}
	return nil
}

// SetupAllK8sClusters sets up all Kubernetes clusters from the configuration.
// Clusters are processed in install order (host clusters before DPU clusters
// when offloading is enabled).
func (m *VMManager) SetupAllK8sClusters() error {
	clusterRoleMapping := m.config.GetClusterRoleMapping()
	for _, clusterCfg := range m.config.ClustersOrderedForInstall() {
		log.Info("\n=== Setting up Kubernetes cluster %s ===", clusterCfg.Name)
		if err := m.setupK8sCluster(clusterCfg.Name, clusterRoleMapping[clusterCfg.Name]); err != nil {
			return fmt.Errorf("failed to setup Kubernetes cluster %s: %w", clusterCfg.Name, err)
		}
	}
	return nil
}

// AssignDpuHostGatewayIPs SSHes into each DPU Host VM and assigns a gateway
// IP to eth0-0 so OVN-Kubernetes DPU Host mode can find an IPv4 address on the
// gateway interface. IPs are allocated from the top of the k8s subnet, skipping
// all IPs already used by VMs or the network gateway.
func (m *VMManager) AssignDpuHostGatewayIPs() error {
	if !m.config.IsOffloadDPU() {
		return nil
	}

	k8sNet := m.config.GetNetworkByType(config.K8sNetworkName)
	if k8sNet == nil || k8sNet.SubnetMask == "" {
		return nil
	}

	prefix, err := config.PrefixLenFromSubnetMask(k8sNet.SubnetMask)
	if err != nil {
		return err
	}

	_, subnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", k8sNet.Gateway, prefix))
	if err != nil {
		return err
	}

	var usedIPs []net.IP
	if gw := net.ParseIP(k8sNet.Gateway); gw != nil {
		usedIPs = append(usedIPs, gw)
	}
	for _, vm := range m.config.VMs {
		if ip := net.ParseIP(vm.K8sNodeIP); ip != nil {
			usedIPs = append(usedIPs, ip)
		}
	}

	gwIf := fmt.Sprintf(network.HostDataIfFmt, 0)

	for _, pair := range m.config.GetHostDPUPairs("") {
		gwIP, err := network.GetFreeIPv4AddressInSubnet(subnet, usedIPs)
		if err != nil {
			return fmt.Errorf("no free IP for %s: %w", pair.HostNode, err)
		}
		usedIPs = append(usedIPs, gwIP)

		gwCIDR := fmt.Sprintf("%s/%d", gwIP, prefix)

		mgmtIP, err := m.GetVMMgmtIP(pair.HostNode)
		if err != nil {
			return fmt.Errorf("failed to get mgmt IP for %s: %w", pair.HostNode, err)
		}

		sshExec := platform.NewSSHExecutor(&m.config.SSH, mgmtIP)
		if err := sshExec.RunCmd(log.LevelDebug, "ip", "addr", "add", gwCIDR, "dev", gwIf, "noprefixroute"); err != nil {
			return fmt.Errorf("failed to assign %s to %s on %s: %w", gwCIDR, gwIf, pair.HostNode, err)
		}

		log.Info("Assigned %s to %s on %s", gwCIDR, gwIf, pair.HostNode)
	}

	return nil
}
