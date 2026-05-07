package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/log"
	"github.com/wizhao/dpu-sim/pkg/platform"
	"github.com/wizhao/dpu-sim/pkg/tft"
)

var (
	tftRepoPath   string
	tftConfigFlag string
	tftPython     string
	tftCluster    string
	tftCheck      bool
)

var tftCmd = &cobra.Command{
	Use:   "tft",
	Short: "Kubernetes traffic flow tests",
	Long: `Run the kubernetes-traffic-flow-tests harness (tft.py) against a dpu-sim cluster with OVN-K DPU offload
(Kind or VM/bare-metal), using the same kubeconfig paths as dpu-sim writes under kubernetes.kubeconfig_dir.

Configure tft: and optional kubeconfig: in the same dpu-sim YAML as documented in
kubernetes-traffic-flow-tests, or pass --tft-config pointing at a standalone TFT YAML.

Requires Python >= 3.11. Install deps once with: dpu-sim tft venv (uses PYTHON or PATH: python3.13, python3.12, …).`,
}

var tftRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run tft.py with config from --config or --tft-config",
	RunE:  runTFT,
}

var tftVenvCmd = &cobra.Command{
	Use:   "venv",
	Short: "Create/update TFT Python venv and pip install requirements.txt",
	Long: `Creates .tft-venv in the kubernetes-traffic-flow-tests checkout only if it is missing,
then runs pip install. An existing venv is not recreated.

The host interpreter must be Python >= 3.11. It defaults to PYTHON, or the first of
python3.13, python3.12, python3.11, python3 on PATH that meets the version requirement.
Override with --python.`,
	RunE: runTFTVenv,
}

func init() {
	tftCmd.PersistentFlags().StringVar(&tftRepoPath, "tft-repo-path", "",
		"Path to an external kubernetes-traffic-flow-tests source tree (overrides auto-clone)")

	tftRunCmd.Flags().StringVar(&tftConfigFlag, "tft-config", "", "Standalone TFT YAML for tft.py; when set, the embedded `tft:` in the dpu-sim config is ignored")
	tftRunCmd.Flags().StringVar(&tftPython, "python", "", "Python for tft.py, >= 3.11 (default: <tft-repo>/.tft-venv/bin/python3 if present, else PATH discovery)")
	tftRunCmd.Flags().StringVar(&tftCluster, "cluster", "", "Kubernetes cluster name for kubeconfig when kubeconfig is omitted in YAML (default: first cluster without DPU-type nodes)")
	tftRunCmd.Flags().BoolVar(&tftCheck, "check", false, "Pass --check to tft.py (exit non-zero if tests fail)")

	tftVenvCmd.Flags().StringVar(&tftPython, "python", "", "Host Python for venv, >= 3.11 (default: PYTHON / PATH discovery)")

	tftCmd.AddCommand(tftRunCmd)
	tftCmd.AddCommand(tftVenvCmd)
	rootCmd.AddCommand(tftCmd)
}

// runTFT executes the Kubernetes Traffic Flow Tests
func runTFT(_ *cobra.Command, args []string) error {
	log.SetLevel(log.ParseLevel(logLevel))
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	repoPath := strings.TrimSpace(tftRepoPath)
	if err := tft.ValidateTFTRepoPath(repoPath); err != nil {
		return fmt.Errorf("--tft-repo-path: %w", err)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	return tft.Run(platform.NewLocalExecutor(), cfg, configPath, tft.RunOptions{
		TFTRepo:   repoPath,
		Python:    tftPython,
		TFTConfig: tftConfigFlag,
		Cluster:   tftCluster,
		Check:     tftCheck,
	})
}

// runTFTVenv creates or updates the TFT Python venv and installs the Python requirements
func runTFTVenv(_ *cobra.Command, args []string) error {
	log.SetLevel(log.ParseLevel(logLevel))
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	repoPath := strings.TrimSpace(tftRepoPath)
	if err := tft.ValidateTFTRepoPath(repoPath); err != nil {
		return fmt.Errorf("--tft-repo-path: %w", err)
	}

	tftRepo, err := tft.ResolveTFTRepo(platform.NewLocalExecutor(), repoPath)
	if err != nil {
		return fmt.Errorf("failed to resolve TFT repo: %w", err)
	}

	return tft.EnsureVenv(platform.NewLocalExecutor(), tftRepo, tftPython)
}
