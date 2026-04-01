package vm

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/log"
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

func kubeJoinEndpoint(joinCommand string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(joinCommand))
	if len(parts) < 3 || parts[0] != "kubeadm" || parts[1] != "join" {
		return "", fmt.Errorf("unexpected kubeadm join command format")
	}
	return parts[2], nil
}

func rewriteKubeJoinEndpoint(joinCommand, endpoint string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(joinCommand))
	if len(parts) < 3 || parts[0] != "kubeadm" || parts[1] != "join" {
		return "", fmt.Errorf("unexpected kubeadm join command format")
	}
	parts[2] = endpoint
	return strings.Join(parts, " "), nil
}

func endpointReachable(exec platform.CommandExecutor, endpoint string) bool {
	host, port, ok := strings.Cut(endpoint, ":")
	if !ok || host == "" || port == "" {
		return false
	}
	if _, err := strconv.Atoi(port); err != nil {
		return false
	}
	checkCmd := fmt.Sprintf("timeout 4 bash -lc \"cat < /dev/null > /dev/tcp/%s/%s\"", host, port)
	_, _, err := exec.ExecuteWithTimeout(checkCmd, 8*time.Second)
	return err == nil
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
	bareMetalRoleMapping := m.config.GetBareMetalClusterRoleMapping()[clusterName]

	clusterCfg := m.config.GetClusterConfig(clusterName)
	if clusterCfg == nil {
		return fmt.Errorf("cluster %s not found in configuration", clusterName)
	}

	// Verify cluster has at least one master node
	masterVMs := clusterRoleMapping[config.ClusterRoleMaster]
	if len(masterVMs) == 0 {
		return fmt.Errorf("no master nodes found for cluster %s", clusterName)
	}
	if len(bareMetalRoleMapping[config.ClusterRoleMaster]) > 0 {
		return fmt.Errorf("baremetal control-plane nodes are not yet supported for cluster %s", clusterName)
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
	clusterInfo, err := k8sMgr.InitializeControlPlane(firstMasterExec, firstMaster.Name, firstMasterMgmtIP, podCIDR, serviceCIDR, fmt.Sprintf("%s:6443", firstMasterMgmtIP), []string{firstMasterMgmtIP, firstMasterK8sIP})
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
	bareMetalWorkers := bareMetalRoleMapping[config.ClusterRoleWorker]
	if len(bareMetalWorkers) > 0 {
		log.Info("=== Adopting and joining baremetal worker nodes ===")
		defaultJoinEndpoint, err := kubeJoinEndpoint(clusterInfo.WorkerJoinCommand)
		if err != nil {
			return fmt.Errorf("failed to parse worker join command endpoint: %w", err)
		}
		fallbackJoinEndpoint := fmt.Sprintf("%s:6443", firstMasterMgmtIP)

		for _, node := range bareMetalWorkers {
			workerExec, err := m.ensureBareMetalSSHAccess(node)
			if err != nil {
				return fmt.Errorf("failed to establish SSH access to baremetal node %s: %w", node.Name, err)
			}

			if err := m.maybeApplyBootc(node, workerExec); err != nil {
				return fmt.Errorf("failed bootc reconcile on baremetal node %s: %w", node.Name, err)
			}

			// Recreate executor in case bootc rebooted and the previous SSH session is stale.
			workerExec = m.globalSSHExecutor(node.MgmtIP)
			if err := workerExec.WaitUntilReady(5 * time.Minute); err != nil {
				return fmt.Errorf("baremetal node %s not reachable after bootc processing: %w", node.Name, err)
			}

			if err := m.resetBareMetalNode(node, workerExec); err != nil {
				return fmt.Errorf("failed to reset baremetal node %s: %w", node.Name, err)
			}

			if err := k8sMgr.InstallKubernetes(workerExec, node.Name, m.config.Kubernetes.Version); err != nil {
				return fmt.Errorf("failed to install Kubernetes on baremetal node %s: %w", node.Name, err)
			}
			if err := k8sMgr.EnsureOVNBrInt(workerExec); err != nil {
				return fmt.Errorf("failed to ensure br-int on baremetal node %s: %w", node.Name, err)
			}
			if err := m.setKubeletNodeIP(node, workerExec); err != nil {
				return fmt.Errorf("failed to set kubelet node-ip on baremetal node %s: %w", node.Name, err)
			}
			if err := m.ensureHybridNetworking(node, firstMasterExec); err != nil {
				return fmt.Errorf("failed to prepare hybrid networking for baremetal node %s: %w", node.Name, err)
			}

			bareMetalJoinInfo := *clusterInfo
			chosenJoinEndpoint := defaultJoinEndpoint
			if !endpointReachable(workerExec, defaultJoinEndpoint) {
				if endpointReachable(workerExec, fallbackJoinEndpoint) {
					chosenJoinEndpoint = fallbackJoinEndpoint
				} else {
					return fmt.Errorf("baremetal node %s cannot reach kubeadm join endpoints %s or %s", node.Name, defaultJoinEndpoint, fallbackJoinEndpoint)
				}
			}

			if chosenJoinEndpoint != defaultJoinEndpoint {
				rewrittenJoinCmd, err := rewriteKubeJoinEndpoint(clusterInfo.WorkerJoinCommand, chosenJoinEndpoint)
				if err != nil {
					return fmt.Errorf("failed to rewrite worker join command endpoint: %w", err)
				}
				bareMetalJoinInfo.WorkerJoinCommand = rewrittenJoinCmd
				log.Warn("Baremetal node %s cannot reach default join endpoint %s; falling back to %s", node.Name, defaultJoinEndpoint, chosenJoinEndpoint)
			}

			if err := k8sMgr.JoinWorker(workerExec, node.Name, &bareMetalJoinInfo); err != nil {
				return fmt.Errorf("failed to join baremetal worker node %s: %w", node.Name, err)
			}
		}
	}

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

	if err := cniMgr.InstallAddons(clusterCfg.Addons, clusterName); err != nil {
		return fmt.Errorf("failed to install addons: %w", err)
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
