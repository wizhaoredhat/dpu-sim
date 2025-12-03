#!/usr/bin/env python3
"""
Kind cluster utilities for container-based simulation
"""

import subprocess
import json
import time
from pathlib import Path
from typing import Dict, Any, Optional, List


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
        print(f"  ✗ Kubeconfig not found: {kubeconfig_path}")
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


def generate_networking_yaml(disable_default_cni: bool, cluster_config: Dict[str, Any]) -> str:
    """Generate the networking section of Kind cluster configuration

    Args:
        disable_default_cni: Whether to disable the default CNI and kube-proxy
        cluster_config: Kubernetes cluster configuration

    Returns:
        Networking YAML section content
    """
    networking_yaml = "networking:\n"
    if disable_default_cni:
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
    local_registry = cluster_config.get('local_registry')

    # Build nodes section
    nodes_yaml = ""
    for node in nodes:
        role = node.get('role', 'worker')
        nodes_yaml += f"  - role: {role}\n"

        # Add extra port mappings for control-plane if specified
        if role == 'control-plane':
            extra_port_mappings = node.get('extra_port_mappings', [])
            if extra_port_mappings:
                nodes_yaml += "    extraPortMappings:\n"
                for mapping in extra_port_mappings:
                    nodes_yaml += f"      - containerPort: {mapping.get('container_port', 80)}\n"
                    nodes_yaml += f"        hostPort: {mapping.get('host_port', 80)}\n"
                    nodes_yaml += f"        protocol: {mapping.get('protocol', 'TCP')}\n"

    # Determine if we need to disable default CNI
    # For OVN-Kubernetes, we need to disable both default CNI and kube-proxy
    disable_default_cni = cni.lower() in ['ovn-kubernetes', 'ovn']
    networking_yaml = generate_networking_yaml(disable_default_cni, cluster_config)

    registry_yaml = generate_registry_yaml(local_registry)

    config = f"""kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
{networking_yaml}{registry_yaml}nodes:
{nodes_yaml}"""

    return config

