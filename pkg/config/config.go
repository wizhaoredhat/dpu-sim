// Package config provides configuration management for the DPU simulator.
//
// This package handles loading and parsing YAML configuration files,
// providing type-safe access to all configuration parameters.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads configuration from a YAML file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}

	if err := cfg.validateAndSetDefaults(); err != nil {
		return nil, fmt.Errorf("config validation and set defaults failed: %w", err)
	}

	return &cfg, nil
}

// validate and set defaults checks that all mandatory fields in the configuration are set
// and sets default values for optional fields.
func (c *Config) validateAndSetDefaults() error {
	var errors []string

	// Validate networks
	for i, net := range c.Networks {
		if net.Name == "" {
			errors = append(errors, fmt.Sprintf("networks[%d]: 'name' is required", i))
		}
		if net.Type == "" {
			errors = append(errors, fmt.Sprintf("networks[%d] (%s): 'type' is required", i, net.Name))
		}
		if net.BridgeName == "" {
			errors = append(errors, fmt.Sprintf("networks[%d] (%s): 'bridge_name' is required", i, net.Name))
		}

		if c.Networks[i].Mode == "" {
			c.Networks[i].Mode = "nat"
		}
		if c.Networks[i].NICModel == "" {
			c.Networks[i].NICModel = "virtio"
		}
		if c.Networks[i].UseOVS == false {
			c.Networks[i].UseOVS = false
		}
		if c.Networks[i].AttachTo == "" {
			c.Networks[i].AttachTo = "any"
		}
	}

	// Validate VMs
	for i, vm := range c.VMs {
		if vm.Name == "" {
			errors = append(errors, fmt.Sprintf("vms[%d]: 'name' is required", i))
		}
		if vm.Type == "" {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'type' is required", i, vm.Name))
		} else if vm.Type != VMHostType && vm.Type != VMDPUType {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'type' must be '%s' or '%s', got '%s'", i, vm.Name, VMHostType, VMDPUType, vm.Type))
		}
		if vm.K8sCluster == "" {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'k8s_cluster' is required", i, vm.Name))
		}
		if vm.K8sRole == "" {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'k8s_role' is required", i, vm.Name))
		}
		if vm.K8sNodeMAC == "" {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'k8s_node_mac' is required", i, vm.Name))
		}
		if vm.K8sNodeIP == "" {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'k8s_node_ip' is required", i, vm.Name))
		}
		if vm.Memory <= 0 {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'memory' must be greater than 0", i, vm.Name))
		}
		if vm.VCPUs <= 0 {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'vcpus' must be greater than 0", i, vm.Name))
		}
		if vm.DiskSize <= 0 {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'disk_size' must be greater than 0", i, vm.Name))
		}
		// If DPU type, host field should reference an existing host
		if vm.Type == VMDPUType && vm.Host == "" {
			errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'host' is required for %s type VMs", i, vm.Name, VMDPUType))
		}
	}

	// Validate operating system (required for VM mode)
	if len(c.VMs) > 0 {
		if c.OperatingSystem.ImageURL == "" {
			errors = append(errors, "VMs are defined, operating_system: 'image_url' is required")
		}
		if c.OperatingSystem.ImageName == "" {
			errors = append(errors, "VMs are defined, operating_system: 'image_name' is required")
		}
	}

	if c.SSH.User == "" {
		c.SSH.User = "root"
	}

	if c.SSH.Password == "" {
		c.SSH.Password = "redhat"
	}

	// Expand tilde in SSH key path
	if c.SSH.KeyPath != "" {
		expanded, err := expandTilde(c.SSH.KeyPath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("ssh.key_path: failed to expand path: %v", err))
		} else {
			c.SSH.KeyPath = expanded
		}
	}

	// Validate Kind nodes (if Kind mode)
	if c.Kind != nil {
		for i, node := range c.Kind.Nodes {
			if node.Role != "" && node.Role != "control-plane" && node.Role != "worker" {
				errors = append(errors, fmt.Sprintf("kind.nodes[%d]: 'role' must be 'control-plane' or 'worker', got '%s'", i, node.Role))
			}
		}
	}

	// Set default Kubernetes version if not specified
	if c.Kubernetes.Version == "" {
		c.Kubernetes.Version = "1.33"
	}

	if c.Kubernetes.KubeconfigDir == "" {
		c.Kubernetes.KubeconfigDir = "kubeconfig"
	}

	// Validate Kubernetes clusters
	for i, cluster := range c.Kubernetes.Clusters {
		if cluster.Name == "" {
			errors = append(errors, fmt.Sprintf("kubernetes.clusters[%d]: 'name' is required", i))
		}
		if c.Kubernetes.Clusters[i].PodCIDR == "" {
			c.Kubernetes.Clusters[i].PodCIDR = "10.244.0.0/16"
		}
		if c.Kubernetes.Clusters[i].ServiceCIDR == "" {
			c.Kubernetes.Clusters[i].ServiceCIDR = "10.245.0.0/16"
		}
		if c.Kubernetes.Clusters[i].CNI == "" {
			errors = append(errors, fmt.Sprintf("kubernetes.clusters[%d]: 'cni' is required", i))
		} else if c.Kubernetes.Clusters[i].CNI != "ovn-kubernetes" && c.Kubernetes.Clusters[i].CNI != "flannel" && c.Kubernetes.Clusters[i].CNI != "kindnet" {
			errors = append(errors, fmt.Sprintf("kubernetes.clusters[%d]: 'cni' must be 'ovn-kubernetes', 'flannel', or 'kindnet', got '%s'", i, c.Kubernetes.Clusters[i].CNI))
		}
	}

	// Validate registry configuration
	if c.Registry != nil {
		for i, container := range c.Registry.Containers {
			if container.Name == "" {
				errors = append(errors, fmt.Sprintf("registry.containers[%d]: 'name' is required", i))
			}
			if container.CNI == "" {
				errors = append(errors, fmt.Sprintf("registry.containers[%d] (%s): 'cni' is required", i, container.Name))
			} else if container.CNI != "ovn-kubernetes" {
				errors = append(errors, fmt.Sprintf("registry.containers[%d] (%s): 'cni' must be 'ovn-kubernetes', got '%s'", i, container.Name, container.CNI))
			}
			if container.Tag == "" {
				errors = append(errors, fmt.Sprintf("registry.containers[%d] (%s): 'tag' is required", i, container.Name))
			}
		}
	}

	// Validate DPU host references exist
	if len(c.VMs) > 0 {
		hostNames := make(map[string]bool)
		for _, vm := range c.VMs {
			if vm.Type == VMHostType {
				hostNames[vm.Name] = true
			}
		}
		for i, vm := range c.VMs {
			if vm.Type == VMDPUType && vm.Host != "" {
				if !hostNames[vm.Host] {
					errors = append(errors, fmt.Sprintf("vms[%d] (%s): 'host' references non-existent host '%s'", i, vm.Name, vm.Host))
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("validation errors:\n  - %s", strings.Join(errors, "; "))
	}

	return nil
}

// GetDeploymentMode determines the deployment mode based on configuration
func (c *Config) GetDeploymentMode() (string, error) {
	hasVMs := len(c.VMs) > 0
	hasKind := c.Kind != nil && len(c.Kind.Nodes) > 0

	if hasKind && hasVMs {
		return "", fmt.Errorf("both 'vms' and 'kind' sections found in config - please use only one deployment mode")
	} else if hasKind {
		return KindDeploymentMode, nil
	} else if hasVMs {
		return VMDeploymentMode, nil
	}

	return "", fmt.Errorf("neither 'vms' nor 'kind' section found in config")
}

// IsKindMode returns true if the configuration is for Kind mode
func (c *Config) IsKindMode() bool {
	mode, _ := c.GetDeploymentMode()
	return mode == KindDeploymentMode
}

// IsVMMode returns true if the configuration is for VM mode
func (c *Config) IsVMMode() bool {
	mode, _ := c.GetDeploymentMode()
	return mode == VMDeploymentMode
}

// GetHostDPUMappings returns all host-to-DPU mappings from VM configuration
func (c *Config) GetHostDPUMappings() []HostDPUMapping {
	// Build map of hosts by name for lookup
	hosts := make(map[string]VMConfig)
	for _, vm := range c.VMs {
		if vm.Type == VMHostType {
			hosts[vm.Name] = vm
		}
	}

	// Build map of host name -> DPU connections
	hostConnections := make(map[string][]DPUConnection)
	for _, vm := range c.VMs {
		if vm.Type == VMDPUType && vm.Host != "" {
			if _, ok := hosts[vm.Host]; ok {
				conn := DPUConnection{
					DPU: vm,
					Link: HostDPULink{
						NetworkName: fmt.Sprintf("h2d-%s-%s", vm.Host, vm.Name),
					},
				}
				hostConnections[vm.Host] = append(hostConnections[vm.Host], conn)
			}
		}
	}

	var mappings []HostDPUMapping
	for hostName, connections := range hostConnections {
		mappings = append(mappings, HostDPUMapping{
			Host:        hosts[hostName],
			Connections: connections,
		})
	}

	return mappings
}

// GetClusterRoleMapping returns a mapping of cluster names to roles and their VMs.
// The returned map has cluster names as keys, and values are ClusterRoleMapping
// which maps roles (master/worker) to slices of VMConfig.
// Example structure: {"cluster1": {"master": [vm1], "worker": [vm2, vm3]}}
func (c *Config) GetClusterRoleMapping() map[string]ClusterRoleMapping {
	result := make(map[string]ClusterRoleMapping)

	for _, vm := range c.VMs {
		clusterName := vm.K8sCluster
		role := ClusterRole(vm.K8sRole)

		if _, ok := result[clusterName]; !ok {
			result[clusterName] = make(ClusterRoleMapping)
		}

		result[clusterName][role] = append(result[clusterName][role], vm)
	}

	return result
}

// GetClusterNames returns all cluster names from configuration
func (c *Config) GetClusterNames() []string {
	names := make([]string, len(c.Kubernetes.Clusters))
	for i, cluster := range c.Kubernetes.Clusters {
		names[i] = cluster.Name
	}
	return names
}

// GetClusterConfig returns the cluster configuration by name
func (c *Config) GetClusterConfig(name string) *ClusterConfig {
	for i := range c.Kubernetes.Clusters {
		if c.Kubernetes.Clusters[i].Name == name {
			return &c.Kubernetes.Clusters[i]
		}
	}
	return nil
}

// GetCNIType returns the CNI type for a cluster
func (c *Config) GetCNIType(clusterName string) CNIType {
	cluster := c.GetClusterConfig(clusterName)
	if cluster == nil {
		// Return default based on mode
		if c.IsKindMode() {
			return "kindnet"
		}
		return "flannel"
	}
	return cluster.CNI
}

// GetKindControlPlaneCount returns the number of control-plane nodes in Kind config
func (c *Config) GetKindControlPlaneCount() int {
	if c.Kind == nil {
		return 0
	}
	count := 0
	for _, node := range c.Kind.Nodes {
		if node.Role == "control-plane" {
			count++
		}
	}
	return count
}

// GetKindWorkerCount returns the number of worker nodes in Kind config
func (c *Config) GetKindWorkerCount() int {
	if c.Kind == nil {
		return 0
	}
	count := 0
	for _, node := range c.Kind.Nodes {
		if node.Role == "worker" || node.Role == "" {
			count++
		}
	}
	return count
}

// GetNetworkByType returns the network configuration by type (e.g., "mgmt" or "k8s")
func (c *Config) GetNetworkByType(networkType string) *NetworkConfig {
	for i := range c.Networks {
		if c.Networks[i].Type == networkType {
			return &c.Networks[i]
		}
	}
	return nil
}

// GetNetworkByName returns the network configuration by name
func (c *Config) GetNetworkByName(name string) *NetworkConfig {
	for i := range c.Networks {
		if c.Networks[i].Name == name {
			return &c.Networks[i]
		}
	}
	return nil
}

// HasRegistry returns true if the configuration includes a local registry
func (c *Config) HasRegistry() bool {
	return c.Registry != nil && len(c.Registry.Containers) > 0
}

// GetRegistryContainerForCNI returns the registry container config for a
// given CNI type, or nil if none is configured.
func (c *Config) GetRegistryContainerForCNI(cniType CNIType) *RegistryContainerConfig {
	if c.Registry == nil {
		return nil
	}
	for i := range c.Registry.Containers {
		if CNIType(c.Registry.Containers[i].CNI) == cniType {
			return &c.Registry.Containers[i]
		}
	}
	return nil
}

func (c *Config) GetRegistryContainer(cniType CNIType) string {
	if c.Registry == nil {
		return ""
	}
	regContainer := c.GetRegistryContainerForCNI(cniType)
	return fmt.Sprintf("localhost:%s/%s", DefaultRegistryPort, regContainer.Tag)
}

func (c *Config) GetRegistryEndpoint() string {
	return fmt.Sprintf("localhost:%s", DefaultRegistryPort)
}

func (c *Config) GetRegistryPort() string {
	return DefaultRegistryPort
}

func (c *Config) GetRegistryImage() string {
	return DefaultRegistryImage
}

func (c *Config) GetRegistryContainerName() string {
	return DefaultRegistryContainerName
}

// expandTilde expands a leading ~ in a path to the user's home directory
func expandTilde(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	if path == "~" {
		return home, nil
	}

	// Handle ~/path format
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}

	// Just ~ prefix without slash - return as is (edge case like ~username not supported)
	return path, nil
}
