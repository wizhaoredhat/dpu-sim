#!/usr/bin/env python3
"""
Bridge and network naming utilities
"""

import hashlib
import subprocess


def generate_bridge_name(host_name: str, dpu_name: str) -> str:
    """Generate a short bridge name from host and DPU names using a hash.

    Linux bridge names are limited to 15 characters (IFNAMSIZ - 1).
    We use 'h2d-' prefix (4 chars) + 8-char hash = 12 chars total.
    """

    combined = f"{host_name}-{dpu_name}"
    hash_digest = hashlib.sha256(combined.encode()).hexdigest()
    # Take first 8 characters of hash
    short_hash = hash_digest[:8]

    return f"h2d-{short_hash}"


def cleanup_ovs_bridge(bridge_name: str) -> None:
    """Remove an OVS bridge"""
    try:
        result = subprocess.run(['ovs-vsctl', 'br-exists', bridge_name],
                              capture_output=True)
        if result.returncode == 0:
            subprocess.run(['ovs-vsctl', 'del-br', bridge_name], check=True)
            print(f"Removed OVS bridge '{bridge_name}'")
    except (subprocess.CalledProcessError, FileNotFoundError):
        pass


def get_host_to_dpu_network_name(host_name: str, dpu_name: str) -> str:
    """Generate a network name for a host to DPU pair"""
    return f"h2d-{host_name}-{dpu_name}"

