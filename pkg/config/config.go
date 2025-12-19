// Package config provides configuration management for the DPU simulator.
//
// This package handles loading and parsing YAML configuration files,
// providing type-safe access to all configuration parameters.
package config

import (
	"fmt"
	"os"
	"path/filepath"

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

	// Set defaults
	if err := cfg.setDefaults(); err != nil {
		return nil, fmt.Errorf("failed to set defaults: %w", err)
	}

	return &cfg, nil
}

// setDefaults sets default values for configuration fields
func (c *Config) setDefaults() error {
	// Set default attach_to for networks if not specified
	for i := range c.Networks {
		if c.Networks[i].AttachTo == "" {
			c.Networks[i].AttachTo = "any"
		}
	}

	// Set default Kubernetes version if not specified
	if c.Kubernetes.Version == "" {
		c.Kubernetes.Version = "1.33"
	}

	// Set default pod CIDR and service CIDR for clusters
	for i := range c.Kubernetes.Clusters {
		if c.Kubernetes.Clusters[i].PodCIDR == "" {
			c.Kubernetes.Clusters[i].PodCIDR = "10.244.0.0/16"
		}
		if c.Kubernetes.Clusters[i].ServiceCIDR == "" {
			c.Kubernetes.Clusters[i].ServiceCIDR = "10.245.0.0/16"
		}
		if c.Kubernetes.Clusters[i].CNI == "" {
			// Default CNI depends on deployment mode
			mode, _ := c.GetDeploymentMode()
			if mode == "kind" {
				c.Kubernetes.Clusters[i].CNI = "kindnet"
			} else {
				c.Kubernetes.Clusters[i].CNI = "flannel"
			}
		}
	}

	// Expand SSH key path
	if c.SSH.KeyPath != "" {
		if c.SSH.KeyPath[:2] == "~/" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			c.SSH.KeyPath = filepath.Join(homeDir, c.SSH.KeyPath[2:])
		}
	}

	// Expand SSH private key path
	if c.SSH.PrivateKeyPath != "" {
		if c.SSH.PrivateKeyPath[:2] == "~/" {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("failed to get home directory: %w", err)
			}
			c.SSH.PrivateKeyPath = filepath.Join(homeDir, c.SSH.PrivateKeyPath[2:])
		}
	} else if c.SSH.KeyPath != "" {
		// Fall back to KeyPath if PrivateKeyPath not specified
		c.SSH.PrivateKeyPath = c.SSH.KeyPath
	}

	if c.SSH.User == "" {
		c.SSH.User = "root"
	}

	if c.SSH.Password == "" {
		c.SSH.Password = "redhat"
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
		return "kind", nil
	} else if hasVMs {
		return "vm", nil
	}

	return "", fmt.Errorf("neither 'vms' nor 'kind' section found in config")
}

// IsKindMode returns true if the configuration is for Kind mode
func (c *Config) IsKindMode() bool {
	mode, _ := c.GetDeploymentMode()
	return mode == "kind"
}

// IsVMMode returns true if the configuration is for VM mode
func (c *Config) IsVMMode() bool {
	mode, _ := c.GetDeploymentMode()
	return mode == "vm"
}

// GetHostDPUPairs returns all host-DPU pairs from VM configuration
func (c *Config) GetHostDPUPairs() []HostDPUPair {
	var pairs []HostDPUPair

	// Build map of hosts by name
	hosts := make(map[string]VMConfig)
	for _, vm := range c.VMs {
		if vm.Type == "host" {
			hosts[vm.Name] = vm
		}
	}

	// Find DPUs and match with their hosts
	for _, vm := range c.VMs {
		if vm.Type == "dpu" && vm.Host != "" {
			if host, ok := hosts[vm.Host]; ok {
				pairs = append(pairs, HostDPUPair{
					Host: host,
					DPU:  vm,
				})
			}
		}
	}

	return pairs
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
func (c *Config) GetCNIType(clusterName string) string {
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
