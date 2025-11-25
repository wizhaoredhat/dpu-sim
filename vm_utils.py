#!/usr/bin/env python3
"""
Shared utilities for VM deployment and cleanup
"""

import libvirt
import subprocess
from pathlib import Path
from typing import Dict, Any, Optional, Tuple

from bridge_utils import generate_bridge_name, cleanup_ovs_bridge, get_host_to_dpu_network_name
from cfg_utils import get_host_dpu_pairs
from ssh_utils import ssh_command


def connect_libvirt() -> Optional[libvirt.virConnect]:
    """Connect to libvirt QEMU system

    Returns:
        libvirt connection object or None if connection failed
    """
    try:
        conn = libvirt.open('qemu:///system')
        if conn is None:
            print('✗ Failed to open connection to qemu:///system')
            return None
        return conn
    except libvirt.libvirtError as e:
        print(f"✗ Failed to connect to libvirt: {e}")
        return None


def get_vm_interface_info_by_type(
    config: Dict[str, Any],
    vm_ip: str,
    network_type: str
) -> Optional[Tuple[str, str]]:
    """Get the interface name and IP address inside a VM for a network of a given type

    This function finds the network with the specified type (e.g., 'k8s' or 'mgmt')
    in the config, then retrieves the corresponding interface info from inside the VM.

    Args:
        config: Configuration dictionary containing network and SSH settings
        vm_ip: IP address of the VM (management IP to SSH into)
        network_type: Type of network to find (e.g., 'k8s', 'mgmt')

    Returns:
        Tuple of (interface_name, ip_address) or None if not found
    """
    # Find the network with the specified type
    network = None
    for net in config.get('networks', []):
        if net.get('type') == network_type:
            network = net
            break

    if not network:
        print(f"✗ No network with type='{network_type}' found in config")
        return None

    network_name = network['name']
    return get_vm_network_interface_info(config, vm_ip, network_name)


def get_vm_network_interface_info(
    config: Dict[str, Any],
    vm_ip: str,
    network_name: str
) -> Optional[Tuple[str, str]]:
    """Get the interface name and IP address inside a VM for a given network

    Args:
        config: Configuration dictionary containing network and SSH settings
        vm_ip: IP address of the VM (management IP to SSH into)
        network_name: Name of the libvirt network (e.g., "ovn-network")

    Returns:
        Tuple of (interface_name, ip_address) or None if not found
    """
    # Find the network config to get the subnet info
    network_config = None
    for net in config.get('networks', []):
        if net['name'] == network_name:
            network_config = net
            break

    if not network_config:
        print(f"✗ Network {network_name} not found in config")
        return None

    # Get the gateway IP and determine the subnet prefix
    gateway = network_config.get('gateway', '')
    if not gateway:
        print(f"✗ No gateway defined for network {network_name}")
        return None

    # Extract subnet prefix from gateway (e.g., "192.168.123" from "192.168.123.1")
    subnet_prefix = '.'.join(gateway.split('.')[:-1])

    # SSH into the VM and get interface info
    # Using 'ip -4 addr show' to list all interfaces with IPv4 addresses
    success, stdout, stderr = ssh_command(config, vm_ip, "ip -4 -o addr show")

    if not success:
        print(f"✗ Failed to get interface info from VM: {stderr}")
        return None

    # Parse output to find the interface matching the network's subnet
    # Format: "2: eth0    inet 192.168.123.11/24 brd 192.168.123.255 scope global ..."
    for line in stdout.splitlines():
        parts = line.split()
        if len(parts) >= 4:
            # parts[1] is interface name (with colon), parts[3] is IP/CIDR
            iface_name = parts[1].rstrip(':')
            ip_with_cidr = parts[3]
            ip_address = ip_with_cidr.split('/')[0]

            # Check if this IP is in the target network's subnet
            if ip_address.startswith(subnet_prefix + '.'):
                return (iface_name, ip_address)

    print(f"✗ No interface found in VM matching network {network_name} (subnet {subnet_prefix}.*)")
    return None


def get_vm_ip(conn: libvirt.virConnect, vm_name: str) -> Optional[str]:
    """Get IP address of a VM

    Args:
        conn: Active libvirt connection
        vm_name: Name of the VM

    Returns:
        IPv4 address of the VM or None if not found
    """
    try:
        vm = conn.lookupByName(vm_name)
        ifaces = vm.interfaceAddresses(libvirt.VIR_DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)

        for iface_name, iface in ifaces.items():
            if iface['addrs']:
                for addr in iface['addrs']:
                    if addr['type'] == 0:  # IPv4
                        return addr['addr']
    except libvirt.libvirtError as e:
        print(f"✗ Error getting IP for {vm_name}: {e}")

    return None


def cleanup_vms(config: Dict[str, Any], conn: libvirt.virConnect) -> None:
    """Remove all VMs defined in config

    Args:
        config: Configuration dictionary containing VM definitions
        conn: Active libvirt connection
    """
    print("--- Cleaning up VMs ---")

    for vm_config in config['vms']:
        vm_name = vm_config['name']

        try:
            vm = conn.lookupByName(vm_name)

            # Stop VM if running
            if vm.isActive():
                print(f"Stopping {vm_name}...")
                vm.destroy()

            # Undefine VM
            print(f"Removing {vm_name}...")
            vm.undefine()

            # Remove disk
            disk_path = Path('/var/lib/libvirt/images') / f'{vm_name}.qcow2'
            if disk_path.exists():
                print(f"Removing disk for {vm_name}...")
                disk_path.unlink()

            # Remove cloud-init ISO
            iso_path = Path('/var/lib/libvirt/images') / f'{vm_name}-cloud-init.iso'
            if iso_path.exists():
                print(f"Removing cloud-init ISO for {vm_name}...")
                iso_path.unlink()

            print(f"✓ Removed {vm_name}")

        except libvirt.libvirtError:
            print(f"VM {vm_name} not found or already removed")


def cleanup_networks(config: Dict[str, Any], conn: libvirt.virConnect) -> None:
    """Remove all networks (explicit and Host to DPU) defined in config

    Args:
        config: Configuration dictionary containing network and VM definitions
        conn: Active libvirt connection
    """
    print("\n--- Cleaning up Networks ---")

    # Cleanup explicit networks from config
    if 'networks' in config:
        networks_config = config['networks']
    else:
        networks_config = []

    for net_config in networks_config:
        net_name = net_config['name']
        bridge_name = net_config.get('bridge_name')
        use_ovs = net_config.get('use_ovs', False)

        try:
            network = conn.networkLookupByName(net_name)

            # Stop network if active
            if network.isActive():
                print(f"Stopping network {net_name}...")
                network.destroy()

            # Undefine network
            print(f"Removing network {net_name}...")
            network.undefine()

            print(f"✓ Removed network {net_name}")

        except libvirt.libvirtError:
            print(f"Network {net_name} not found or already removed")

        # Cleanup OVS bridge if it was used
        if use_ovs and bridge_name:
            cleanup_ovs_bridge(bridge_name)

    # Cleanup Host to DPU networks
    pairs = get_host_dpu_pairs(config)
    for host, dpu in pairs:
        net_name = get_host_to_dpu_network_name(host['name'], dpu['name'])
        bridge_name = generate_bridge_name(host['name'], dpu['name'])

        try:
            network = conn.networkLookupByName(net_name)

            # Stop network if active
            if network.isActive():
                print(f"Stopping host to DPU network {net_name}...")
                network.destroy()

            # Undefine network
            print(f"Removing host to DPU  network {net_name}...")
            network.undefine()

            print(f"✓ Removed host to DPU  network {net_name}")

        except libvirt.libvirtError:
            print(f"Host to DPU network {net_name} not found or already removed")

        # Cleanup OVS bridge
        cleanup_ovs_bridge(bridge_name)

