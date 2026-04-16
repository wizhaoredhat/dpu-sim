package cni

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/deviceplugin"
	"github.com/wizhao/dpu-sim/pkg/k8s"
	"github.com/wizhao/dpu-sim/pkg/log"
)

// PostInstall applies cluster-environment patches after the CNI, device plugin (when
// applicable), and addons are installed. For example, in OVN-Kubernetes DPU-host
// mode it patches system Deployments so workloads request the simulated VF.
func (m *CNIManager) PostInstall(clusterName string) error {
	mode := m.detectOVNKMode(clusterName)

	if mode == ovnkModeDPUHost {
		log.Info("\n=== Patching cluster environment on %s ===", clusterName)
		if err := m.ensureDPUHostSystemDeployments(); err != nil {
			return fmt.Errorf("cluster environment patch: %w", err)
		}
	}

	// Recreate CoreDNS after Multus is active or a CNI is installed so pods pick up stable CNI wiring.
	if err := m.k8sClient.RolloutRestartDeployment("kube-system", "coredns"); err != nil {
		log.Warn("failed to restart coredns after multus install: %v", err)
	}
	if err := m.k8sClient.WaitForDeploymentAvailable("kube-system", "coredns", 5*time.Minute); err != nil {
		log.Warn("Warning: coredns deployment is not available after multus install: %v", err)
	}

	return nil
}

// ensureDPUHostSystemDeployments patches CoreDNS and (if present)
// local-path-provisioner so pods request the simulated VF and schedule only on
// dpu-host nodes. Without this, DPU-host CNI rejects pods with "device ID must be provided".
// TODO: This is considered a workaround for now.
func (m *CNIManager) ensureDPUHostSystemDeployments() error {
	vf := deviceplugin.VFResourceName
	if err := patchDeploymentForDPUHostPodNetworking(m.k8sClient, "kube-system", "coredns", vf, false); err != nil {
		return err
	}
	if err := patchDeploymentForDPUHostPodNetworking(m.k8sClient, "local-path-storage", "local-path-provisioner", vf, true); err != nil {
		return err
	}
	return nil
}

// PatchDeploymentForDPUHostPodNetworking adds vfResourceName (e.g. dpusim.io/vf) to
// requests and limits for every container and init container in the pod template, and
// requires scheduling onto nodes labeled DPU Host.
// ignoreNotFound treats a missing Deployment as success (for optional add-ons).
func patchDeploymentForDPUHostPodNetworking(c *k8s.K8sClient, namespace, deploymentName, vfResourceName string, ignoreNotFound bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dep, err := c.Clientset().AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		if ignoreNotFound && apierrors.IsNotFound(err) {
			log.Info("Deployment %s/%s not found, skipping DPU-host simulated VF patch", namespace, deploymentName)
			return nil
		}
		return fmt.Errorf("get deployment %s/%s: %w", namespace, deploymentName, err)
	}

	qty := resource.MustParse("1")
	rn := corev1.ResourceName(vfResourceName)
	pod := &dep.Spec.Template.Spec

	for i := range pod.InitContainers {
		patchContainerVFRequest(&pod.InitContainers[i], rn, qty)
	}
	for i := range pod.Containers {
		patchContainerVFRequest(&pod.Containers[i], rn, qty)
	}
	mergeDPUHostNodeAffinity(pod)

	if _, err := c.Clientset().AppsV1().Deployments(namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update deployment %s/%s for DPU-host simulated VF: %w", namespace, deploymentName, err)
	}

	log.Info("✓ Patched deployment %s/%s for DPU-host simulated VF (%s)", namespace, deploymentName, vfResourceName)
	return nil
}

// patchContainerVFRequest adds the simulated VF resource name and quantity to the container requests and limits
func patchContainerVFRequest(co *corev1.Container, rn corev1.ResourceName, qty resource.Quantity) {
	if co.Resources.Requests == nil {
		co.Resources.Requests = corev1.ResourceList{}
	}
	if co.Resources.Limits == nil {
		co.Resources.Limits = corev1.ResourceList{}
	}
	co.Resources.Requests[rn] = qty
	co.Resources.Limits[rn] = qty
}

// mergeDPUHostNodeAffinity ensures pod schedules only on nodes labeled with the DPU Host node label
func mergeDPUHostNodeAffinity(pod *corev1.PodSpec) {
	req := corev1.NodeSelectorRequirement{
		Key:      config.DPUHostNodeLabelKey,
		Operator: corev1.NodeSelectorOpExists,
	}
	if pod.Affinity == nil {
		pod.Affinity = &corev1.Affinity{}
	}
	if pod.Affinity.NodeAffinity == nil {
		pod.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}
	na := pod.Affinity.NodeAffinity
	if na.RequiredDuringSchedulingIgnoredDuringExecution == nil || len(na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		na.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: []corev1.NodeSelectorRequirement{req}},
			},
		}
		return
	}
	term := &na.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0]
	for _, e := range term.MatchExpressions {
		if e.Key == config.DPUHostNodeLabelKey && e.Operator == corev1.NodeSelectorOpExists {
			return
		}
	}
	term.MatchExpressions = append(term.MatchExpressions, req)
}
