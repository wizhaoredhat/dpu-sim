#!/usr/bin/env python3
"""
Software Installation Script
SSHs into each VM and installs Software Components
"""

import sys
import time
import traceback
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor, as_completed
from vm_utils import connect_libvirt, get_vm_ip
from cfg_utils import load_config
from ssh_utils import ssh_command


class SoftwareInstaller:
    def __init__(self, config_path="config.yaml"):
        self.config = load_config(config_path)
        self.conn = None
        self.results = {}
        # Default to Kubernetes version 1.33
        self.k8s_version = self.config.get('kubernetes', {}).get('version', '1.33')

    def connect(self) -> bool:
        """Connect to libvirt"""
        self.conn = connect_libvirt()
        if self.conn:
            print(f"Connected to libvirt: {self.conn.getHostname()}")
            return True
        return False

    def get_vm_ip_with_retry(self, vm_name, max_attempts=30):
        """Get IP address of a VM with retry logic"""
        for attempt in range(max_attempts):
            ip = get_vm_ip(self.conn, vm_name)
            if ip:
                return ip

            if attempt < max_attempts - 1:
                time.sleep(2)

        return None

    def wait_for_ssh(self, ip, timeout=60):
        """Wait for SSH to become available"""
        print(f"  Waiting for SSH on {ip}...", end='', flush=True)

        start_time = time.time()
        while time.time() - start_time < timeout:
            success, _, _ = ssh_command(self.config, ip, 'echo test', capture_output=True, timeout=10)
            if success:
                return True
            time.sleep(2)

        return False

    def _disable_swap(self, ip: str) -> bool:
        """Disable swap on the VM
        From https://github.com/cri-o/packaging/blob/main/README.md

        Args:
            ip: IP address of the Host
        Returns:
            True if swap is disabled, False otherwise
        """
        print(f"\n--- Disabling swap ---")
        success, stdout, stderr = ssh_command(
            self.config, ip,
            "sudo swapoff -a && sudo sed -i '/ swap / s/^/#/' /etc/fstab",
            capture_output=True, timeout=300
        )

        if success:
            print("✓ Swap disabled")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to disable swap: {stderr}")
        return success

    def _configure_kernel_modules(self, ip: str) -> bool:
        """Configure kernel modules for Kubernetes
        From https://kubernetes.io/docs/setup/production-environment/container-runtimes/#configuring-the-container-runtime

        Args:
            ip: IP address of the Host
        Returns:
            True if kernel modules are configured, False otherwise
        """
        print(f"\n--- Configuring kernel modules ---")
        kernel_config = """
sudo tee /etc/modules-load.d/k8s.conf > /dev/null <<EOF
overlay
br_netfilter
EOF

sudo modprobe overlay
sudo modprobe br_netfilter

# Enable IPv4 packets to be routed between interfaces
sudo tee /etc/sysctl.d/k8s.conf > /dev/null <<EOF
net.bridge.bridge-nf-call-iptables = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward = 1
EOF

# Apply sysctl params without reboot
sudo sysctl --system > /dev/null 2>&1
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, kernel_config,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ Kernel modules configured")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to configure kernel modules: {stderr}")
        return success

    def _install_crio(self, ip: str) -> bool:
        """Install and configure CRI-O
        From https://github.com/cri-o/packaging/blob/main/README.md

        Args:
            ip: IP address of the Host
        Returns:
            True if CRI-O is installed and configured, False otherwise
        """
        print(f"\n--- Installing CRI-O ---")
        crio_install = f"""
# Set CRI-O version to match Kubernetes version ({self.k8s_version})
export CRIO_VERSION={self.k8s_version}

# Add CRI-O repository
sudo tee /etc/yum.repos.d/cri-o.repo > /dev/null <<EOF
[cri-o]
name=CRI-O
baseurl=https://pkgs.k8s.io/addons:/cri-o:/stable:/v${{CRIO_VERSION}}/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/addons:/cri-o:/stable:/v${{CRIO_VERSION}}/rpm/repodata/repomd.xml.key
EOF

# Install CRI-O and dependencies
sudo dnf install -y cri-o iproute-tc > /dev/null 2>&1 && \
sudo systemctl enable crio > /dev/null 2>&1 && \
sudo systemctl start crio
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, crio_install,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ CRI-O installed and configured")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to install CRI-O: {stderr}")
        return success

    def _install_openvswitch(self, ip: str) -> bool:
        """Install and configure Open vSwitch
        Args:
            ip: IP address of the Host
        Returns:
            True if Open vSwitch is installed and configured, False otherwise
        """
        print(f"\n--- Installing Open vSwitch ---")
        ovs_install = """
sudo dnf install -y openvswitch > /dev/null 2>&1 && \
sudo systemctl enable openvswitch > /dev/null 2>&1 && \
sudo systemctl start openvswitch
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, ovs_install,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ Open vSwitch installed")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to install Open vSwitch: {stderr}")
        return success

    def _add_kubernetes_repo(self, ip: str) -> bool:
        """Add Kubernetes repository"""
        print(f"\n--- Adding Kubernetes repository ---")
        k8s_repo = f"""
sudo tee /etc/yum.repos.d/kubernetes.repo > /dev/null <<EOF
[kubernetes]
name=Kubernetes
baseurl=https://pkgs.k8s.io/core:/stable:/v{self.k8s_version}/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/core:/stable:/v{self.k8s_version}/rpm/repodata/repomd.xml.key
exclude=kubelet kubeadm kubectl cri-tools kubernetes-cni
EOF
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, k8s_repo,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ Kubernetes repository added")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to add Kubernetes repository: {stderr}")
        return success

    def _install_kubernetes(self, ip: str) -> bool:
        """Install Kubernetes components
        From https://kubernetes.io/docs/setup/production-environment/container-runtimes/#installing-cri-o
        Args:
            ip: IP address of the Host
        Returns:
            True if Kubernetes components are installed, False otherwise
        """
        print(f"\n--- Installing Kubernetes components ---")
        k8s_install = """
sudo dnf install -y kubelet kubeadm kubectl --setopt=disable_excludes=kubernetes > /dev/null 2>&1 && \
sudo systemctl enable kubelet > /dev/null 2>&1
"""
        print("  Installing kubeadm, kubelet, kubectl...")
        success, stdout, stderr = ssh_command(
            self.config, ip, k8s_install,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ Kubernetes components installed")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to install Kubernetes: {stderr}")
        return success

    def _configure_firewall(self, ip: str) -> bool:
        """Configure firewall for Kubernetes"
        Args:
            ip: IP address of the Host
        Returns:
            True if firewall is configured, False otherwise
        """
        print(f"\n--- Configuring firewall ---")
        firewall_config = """
if sudo systemctl is-active firewalld > /dev/null 2>&1; then
    sudo firewall-cmd --permanent --add-port=6443/tcp > /dev/null 2>&1
    sudo firewall-cmd --permanent --add-port=2379-2380/tcp > /dev/null 2>&1
    sudo firewall-cmd --permanent --add-port=10250/tcp > /dev/null 2>&1
    sudo firewall-cmd --permanent --add-port=10251/tcp > /dev/null 2>&1
    sudo firewall-cmd --permanent --add-port=10252/tcp > /dev/null 2>&1
    sudo firewall-cmd --permanent --add-port=10255/tcp > /dev/null 2>&1
    sudo firewall-cmd --permanent --add-port=30000-32767/tcp > /dev/null 2>&1
    sudo firewall-cmd --reload > /dev/null 2>&1
    echo "configured"
fi
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, firewall_config,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ Firewall configured")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to configure firewall: {stderr}")
        return success

    def _verify_installation(self, ip):
        """Verify all installed components"""
        print(f"\n--- Verifying installation ---")

        all_verified = True

        # Verify kubeadm
        success, stdout, stderr = ssh_command(
            self.config, ip, "kubeadm version -o short 2>/dev/null",
            capture_output=True, timeout=300
        )
        if success and stdout:
            print(f"✓ kubeadm: {stdout.strip()}")
        else:
            print(f"✗ kubeadm not found: {stderr}")
            all_verified = False

        # Verify CRI-O
        success, stdout, stderr = ssh_command(
            self.config, ip, "sudo systemctl is-active crio",
            capture_output=True, timeout=300
        )
        if success:
            print(f"✓ CRI-O: {stdout.strip()}")
        else:
            print(f"✗ CRI-O not active: {stderr}")
            all_verified = False

        # Verify OVS
        success, stdout, stderr = ssh_command(
            self.config, ip, "sudo ovs-vsctl --version | head -n 1",
            capture_output=True, timeout=300
        )
        if success and stdout:
            print(f"✓ Open vSwitch: {stdout.strip()}")
        else:
            print(f"✗ OvS not found: {stderr}")
            all_verified = False

        return all_verified

    def install_on_vm(self, vm_name):
        """Install Kubernetes and OVS on a VM"""

        print("=== VM Software Installation Starting ===\n")
        if not self.connect():
            return False

        print(f"Kubernetes version: {self.k8s_version}")
        print(f"Installing on {vm_name}")
        print(f"Getting IP address for {vm_name}...")
        ip = self.get_vm_ip_with_retry(vm_name)
        if not ip:
            print(f"✗ Failed: Could not get IP address for {vm_name}")
            return False

        print(f"✓ IP Address: {ip}")

        if not self.wait_for_ssh(ip):
            print(f"✗ Failed: SSH is not available on {vm_name}")
            return False

        if not self._disable_swap(ip):
            return False

        if not self._configure_kernel_modules(ip):
            return False

        if not self._install_crio(ip):
            return False

        if not self._install_openvswitch(ip):
            return False

        if not self._add_kubernetes_repo(ip):
            return False

        if not self._install_kubernetes(ip):
            return False

        if not self._configure_firewall(ip):
            return False

        if not self._verify_installation(ip):
            return False

        print(f"✓ {vm_name} installation complete!")

        return True

    def install_all_vms(self, parallel=False):
        """Install software on all VMs"""

        vm_names = [vm['name'] for vm in self.config['vms']]

        if parallel:
            print("Installing on all VMs in parallel...\n")
            # Install in parallel
            with ThreadPoolExecutor(max_workers=4) as executor:
                future_to_vm = {
                    executor.submit(self.install_on_vm, vm_name): vm_name
                }

                for future in as_completed(future_to_vm):
                    vm_name = future_to_vm[future]
                    try:
                        success = future.result()
                        self.results[vm_name] = success
                    except Exception as e:
                        print(f"✗ Error installing on {vm_name}: {e}")
                        self.results[vm_name] = False
        else:
            print("Installing on VMs sequentially...\n")
            # Install sequentially for clearer output
            for vm_name in vm_names:
                try:
                    success = self.install_on_vm(vm_name)
                    self.results[vm_name] = success
                except Exception as e:
                    print(f"✗ Error installing on {vm_name}: {e}")
                    self.results[vm_name] = False

        self.print_summary()

    def print_summary(self):
        """Print installation summary"""
        print("=== VM Software Installation Summary ===")

        all_success = True
        for vm_name in sorted(self.results.keys()):
            success = self.results[vm_name]
            status = "✓ Success" if success else "✗ Failed"
            print(f"  {vm_name:<20} {status}")
            if not success:
                all_success = False

        if all_success:
            print("✓ All VMs have been configured successfully!")
            print("\nNext steps:")
            print("  1. Verify: python3 verify_setup.py")
            print("  2. SSH into VM: python3 vmctl.py ssh <vm-name>")
        else:
            print("✗ Some VMs failed to install properly")
            print("\nTroubleshooting:")
            print("  - Check VM is running: python3 vmctl.py list")
            print("  - Check SSH access: python3 vmctl.py ssh <vm-name>")
            print("  - Retry installation: python3 install_software.py")

        return all_success

    def cleanup(self):
        """Cleanup resources"""
        if self.conn:
            self.conn.close()


def main():
    import argparse

    parser = argparse.ArgumentParser(
        description='Install Software Components on VMs',
        formatter_class=argparse.RawDescriptionHelpFormatter
    )
    parser.add_argument('--config',
                       default='config.yaml',
                       help='Path to configuration file (default: config.yaml)')
    parser.add_argument('--parallel', '-p', action='store_true',
                       help='Install on all VMs in parallel (faster but harder to debug)')
    parser.add_argument('--vm', metavar='VM_NAME',
                       help='Install only on specific VM')

    args = parser.parse_args()

    # Validate config file exists
    config_path = Path(args.config)
    if not config_path.exists():
        print(f"✗ Error: Configuration file '{args.config}' not found!")
        sys.exit(1)

    installer = SoftwareInstaller(config_path=args.config)

    try:
        if not installer.connect():
            sys.exit(1)

        if args.vm:
            success = installer.install_on_vm(args.vm)
            sys.exit(0 if success else 1)
        else:
            # Install on all VMs
            installer.install_all_vms(parallel=args.parallel)
            sys.exit(0 if all(installer.results.values()) else 1)

    except KeyboardInterrupt:
        print("\n\n✗ Installation interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Error during installation: {e}")
        traceback.print_exc()
        sys.exit(1)
    finally:
        installer.cleanup()


if __name__ == '__main__':
    main()

