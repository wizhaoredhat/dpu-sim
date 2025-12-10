#!/usr/bin/env python3
"""
OVN-Kubernetes Deployment Script
Downloads and preapres the OVN-Kubernetes deployment using the daemonset.sh script
"""

import os
import subprocess
import time
from pathlib import Path
from typing import Dict, Any, Optional


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
    else:
        # Update existing repository
        print(f"Updating existing repository at {repo_path}...")
        result = subprocess.run(
            ['git', 'fetch', 'origin', branch],
            cwd=str(repo_path),
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"    ✗ Failed to fetch updates: {result.stderr}")
            return False

        result = subprocess.run(
            ['git', 'reset', '--hard', f'origin/{branch}'],
            cwd=str(repo_path),
            capture_output=True, text=True
        )
        if result.returncode != 0:
            print(f"✗ Failed to reset to latest: {result.stderr}")
            return False

        print(f"✓ OVN-Kubernetes repository updated to latest {branch}")

    return True


def get_daemonset_script_path(repo_path: Path = DEFAULT_OVN_REPO_PATH) -> Path:
    """Get the path to the daemonset.sh script

    Args:
        repo_path: Path to the OVN-Kubernetes repository

    Returns:
        Path to the daemonset.sh script
    """
    return repo_path / "dist" / "images" / "daemonset.sh"


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
        force_update: If True, force re-clone of repository

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
