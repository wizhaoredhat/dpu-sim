package config

import "fmt"

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
	Subnet     string `yaml:"subnet,omitempty"`
	SubnetMask string `yaml:"subnet_mask,omitempty"`
	Netmask    string `yaml:"netmask,omitempty"`
	DHCPStart  string `yaml:"dhcp_start,omitempty"`
	DHCPEnd    string `yaml:"dhcp_end,omitempty"`
	Mode       string `yaml:"mode"`
	NICModel   string `yaml:"nic_model"`
	UseOVS     bool   `yaml:"use_ovs,omitempty"`
	AttachTo   string `yaml:"attach_to,omitempty"`
}

// VMConfig represents a virtual machine configuration
type VMConfig struct {
	Name       string   `yaml:"name"`
	Type       string   `yaml:"type"`
	K8sCluster string   `yaml:"k8s_cluster,omitempty"`
	K8sRole    string   `yaml:"k8s_role,omitempty"`
	K8sNodeMAC string   `yaml:"k8s_node_mac,omitempty"`
	K8sNodeIP  string   `yaml:"k8s_node_ip,omitempty"`
	Host       string   `yaml:"host,omitempty"`
	Memory     int      `yaml:"memory"`
	VCPUs      int      `yaml:"vcpus"`
	DiskSize   int      `yaml:"disk_size"`
	Networks   []string `yaml:"networks,omitempty"`
	IP         string   `yaml:"ip,omitempty"`
	Packages   []string `yaml:"packages,omitempty"`
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
	Name             string `yaml:"name"`
	ImageURL         string `yaml:"image_url"`
	CloudImageURL    string `yaml:"cloud_image_url"`
	ImageName        string `yaml:"image_name"`
	CloudInitISOName string `yaml:"cloud_init_iso_name"`
}

// SSHConfig represents SSH configuration
type SSHConfig struct {
	User           string `yaml:"user"`
	KeyPath        string `yaml:"key_path"`
	PrivateKeyPath string `yaml:"private_key_path"`
	Password       string `yaml:"password"`
}

// KubernetesConfig represents Kubernetes configuration
type KubernetesConfig struct {
	Version  string          `yaml:"version"`
	Clusters []ClusterConfig `yaml:"clusters"`
}

// ClusterConfig represents a Kubernetes cluster configuration
type ClusterConfig struct {
	Name          string          `yaml:"name"`
	PodCIDR       string          `yaml:"pod_cidr"`
	ServiceCIDR   string          `yaml:"service_cidr"`
	CNI           string          `yaml:"cni"`
	LocalRegistry *RegistryConfig `yaml:"local_registry,omitempty"`
}

// RegistryConfig represents a local container registry configuration
type RegistryConfig struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
}

// HostDPUPair represents a host-DPU pairing
type HostDPUPair struct {
	Host VMConfig
	DPU  VMConfig
}

// GetSubnetCIDR returns the subnet in CIDR notation (e.g., "192.168.120.0/24")
// derived from the gateway and subnet mask
func (n *NetworkConfig) GetSubnetCIDR() string {
	if n.Gateway == "" || n.SubnetMask == "" {
		return ""
	}
	// Convert subnet mask to prefix length
	prefix := subnetMaskToPrefix(n.SubnetMask)
	// Calculate network address from gateway and subnet mask
	networkAddr := calculateNetworkAddress(n.Gateway, n.SubnetMask)
	return networkAddr + "/" + prefix
}

// subnetMaskToPrefix converts a subnet mask to prefix length (e.g., "255.255.255.0" -> "24")
func subnetMaskToPrefix(mask string) string {
	parts := splitIPv4(mask)
	if len(parts) != 4 {
		return "24" // default to /24
	}
	bits := 0
	for _, p := range parts {
		for p > 0 {
			bits += int(p & 1)
			p >>= 1
		}
	}
	return fmt.Sprintf("%d", bits)
}

// calculateNetworkAddress calculates the network address from gateway and subnet mask
func calculateNetworkAddress(gateway, mask string) string {
	gwParts := splitIPv4(gateway)
	maskParts := splitIPv4(mask)
	if len(gwParts) != 4 || len(maskParts) != 4 {
		return gateway // fallback to gateway if parsing fails
	}
	return fmt.Sprintf("%d.%d.%d.%d",
		gwParts[0]&maskParts[0],
		gwParts[1]&maskParts[1],
		gwParts[2]&maskParts[2],
		gwParts[3]&maskParts[3])
}

// splitIPv4 splits an IPv4 address string into its octets
func splitIPv4(ip string) []uint8 {
	var parts []uint8
	var current uint8
	for _, c := range ip {
		if c == '.' {
			parts = append(parts, current)
			current = 0
		} else if c >= '0' && c <= '9' {
			current = current*10 + uint8(c-'0')
		}
	}
	parts = append(parts, current)
	return parts
}
