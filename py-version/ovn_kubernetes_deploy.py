#!/usr/bin/env python3
"""
OVN-Kubernetes Deployment Script
Downloads and preapres the OVN-Kubernetes deployment using the daemonset.sh script
"""

import os
import subprocess
from pathlib import Path
from typing import Dict, Any, Optional
from kind_utils import load_docker_image


# Default OVN-Kubernetes repository settings
DEFAULT_OVN_REPO_URL = "https://github.com/ovn-org/ovn-kubernetes.git"
DEFAULT_OVN_REPO_PATH = Path(__file__).parent / "ovn-kubernetes"
DEFAULT_OVN_IMAGE = "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master"

def clone_ovn_kubernetes(
    repo_path: Path = DEFAULT_OVN_REPO_PATH,
    repo_url: str = DEFAULT_OVN_REPO_URL,
    branch: str = "master",
) -> bool:
    """Clone or update the OVN-Kubernetes repository

    Args:
        repo_path: Local path to clone the repository
        repo_url: URL of the OVN-Kubernetes repository
        branch: Git branch to checkout

    Returns:
        True if successful, False otherwise
    """
    print(f"Setting up OVN-Kubernetes repository...")

    # Clone if repo doesn't exist
    if not repo_path.exists():
        print(f"OVN-Kubernetes missing ... cloning OVN-Kubernetes from {repo_url}...")
        result = subprocess.run(
            ['git', 'clone', '--branch', branch,
            repo_url, str(repo_path)],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"✗ Failed to clone OVN-Kubernetes: {result.stderr}")
            return False
        print(f"✓ Repository cloned to {repo_path}")

    print(f"✓ OVN-Kubernetes repository exists at {repo_path}")

    return True


def get_daemonset_script_path(repo_path: Path = DEFAULT_OVN_REPO_PATH) -> Path:
    """Get the path to the daemonset.sh script

    Args:
        repo_path: Path to the OVN-Kubernetes repository

    Returns:
        Path to the daemonset.sh script
    """
    return repo_path / "dist" / "images" / "daemonset.sh"


def load_ovn_image(
    cluster_name: str,
    ovn_image: str = DEFAULT_OVN_IMAGE,
    local_registry: Optional[Dict[str, Any]] = None
) -> bool:
    """Load the OVN-Kubernetes image into the Kind cluster

    This loads the OVN-Kubernetes Docker image into the Kind cluster nodes.
    If a local registry is configured, this function skips loading since
    images should be pushed to the registry instead.

    Args:
        cluster_name: Name of the Kind cluster
        ovn_image: OVN-Kubernetes image to load (default: DEFAULT_OVN_IMAGE)
        local_registry: Local registry configuration dict, or None if not configured

    Returns:
        True if successful (or skipped due to local registry), False otherwise
    """
    print(f"Loading OVN-Kubernetes image into cluster '{cluster_name}'...")
    return load_docker_image(cluster_name, ovn_image, local_registry)


def run_daemonset_script(
    kubeconfig_path: str,
    api_server_url: str,
    pod_cidr: str = "10.244.0.0/16",
    service_cidr: str = "10.96.0.0/16",
    ovn_image: str = DEFAULT_OVN_IMAGE,
    repo_path: Path = DEFAULT_OVN_REPO_PATH,
    extra_args: Optional[Dict[str, str]] = None
) -> bool:
    """Run the OVN-Kubernetes daemonset.sh script

    Args:
        kubeconfig_path: Path to the kubeconfig file
        api_server_url: Kubernetes API server URL (e.g., https://172.18.0.2:6443)
        pod_cidr: Pod network CIDR (default: 10.244.0.0/16)
        service_cidr: Service network CIDR (default: 10.96.0.0/16)
        gateway_mode: Gateway mode - 'shared' or 'local' (default: shared)
        ovn_image: OVN-Kubernetes image to use
        repo_path: Path to the OVN-Kubernetes repository
        extra_args: Additional arguments to pass to daemonset.sh

    Returns:
        True if successful, False otherwise
    """
    daemonset_script = get_daemonset_script_path(repo_path)

    if not daemonset_script.exists():
        print(f"✗ OVN-Kubernetes daemonset.sh script not found at {daemonset_script}")
        return False

    # Make sure the script is executable
    os.chmod(daemonset_script, 0o755)

    cmd = [
        str(daemonset_script),
        f"--image={ovn_image}",
        f"--net-cidr={pod_cidr}",
        f"--svc-cidr={service_cidr}",
        f"--k8s-apiserver={api_server_url}",
        f"--gateway-mode=shared",
        f"--dummy-gateway-bridge=false",
        f"--gateway-options=",
        f"--enable-ipsec=false",
        f"--hybrid-enabled=false",
        f"--disable-snat-multiple-gws=false",
        f"--disable-forwarding=false",
        f"--ovn-encap-port=",
        f"--disable-pkt-mtu-check=false",
        f"--ovn-empty-lb-events=false",
        f"--multicast-enabled=false",
        f"--ovn-master-count=1",
        f"--ovn-unprivileged-mode=no",
        f"--master-loglevel=5",
        f"--node-loglevel=5",
        f"--dbchecker-loglevel=5",
        f"--ovn-loglevel-northd=-vconsole:info -vfile:info",
        f"--ovn-loglevel-nb=-vconsole:info -vfile:info",
        f"--ovn-loglevel-sb=-vconsole:info -vfile:info",
        f"--ovn-loglevel-controller=-vconsole:info",
        f"--ovnkube-libovsdb-client-logfile=",
        f"--ovnkube-config-duration-enable=true",
        f"--admin-network-policy-enable=true",
        f"--egress-ip-enable=true",
        f"--egress-ip-healthcheck-port=9107",
        f"--egress-firewall-enable=true",
        f"--egress-qos-enable=true",
        f"--egress-service-enable=true",
        f"--v4-join-subnet=100.64.0.0/16",
        f"--v6-join-subnet=fd98::/64",
        f"--v4-masquerade-subnet=169.254.0.0/17",
        f"--v6-masquerade-subnet=fd69::/112",
        f"--v4-transit-subnet=100.88.0.0/16",
        f"--v6-transit-subnet=fd97::/64",
        f"--ex-gw-network-interface=",
        f"--multi-network-enable=false",
        f"--network-segmentation-enable=false",
        f"--preconfigured-udn-addresses-enable=false",
        f"--route-advertisements-enable=false",
        f"--advertise-default-network=false",
        f"--advertised-udn-isolation-mode=strict",
        f"--ovnkube-metrics-scale-enable=false",
        f"--compact-mode=false",
        f"--enable-multi-external-gateway=true",
        f"--enable-ovnkube-identity=true",
        f"--enable-persistent-ips=true",
        f"--network-qos-enable=false",
        f"--mtu=1400",
        f"--enable-dnsnameresolver=false",
        f"--enable-observ=false",
    ]

    # Add any extra arguments
    if extra_args:
        for key, value in extra_args.items():
            cmd.append(f"--{key}={value}")

    print(f"Running daemonset.sh with command:")
    print(f"    {' '.join(cmd)}")

    # Set environment with KUBECONFIG
    env = os.environ.copy()
    env['KUBECONFIG'] = kubeconfig_path

    # Run the script from the dist/images directory
    result = subprocess.run(
        cmd,
        cwd=str(daemonset_script.parent),
        capture_output=True,
        text=True,
        env=env
    )

    if result.returncode != 0:
        print(f"✗ daemonset.sh failed with exit code {result.returncode}")
        print(f"stdout: {result.stdout}")
        print(f"stderr: {result.stderr}")
        return False

    print(f"✓ daemonset.sh completed successfully")
    print(f"result.stdout: {result.stdout}")

    return True


def get_yaml_dir_path(repo_path: Path = DEFAULT_OVN_REPO_PATH) -> Path:
    """Get the path to the OVN-Kubernetes yaml directory

    Args:
        repo_path: Path to the OVN-Kubernetes repository

    Returns:
        Path to the dist/yaml directory
    """
    return repo_path / "dist" / "yaml"


def apply_manifests(
    kubeconfig_path: str,
    manifests: list[str],
    yaml_dir: Optional[Path] = None
) -> bool:
    """Apply a list of Kubernetes manifests

    Supports both local file paths and external URLs. For local files,
    if a yaml_dir is provided and the manifest is not an absolute path,
    it will be resolved relative to that directory.

    Args:
        kubeconfig_path: Path to the kubeconfig file
        manifests: List of manifest paths (local files or URLs)
        yaml_dir: Optional directory to resolve relative local file paths

    Returns:
        True if all manifests were applied successfully, False otherwise
    """
    for manifest in manifests:
        # Check if manifest is external (URL) or local file
        is_external = manifest.startswith("http")

        if is_external:
            manifest_name = manifest.split('/')[-1]
            manifest_source = manifest
            print(f"  Applying external {manifest_name}...")
        else:
            manifest_name = manifest
            # Resolve local path: use yaml_dir if provided, otherwise use as-is
            if yaml_dir and not os.path.isabs(manifest):
                manifest_path = yaml_dir / manifest_name
            else:
                manifest_path = Path(manifest)
            if not manifest_path.exists():
                print(f"✗ Manifest not found: {manifest_path}")
                return False
            manifest_source = str(manifest_path)
            print(f"  Applying {manifest_name}...")

        result = subprocess.run(
            ['kubectl', '--kubeconfig', kubeconfig_path, 'apply', '-f', manifest_source],
            capture_output=True,
            text=True
        )
        if result.returncode != 0:
            print(f"✗ Failed to apply {manifest_name}: {result.stderr}")
            return False
        print(f"✓ {manifest_name} applied")

    return True


def label_ovn_master_nodes(
    kubeconfig_path: str,
) -> bool:
    """Label master nodes for OVN HA deployment

    This function labels the master nodes with the required labels for
    OVN-Kubernetes to deploy the ovnkube-db components. It also
    removes the control-plane taints to allow workloads to be scheduled
    on master nodes.

    Args:
        kubeconfig_path: Path to the kubeconfig file

    Returns:
        True if successful, False otherwise
    """
    print(f"Labeling master nodes for OVN HA...")

    # Get nodes from the cluster using kubectl
    result = subprocess.run(
        ['kubectl', '--kubeconfig', kubeconfig_path, 'get', 'nodes',
         '-o', 'jsonpath={.items[*].metadata.name}'],
        capture_output=True,
        text=True
    )
    if result.returncode != 0:
        print(f"✗ Failed to get nodes: {result.stderr}")
        return False

    nodes = sorted(result.stdout.strip().split())

    print(f"Master nodes to label and remove taints from: {nodes}")

    for node in nodes:
        # Label node for OVN HA (k8s.ovn.org/ovnkube-db=true)
        # and add control-plane role label
        result = subprocess.run(
            ['kubectl', '--kubeconfig', kubeconfig_path, 'label', 'node', node,
             'k8s.ovn.org/ovnkube-db=true',
             'node-role.kubernetes.io/control-plane=',
             '--overwrite'],
            capture_output=True,
            text=True
        )
        if result.returncode != 0:
            print(f"✗ Failed to label node '{node}': {result.stderr}")
            return False
        print(f"✓ Labeled node '{node}'")

        # Remove master taint (for older k8s versions)
        # Do not error if it fails to remove the taint
        subprocess.run(
            ['kubectl', '--kubeconfig', kubeconfig_path, 'taint', 'node', node,
                'node-role.kubernetes.io/master:NoSchedule-'],
            capture_output=True,
            text=True
        )

        # Remove control-plane taint
        subprocess.run(
            ['kubectl', '--kubeconfig', kubeconfig_path, 'taint', 'node', node,
                'node-role.kubernetes.io/control-plane:NoSchedule-'],
            capture_output=True,
            text=True
        )
        print(f"✓ Removed taints from node '{node}'")

    print(f"✓ Master nodes labeled for OVN HA")
    return True


def install_ovn_kubernetes(
    kubeconfig_path: str,
    repo_path: Path = DEFAULT_OVN_REPO_PATH
) -> bool:
    """Apply the OVN-Kubernetes manifest files from dist/yaml
    and set the appropriate labels and taints to the nodes.

    This applies all YAML manifest files generated by daemonset.sh
    to the Kubernetes cluster in the required order.

    Args:
        kubeconfig_path: Path to the kubeconfig file
        repo_path: Path to the OVN-Kubernetes repository

    Returns:
        True if all manifests were applied successfully, False otherwise
    """
    yaml_dir = get_yaml_dir_path(repo_path)

    if not yaml_dir.exists():
        print(f"✗ YAML directory not found: {yaml_dir}")
        return False

    # Ordered list of manifests to apply (URLs are external, filenames are local)
    manifests = [
        "k8s.ovn.org_egressfirewalls.yaml",
        "k8s.ovn.org_egressips.yaml",
        "k8s.ovn.org_egressqoses.yaml",
        "k8s.ovn.org_egressservices.yaml",
        "k8s.ovn.org_adminpolicybasedexternalroutes.yaml",
        "k8s.ovn.org_networkqoses.yaml",
        "k8s.ovn.org_userdefinednetworks.yaml",
        "k8s.ovn.org_clusteruserdefinednetworks.yaml",
        "k8s.ovn.org_routeadvertisements.yaml",
        "k8s.ovn.org_clusternetworkconnects.yaml",
        # NOTE: When you update vendoring versions for the ANP & BANP APIs, we must update the version of the CRD we pull from in the below URL
        "https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/v0.1.5/config/crd/experimental/policy.networking.k8s.io_adminnetworkpolicies.yaml",
        "https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/v0.1.5/config/crd/experimental/policy.networking.k8s.io_baselineadminnetworkpolicies.yaml",
        "ovn-setup.yaml",
        "rbac-ovnkube-identity.yaml",
        "rbac-ovnkube-cluster-manager.yaml",
        "rbac-ovnkube-master.yaml",
        "rbac-ovnkube-node.yaml",
        "rbac-ovnkube-db.yaml",
    ]

    print(f"Applying OVN-Kubernetes manifests...")
    if not apply_manifests(kubeconfig_path, manifests, yaml_dir):
        return False

    # Label master nodes for OVN HA
    if not label_ovn_master_nodes(kubeconfig_path):
        return False

    global_zone_manifests = [
        "ovs-node.yaml",
        "ovnkube-db.yaml",
        "ovnkube-identity.yaml",
        "ovnkube-master.yaml",
        "ovnkube-node.yaml",
    ]

    print(f"Applying OVN-Kubernetes global zone manifests...")
    if not apply_manifests(kubeconfig_path, global_zone_manifests, yaml_dir):
        return False

    return True


def prepare_ovn_kubernetes_deployment(
    kubeconfig_path: str,
    api_server_url: str,
    cluster_config: Dict[str, Any],
    repo_path: Path = DEFAULT_OVN_REPO_PATH
) -> bool:
    """Main function to deploy OVN-Kubernetes using daemonset.sh

    This function:
    1. Clones/updates the OVN-Kubernetes repository
    2. Runs the daemonset.sh script
    3. Waits for OVN pods to be ready

    Args:
        kubeconfig_path: Path to the kubeconfig file
        api_server_url: Kubernetes API server URL
        cluster_config: Cluster configuration dictionary
        repo_path: Local path for OVN-Kubernetes repository

    Returns:
        True if deployment was successful, False otherwise
    """
    print(f"\n--- Creating OVN-Kubernetes deployment using daemonset.sh ---")

    # Extract configuration
    pod_cidr = cluster_config.get('pod_cidr', '10.244.0.0/16')
    service_cidr = cluster_config.get('service_cidr', '10.96.0.0/16')

    print(f"Configuration:")
    print(f"  Pod CIDR: {pod_cidr}")
    print(f"  Service CIDR: {service_cidr}")
    print(f"  API Server: {api_server_url}")

    if not clone_ovn_kubernetes(repo_path=repo_path):
        return False

    if not run_daemonset_script(
        kubeconfig_path=kubeconfig_path,
        api_server_url=api_server_url,
        pod_cidr=pod_cidr,
        service_cidr=service_cidr,
        repo_path=repo_path
    ):
        return False

    print(f"\n  ✓ OVN-Kubernetes daemonset.sh completed successfully")
    return True
