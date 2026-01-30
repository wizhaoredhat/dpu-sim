#!/usr/bin/env python3
"""
Configuration utilities
"""

import yaml
from typing import Dict, List, Tuple, Any, Optional


def load_config(config_path: str = "config.yaml") -> Dict[str, Any]:
    """Load configuration from YAML file

    Args:
        config_path: Path to the configuration YAML file

    Returns:
        Dictionary representation of the configuration YAML file
    """
    with open(config_path, 'r') as f:
        return yaml.safe_load(f)


def get_deployment_mode(config: Dict[str, Any]) -> str:
    """Determine deployment mode from configuration

    Args:
        config: Configuration dictionary

    Returns:
        'vm' for VM-based deployment, 'kind' for container-based deployment
    """
    has_vms = 'vms' in config and config['vms']
    has_kind = 'kind' in config and config['kind']

    if has_kind:
        return 'kind'
    elif has_vms:
        return 'vm'
    else:
        raise ValueError("Neither 'vms' nor 'kind' section found in config")


def is_kind_mode(config: Dict[str, Any]) -> bool:
    """Check if configuration uses Kind mode

    Args:
        config: Configuration dictionary

    Returns:
        True if Kind mode, False otherwise
    """
    return get_deployment_mode(config) == 'kind'


def is_vm_mode(config: Dict[str, Any]) -> bool:
    """Check if configuration uses VM mode

    Args:
        config: Configuration dictionary

    Returns:
        True if VM mode, False otherwise
    """
    return get_deployment_mode(config) == 'vm'


def get_host_dpu_pairs(config: Dict[str, Any]) -> List[Tuple[Dict[str, Any], Dict[str, Any]]]:
    """Get Host-DPU pairs from VM config

    Args:
        config: Configuration dictionary

    Returns:
        List of (host, dpu) config tuples
    """
    if 'vms' not in config:
        return []

    pairs = []
    hosts = {vm['name']: vm for vm in config['vms'] if vm.get('type') == 'host'}
    dpus = [vm for vm in config['vms'] if vm.get('type') == 'dpu']

    for dpu in dpus:
        host_name = dpu.get('host')
        if host_name and host_name in hosts:
            pairs.append((hosts[host_name], dpu))

    return pairs


def get_kind_nodes(config: Dict[str, Any]) -> List[Dict[str, Any]]:
    """Get Kind node configurations

    Args:
        config: Configuration dictionary

    Returns:
        List of Kind node configuration dicts
    """
    return config.get('kind', {}).get('nodes', [])


def get_kind_control_plane_count(config: Dict[str, Any]) -> int:
    """Get number of control-plane nodes in Kind config

    Args:
        config: Configuration dictionary

    Returns:
        Number of control-plane nodes
    """
    nodes = get_kind_nodes(config)
    return sum(1 for n in nodes if n.get('role') == 'control-plane')


def get_kind_worker_count(config: Dict[str, Any]) -> int:
    """Get number of worker nodes in Kind config

    Args:
        config: Configuration dictionary

    Returns:
        Number of worker nodes
    """
    nodes = get_kind_nodes(config)
    return sum(1 for n in nodes if n.get('role', 'worker') == 'worker')


def get_cluster_names(config: Dict[str, Any]) -> List[str]:
    """Get all cluster names from config

    Args:
        config: Configuration dictionary

    Returns:
        List of cluster names
    """
    clusters = config.get('kubernetes', {}).get('clusters', [])
    return [c.get('name') for c in clusters if c.get('name')]


def get_cluster_config(config: Dict[str, Any], cluster_name: str) -> Optional[Dict[str, Any]]:
    """Get Kubernetes cluster configuration by name

    Args:
        config: Configuration dictionary
        cluster_name: Name of the cluster

    Returns:
        Cluster configuration dict or None if not found
    """
    clusters = config.get('kubernetes', {}).get('clusters', [])
    for cluster in clusters:
        if cluster['name'] == cluster_name:
            return cluster
    return None


def get_cni_type(config: Dict[str, Any], cluster_name: str) -> str:
    """Get CNI type for a cluster

    Args:
        config: Configuration dictionary
        cluster_name: Name of the cluster

    Returns:
        CNI type string (default: 'kindnet' for Kind, 'flannel' for VMs)
    """
    cluster = get_cluster_config(config, cluster_name)
    if cluster:
        default_cni = 'kindnet' if is_kind_mode(config) else 'flannel'
        return cluster.get('cni', default_cni).lower()
    return 'kindnet' if is_kind_mode(config) else 'flannel'

