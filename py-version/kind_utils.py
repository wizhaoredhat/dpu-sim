#!/usr/bin/env python3
"""
Kind cluster utilities for container-based simulation
"""

import subprocess
import json
import re
import time
from pathlib import Path
from typing import Dict, Any, Optional, List


def is_ovn_kubernetes(cni: str) -> bool:
    """Check if the CNI is OVN-Kubernetes

    Args:
        cni: The CNI name to check

    Returns:
        True if the CNI is OVN-Kubernetes, False otherwise
    """
    return cni.lower() in ['ovn-kubernetes', 'ovn']


def check_kind_installed() -> bool:
    """Check if kind is installed and accessible

    Returns:
        True if kind is installed, False otherwise
    """
    try:
        result = subprocess.run(['kind', 'version'],
                                capture_output=True, text=True)
        return result.returncode == 0
    except FileNotFoundError:
        return False


def check_docker_running() -> bool:
    """Check if Docker daemon is running

    Returns:
        True if Docker is running, False otherwise
    """
    try:
        result = subprocess.run(['docker', 'info'],
                                capture_output=True, text=True)
        return result.returncode == 0
    except FileNotFoundError:
        return False


def get_kind_clusters() -> List[str]:
    """Get list of existing Kind clusters

    Returns:
        List of cluster names
    """
    try:
        result = subprocess.run(['kind', 'get', 'clusters'],
                                capture_output=True, text=True)
        if result.returncode == 0:
            clusters = result.stdout.strip().split('\n')
            return [c for c in clusters if c]  # Filter empty strings
        return []
    except (FileNotFoundError, subprocess.CalledProcessError):
        return []


def cluster_exists(cluster_name: str) -> bool:
    """Check if a Kind cluster exists

    Args:
        cluster_name: Name of the cluster

    Returns:
        True if cluster exists, False otherwise
    """
    return cluster_name in get_kind_clusters()


def delete_cluster(cluster_name: str) -> bool:
    """Delete a Kind cluster

    Args:
        cluster_name: Name of the cluster to delete

    Returns:
        True if deletion was successful, False otherwise
    """
    try:
        result = subprocess.run(
            ['kind', 'delete', 'cluster', '--name', cluster_name],
            capture_output=True, text=True
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.CalledProcessError):
        return False


def get_cluster_nodes(cluster_name: str) -> List[Dict[str, str]]:
    """Get nodes in a Kind cluster

    Args:
        cluster_name: Name of the cluster

    Returns:
        List of node info dicts with 'name' and 'role' keys
    """
    try:
        result = subprocess.run(
            ['kind', 'get', 'nodes', '--name', cluster_name],
            capture_output=True, text=True
        )
        if result.returncode != 0:
            return []

        nodes = []
        for node_name in result.stdout.strip().split('\n'):
            if not node_name:
                continue

            # Determine role from node name
            if 'control-plane' in node_name:
                role = 'control-plane'
            else:
                role = 'worker'

            nodes.append({
                'name': node_name,
                'role': role
            })

        return nodes
    except (FileNotFoundError, subprocess.CalledProcessError):
        return []


def get_node_ip(node_name: str) -> Optional[str]:
    """Get IP address of a Kind node container

    Args:
        node_name: Name of the Kind node container

    Returns:
        IP address or None if not found
    """
    try:
        result = subprocess.run(
            ['docker', 'inspect', '-f',
             '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}',
             node_name],
            capture_output=True, text=True
        )
        if result.returncode == 0:
            ip = result.stdout.strip()
            return ip if ip else None
        return None
    except (FileNotFoundError, subprocess.CalledProcessError):
        return None


def exec_on_node(node_name: str, command: str, timeout: int = 300) -> tuple[bool, str, str]:
    """Execute a command on a Kind node

    Args:
        node_name: Name of the Kind node container
        command: Command to execute
        timeout: Command timeout in seconds

    Returns:
        Tuple of (success, stdout, stderr)
    """
    try:
        result = subprocess.run(
            ['docker', 'exec', node_name, 'bash', '-c', command],
            capture_output=True,
            text=True,
            timeout=timeout
        )
        return (
            result.returncode == 0,
            result.stdout,
            result.stderr
        )
    except subprocess.TimeoutExpired:
        return False, '', f'Command timed out after {timeout}s'
    except Exception as e:
        return False, '', str(e)


def copy_to_node(node_name: str, src: str, dest: str) -> bool:
    """Copy a file to a Kind node

    Args:
        node_name: Name of the Kind node container
        src: Source file path on host
        dest: Destination path on node

    Returns:
        True if successful, False otherwise
    """
    try:
        result = subprocess.run(
            ['docker', 'cp', src, f'{node_name}:{dest}'],
            capture_output=True, text=True
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.CalledProcessError):
        return False


def get_kubeconfig(cluster_name: str) -> Optional[str]:
    """Get kubeconfig for a Kind cluster

    Args:
        cluster_name: Name of the cluster

    Returns:
        Kubeconfig content as string, or None if failed
    """
    try:
        result = subprocess.run(
            ['kind', 'get', 'kubeconfig', '--name', cluster_name],
            capture_output=True, text=True
        )
        if result.returncode == 0:
            return result.stdout
        return None
    except (FileNotFoundError, subprocess.CalledProcessError):
        return None


def export_kubeconfig(cluster_name: str, kubeconfig_path: str) -> bool:
    """Export kubeconfig to a file

    Args:
        cluster_name: Name of the cluster
        kubeconfig_path: Path to save kubeconfig

    Returns:
        True if successful, False otherwise
    """
    kubeconfig = get_kubeconfig(cluster_name)
    if kubeconfig:
        try:
            with open(kubeconfig_path, 'w') as f:
                f.write(kubeconfig)
            return True
        except IOError:
            return False
    return False


def wait_for_node_ready(kubeconfig_path: str, timeout: int = 300) -> bool:
    """Wait for all nodes in a cluster to be ready

    Args:
        kubeconfig_path: Path to the kubeconfig file
        timeout: Timeout in seconds

    Returns:
        True if all nodes are ready, False otherwise
    """
    if not Path(kubeconfig_path).exists():
        print(f"✗ Kubeconfig not found: {kubeconfig_path}")
        return False

    start_time = time.time()
    while time.time() - start_time < timeout:
        result = subprocess.run(
            ['kubectl', '--kubeconfig', kubeconfig_path,
             'get', 'nodes', '-o', 'json'],
            capture_output=True, text=True
        )

        if result.returncode == 0:
            try:
                nodes_json = json.loads(result.stdout)
                nodes = nodes_json.get('items', [])

                if not nodes:
                    time.sleep(2)
                    continue

                all_ready = True
                for node in nodes:
                    conditions = node.get('status', {}).get('conditions', [])
                    ready_condition = next(
                        (c for c in conditions if c['type'] == 'Ready'), None
                    )
                    if not ready_condition or ready_condition.get('status') != 'True':
                        all_ready = False
                        break

                if all_ready:
                    return True

            except json.JSONDecodeError:
                pass

        time.sleep(2)

    return False


def generate_networking_yaml(is_ovn_k: bool, cluster_config: Dict[str, Any]) -> str:
    """Generate the networking section of Kind cluster configuration

    Args:
        is_ovn_k: Whether the cluster is using OVN-Kubernetes
        cluster_config: Kubernetes cluster configuration

    Returns:
        Networking YAML section content
    """
    networking_yaml = "networking:\n"
    # For OVN-Kubernetes, we need to disable both default CNI and kube-proxy
    if is_ovn_k:
        networking_yaml += "  disableDefaultCNI: true\n"
        networking_yaml += "  kubeProxyMode: none\n"

    # Add pod and service subnets
    pod_cidr = cluster_config.get('pod_cidr', '10.244.0.0/16')
    service_cidr = cluster_config.get('service_cidr', '10.96.0.0/16')
    networking_yaml += f"  podSubnet: \"{pod_cidr}\"\n"
    networking_yaml += f"  serviceSubnet: \"{service_cidr}\"\n"
    networking_yaml += f"  ipFamily: \"ipv4\"\n"

    return networking_yaml


def generate_registry_yaml(local_registry: Optional[Dict[str, Any]]) -> str:
    """Generate the containerdConfigPatches section for local registry

    Args:
        local_registry: Local registry configuration dict, or None if not configured

    Returns:
        Registry YAML section content (empty string if not configured or not enabled)
    """
    if not local_registry:
        return ""

    registry_name = local_registry.get('name', 'kind-registry')
    registry_port = local_registry.get('port', 5000)
    return f"""containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:{registry_port}"]
    endpoint = ["http://{registry_name}:{registry_port}"]
"""


def generate_kubeadm_config_patches(is_ovn_k: bool, cluster_log_level: int = 4) -> str:
    """Generate kubeadmConfigPatches to disable service-lb-controller for OVN-Kubernetes

    Args:
        is_ovn_k: Whether the cluster is using OVN-Kubernetes

    Returns:
        kubeadmConfigPatches YAML section content (empty string if not needed)
    """
    if not is_ovn_k:
        return ""

    return f"""kubeadmConfigPatches:
- |
  kind: ClusterConfiguration
  metadata:
    name: config
  apiServer:
    extraArgs:
      "v": "{cluster_log_level}"
  controllerManager:
    extraArgs:
      "v": "{cluster_log_level}"
      "controllers": "*,bootstrap-signer-controller,token-cleaner-controller,-service-lb-controller"
  scheduler:
    extraArgs:
      "v": "{cluster_log_level}"
  networking:
    # DNS comain for k8s services
    dnsDomain: "cluster.local"
  ---
  kind: InitConfiguration
  nodeRegistration:
    kubeletExtraArgs:
      "v": "{cluster_log_level}"
  ---
  kind: JoinConfiguration
  nodeRegistration:
    kubeletExtraArgs:
      "v": "{cluster_log_level}"
"""

def generate_nodes_yaml(is_ovn_k: bool, nodes: List[Dict[str, Any]]) -> str:
    """Generate the nodes section of Kind cluster configuration

    Args:
        nodes: List of node configurations

    Returns:
        Nodes YAML section content
    """
    nodes_yaml = "nodes:\n"
    for node in nodes:
        role = node.get('role', 'worker')
        nodes_yaml += f"  - role: {role}\n"

        # Add kubeadmConfigPatches for control-plane with InitConfiguration
        if role == 'control-plane' and is_ovn_k:
            nodes_yaml += "    kubeadmConfigPatches:\n"
            nodes_yaml += "    - |\n"
            nodes_yaml += "      kind: InitConfiguration\n"
            nodes_yaml += "      nodeRegistration:\n"
            nodes_yaml += "        kubeletExtraArgs:\n"
            nodes_yaml += "          node-labels: \"ingress-ready=true\"\n"
            nodes_yaml += "          authorization-mode: \"AlwaysAllow\"\n"

    return nodes_yaml


def generate_kind_config(kind_config: Dict[str, Any], cluster_config: Dict[str, Any]) -> str:
    """Generate Kind cluster configuration YAML

    Args:
        kind_config: Kind configuration from the main config
        cluster_config: Kubernetes cluster configuration

    Returns:
        Kind config YAML content
    """
    nodes = kind_config.get('nodes', [])
    cni = cluster_config.get('cni', 'kindnet')
    local_registry = cluster_config.get('local_registry', None)

    is_ovn_k = is_ovn_kubernetes(cni)
    nodes_yaml = generate_nodes_yaml(is_ovn_k, nodes)
    networking_yaml = generate_networking_yaml(is_ovn_k, cluster_config)
    registry_yaml = generate_registry_yaml(local_registry)
    kubeadm_patches_yaml = generate_kubeadm_config_patches(is_ovn_k)

    config = f"""kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
{networking_yaml}{registry_yaml}{kubeadm_patches_yaml}{nodes_yaml}"""

    return config


def configure_ipv6_on_nodes(cluster_name: str) -> bool:
    """Configure IPv6 settings on all nodes in the cluster

    Enables IPv6 and IPv6 forwarding on all Kind nodes by setting:
    - net.ipv6.conf.all.disable_ipv6=0
    - net.ipv6.conf.all.forwarding=1

    Args:
        cluster_name: Name of the cluster

    Returns:
        True if all nodes were configured successfully, False otherwise
    """
    print(f"  Configuring IPv6 on all nodes...")

    nodes = get_cluster_nodes(cluster_name)
    if not nodes:
        print(f"✗ No nodes found for cluster '{cluster_name}'")
        return False

    all_success = True
    for node in nodes:
        node_name = node['name']

        # Enable IPv6
        success, stdout, stderr = exec_on_node(
            node_name,
            'sysctl -w net.ipv6.conf.all.disable_ipv6=0'
        )
        if not success:
            print(f"✗ Failed to enable IPv6 on {node_name}: {stderr}")
            all_success = False
            continue

        # Enable IPv6 forwarding
        success, stdout, stderr = exec_on_node(
            node_name,
            'sysctl -w net.ipv6.conf.all.forwarding=1'
        )
        if not success:
            print(f"✗ Failed to enable IPv6 forwarding on {node_name}: {stderr}")
            all_success = False
            continue

        print(f"✓ {node_name}: IPv6 enabled and forwarding configured")

    if all_success:
        print(f"✓ IPv6 configured on all {len(nodes)} node(s)")

    return all_success

def get_api_server_url(cluster_name: str) -> Optional[str]:
    """Get the API server URL for in-cluster communication

    This retrieves the internal kubeconfig from Kind and resolves the
    control plane node name to its IP address in the kind network.

    Args:
        cluster_name: Name of the Kind cluster

    Returns:
        API server URL string (e.g., https://172.18.0.2:6443), or None if failed
    """
    # Get the internal kubeconfig
    result = subprocess.run(
        ['kind', 'get', 'kubeconfig', '--internal', '--name', cluster_name],
        capture_output=True, text=True
    )

    if result.returncode != 0:
        return None

    # Extract the server URL from the kubeconfig
    dns_name_url = None
    for line in result.stdout.split('\n'):
        if 'server:' in line:
            parts = line.split()
            if len(parts) >= 2:
                dns_name_url = parts[1]
                break

    if not dns_name_url:
        return None

    # Extract the control plane node name (remove https:// and port)
    # e.g., https://cluster-control-plane:6443 -> cluster-control-plane
    cp_node = dns_name_url.replace('https://', '').split(':')[0]

    # Get the node IP address from Docker
    result = subprocess.run(
        ['docker', 'inspect', '-f',
         '{{.NetworkSettings.Networks.kind.IPAddress}}', cp_node],
        capture_output=True, text=True
    )

    if result.returncode != 0 or not result.stdout.strip():
        return None

    node_ip = result.stdout.strip()

    # Replace node name with node IP address in the URL
    api_url = dns_name_url.replace(cp_node, node_ip)

    return api_url


def load_docker_image(cluster_name: str, image: str,
                      local_registry: Optional[Dict[str, Any]] = None) -> bool:
    """Load a Docker image into a Kind cluster

    This loads a local Docker image into all nodes in the Kind cluster.
    If a local registry is configured, this function does nothing since
    images should be pushed to the registry instead.

    Args:
        cluster_name: Name of the Kind cluster
        image: Docker image name (with optional tag)
        local_registry: Local registry configuration dict, or None if not configured

    Returns:
        True if successful (or skipped due to local registry), False otherwise
    """
    # Skip if local registry is configured - images should be pushed there instead
    if local_registry:
        print(f"Skipping image load for '{image}' - using local registry instead")
        return True

    try:
        # Pull the image first to make sure it exists locally
        print(f"Pulling image '{image}'...")
        pull_result = subprocess.run(
            ['docker', 'pull', image],
            capture_output=True, text=True
        )
        if pull_result.returncode != 0:
            print(f"✗ Failed to pull image '{image}': {pull_result.stderr}")
            return False

        result = subprocess.run(
            ['kind', 'load', 'docker-image', image, '--name', cluster_name],
            capture_output=True, text=True
        )
        if result.returncode == 0:
            print(f"✓ Loaded image '{image}' into cluster '{cluster_name}'")
            return True
        else:
            print(f"✗ Failed to load image '{image}': {result.stderr}")
            return False
    except FileNotFoundError:
        print("✗ kind command not found")
        return False
    except Exception as e:
        print(f"✗ Error loading image: {e}")
        return False


def patch_coredns_for_ovn(kubeconfig_path: str, dns_server: str = "8.8.8.8") -> bool:
    """Patch CoreDNS configmap for OVN-Kubernetes compatibility

    This patches CoreDNS to:
    1. Work in an offline environment (no IPv6 connectivity)
    2. Handle additional domains (like .net.) and return NXDOMAIN instead of SERVFAIL

    Args:
        kubeconfig_path: Path to the kubeconfig file
        dns_server: DNS server to use for forwarding (default: 8.8.8.8)

    Returns:
        True if CoreDNS was patched successfully, False otherwise
    """
    # Get the current CoreDNS configmap
    result = subprocess.run(
        ['kubectl', '--kubeconfig', kubeconfig_path,
         'get', '-oyaml', '-n=kube-system', 'configmap/coredns'],
        capture_output=True, text=True
    )

    if result.returncode != 0:
        print(f"✗ Failed to get CoreDNS configmap: {result.stderr}")
        return False

    original_coredns = result.stdout

    # Apply the patches line by line
    patched_lines = []
    for line in original_coredns.split('\n'):
        # Skip lines containing 'upstream', 'fallthrough', or 'loop'
        # These are the problematic lines that need to be removed
        if re.search(r'^\s*upstream\s*$', line):
            continue
        if re.search(r'^\s*fallthrough.*$', line):
            continue
        if re.search(r'^\s*loop\s*$', line):
            continue

        # Add 'net' after 'kubernetes cluster.local'
        line = re.sub(r'^(\s*kubernetes cluster\.local)', r'\1 net', line)

        # Replace forward line to use specified DNS server
        line = re.sub(r'^(\s*forward \.).*$', rf'\1 {dns_server} {{', line)

        patched_lines.append(line)

    patched_coredns = '\n'.join(patched_lines)

    # Apply the patched configmap
    result = subprocess.run(
        ['kubectl', '--kubeconfig', kubeconfig_path, 'apply', '-f', '-'],
        input=patched_coredns, capture_output=True, text=True
    )

    if result.returncode != 0:
        print(f"✗ Failed to apply patched CoreDNS configmap: {result.stderr}")
        return False

    return True
