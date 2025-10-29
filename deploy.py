#!/usr/bin/env python3

"""
VM Deployment Script
Deploys VMs with custom libvirt network
"""

import os
import sys
import yaml
import libvirt
import subprocess
import time
import argparse
import traceback
from pathlib import Path
from typing import Optional, Dict, Any, List
from vm_utils import cleanup_vms, cleanup_networks, connect_libvirt, get_vm_ip
from cfg_utils import load_config, get_host_dpu_pairs
from bridge_utils import generate_bridge_name, get_host_to_dpu_network_name
from ssh_utils import ssh_command


class VMDeployer:
    def __init__(self, config_path: str = "config.yaml") -> None:
        self.config = load_config(config_path)
        self.conn: Optional[libvirt.virConnect] = None
        self.networks: List[libvirt.virNetwork] = []
        self.vms: List[libvirt.virDomain] = []

    def connect(self) -> bool:
        """Connect to libvirt"""
        self.conn = connect_libvirt()
        if self.conn:
            print(f"✓ Connected to libvirt: {self.conn.getHostname()}")
            return True
        return False

    def cleanup_all(self) -> None:
        """Cleanup all VMs and networks"""
        print("\n=== Cleaning up existing VMs and networks ===\n")
        cleanup_vms(self.config, self.conn)
        cleanup_networks(self.config, self.conn)

    def create_ovs_bridge(self, bridge_name: str) -> bool:
        """Create an OVS bridge if it doesn't exist"""
        try:
            # Check if bridge exists
            result = subprocess.run(['ovs-vsctl', 'br-exists', bridge_name],
                                  capture_output=True)
            if result.returncode == 0:
                print(f"OVS bridge '{bridge_name}' already exists")
                return True

            # Create OVS bridge
            subprocess.run(['ovs-vsctl', 'add-br', bridge_name], check=True)
            print(f"✓Created OVS bridge '{bridge_name}'")

            # Bring bridge up
            subprocess.run(['ip', 'link', 'set', bridge_name, 'up'], check=True)
            return True
        except subprocess.CalledProcessError as e:
            print(f"✗ Failed to create OVS bridge: {e}")
            return False
        except FileNotFoundError:
            print("ovs-vsctl not found. Please install Open vSwitch:")
            print("  sudo dnf install openvswitch")
            print("  sudo systemctl start openvswitch")
            return False


    def create_network(self, net_config: Dict[str, Any]) -> bool:
        """Create a custom libvirt network"""
        net_name = net_config['name']
        bridge_name = net_config['bridge_name']
        net_mode = net_config['mode']
        use_ovs = net_config.get('use_ovs', False)

        if net_mode == 'l2-bridge':
            if use_ovs:
                # Create OVS bridge externally
                if not self.create_ovs_bridge(bridge_name):
                    return False

                # Pure Layer 2 OVS bridge - libvirt will use the existing OVS bridge
                network_xml = f"""
<network>
  <name>{net_name}</name>
  <forward mode='bridge'/>
  <bridge name='{bridge_name}'/>
  <virtualport type='openvswitch'/>
</network>
"""
            else:
                # Pure Layer 2 Linux bridge - no IP, no DHCP, no routing
                network_xml = f"""
<network>
  <name>{net_name}</name>
  <bridge name='{bridge_name}'/>
</network>
"""
        elif net_mode == 'nat':
            # NAT mode - VMs can access internet
            network_xml = f"""
<network>
  <name>{net_name}</name>
  <bridge name='{bridge_name}'/>
  <forward mode='nat'/>
  <ip address='{net_config['gateway']}' netmask='{net_config['subnet_mask']}'>
    <dhcp>
      <range start='{net_config['dhcp_start']}' end='{net_config['dhcp_end']}'/>
    </dhcp>
  </ip>
</network>
"""
        else:
            print(f"✗ Failed parsed unknown network mode: {net_mode}")
            return False

        try:
            network = self.conn.networkDefineXML(network_xml)
            network.setAutostart(1)
            network.create()
            print(f"✓ Created and started network '{net_name}' (mode: {net_mode})")
            self.networks.append(network)
            return True
        except libvirt.libvirtError as e:
            print(f"✗ Failed to create network '{net_name}': {e}")
            return False

    def create_host_to_dpu_networks(self) -> bool:
        """Create host to DPU layer 2 bridge networks for each Host-DPU pair"""
        pairs = get_host_dpu_pairs(self.config)

        for host, dpu in pairs:
            net_name = get_host_to_dpu_network_name(host['name'], dpu['name'])
            bridge_name = generate_bridge_name(host['name'], dpu['name'])

            net_config = {
                'name': net_name,
                'bridge_name': bridge_name,
                'mode': 'l2-bridge',
                'nic_model': 'igb',
                'use_ovs': True
            }

            print(f"Creating host to dpu network '{net_name}' for {host['name']} <-> {dpu['name']} (bridge: {bridge_name})")
            if not self.create_network(net_config):
                print(f"✗ Failed to create host to dpu network {net_name}")
                return False

        return True

    def create_networks(self) -> bool:
        """Create all configured networks"""
        # Create networks from config
        if 'networks' in self.config:
            networks_config = self.config['networks']
            for net_config in networks_config:
                if not self.create_network(net_config):
                    return False
        else:
            print("No network configuration found")

        # Create Host to DPU networks
        if not self.create_host_to_dpu_networks():
            return False

        return True

    def download_cloud_image(self) -> Optional[str]:
        """Download cloud image if not exists"""
        os_config = self.config['operating_system']
        image_path = Path('/var/lib/libvirt/images') / os_config['image_name']

        # Ensure directory exists
        image_path.parent.mkdir(parents=True, exist_ok=True)

        if image_path.exists():
            print(f"✓ OS image already exists at {image_path}")
            return str(image_path)

        print(f"Downloading OS image...")
        print(f"URL: {os_config['image_url']}")

        try:
            subprocess.run([
                'wget',
                '-O', str(image_path),
                os_config['image_url']
            ], check=True)
            print(f"✓ Downloaded image to {image_path}")
            return str(image_path)
        except subprocess.CalledProcessError as e:
            print(f"✗ Failed to download image: {e}")
            return None

    def create_cloud_init_iso(self, vm_name: str, vm_config: Dict[str, Any]) -> Optional[str]:
        """Create cloud-init ISO for VM initialization"""
        cloud_init_dir = Path('/tmp') / f'cloud-init-{vm_name}'
        cloud_init_dir.mkdir(exist_ok=True)

        # Get SSH public key
        ssh_key_path = Path(self.config['ssh']['key_path']).expanduser()
        pub_key_path = Path(str(ssh_key_path) + '.pub')

        if not pub_key_path.exists():
            print(f"✗ SSH public key not found at {pub_key_path}")
            print("Please generate SSH key with: ssh-keygen -t rsa -b 4096")
            return None

        with open(pub_key_path, 'r') as f:
            ssh_public_key = f.read().strip()

        # Create meta-data
        meta_data = f"""instance-id: {vm_name}
local-hostname: {vm_name}
"""

        # Get password from config, default to 'redhat' if not specified
        password = self.config['ssh'].get('password', 'redhat')

        # Create user-data
        user_data = f"""#cloud-config
users:
  - name: {self.config['ssh']['user']}
    sudo: ALL=(ALL) NOPASSWD:ALL
    groups: wheel
    shell: /bin/bash
    ssh_authorized_keys:
      - {ssh_public_key}

# Set password for console access (password: {password})
chpasswd:
  list: |
    {self.config['ssh']['user']}:{password}
  expire: False

# Enable password authentication for emergency access
ssh_pwauth: True

# Update packages
package_update: true
package_upgrade: false

runcmd:
  - systemctl enable sshd
  - systemctl start sshd
"""

        # Write files
        with open(cloud_init_dir / 'meta-data', 'w') as f:
            f.write(meta_data)

        with open(cloud_init_dir / 'user-data', 'w') as f:
            f.write(user_data)

        # Create ISO
        iso_path = Path('/var/lib/libvirt/images') / f'{vm_name}-cloud-init.iso'

        try:
            subprocess.run([
                'genisoimage',
                '-output', str(iso_path),
                '-volid', 'cidata',
                '-joliet',
                '-rock',
                str(cloud_init_dir / 'user-data'),
                str(cloud_init_dir / 'meta-data')
            ], check=True, capture_output=True)
            print(f"✓ Created cloud-init ISO at {iso_path}")
            return str(iso_path)
        except subprocess.CalledProcessError as e:
            print(f"✗ Failed to create cloud-init ISO: {e}")
            print("Please install genisoimage: sudo dnf install genisoimage")
            return None

    def create_vm_disk(self, vm_name: str, base_image: str, size_gb: int) -> Optional[str]:
        """Create VM disk from base image"""
        disk_path = Path('/var/lib/libvirt/images') / f'{vm_name}.qcow2'

        if disk_path.exists():
            print(f"✓ Disk for {vm_name} already exists at {disk_path}")
            return str(disk_path)

        try:
            # Create disk from base image
            subprocess.run([
                'qemu-img', 'create',
                '-f', 'qcow2',
                '-F', 'qcow2',
                '-b', base_image,
                str(disk_path),
                f'{size_gb}G'
            ], check=True)
            print(f"✓ Created disk for {vm_name} at {disk_path}")
            return str(disk_path)
        except subprocess.CalledProcessError as e:
            print(f"✗ Failed to create disk: {e}")
            return None

    def create_vm(self, vm_config: Dict[str, Any]) -> Optional[libvirt.virDomain]:
        """Create and start a VM"""
        vm_name = vm_config['name']

        # Download base image
        base_image = self.download_cloud_image()
        if not base_image:
            return None

        # Create VM disk
        disk_path = self.create_vm_disk(vm_name, base_image, vm_config['disk_size'])
        if not disk_path:
            return None

        # Create cloud-init ISO
        cloud_init_iso = self.create_cloud_init_iso(vm_name, vm_config)
        if not cloud_init_iso:
            return None

        # Build network interfaces for all configured networks
        network_interfaces = ""
        vm_type = vm_config.get('type')
        if 'networks' in self.config:
            for net_config in self.config['networks']:
                # Check if this network should attach to this VM type
                attach_to = net_config.get('attach_to', 'any')
                if attach_to != 'any' and attach_to != vm_type:
                    # Skip this network for this VM type
                    continue

                # Get NIC model (default to virtio if not specified)
                nic_model = net_config.get('nic_model', 'virtio')
                use_ovs = net_config.get('use_ovs', False)

                # Add virtualport for OVS networks
                virtualport = ""
                if use_ovs:
                    virtualport = "\n      <virtualport type='openvswitch'/>"

                network_interfaces += f"""    <interface type='network'>
      <source network='{net_config['name']}'/>{virtualport}
      <model type='{nic_model}'/>
    </interface>
"""

        # Add implicit default network interface for host-DPU pairs
        if vm_type == 'host':
            # Find associated DPU
            for dpu in self.config['vms']:
                if dpu.get('type') == 'dpu' and dpu.get('host') == vm_name:
                    net_name = get_host_to_dpu_network_name(vm_name, dpu['name'])
                    network_interfaces += f"""    <interface type='network'>
      <source network='{net_name}'/>
      <virtualport type='openvswitch'/>
      <model type='igb'/>
    </interface>
"""
                    break
        elif vm_type == 'dpu':
            # Find associated host
            host_name = vm_config.get('host')
            if host_name:
                net_name = get_host_to_dpu_network_name(host_name, vm_name)
                network_interfaces += f"""    <interface type='network'>
      <source network='{net_name}'/>
      <virtualport type='openvswitch'/>
      <model type='igb'/>
    </interface>
"""

        # Create VM XML
        vm_xml = f"""
<domain type='kvm'>
  <name>{vm_name}</name>
  <memory unit='MiB'>{vm_config['memory']}</memory>
  <vcpu>{vm_config['vcpus']}</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
    <ioapic driver='qemu'/>
  </features>
  <cpu mode='host-passthrough'/>
  <iommu model='intel'>
    <driver intremap='on' caching_mode='on' iotlb='on'/>
  </iommu>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <emulator>/usr/libexec/qemu-kvm</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='{disk_path}'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='{cloud_init_iso}'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
{network_interfaces}    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <graphics type='vnc' port='-1' autoport='yes'/>
  </devices>
</domain>
"""

        try:
            vm = self.conn.defineXML(vm_xml)
            vm.setAutostart(1)
            vm.create()
            print(f"✓ Created and started VM '{vm_name}'")
            self.vms.append(vm)
            return vm
        except libvirt.libvirtError as e:
            print(f"✗ Failed to create VM: {e}")
            return None

    def wait_for_vms(self) -> None:
        """Wait for VMs to get IP addresses and SSH connectivity"""
        print("Waiting for VMs to boot and get IP addresses...")
        print("This may take 1-2 minutes...\n")

        for vm_config in self.config['vms']:
            vm_name = vm_config['name']
            max_attempts = 60
            ip = None

            # First, wait for IP address
            for attempt in range(max_attempts):
                ip = get_vm_ip(self.conn, vm_name)
                if ip:
                    break

                if attempt < max_attempts - 1:
                    time.sleep(2)
                    continue

            # Now wait for SSH to be available
            if ip:
                print(f"Waiting for SSH on VM: {vm_name} (IP: {ip})...")
                ssh_max_attempts = 30
                ssh_ready = False

                for ssh_attempt in range(ssh_max_attempts):
                    success, _, _ = ssh_command(self.config, ip, "echo 'SSH Ready'", timeout=5)
                    if success:
                        print(f"✓ SSH is ready on {vm_name}")
                        ssh_ready = True
                        break

                    if ssh_attempt < ssh_max_attempts - 1:
                        time.sleep(2)

                if not ssh_ready:
                    print(f"✗ Warning: SSH did not become ready on {vm_name} after {ssh_max_attempts} attempts\n")
            else:
                print(f"✗ Warning: Could not get IP address for {vm_name}\n")

    def get_vm_interfaces(self, vm_name: str) -> Optional[List[Dict[str, str]]]:
        """Get network interface information from a VM"""
        ip = get_vm_ip(self.conn, vm_name)
        if not ip:
            print(f"✗ Could not get IP for {vm_name}")
            return None

        # Get interface names and MAC addresses
        success, output, error = ssh_command(self.config,
            ip,
            "ip -o link show | grep -v 'lo:' | awk '{print $2, $(NF-2)}' | sed 's/:$//' | sed 's/@.*/:/'",
            capture_output=True,
            timeout=10)

        if not success:
            print(f"✗ Warning: Could not retrieve interfaces from {vm_name}: {error}")
            return None

        interfaces = []
        for line in output.strip().split('\n'):
            if line:
                parts = line.split()
                if len(parts) >= 2:
                    iface_name = parts[0].rstrip(':')
                    mac = parts[1] if len(parts) > 1 else "unknown"
                    interfaces.append({
                        'name': iface_name,
                        'mac': mac
                    })

        return interfaces

    def get_vm_network_mapping(self, vm_config: Dict[str, Any]) -> List[Dict[str, str]]:
        """Build expected network mapping for a VM based on configuration"""
        vm_name = vm_config['name']
        vm_type = vm_config.get('type')
        network_mapping = []

        # Add configured networks
        if 'networks' in self.config:
            for net_config in self.config['networks']:
                # Check if this network should attach to this VM type
                attach_to = net_config.get('attach_to', 'any')
                if attach_to != 'any' and attach_to != vm_type:
                    continue

                network_mapping.append({
                    'network': net_config['name'],
                    'bridge': net_config['bridge_name'],
                    'mode': net_config['mode']
                })

        # Add host-to-DPU networks
        if vm_type == 'host':
            for dpu in self.config['vms']:
                if dpu.get('type') == 'dpu' and dpu.get('host') == vm_name:
                    net_name = get_host_to_dpu_network_name(vm_name, dpu['name'])
                    bridge_name = generate_bridge_name(vm_name, dpu['name'])
                    network_mapping.append({
                        'network': net_name,
                        'bridge': bridge_name,
                        'mode': 'host-to-dpu'
                    })
                    break
        elif vm_type == 'dpu':
            host_name = vm_config.get('host')
            if host_name:
                net_name = get_host_to_dpu_network_name(host_name, vm_name)
                bridge_name = generate_bridge_name(host_name, vm_name)
                network_mapping.append({
                    'network': net_name,
                    'bridge': bridge_name,
                    'mode': 'host-to-dpu'
                })

        return network_mapping

    def display_vm_network_info(self) -> None:
        """Display network interface information for all VMs"""
        print("--- Network Interface Information ---")

        for vm_config in self.config['vms']:
            vm_name = vm_config['name']
            vm_type = vm_config.get('type', 'N/A')

            print(f"\nVM: {vm_name} (type: {vm_type})")

            # Get actual interfaces from the VM
            interfaces = self.get_vm_interfaces(vm_name)
            if not interfaces:
                print("✗ Warning: Could not retrieve interface information")
                continue

            # Get expected network mapping
            network_mapping = self.get_vm_network_mapping(vm_config)

            # Display interface information
            print(f"  {'Interface':<15} {'Network':<30} {'Mode':<15}")

            for idx, iface in enumerate(interfaces):
                if idx < len(network_mapping):
                    net_info = network_mapping[idx]
                    print(f"  {iface['name']:<15} {net_info['network']:<30} {net_info['mode']:<15}")
                else:
                    print(f"  {iface['name']:<15} {'(unmapped)':<30} {'-':<15}")

            # Show any unmapped networks
            if len(network_mapping) > len(interfaces):
                print(f"\n✗ Warning: Expected {len(network_mapping)} interfaces but found {len(interfaces)}")


    def deploy(self, cleanup: bool = True) -> bool:
        """Main deployment workflow"""
        print("=== VM Deployment Starting ===\n")

        if not self.connect():
            return False

        if cleanup:
            self.cleanup_all()

        print("\n--- Creating Networks ---")
        if not self.create_networks():
            return False

        print("\n--- Creating VMs ---")
        for vm_config in self.config['vms']:
            if not self.create_vm(vm_config):
                print(f"✗ Warning: Failed to create VM {vm_config['name']}")

        self.wait_for_vms()

        self.display_vm_network_info()

        print("\n=== Deployment Complete ===")
        print("\nVMs are now running. Next steps:")
        print("  1. List VMs: python3 vmctl.py list")
        print("  2. SSH into VMs: python3 vmctl.py ssh <vm-name>")

        return True

    def cleanup(self) -> None:
        """Cleanup resources"""
        if self.conn:
            self.conn.close()


def main():
    parser = argparse.ArgumentParser(
        description='Deploy VMs with libvirt network to simulate DPU Networks',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python3 deploy.py                       # Deploy with automatic cleanup (idempotent)
  python3 deploy.py --no-cleanup          # Deploy without cleaning up existing VMs and networks
  python3 deploy.py --config custom.yaml  # Use custom config file
        """
    )
    parser.add_argument(
        '--config',
        default='config.yaml',
        help='Path to configuration file (default: config.yaml)'
    )
    parser.add_argument(
        '--no-cleanup',
        action='store_true',
        help='Skip cleanup of existing resources before deployment (not recommended)'
    )

    args = parser.parse_args()

    # Validate config file exists
    config_path = Path(args.config)
    if not config_path.exists():
        print(f"✗ Error: Configuration file '{args.config}' not found!")
        sys.exit(1)

    deployer = VMDeployer(config_path=args.config)

    try:
        success = deployer.deploy(cleanup=not args.no_cleanup)
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n\n✗ Deployment interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Error during deployment: {e}")
        traceback.print_exc()
        sys.exit(1)
    finally:
        deployer.cleanup()


if __name__ == '__main__':
    main()

