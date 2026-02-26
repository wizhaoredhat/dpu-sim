package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
networks:
  - name: "mgmt-network"
    type: "mgmt"
    bridge_name: "virbr-mgmt"
    gateway: "192.168.120.1"
    subnet_mask: "255.255.255.0"
    dhcp_start: "192.168.120.10"
    dhcp_end: "192.168.120.100"
    mode: "nat"
    nic_model: "virtio"

vms:
  - name: "master-1"
    type: "host"
    k8s_cluster: "cluster-1"
    k8s_role: "master"
    k8s_node_mac: "52:54:00:00:01:11"
    k8s_node_ip: "192.168.123.11"
    memory: 4096
    vcpus: 2
    disk_size: 20

operating_system:
  image_url: https://example.com/fedora.qcow2
  image_name: "Fedora-x86_64.qcow2"

ssh:
  user: "root"
  key_path: "~/.ssh/id_rsa"
  password: "redhat"

kubernetes:
  version: "1.33"
  clusters:
    - name: "cluster-1"
      pod_cidr: "10.244.0.0/16"
      service_cidr: "10.245.0.0/16"
      cni: "ovn-kubernetes"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err)

	// Test loading config
	cfg, err := LoadConfig(configPath)
	require.NoError(t, err)
	assert.NotNil(t, cfg)

	// Verify basic fields
	assert.Equal(t, 1, len(cfg.Networks))
	assert.Equal(t, "mgmt-network", cfg.Networks[0].Name)
	assert.Equal(t, "any", cfg.Networks[0].AttachTo) // Default value

	assert.Equal(t, 1, len(cfg.VMs))
	assert.Equal(t, "master-1", cfg.VMs[0].Name)
	assert.Equal(t, "host", cfg.VMs[0].Type)

	assert.Equal(t, "1.33", cfg.Kubernetes.Version)
	assert.Equal(t, 1, len(cfg.Kubernetes.Clusters))
	assert.Equal(t, "cluster-1", cfg.Kubernetes.Clusters[0].Name)
}

func TestGetDeploymentMode(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expected    string
		expectError bool
	}{
		{
			name: "VM mode",
			config: Config{
				VMs: []VMConfig{{Name: "vm1"}},
			},
			expected:    VMDeploymentMode,
			expectError: false,
		},
		{
			name: "Kind mode",
			config: Config{
				Kind: &KindConfig{
					Nodes: []KindNodeConfig{{Role: "control-plane"}},
				},
			},
			expected:    KindDeploymentMode,
			expectError: false,
		},
		{
			name: "Both modes - error",
			config: Config{
				VMs: []VMConfig{{Name: "vm1"}},
				Kind: &KindConfig{
					Nodes: []KindNodeConfig{{Role: "control-plane"}},
				},
			},
			expected:    "",
			expectError: true,
		},
		{
			name:        "No mode - error",
			config:      Config{},
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := tt.config.GetDeploymentMode()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, mode)
			}
		})
	}
}

func TestGetHostDPUMappings(t *testing.T) {
	cfg := Config{
		VMs: []VMConfig{
			{Name: "host-1", Type: VMHostType},
			{Name: "dpu-1a", Type: VMDPUType, Host: "host-1"},
			{Name: "dpu-1b", Type: VMDPUType, Host: "host-1"},
			{Name: "host-2", Type: VMHostType},
			{Name: "dpu-2", Type: VMDPUType, Host: "host-2"},
			{Name: "standalone", Type: VMHostType},
		},
	}

	mappings := cfg.GetHostDPUMappings()
	assert.Equal(t, 2, len(mappings))

	// Build a map for easier verification (order may vary due to map iteration)
	mappingsByHost := make(map[string]HostDPUMapping)
	for _, m := range mappings {
		mappingsByHost[m.Host.Name] = m
	}

	// Verify host-1 has 2 DPUs
	host1Mapping := mappingsByHost["host-1"]
	assert.Equal(t, "host-1", host1Mapping.Host.Name)
	assert.Equal(t, 2, len(host1Mapping.Connections))

	// Verify network names follow h2d-{host}-{dpu} format
	dpuNames := make(map[string]string)
	for _, conn := range host1Mapping.Connections {
		dpuNames[conn.DPU.Name] = conn.Link.NetworkName
	}
	assert.Equal(t, "h2d-host-1-dpu-1a", dpuNames["dpu-1a"])
	assert.Equal(t, "h2d-host-1-dpu-1b", dpuNames["dpu-1b"])

	// Verify host-2 has 1 DPU
	host2Mapping := mappingsByHost["host-2"]
	assert.Equal(t, "host-2", host2Mapping.Host.Name)
	assert.Equal(t, 1, len(host2Mapping.Connections))
	assert.Equal(t, "dpu-2", host2Mapping.Connections[0].DPU.Name)
	assert.Equal(t, "h2d-host-2-dpu-2", host2Mapping.Connections[0].Link.NetworkName)
}

func TestGetClusterConfig(t *testing.T) {
	cfg := Config{
		Kubernetes: KubernetesConfig{
			Clusters: []ClusterConfig{
				{Name: "cluster-1", PodCIDR: "10.244.0.0/16"},
				{Name: "cluster-2", PodCIDR: "10.245.0.0/16"},
			},
		},
	}

	// Test existing cluster
	cluster := cfg.GetClusterConfig("cluster-1")
	require.NotNil(t, cluster)
	assert.Equal(t, "cluster-1", cluster.Name)
	assert.Equal(t, "10.244.0.0/16", cluster.PodCIDR)

	// Test non-existing cluster
	cluster = cfg.GetClusterConfig("non-existent")
	assert.Nil(t, cluster)
}

func TestGetKindNodeCounts(t *testing.T) {
	cfg := Config{
		Kind: &KindConfig{
			Nodes: []KindNodeConfig{
				{Role: "control-plane"},
				{Role: "worker"},
				{Role: "worker"},
				{Role: ""}, // Empty role defaults to worker
			},
		},
	}

	assert.Equal(t, 1, cfg.GetKindControlPlaneCount())
	assert.Equal(t, 3, cfg.GetKindWorkerCount())
}

func TestIsKindMode(t *testing.T) {
	vmConfig := Config{
		VMs: []VMConfig{{Name: "vm1"}},
	}
	assert.False(t, vmConfig.IsKindMode())
	assert.True(t, vmConfig.IsVMMode())

	kindConfig := Config{
		Kind: &KindConfig{
			Nodes: []KindNodeConfig{{Role: "control-plane"}},
		},
	}
	assert.True(t, kindConfig.IsKindMode())
	assert.False(t, kindConfig.IsVMMode())
}

func TestValidateOperatingSystemAllowsImageRef(t *testing.T) {
	cfg := Config{
		Networks: []NetworkConfig{
			{
				Name:       "mgmt-network",
				Type:       MgmtNetworkName,
				BridgeName: "virbr-mgmt",
				Mode:       "nat",
				NICModel:   "virtio",
			},
		},
		VMs: []VMConfig{
			{
				Name:       "master-1",
				Type:       VMHostType,
				K8sCluster: "cluster-1",
				K8sRole:    string(ClusterRoleMaster),
				K8sNodeMAC: "52:54:00:00:01:11",
				K8sNodeIP:  "192.168.123.11",
				Memory:     4096,
				VCPUs:      2,
				DiskSize:   20,
			},
		},
		OperatingSystem: OSConfig{
			ImageRef:  "ghcr.io/example/fedora-cloud:43",
			ImageName: "Fedora-x86_64.qcow2",
		},
		Kubernetes: KubernetesConfig{
			Clusters: []ClusterConfig{
				{Name: "cluster-1", CNI: CNIOVNKubernetes},
			},
		},
	}

	err := cfg.validateAndSetDefaults()
	require.NoError(t, err)
}

func TestValidateOperatingSystemRequiresURLOrRef(t *testing.T) {
	cfg := Config{
		Networks: []NetworkConfig{
			{
				Name:       "mgmt-network",
				Type:       MgmtNetworkName,
				BridgeName: "virbr-mgmt",
				Mode:       "nat",
				NICModel:   "virtio",
			},
		},
		VMs: []VMConfig{
			{
				Name:       "master-1",
				Type:       VMHostType,
				K8sCluster: "cluster-1",
				K8sRole:    string(ClusterRoleMaster),
				K8sNodeMAC: "52:54:00:00:01:11",
				K8sNodeIP:  "192.168.123.11",
				Memory:     4096,
				VCPUs:      2,
				DiskSize:   20,
			},
		},
		OperatingSystem: OSConfig{
			ImageName: "Fedora-x86_64.qcow2",
		},
		Kubernetes: KubernetesConfig{
			Clusters: []ClusterConfig{
				{Name: "cluster-1", CNI: CNIOVNKubernetes},
			},
		},
	}

	err := cfg.validateAndSetDefaults()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "one of 'image_url' or 'image_ref' is required"))
}
