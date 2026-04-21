package tft

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wizhao/dpu-sim/pkg/config"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestHasEmbeddedTFT(t *testing.T) {
	t.Parallel()
	var empty config.Config
	require.False(t, HasEmbeddedTFT(&empty))

	n := yaml.Node{Kind: yaml.SequenceNode}
	cfg := config.Config{TFT: &config.TrafficFlowTestsSubtree{}}
	_ = cfg.TFT.UnmarshalYAML(&n)
	require.True(t, HasEmbeddedTFT(&cfg))
}

func TestTFTDefaultCluster_PrefersHostOverDPU(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Kind: &config.KindConfig{Nodes: []config.KindNodeConfig{
			{Name: "cp-dpu", K8sCluster: "dpu-c", K8sRole: "control-plane"},
			{Name: "dw", Type: config.DpuType, K8sCluster: "dpu-c", K8sRole: "worker", Host: "hw"},
			{Name: "cp-host", K8sCluster: "host-c", K8sRole: "control-plane"},
			{Name: "hw", Type: config.HostType, K8sCluster: "host-c", K8sRole: "worker"},
		}},
		Kubernetes: config.KubernetesConfig{
			Clusters: []config.ClusterConfig{
				{Name: "dpu-c", CNI: config.CNIFlannel},
				{Name: "host-c", CNI: config.CNIOVNKubernetes},
			},
		},
	}
	name, err := tftDefaultCluster(cfg)
	require.NoError(t, err)
	require.Equal(t, "host-c", name)
}

func TestResolveKubeconfigPath_DefaultCluster(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "cfg.yaml")
	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			KubeconfigDir: "kubeconfig",
			Clusters: []config.ClusterConfig{
				{Name: "dpu-sim-host"},
			},
		},
	}
	oldWd, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.MkdirAll("kubeconfig", 0o755))
	want := filepath.Join(tmp, "kubeconfig", "dpu-sim-host.kubeconfig")
	require.NoError(t, os.WriteFile(want, []byte("x"), 0o600))

	got, err := ResolveKubeconfigPath(cfg, cfgPath, "")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestResolveKubeconfigPath_MissingClusterKubeconfig(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "cfg.yaml")
	cfg := &config.Config{
		Kubernetes: config.KubernetesConfig{
			KubeconfigDir: "kubeconfig",
			Clusters: []config.ClusterConfig{
				{Name: "missing-cluster"},
			},
		},
	}
	oldWd, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmp))
	defer func() { _ = os.Chdir(oldWd) }()
	require.NoError(t, os.MkdirAll("kubeconfig", 0o755))

	_, err := ResolveKubeconfigPath(cfg, cfgPath, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing-cluster")
	require.Contains(t, err.Error(), "dpu-sim deploy")
}

func TestResolveKubeconfigPath_MissingYamlKubeconfig(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "cfg.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("x"), 0o644))
	cfg := &config.Config{
		TrafficFlowTestsKubeconfig: "no-such.kubeconfig",
		Kubernetes: config.KubernetesConfig{
			Clusters: []config.ClusterConfig{{Name: "c"}},
		},
	}

	_, err := ResolveKubeconfigPath(cfg, cfgPath, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "kubeconfig:")
}

func TestGetDeploymentModeAllowsVM(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		VMs: []config.VMConfig{{
			Name: "worker-1", Type: config.HostType, K8sCluster: "cluster-1", K8sRole: "worker",
			K8sNodeMAC: "52:54:00:00:01:11", K8sNodeIP: "192.168.1.2", Memory: 4096, VCPUs: 2, DiskSize: 20,
		}},
		Kubernetes: config.KubernetesConfig{
			Clusters: []config.ClusterConfig{{Name: "cluster-1", CNI: config.CNIOVNKubernetes}},
		},
	}
	mode, err := cfg.GetDeploymentMode()
	require.NoError(t, err)
	require.Equal(t, config.VMDeploymentMode, mode)
}

func TestResolvePathRelativeToConfig(t *testing.T) {
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "d", "cfg.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o755))
	kc := filepath.Join(tmp, "d", "kc", "a.kubeconfig")
	require.NoError(t, os.MkdirAll(filepath.Dir(kc), 0o755))
	require.NoError(t, os.WriteFile(kc, []byte("x"), 0o600))

	got, err := resolvePathRelativeToConfig(cfg, "kc/a.kubeconfig")
	require.NoError(t, err)
	require.Equal(t, kc, got)
}
