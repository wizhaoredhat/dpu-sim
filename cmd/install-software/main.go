package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/cni"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/vm"
	"libvirt.org/go/libvirt"
)

var (
	configPath  string
	parallel    bool
	vmName      string
	skipCNI     bool
	clusterName string
)

var rootCmd = &cobra.Command{
	Use:   "install-software",
	Short: "Install software components on VMs or Kind clusters",
	Long:  `Install Kubernetes and CNI components on VMs via SSH or on Kind clusters via kubectl`,
	RunE:  runInstallSoftware,
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "config.yaml", "Path to configuration file")
	rootCmd.Flags().BoolVarP(&parallel, "parallel", "p", false, "Install on all VMs in parallel")
	rootCmd.Flags().StringVar(&vmName, "vm", "", "Install only on specific VM")
	rootCmd.Flags().BoolVar(&skipCNI, "skip-cni", false, "Skip CNI installation")
	rootCmd.Flags().StringVar(&clusterName, "cluster", "", "Target specific cluster (for Kind deployments)")
}

func runInstallSoftware(cmd *cobra.Command, args []string) error {
	fmt.Println("=== Software Installation ===")
	fmt.Printf("Configuration: %s\n", configPath)

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine deployment mode
	mode, err := cfg.GetDeploymentMode()
	if err != nil {
		return fmt.Errorf("failed to determine deployment mode: %w", err)
	}
	fmt.Printf("Deployment mode: %s\n", mode)

	switch mode {
	case "vm":
		return installK8sOnMachines(cfg)
	case "kind":
		return installOnKind(cfg)
	default:
		return fmt.Errorf("unknown deployment mode: %s", mode)
	}
}

// installK8sOnVMs installs all the software components
func installK8sOnMachines(cfg *config.Config) error {
	fmt.Println("\n=== Installing on VM-based deployment ===")

	// Connect to libvirt to get VM IPs
	conn, err := vm.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	defer conn.Close()

	k8sMgr := k8s.NewK8sMachineManager(cfg)
	cniMgr := cni.NewCNIManager(cfg)
	_ = cniMgr // TODO: use cniMgr for CNI installation

	// Install on each VM in the config file
	for _, vmCfg := range cfg.VMs {
		// Install only on specific VM if vmName is set
		if vmName != "" && vmCfg.Name != vmName {
			continue
		}

		// Get VM IP
		ip, err := vm.GetVMMgmtIP(conn, vmCfg.Name, cfg)
		if err != nil {
			fmt.Printf("Warning: failed to get IP for %s: %v\n", vmCfg.Name, err)
			continue
		}

		fmt.Printf("\n--- Installing on %s (%s) ---\n", vmCfg.Name, ip)

		// Get Kubernetes version from config
		k8sVersion := cfg.Kubernetes.Version
		if k8sVersion == "" {
			return fmt.Errorf("Kubernetes version is not set")
		}

		// Install Kubernetes
		if err := k8sMgr.InstallKubernetes(ip, vmCfg.Name, k8sVersion); err != nil {
			return fmt.Errorf("failed to install Kubernetes on %s: %w", vmCfg.Name, err)
		}
	}
	return nil
}

func setupK8sCluster(cfg *config.Config, conn *libvirt.Connect) error {
	masterVMs := make(map[string][]config.VMConfig)
	workerVMs := make(map[string][]config.VMConfig)
	for _, vmCfg := range cfg.VMs {
		k8s_role := vmCfg.K8sRole
		k8s_cluster := vmCfg.K8sCluster

		if k8s_role == "master" {
			masterVMs[k8s_cluster] = append(masterVMs[k8s_cluster], vmCfg)
		} else {
			workerVMs[k8s_cluster] = append(workerVMs[k8s_cluster], vmCfg)
		}
	}
	_ = workerVMs // TODO: use workerVMs

	for cluster, masters := range masterVMs {
		clusterCfg := cfg.GetClusterConfig(cluster)
		podCIDR := clusterCfg.PodCIDR
		serviceCIDR := clusterCfg.ServiceCIDR
		masterVM := masters[0]
		masterMgmtIP, err := vm.GetVMMgmtIP(conn, masterVM.Name, cfg)
		if err != nil {
			return fmt.Errorf("failed to get master IP: %w", err)
		}
		masterK8sIP := masterVM.K8sNodeIP

		// TODO: implement cluster setup
		_ = podCIDR
		_ = serviceCIDR
		_ = masterMgmtIP
		_ = masterK8sIP
	}

	return nil
}

/*
		for clusterName, masterVM := range masterVMs {
			if err := k8sMgr.InitializeControlPlane(masterVM.IP, masterVM.Name, clusterName); err != nil {
				return fmt.Errorf("failed to initialize control plane on %s: %w", masterVM.Name, err)
			}

			// Save kubeconfig
			kubeconfigPath := filepath.Join("kubeconfig", fmt.Sprintf("%s.yaml", vmCfg.K8sCluster))
			if err := os.MkdirAll("kubeconfig", 0755); err != nil {
				return fmt.Errorf("failed to create kubeconfig directory: %w", err)
			}
			if err := k8sMgr.GetKubeconfig(ip, kubeconfigPath); err != nil {
				fmt.Printf("Warning: failed to save kubeconfig: %v\n", err)
			}
		}

		// Install CNI on control plane nodes
		if vmCfg.K8sRole == "control-plane" && !skipCNI {
			clusterCfg := cfg.GetClusterConfig(vmCfg.K8sCluster)
			if clusterCfg != nil && clusterCfg.CNI != "" {
				cniType := cni.CNIType(clusterCfg.CNI)
				if err := cniMgr.InstallCNIOnVM(ip, vmCfg.Name, cniType, vmCfg.K8sCluster); err != nil {
					return fmt.Errorf("failed to install CNI on %s: %w", vmCfg.Name, err)
				}
			}
		}
	}

	// Join worker nodes
	if !skipK8s {
		if err := joinWorkerNodes(cfg, conn, k8sMgr); err != nil {
			return fmt.Errorf("failed to join worker nodes: %w", err)
		}
	}

	fmt.Println("\n✓ Software installation complete on VMs")
	return nil
}
*/

func installOnKind(cfg *config.Config) error {
	fmt.Println("\n=== Installing CNI on Kind clusters ===")

	cniMgr := cni.NewCNIManager(cfg)

	for _, cluster := range cfg.Kubernetes.Clusters {
		if clusterName != "" && cluster.Name != clusterName {
			continue
		}

		if skipCNI {
			fmt.Printf("Skipping CNI installation on cluster %s\n", cluster.Name)
			continue
		}

		fmt.Printf("\n--- Installing CNI on cluster %s ---\n", cluster.Name)

		kubeconfigPath := filepath.Join("kubeconfig", fmt.Sprintf("%s.yaml", cluster.Name))
		cniType := cni.CNIType(cluster.CNI)

		if err := cniMgr.InstallCNI(cniType, cluster.Name, kubeconfigPath); err != nil {
			return fmt.Errorf("failed to install CNI on cluster %s: %w", cluster.Name, err)
		}
	}

	fmt.Println("\n✓ CNI installation complete on Kind clusters")
	return nil
}

func joinWorkerNodes(cfg *config.Config, conn *libvirt.Connect, k8sMgr *k8s.K8sMachineManager) error {
	// Group VMs by cluster
	clusterVMs := make(map[string][]config.VMConfig)
	for _, vmCfg := range cfg.VMs {
		if vmCfg.K8sCluster != "" {
			clusterVMs[vmCfg.K8sCluster] = append(clusterVMs[vmCfg.K8sCluster], vmCfg)
		}
	}

	// For each cluster, join worker nodes
	for clusterName, vms := range clusterVMs {
		// Find control plane node
		var controlPlaneIP string
		for _, vmCfg := range vms {
			if vmCfg.K8sRole == "control-plane" {
				ip, err := vm.GetVMMgmtIP(conn, vmCfg.Name, cfg)
				if err != nil {
					return fmt.Errorf("failed to get control plane IP: %w", err)
				}
				controlPlaneIP = ip
				break
			}
		}

		if controlPlaneIP == "" {
			continue
		}

		// Get join command
		token, hash, err := k8sMgr.GetJoinCommand(controlPlaneIP)
		if err != nil {
			return fmt.Errorf("failed to get join command for cluster %s: %w", clusterName, err)
		}

		// Join worker nodes
		for _, vmCfg := range vms {
			if vmCfg.K8sRole == "worker" {
				ip, err := vm.GetVMMgmtIP(conn, vmCfg.Name, cfg)
				if err != nil {
					fmt.Printf("Warning: failed to get IP for worker %s: %v\n", vmCfg.Name, err)
					continue
				}

				if err := k8sMgr.JoinNode(ip, vmCfg.Name, controlPlaneIP, token, hash); err != nil {
					return fmt.Errorf("failed to join worker %s: %w", vmCfg.Name, err)
				}
			}
		}
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
