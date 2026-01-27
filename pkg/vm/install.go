package vm

import (
	"fmt"
	"time"

	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/platform"
)

// InstallKubernetes installs the software components on a VM
func (m *VMManager) InstallKubernetes(vmName string) error {
	fmt.Println("\n=== Installing Kubernetes on VM-based deployment ===")

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

		fmt.Printf("\n--- Installing on %s (%s) ---\n", vmCfg.Name, mgmtIP)

		// Get Kubernetes version from config
		k8sVersion := m.config.Kubernetes.Version
		if k8sVersion == "" {
			return fmt.Errorf("Kubernetes version is not set")
		}

		cmdExec := platform.NewSSHExecutor(&m.config.SSH, mgmtIP)
		if err := cmdExec.WaitUntilReady(5 * time.Minute); err != nil {
			return fmt.Errorf("failed to wait for SSH on %s: %w", vmCfg.Name, err)
		}

		// Install Kubernetes
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

			fmt.Printf("Setting up OVN br-ex on %s (%s) - Mgmt IP: %s, K8s IP: %s\n",
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

	if cniType == string(cni.CNIOVNKubernetes) {
		if err := m.setupOVNBrExForCluster(clusterRoleMapping, k8sMgr); err != nil {
			return err
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

	fmt.Printf("=== Initializing first control plane node: %s ===\n", firstMaster.Name)
	clusterInfo, err := k8sMgr.InitializeControlPlane(firstMasterExec, firstMaster.Name, firstMasterK8sIP, podCIDR, serviceCIDR)
	if err != nil {
		return fmt.Errorf("failed to initialize control plane on %s: %w", firstMaster.Name, err)
	}

	if err := k8s.SaveKubeconfigToFile(clusterInfo.Kubeconfig, clusterName, m.config.Kubernetes.KubeconfigDir); err != nil {
		return fmt.Errorf("failed to save kubeconfig for cluster %s: %w", clusterName, err)
	}

	cniMgr, err := cni.NewCNIManagerWithKubeconfig(m.config, clusterInfo.Kubeconfig)
	if err != nil {
		return fmt.Errorf("failed to create CNI manager: %w", err)
	}

	if err := cniMgr.InstallCNI(cni.CNIType(cniType), clusterName, firstMasterK8sIP); err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	// Join additional master nodes to the control plane
	if len(masterVMs) > 1 {
		fmt.Printf("=== Joining additional control plane nodes ===\n")
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
		fmt.Printf("=== Joining worker nodes ===\n")
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

	fmt.Printf("✓ Kubernetes cluster %s setup complete\n", clusterName)
	return nil
}

// SetupAllK8sClusters sets up all Kubernetes clusters from the configuration
func (m *VMManager) SetupAllK8sClusters() error {
	clusterRoleMapping := m.config.GetClusterRoleMapping()
	for _, clusterName := range m.config.GetClusterNames() {
		fmt.Printf("=== Setting up Kubernetes cluster %s ===\n", clusterName)
		if err := m.setupK8sCluster(clusterName, clusterRoleMapping[clusterName]); err != nil {
			return fmt.Errorf("failed to setup Kubernetes cluster %s: %w", clusterName, err)
		}
	}
	return nil
}
