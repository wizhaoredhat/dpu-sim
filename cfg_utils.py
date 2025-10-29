#!/usr/bin/env python3
"""
Configuration utilities
"""

import yaml
from typing import Dict, List, Tuple, Any


def load_config(config_path: str = "config.yaml") -> Dict[str, Any]:
    """Load configuration from YAML file

    Args:
        config_path: Path to the configuration YAML file

    Returns:
        Dictionary representation of the configuration YAML file
    """
    with open(config_path, 'r') as f:
        return yaml.safe_load(f)


def get_host_dpu_pairs(config: Dict[str, Any]) -> List[Tuple[Dict[str, Any], Dict[str, Any]]]:
    """Get Host-DPU pairs from VM config"""
    pairs = []
    hosts = {vm['name']: vm for vm in config['vms'] if vm.get('type') == 'host'}
    dpus = [vm for vm in config['vms'] if vm.get('type') == 'dpu']

    for dpu in dpus:
        host_name = dpu.get('host')
        if host_name and host_name in hosts:
            pairs.append((hosts[host_name], dpu))

    return pairs

