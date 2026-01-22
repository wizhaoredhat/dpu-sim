package config

import (
	"fmt"
	"net"
)

const MgmtNetworkName = "mgmt"
const K8sNetworkName = "k8s"
const VMDeploymentMode = "vm"
const KindDeploymentMode = "kind"
const VMHostType = "host"
const VMDPUType = "dpu"

// Config represents the complete DPU simulator configuration
type Config struct {
	Networks        []NetworkConfig  `yaml:"networks"`
	VMs             []VMConfig       `yaml:"vms"`
	Kind            *KindConfig      `yaml:"kind,omitempty"`
	OperatingSystem OSConfig         `yaml:"operating_system"`
	SSH             SSHConfig        `yaml:"ssh"`
	Kubernetes      KubernetesConfig `yaml:"kubernetes"`
}

// NetworkConfig represents a network configuration
type NetworkConfig struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	BridgeName string `yaml:"bridge_name"`
	Gateway    string `yaml:"gateway,omitempty"`
	SubnetMask string `yaml:"subnet_mask,omitempty"`
	DHCPStart  string `yaml:"dhcp_start,omitempty"`
	DHCPEnd    string `yaml:"dhcp_end,omitempty"`
	Mode       string `yaml:"mode"`
	NICModel   string `yaml:"nic_model"`
	UseOVS     bool   `yaml:"use_ovs,omitempty"`
	AttachTo   string `yaml:"attach_to,omitempty"`
}

// VMConfig represents a virtual machine configuration
type VMConfig struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	K8sCluster string `yaml:"k8s_cluster,omitempty"`
	K8sRole    string `yaml:"k8s_role,omitempty"`
	K8sNodeMAC string `yaml:"k8s_node_mac,omitempty"`
	K8sNodeIP  string `yaml:"k8s_node_ip,omitempty"`
	Host       string `yaml:"host,omitempty"`
	Memory     int    `yaml:"memory"`
	VCPUs      int    `yaml:"vcpus"`
	DiskSize   int    `yaml:"disk_size"`
}

// KindConfig represents Kind cluster configuration
type KindConfig struct {
	Nodes []KindNodeConfig `yaml:"nodes"`
}

// KindNodeConfig represents a Kind node configuration
type KindNodeConfig struct {
	Role string `yaml:"role"`
}

// OSConfig represents operating system configuration
type OSConfig struct {
	ImageURL  string `yaml:"image_url"`
	ImageName string `yaml:"image_name"`
}

// SSHConfig represents SSH configuration
type SSHConfig struct {
	User     string `yaml:"user"`
	KeyPath  string `yaml:"key_path"`
	Password string `yaml:"password"`
}

// KubernetesConfig represents Kubernetes configuration
type KubernetesConfig struct {
	Version       string          `yaml:"version"`
	KubeconfigDir string          `yaml:"kubeconfig_dir,omitempty"`
	Clusters      []ClusterConfig `yaml:"clusters"`
}

// GetKubeconfigDir returns the kubeconfig directory, defaulting to "kubeconfig" if not set
func (k *KubernetesConfig) GetKubeconfigDir() string {
	if k.KubeconfigDir == "" {
		return "kubeconfig"
	}
	return k.KubeconfigDir
}

// ClusterConfig represents a Kubernetes cluster configuration
type ClusterConfig struct {
	Name        string `yaml:"name"`
	PodCIDR     string `yaml:"pod_cidr"`
	ServiceCIDR string `yaml:"service_cidr"`
	CNI         string `yaml:"cni"`
}

// HostDPULink represents network link information between a host and DPU
type HostDPULink struct {
	NetworkName string // Network name in format "h2d-{host_name}-{dpu_name}"
}

// DPUConnection represents a DPU and its link to the host
type DPUConnection struct {
	DPU  VMConfig
	Link HostDPULink
}

// HostDPUMapping represents a host and all its connected DPUs
type HostDPUMapping struct {
	Host        VMConfig
	Connections []DPUConnection
}

type ClusterRole string

const (
	ClusterRoleMaster ClusterRole = "master"
	ClusterRoleWorker ClusterRole = "worker"
)

// ClusterRoleMapping maps roles (master/worker) to their VM configurations
type ClusterRoleMapping map[ClusterRole][]VMConfig

// GetSubnetCIDR returns the subnet in CIDR notation (e.g., "192.168.120.0/24")
// derived from the gateway and subnet mask
func (n *NetworkConfig) GetSubnetCIDR() string {
	if n.Gateway == "" || n.SubnetMask == "" {
		return ""
	}

	gwIP := net.ParseIP(n.Gateway)
	maskIP := net.ParseIP(n.SubnetMask)
	if gwIP == nil || maskIP == nil {
		return ""
	}

	mask := net.IPMask(maskIP.To4())
	networkAddr := gwIP.Mask(mask)
	ones, _ := mask.Size()

	return fmt.Sprintf("%s/%d", networkAddr, ones)
}
