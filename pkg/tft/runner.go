// Package tft runs the kubernetes-traffic-flow-tests harness against dpu-sim Kubernetes
// clusters (Kind or VM/bare-metal deployments that use the same kubeconfig layout).
package tft

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/log"

	"gopkg.in/yaml.v3"
)

// RunOptions controls how tft.py is invoked.
type RunOptions struct {
	TFTRepo   string
	Python    string
	TFTConfig string
	Cluster   string
	Check     bool
}

// ResolveTFTRepo returns an absolute path to the kubernetes-traffic-flow-tests checkout.
func ResolveTFTRepo(repoFlag, dpuSimConfigPath string) (string, error) {
	if repoFlag != "" {
		return verifyTFTRepo(repoFlag)
	}

	if tftRepoFromEnv := strings.TrimSpace(os.Getenv("DPU_SIM_TFT_REPO")); tftRepoFromEnv != "" {
		return verifyTFTRepo(tftRepoFromEnv)
	}

	cfgDir := filepath.Dir(dpuSimConfigPath)
	candidates := []string{
		"kubernetes-traffic-flow-tests",
		filepath.Join(cfgDir, "kubernetes-traffic-flow-tests"),
		filepath.Join(cfgDir, "..", "kubernetes-traffic-flow-tests"),
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(abs, "tft.py")); err == nil {
			return abs, nil
		}
	}

	return "", fmt.Errorf("kubernetes-traffic-flow-tests not found (expected tft.py); set --tft-repo or DPU_SIM_TFT_REPO")
}

// verifyTFTRepo verifies that the directory contains a tft.py file. This assumes that we will never rename the
// main entrypoint python script.
func verifyTFTRepo(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "tft.py")); err != nil {
		return "", fmt.Errorf("tft.py not found in %s: %w", abs, err)
	}
	return abs, nil
}

// tftDefaultCluster picks the kubeconfig cluster for TFT when none is specified:
// the first kubernetes.clusters entry that does not contain DPU-type nodes (tenant / host side).
// If every listed cluster has DPU workers, falls back to GetDPUHostClusterName(), then the first cluster.
func tftDefaultCluster(cfg *config.Config) (string, error) {
	if len(cfg.Kubernetes.Clusters) == 0 {
		return "", fmt.Errorf("no kubernetes.clusters in config and kubeconfig not set")
	}
	for _, cl := range cfg.Kubernetes.Clusters {
		if !cfg.IsDPUCluster(cl.Name) {
			return cl.Name, nil
		}
	}
	if host := cfg.GetDPUHostClusterName(); host != "" {
		return host, nil
	}
	return cfg.Kubernetes.Clusters[0].Name, nil
}

// ResolveKubeconfigPath picks the absolute kubeconfig path for TFT.
func ResolveKubeconfigPath(cfg *config.Config, dpuSimConfigPath, clusterOverride string) (string, error) {
	if strings.TrimSpace(cfg.TrafficFlowTestsKubeconfig) != "" {
		if c := strings.TrimSpace(clusterOverride); c != "" {
			log.Warn("tft: --cluster %q is ignored because kubeconfig: is set in the dpu-sim config (%q)",
				c, strings.TrimSpace(cfg.TrafficFlowTestsKubeconfig))
		}
		path, err := resolvePathRelativeToConfig(dpuSimConfigPath, cfg.TrafficFlowTestsKubeconfig)
		if err != nil {
			return "", err
		}
		if err := kubeconfigPathMustExist(path, ""); err != nil {
			return "", err
		}
		return path, nil
	}

	cluster := strings.TrimSpace(clusterOverride)
	if cluster == "" {
		var err error
		cluster, err = tftDefaultCluster(cfg)
		if err != nil {
			return "", err
		}
	}

	rel := k8s.GetKubeconfigPath(cluster, cfg.Kubernetes.GetKubeconfigDir())
	absPath, err := filepath.Abs(rel)
	if err != nil {
		return "", fmt.Errorf("kubeconfig path: %w", err)
	}
	if err := kubeconfigPathMustExist(absPath, cluster); err != nil {
		return "", err
	}
	return absPath, nil
}

// kubeconfigPathMustExist returns an error if absPath is missing or not a regular file.
// When clusterForMsg is non-empty, errors assume the path came from kubernetes.kubeconfig_dir + cluster.
func kubeconfigPathMustExist(absPath, clusterForMsg string) error {
	st, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if clusterForMsg != "" {
				return fmt.Errorf("kubeconfig for cluster %q not found at %s; did you run dpu-sim deploy?", clusterForMsg, absPath)
			}
			return fmt.Errorf("kubeconfig file not found at %s (from kubeconfig: in dpu-sim config); fix the path or run dpu-sim deploy", absPath)
		}
		return fmt.Errorf("kubeconfig %s: %w", absPath, err)
	}
	if st.IsDir() {
		return fmt.Errorf("kubeconfig path %s is a directory, not a file", absPath)
	}
	return nil
}

func resolvePathRelativeToConfig(dpuSimConfigPath, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	base := filepath.Dir(dpuSimConfigPath)
	if base == "" || base == "." {
		return filepath.Abs(p)
	}
	return filepath.Abs(filepath.Join(base, p))
}

// HasEmbeddedTFT reports whether the dpu-sim config carries a tft: section for the harness.
func HasEmbeddedTFT(cfg *config.Config) bool {
	return cfg.TFT != nil && cfg.TFT.Node() != nil
}

// Run executes tft.py. Either opts.TFTConfig is set, or cfg must include a tft: block.
func Run(cfg *config.Config, dpuSimConfigPath string, opts RunOptions) error {
	if _, err := cfg.GetDeploymentMode(); err != nil {
		return fmt.Errorf("dpu-sim tft needs a valid deployment config (kind: or vms:/baremetal:): %w", err)
	}
	repo, err := ResolveTFTRepo(opts.TFTRepo, dpuSimConfigPath)
	if err != nil {
		return err
	}
	py, err := PythonForTFTRun(repo, opts.Python)
	if err != nil {
		return fmt.Errorf("tft python: %w", err)
	}

	var tftYAML string

	if strings.TrimSpace(opts.TFTConfig) != "" {
		tftYAML, err = filepath.Abs(opts.TFTConfig)
		if err != nil {
			return fmt.Errorf("tft config path: %w", err)
		}
		if _, err := os.Stat(tftYAML); err != nil {
			return fmt.Errorf("tft config file: %w", err)
		}
	} else {
		if !HasEmbeddedTFT(cfg) {
			return fmt.Errorf("no tft: in dpu-sim config; pass --tft-config or add tft: to the YAML")
		}
		kc, err := ResolveKubeconfigPath(cfg, dpuSimConfigPath, opts.Cluster)
		if err != nil {
			return err
		}
		f, err := os.CreateTemp("", "dpu-sim-tft-*.yaml")
		if err != nil {
			return fmt.Errorf("temp tft config: %w", err)
		}
		tftYAML = f.Name()
		defer func() { _ = os.Remove(tftYAML) }()

		enc := yaml.NewEncoder(f)
		enc.SetIndent(2)
		emit := struct {
			TFT        *yaml.Node `yaml:"tft"`
			Kubeconfig string     `yaml:"kubeconfig,omitempty"`
		}{
			TFT:        cfg.TFT.Node(),
			Kubeconfig: kc,
		}
		if err := enc.Encode(&emit); err != nil {
			_ = f.Close()
			return fmt.Errorf("marshal tft config: %w", err)
		}
		if err := enc.Close(); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}

	log.Info("Running kubernetes-traffic-flow-tests from %s", repo)
	log.Info("TFT config: %s", tftYAML)
	log.Info("Python: %s", py)

	args := []string{"tft.py", tftYAML}
	if opts.Check {
		args = append(args, "--check")
	}
	cmd := exec.Command(py, args...)
	cmd.Dir = repo
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tft.py: %w", err)
	}
	return nil
}
