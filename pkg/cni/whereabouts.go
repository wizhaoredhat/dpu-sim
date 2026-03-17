package cni

import (
	"bytes"
	"fmt"
	"os/exec"
	"time"

	"github.com/wizhao/dpu-sim/pkg/log"
)

const (
	WhereaboutsVersion   = "0.9.2"
	whereaboutsChartName = "whereabouts"
	whereaboutsChartRef  = "oci://ghcr.io/k8snetworkplumbingwg/whereabouts-chart"
)

func (m *CNIManager) installWhereabouts(clusterName string) error {
	if m.k8sClient == nil {
		return fmt.Errorf("kubernetes client is required to install whereabouts addon on cluster %s", clusterName)
	}

	log.Info("Installing whereabouts %s on cluster %s...", WhereaboutsVersion, clusterName)
	manifest, err := m.renderWhereaboutsHelmManifest()
	if err != nil {
		return err
	}

	if m.shouldUseWritableCNIBinDir() {
		manifest = rewriteCNIHostPathOnly(manifest, writableCNIBinDir)
		log.Info("Detected bootc/read-only root setup, patching Whereabouts CNI binary path to %s", writableCNIBinDir)
	}

	if err := m.k8sClient.ApplyManifest(manifest); err != nil {
		return fmt.Errorf("failed to apply whereabouts helm-rendered manifest: %w", err)
	}
	m.k8sClient.InvalidateDiscoveryCache()

	if err := m.k8sClient.WaitForPodsReady("kube-system", "app.kubernetes.io/instance=whereabouts", 5*time.Minute); err != nil {
		return fmt.Errorf("whereabouts daemonset pods are not ready: %w", err)
	}

	log.Info("✓ whereabouts is installed and ready on cluster %s", clusterName)
	return nil
}

func (m *CNIManager) renderWhereaboutsHelmManifest() ([]byte, error) {
	args := []string{
		"template",
		whereaboutsChartName,
		whereaboutsChartRef,
		"--version",
		WhereaboutsVersion,
		"--namespace",
		"kube-system",
		"--include-crds",
		"--set",
		"nodeSliceController.enabled=false",
	}

	if m.kubeconfigPath != "" {
		args = append(args, "--kubeconfig", m.kubeconfigPath)
	}

	cmd := exec.Command("helm", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to render whereabouts helm chart: %s\n%s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}
