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
from vm_utils import connect_libvirt, get_vm_ip, get_vm_interface_info_by_type
from cfg_utils import load_config
from ssh_utils import ssh_command


class SoftwareInstaller:
    def __init__(self, config_path="config.yaml"):
        self.config = load_config(config_path)
        self.conn = None
        self.results = {}
        self.cluster_setup_results = {}  # Track cluster setup status
        # Default to Kubernetes version 1.33
        self.k8s_version = self.config.get('kubernetes', {}).get('version', '1.33')

    def connect(self) -> bool:
        """Connect to libvirt"""
        self.conn = connect_libvirt()
        if self.conn:
            print(f"Connected to libvirt: {self.conn.getHostname()}")
            return True
        return False

    def get_vm_ip_with_retry(self, vm_name: str, max_attempts: int = 30) -> str | None:
        """Get IP address of a VM with retry logic"""
        for attempt in range(max_attempts):
            ip = get_vm_ip(self.conn, vm_name)
            if ip:
                return ip

            if attempt < max_attempts - 1:
                time.sleep(2)

        return None

    def wait_for_ssh(self, ip: str, timeout: int = 60) -> bool:
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
        disable_swap_cmd = """
# Disable all active swap
sudo swapoff -a

# Comment out swap entries in fstab
sudo sed -i '/ swap / s/^/#/' /etc/fstab
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, disable_swap_cmd,
            capture_output=True, timeout=300
        )

        if success:
            print("✓ Swap disabled")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to disable swap: {stderr}")
        return success

    def _set_hostname(self, ip: str, vm_name: str) -> bool:
        """Set the hostname on the VM to match the VM name from config

        Args:
            ip: IP address of the VM
            vm_name: Name of the VM from the YAML config
        Returns:
            True if hostname is set, False otherwise
        """
        print(f"\n--- Setting hostname to '{vm_name}' ---")
        set_hostname_cmd = f"sudo hostnamectl set-hostname {vm_name}"
        success, stdout, stderr = ssh_command(
            self.config, ip, set_hostname_cmd,
            capture_output=True, timeout=60
        )

        if success:
            print(f"✓ Hostname set to '{vm_name}'")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to set hostname: {stderr}")
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
sudo dnf install -y NetworkManager-ovs > /dev/null 2>&1 && \
sudo dnf install -y openvswitch > /dev/null 2>&1 && \
sudo systemctl enable openvswitch > /dev/null 2>&1 && \
sudo systemctl restart NetworkManager > /dev/null 2>&1 && \
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
        disable_firewall = """
sudo systemctl disable --now firewalld
sudo dnf remove -y firewalld
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, disable_firewall,
            capture_output=True, timeout=300
        )
        if success:
            print("✓ Firewalld disabled and removed")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to disable/remove firewalld: {stderr}")
        return success

    def _verify_installation(self, ip: str) -> bool:
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

    def _initialize_k8s_master(self, mgmt_ip: str, k8s_node_ip: str, vm_name: str, pod_cidr: str, service_cidr: str) -> tuple[bool, str | None]:
        """Initialize Kubernetes master node
        Args:
            mgmt_ip: Management IP address of the master node (for SSH)
            k8s_node_ip: Kubernetes node IP address (for apiserver)
            vm_name: Name of the VM
            pod_cidr: Pod network CIDR for the cluster
            service_cidr: Service network CIDR for the cluster
        Returns:
            Tuple of (success, join_command) where join_command is the command workers need to join
        """
        print(f"\n--- Initializing Kubernetes Master on {vm_name} ---")
        print(f"  Management IP: {mgmt_ip}")
        print(f"  Kubernetes Node IP: {k8s_node_ip}")
        print(f"  Pod Network CIDR: {pod_cidr}")
        print(f"  Service CIDR: {service_cidr}")

        # Initialize cluster with kubeadm
        init_cmd = f"""
sudo kubeadm init --pod-network-cidr={pod_cidr} --service-cidr={service_cidr} --apiserver-advertise-address={k8s_node_ip} 2>&1 | tee /tmp/kubeadm-init.log
"""
        print("  Initializing cluster...")
        success, stdout, stderr = ssh_command(
            self.config, mgmt_ip, init_cmd,
            capture_output=True, timeout=600
        )

        if not success:
            print(f"✗ Failed to initialize cluster: {stderr}")
            return False, None

        if stdout:
            print(f"{stdout}")

        print(f"✓ Cluster initialized successfully")

        # Setup kubectl for root user
        setup_kubectl = """
mkdir -p /root/.kube
sudo cp /etc/kubernetes/admin.conf /root/.kube/config
sudo chown root:root /root/.kube/config
"""
        success, stdout, stderr = ssh_command(
            self.config, mgmt_ip, setup_kubectl,
            capture_output=True, timeout=300
        )

        if not success:
            print(f"✗ Failed to setup kubectl: {stderr}")
            return False, None

        print("✓ kubectl configured")

        # Extract join command
        get_join_cmd = "sudo kubeadm token create --print-join-command 2>/dev/null"
        success, join_command, stderr = ssh_command(
            self.config, mgmt_ip, get_join_cmd,
            capture_output=True, timeout=300
        )

        if not success or not join_command:
            print(f"✗ Failed to get join command: {stderr}")
            return False, None

        join_command = join_command.strip()
        print(f"✓ Join command generated")

        return True, join_command

    def _install_flannel(self, ip: str, vm_name: str, pod_cidr: str) -> bool:
        """Install Flannel CNI on the master node
        Args:
            ip: IP address of the master node
            vm_name: Name of the VM
            pod_cidr: Pod network CIDR for the cluster
        Returns:
            True if Flannel is installed, False otherwise
        """
        print(f"\n--- Installing Flannel CNI on {vm_name} ---")

        # If pod_cidr is not the default Flannel CIDR, we need to patch it
        if pod_cidr != "10.244.0.0/16":
            print(f"  Using custom pod CIDR: {pod_cidr}")
            flannel_install = f"""
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
kubectl patch configmap kube-flannel-cfg -n kube-flannel --type merge -p '{{"data":{{"net-conf.json":"{{\\\"Network\\\": \\\"{pod_cidr}\\\", \\\"Backend\\\": {{\\\"Type\\\": \\\"vxlan\\\"}}}}"}}}}' || true
kubectl rollout restart daemonset kube-flannel-ds -n kube-flannel || true
"""
        else:
            flannel_install = """
kubectl apply -f https://github.com/flannel-io/flannel/releases/latest/download/kube-flannel.yml
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, flannel_install,
            capture_output=True, timeout=300
        )

        if success:
            print("✓ Flannel CNI installed")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to install Flannel: {stderr}")
            return False

        # Wait for Flannel pods to be ready
        print("  Waiting for Flannel pods to be ready...")
        wait_cmd = """
for i in {1..30}; do
    if kubectl get pods -n kube-flannel -o jsonpath='{.items[*].status.containerStatuses[*].ready}' 2>/dev/null | grep -q true; then
        echo "ready"
        exit 0
    fi
    sleep 2
done
echo "timeout"
exit 1
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, wait_cmd,
            capture_output=True, timeout=90
        )

        if success and "ready" in stdout:
            print("✓ Flannel pods are ready")
            return True
        else:
            print("✗ Flannel pods may still be initializing (this is normal)")
            return True  # Don't fail the installation

    def _install_ovn_kubernetes(self, mgmt_ip: str, k8s_node_ip: str, vm_name: str, pod_cidr: str, service_cidr: str) -> bool:
        """Install OVN-Kubernetes CNI on the master node
        OVN-Kubernetes provides advanced networking features including:
        - Network policies
        - Egress IPs
        - Hardware offload support for SmartNICs/DPUs

        Args:
            mgmt_ip: Management IP address of the master node (for SSH)
            k8s_node_ip: Kubernetes node IP address (for apiserver)
            vm_name: Name of the VM
            pod_cidr: Pod network CIDR for the cluster
            service_cidr: Service network CIDR for the cluster
        Returns:
            True if OVN-Kubernetes is installed, False otherwise
        """
        print(f"\n--- Installing OVN-Kubernetes CNI on {vm_name} ---")
        print(f"  Pod CIDR: {pod_cidr}")
        print(f"  Service CIDR: {service_cidr}")
        print(f"  K8s API Server: https://{k8s_node_ip}:6443")

        # Clone OVN-Kubernetes repo and apply manifests
        # OVN-Kubernetes requires downloading the repo and running the installation script
        # All documented here: https://github.com/ovn-kubernetes/ovn-kubernetes/blob/master/docs/installation/INSTALL.KUBEADM.md
        ovn_image = "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master"
        ovnk_install = f"""
sudo dnf install -y git python3 pip3 > /dev/null 2>&1

# Clone OVN-Kubernetes repository
cd /tmp
rm -rf ovn-kubernetes
git clone https://github.com/ovn-org/ovn-kubernetes.git --depth 1

cd ovn-kubernetes/dist/images

# Set the OVN-Kubernetes image
# From: https://github.com/ovn-kubernetes/ovn-kubernetes/blob/master/docs/developer-guide/image-build.md

# Generate the OVN-Kubernetes manifests (uses python3 and pip3 to generate the yaml files below)
./daemonset.sh --image={ovn_image} \
    --net-cidr={pod_cidr} \
    --svc-cidr={service_cidr} \
    --gateway-mode="shared" \
    --k8s-apiserver=https://{k8s_node_ip}:6443

# Apply the manifests
# ovn-setup.yaml creates the ovn-kubernetes namespace and RBAC resources (no pods are created)
kubectl create -f ../yaml/ovn-setup.yaml
# Create all RBAC resources and service accounts
kubectl create -f ../yaml/rbac-ovnkube-master.yaml
kubectl create -f ../yaml/rbac-ovnkube-db.yaml
kubectl create -f ../yaml/rbac-ovnkube-node.yaml
kubectl create -f ../yaml/rbac-ovnkube-identity.yaml

# ovnkube-identity.yaml creates the ovnkube-identity deployment which approves pending CSRs
kubectl create -f ../yaml/ovnkube-identity.yaml

# ovnkube-db.yaml creates the ovnkube-db deployment
kubectl create -f ../yaml/ovnkube-db.yaml

# Wait for ovnkube-db deployment to be ready
kubectl rollout status deployment/ovnkube-db -n ovn-kubernetes --timeout=300s

# ovnkube-master.yaml creates the ovnkube-master deployment
kubectl create -f ../yaml/ovnkube-master.yaml

# Wait for ovnkube-master deployment to be ready
kubectl rollout status deployment/ovnkube-master -n ovn-kubernetes --timeout=300s

# Wait for ovnkube-master pod to be fully ready (all containers running)
echo "Waiting for ovnkube-master pod to be fully ready..."
kubectl wait --for=condition=Ready pod -l name=ovnkube-master -n ovn-kubernetes --timeout=300s

# ovnkube-node.yaml creates the ovnkube-node daemonset
kubectl create -f ../yaml/ovnkube-node.yaml

# Auto-approve any pending CSRs for ovnkube-node
sleep 5
for csr in $(kubectl get csr -o name 2>/dev/null); do
    kubectl certificate approve $csr 2>/dev/null || true
done

# Wait for ovnkube-node daemonset to be ready
kubectl rollout status daemonset/ovnkube-node -n ovn-kubernetes --timeout=300s

# Delete kube-proxy DaemonSet
kubectl delete ds -n kube-system kube-proxy
"""
        success, stdout, stderr = ssh_command(
            self.config, mgmt_ip, ovnk_install,
            capture_output=True, timeout=600
        )

        if success:
            print("✓ OVN-Kubernetes CNI installed")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to install OVN-Kubernetes: {stderr}")
            return False

        # Wait for OVN-Kubernetes pods to be ready
        print("  Waiting for OVN-Kubernetes pods to be ready...")
        wait_cmd = """
for i in {1..60}; do
    # Check if ovnkube-master and ovnkube-node pods are running
    master_ready=$(kubectl get pods -n ovn-kubernetes -l name=ovnkube-master -o jsonpath='{.items[*].status.phase}' 2>/dev/null | grep -c Running || echo 0)
    node_ready=$(kubectl get pods -n ovn-kubernetes -l name=ovnkube-node -o jsonpath='{.items[*].status.phase}' 2>/dev/null | grep -c Running || echo 0)

    if [ "$master_ready" -ge 1 ] && [ "$node_ready" -ge 1 ]; then
        echo "ready"
        exit 0
    fi
    sleep 5
done
echo "timeout"
exit 1
"""
        success, stdout, stderr = ssh_command(
            self.config, mgmt_ip, wait_cmd,
            capture_output=True, timeout=320
        )

        if success and "ready" in stdout:
            print("✓ OVN-Kubernetes pods are ready")
        else:
            print("✗ OVN-Kubernetes pods may still be initializing (this is normal)")

        # Display OVN-Kubernetes status
        print("  Checking OVN-Kubernetes status...")
        status_cmd = """
echo "OVN-Kubernetes Pods:"
kubectl get pods -n ovn-kubernetes 2>/dev/null || echo "Namespace not found yet"
"""
        success, stdout, stderr = ssh_command(
            self.config, mgmt_ip, status_cmd,
            capture_output=True, timeout=30
        )

        if success and stdout:
            print(stdout)

        print("✓ OVN-Kubernetes CNI setup complete")
        return True

    def _setup_ovn_brex(self, ip: str, vm_name: str) -> bool:
        """Setup br-ex (external bridge) for OVN-Kubernetes
        The br-ex bridge is the external gateway bridge that handles traffic
        between the cluster and external networks. This is required for
        OVN-Kubernetes gateway functionality.

        This function finds the network with type="k8s" in the config, gets the
        corresponding interface inside the VM, creates br-ex, adds that interface
        to the bridge, and moves the IP address to br-ex.

        Args:
            ip: IP address of the node (management IP to SSH into)
            vm_name: Name of the VM
        Returns:
            True if br-ex is setup successfully, False otherwise
        """
        print(f"\n--- Setting up br-ex bridge on {vm_name} ---")

        # Get the interface name and IP address for the MGMT network
        result = get_vm_interface_info_by_type(self.config, ip, 'mgmt')
        if not result:
            print("✗ Could not find interface for MGMT network in VM")
            return False

        mgmt_iface_name, mgmt_iface_ip = result
        print(f"  MGMT Interface: {mgmt_iface_name}, IP: {mgmt_iface_ip}")

        # Get the interface name and IP address for the K8s network
        result = get_vm_interface_info_by_type(self.config, ip, 'k8s')
        if not result:
            print("✗ Could not find interface for K8s network in VM")
            return False

        iface_name, iface_ip = result
        print(f"  K8s Interface: {iface_name}, IP: {iface_ip}")

        # Setup br-ex OVS bridge for OVN-Kubernetes
        # This creates the external bridge, adds the interface, and moves the IP
        brex_setup = f"""
BRIDGE_NAME=br-ex
IF1={mgmt_iface_name}
IF1_CONN=$(nmcli -g GENERAL.CONNECTION device show $IF1)
IF2={iface_name}
IF2_MAC=$(cat /sys/class/net/$IF2/address)

nmcli c add type ovs-bridge conn.interface $BRIDGE_NAME con-name $BRIDGE_NAME
nmcli c add type ovs-port conn.interface $BRIDGE_NAME master $BRIDGE_NAME con-name ovs-port-$BRIDGE_NAME
nmcli c add type ovs-interface slave-type ovs-port conn.interface $BRIDGE_NAME master ovs-port-$BRIDGE_NAME con-name ovs-if-$BRIDGE_NAME
nmcli c add type ovs-port conn.interface $IF2 master $BRIDGE_NAME con-name ovs-port-$IF2
nmcli c add type ethernet conn.interface $IF2 master ovs-port-$IF2 con-name ovs-if-$IF2
nmcli conn delete $IF2
sudo ip addr flush dev $IF2
nmcli conn mod $BRIDGE_NAME connection.autoconnect yes
nmcli conn mod ovs-if-$IF2 connection.autoconnect yes
nmcli conn mod ovs-port-$IF2 connection.autoconnect yes
nmcli conn mod ovs-if-$BRIDGE_NAME connection.autoconnect yes
nmcli conn mod ovs-port-$BRIDGE_NAME connection.autoconnect yes
# Set the br-ex interface to use DHCP
nmcli conn mod ovs-if-$BRIDGE_NAME ipv4.method auto
nmcli conn mod ovs-if-$BRIDGE_NAME ipv4.route-metric 50
# Set the br-ex interface to be the default route
nmcli conn mod ovs-if-$BRIDGE_NAME ipv4.never-default no
# Set the MAC address of the br-ex interface to the MAC address of the IF2 interface
# to get the same DHCP lease on the br-ex interface as the IF2 interface
nmcli conn mod ovs-if-$BRIDGE_NAME 802-3-ethernet.cloned-mac-address $IF2_MAC

# Make sure the MGMT interface is not the default route
nmcli conn mod "$IF1_CONN" ipv4.never-default yes
nmcli conn mod "$IF1_CONN" ipv4.ignore-auto-dns yes
nmcli conn up "$IF1_CONN"

# Activate the OVS connections immediately (order matters: bridge first, then ports)
nmcli conn up $BRIDGE_NAME
nmcli conn up ovs-if-$IF2
nmcli conn up ovs-port-$IF2
nmcli conn up ovs-if-$BRIDGE_NAME
nmcli conn up ovs-port-$BRIDGE_NAME

# Known issue for br-int bridge (not properly created by OVN)
ovs-vsctl add-br br-int

# Set br-ex as the external bridge for OVN
#sudo ovs-vsctl set open_vswitch . external-ids:ovn-bridge-mappings="physnet1:br-ex"
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, brex_setup,
            capture_output=True, timeout=120
        )

        if success:
            print("✓ br-ex bridge created and configured")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to setup br-ex bridge: {stderr}")
            return False

        # Verify the bridge is properly configured
        verify_cmd = """
echo "OVS bridges:"
sudo ovs-vsctl list-br
sudo ovs-vsctl show

echo "Routes:"
ip r

echo "Bridge br-ex interface status:"
ip addr ls dev br-ex

echo "NMCLI connections:"
nmcli conn

echo "OVN external-ids:"
sudo ovs-vsctl get open_vswitch . external-ids
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, verify_cmd,
            capture_output=True, timeout=30
        )

        if success and stdout:
            print(stdout)

        print("✓ br-ex bridge setup complete")
        return True

    def _install_multus(self, ip: str, vm_name: str) -> bool:
        """Install Multus CNI on the master node
        Multus is a meta-plugin that enables attaching multiple network interfaces to pods.
        It wraps an existing CNI plugin (like Flannel) as the default network and allows
        additional networks to be attached via NetworkAttachmentDefinitions.

        Args:
            ip: IP address of the master node
            vm_name: Name of the VM
        Returns:
            True if Multus is installed, False otherwise
        """
        print(f"\n--- Installing Multus CNI on {vm_name} ---")

        # Install Multus thick plugin (recommended for most deployments)
        # The thick plugin runs as a daemon and provides better stability
        multus_install = """
kubectl apply -f https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, multus_install,
            capture_output=True, timeout=300
        )

        if success:
            print("✓ Multus CNI installed")
            if stdout:
                print(stdout)
        else:
            print(f"✗ Failed to install Multus: {stderr}")
            return False

        # Wait for Multus pods to be ready
        print("  Waiting for Multus pods to be ready...")
        wait_cmd = """
for i in {1..30}; do
    if kubectl get pods -n kube-system -l app=multus -o jsonpath='{.items[*].status.containerStatuses[*].ready}' 2>/dev/null | grep -q true; then
        echo "ready"
        exit 0
    fi
    sleep 2
done
echo "timeout"
exit 1
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, wait_cmd,
            capture_output=True, timeout=90
        )

        if success and "ready" in stdout:
            print("✓ Multus pods are ready")
        else:
            print("✗ Multus pods may still be initializing (this is normal)")

        # Verify Multus installation by checking the CNI config
        print("  Verifying Multus CNI configuration...")
        verify_cmd = """
# Check if Multus has created its CNI config
if ls /etc/cni/net.d/*multus* 2>/dev/null; then
    echo "Multus CNI config found"
else
    echo "Waiting for Multus to create CNI config..."
    sleep 5
fi

# List CNI configs to show Multus is wrapping Flannel
echo "CNI configurations:"
ls -la /etc/cni/net.d/ 2>/dev/null || echo "CNI directory not accessible"
"""
        success, stdout, stderr = ssh_command(
            self.config, ip, verify_cmd,
            capture_output=True, timeout=30
        )

        if success and stdout:
            print(stdout)

        print("✓ Multus CNI setup complete - Flannel is now the default network")
        print("  Additional networks can be added via NetworkAttachmentDefinitions")

        return True

    def _join_k8s_worker(self, ip: str, vm_name: str, join_command: str) -> bool:
        """Join a worker node to the Kubernetes cluster
        Args:
            ip: IP address of the worker node
            vm_name: Name of the VM
            join_command: The kubeadm join command from the master
        Returns:
            True if worker joined successfully, False otherwise
        """
        print(f"\n--- Joining {vm_name} to Kubernetes cluster ---")

        # Execute join command
        success, stdout, stderr = ssh_command(
            self.config, ip, f"sudo {join_command}",
            capture_output=True, timeout=300
        )

        if success:
            print(f"✓ {vm_name} joined the cluster")
            if stdout:
                print(stdout)
            return True
        else:
            print(f"✗ Failed to join {vm_name} to cluster: {stderr}")
            return False

    def _approve_pending_csrs(self, master_ip: str) -> bool:
        """Approve all pending Certificate Signing Requests (CSRs)

        This is needed after worker nodes join the cluster, as kubelet generates
        CSRs that need to be approved for the node to be fully functional.

        Args:
            master_ip: IP address of the master node (to run kubectl commands)
        Returns:
            True if CSRs were approved successfully, False otherwise
        """
        print("  Approving pending CSRs...")

        # Get list of pending CSRs and approve them
        approve_cmd = """
for csr in $(kubectl get csr -o jsonpath='{.items[?(@.status.conditions==null)].metadata.name}' 2>/dev/null); do
    if [ -n "$csr" ]; then
        echo "Approving CSR: $csr"
        kubectl certificate approve "$csr" 2>/dev/null || true
    fi
done

# Also approve any CSRs that are explicitly in Pending state
for csr in $(kubectl get csr 2>/dev/null | grep Pending | awk '{print $1}'); do
    if [ -n "$csr" ]; then
        echo "Approving CSR: $csr"
        kubectl certificate approve "$csr" 2>/dev/null || true
    fi
done
echo "CSR approval complete"
"""
        success, stdout, stderr = ssh_command(
            self.config, master_ip, approve_cmd,
            capture_output=True, timeout=60
        )

        if success:
            if stdout and "Approving CSR:" in stdout:
                print(f"✓ CSRs approved")
                print(stdout)
            else:
                print("  No pending CSRs to approve")
            return True
        else:
            print(f"  Warning: CSR approval check failed: {stderr}")
            # Don't fail the whole process for CSR issues
            return True

    def _wait_for_ovnkube_node_and_approve_csrs(self, master_ip: str, worker_node_name: str, max_retries: int = 3) -> bool:
        """Wait for ovnkube-node pod to be scheduled on a worker, then approve CSRs

        When a worker joins an OVN-Kubernetes cluster, the ovnkube-node daemonset
        schedules a pod on that worker. The pod generates CSRs that need to be approved
        before the pod can become fully ready.

        Args:
            master_ip: IP address of the master node (to run kubectl commands)
            worker_node_name: Name of the worker node to wait for
            max_retries: Maximum number of retry attempts
        Returns:
            True if successful, False otherwise
        """
        for attempt in range(1, max_retries + 1):
            print(f"  Waiting for ovnkube-node pod on {worker_node_name} (attempt {attempt}/{max_retries})...")

            # Wait for ovnkube-node pod to be scheduled on this worker
            wait_cmd = f"""
for i in $(seq 1 24); do
    # Check if ovnkube-node pod exists on this node
    pod=$(kubectl get pods -n ovn-kubernetes -l name=ovnkube-node \
        --field-selector spec.nodeName={worker_node_name} \
        -o jsonpath='{{.items[0].metadata.name}}' 2>/dev/null)
    if [ -n "$pod" ]; then
        echo "pod_found:$pod"
        exit 0
    fi
    sleep 5
done
echo "timeout"
exit 1
"""
            success, stdout, stderr = ssh_command(
                self.config, master_ip, wait_cmd,
                capture_output=True, timeout=150
            )

            if not success or "timeout" in stdout:
                print(f"  Warning: ovnkube-node pod not found on {worker_node_name}")
                continue

            pod_name = stdout.strip().split("pod_found:")[-1].strip()
            print(f"  Found ovnkube-node pod: {pod_name}")

            self._approve_pending_csrs(master_ip)

            # Wait for all containers in the ovnkube-node pod to become ready
            print(f"  Waiting for all ovnkube-node containers to be ready on {worker_node_name}...")
            ready_cmd = f"""
POD=$(kubectl get pods -n ovn-kubernetes -l name=ovnkube-node \
    --field-selector spec.nodeName={worker_node_name} \
    -o jsonpath='{{.items[0].metadata.name}}' 2>/dev/null)

if [ -z "$POD" ]; then
    echo "no_pod"
    exit 1
fi

# Wait up to 60 seconds for all containers to be ready
for i in $(seq 1 12); do
    TOTAL=$(kubectl get pod "$POD" -n ovn-kubernetes -o jsonpath='{{.spec.containers[*].name}}' 2>/dev/null | wc -w)
    READY=$(kubectl get pod "$POD" -n ovn-kubernetes -o jsonpath='{{.status.containerStatuses[?(@.ready==true)].name}}' 2>/dev/null | wc -w)

    if [ "$TOTAL" -gt 0 ] && [ "$TOTAL" -eq "$READY" ]; then
        echo "all_ready:$READY/$TOTAL"
        exit 0
    fi
    echo "waiting:$READY/$TOTAL"
    sleep 5
done
echo "timeout:$READY/$TOTAL"
exit 1
"""
            success, stdout, stderr = ssh_command(
                self.config, master_ip, ready_cmd,
                capture_output=True, timeout=90
            )

            if success and "all_ready:" in stdout:
                container_status = stdout.strip().split("all_ready:")[-1].strip()
                print(f"  ✓ All ovnkube-node containers are ready on {worker_node_name} ({container_status})")
                return True
            else:
                # Extract last status for logging
                if "timeout:" in stdout or "waiting:" in stdout:
                    status_line = [l for l in stdout.strip().split('\n') if ':' in l][-1] if stdout else "unknown"
                    print(f"  ovnkube-node containers not ready yet on {worker_node_name} ({status_line}), retrying...")
                else:
                    print(f"  ovnkube-node not ready yet on {worker_node_name}, retrying...")
                self._approve_pending_csrs(master_ip)

        print(f"  ✗ Failed: ovnkube-node did not become ready on {worker_node_name} after {max_retries} attempts")
        return False

    def install_on_vm(self, vm_name: str) -> bool:
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

        if not self._set_hostname(ip, vm_name):
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

    def get_cluster_config(self, cluster_name: str) -> dict | None:
        """Get cluster configuration by name
        Args:
            cluster_name: Name of the cluster
        Returns:
            Cluster configuration dict or None if not found
        """
        clusters = self.config.get('kubernetes', {}).get('clusters', [])
        for cluster in clusters:
            if cluster['name'] == cluster_name:
                return cluster
        return None

    def setup_k8s_cluster(self) -> bool:
        """Setup Kubernetes clusters - initialize masters and join workers"""
        print("\n=== Setting up Kubernetes Clusters ===\n")

        # Group VMs by cluster
        clusters_vms = {}  # cluster_name -> {'masters': [], 'workers': []}

        for vm in self.config['vms']:
            k8s_role = vm.get('k8s_role')
            k8s_cluster = vm.get('k8s_cluster')

            if not k8s_role or not k8s_cluster:
                continue

            if k8s_cluster not in clusters_vms:
                clusters_vms[k8s_cluster] = {'masters': [], 'workers': []}

            if k8s_role == 'master':
                clusters_vms[k8s_cluster]['masters'].append(vm)
            elif k8s_role == 'worker':
                clusters_vms[k8s_cluster]['workers'].append(vm)

        if not clusters_vms:
            print("✗ No clusters found, skipping cluster setup")
            return True

        # Setup each cluster
        for cluster_name, vms in clusters_vms.items():
            print(f"Setting up cluster: {cluster_name}")

            cluster_config = self.get_cluster_config(cluster_name)
            if not cluster_config:
                print(f"✗ Cluster configuration not found for '{cluster_name}'")
                self.cluster_setup_results[cluster_name] = False
                return False

            pod_cidr = cluster_config.get('pod_cidr', '10.244.0.0/16')
            service_cidr = cluster_config.get('service_cidr', '10.245.0.0/16')
            print(f"  Pod Network CIDR: {pod_cidr}")
            print(f"  Service CIDR: {service_cidr}")

            master_vms = vms['masters']
            worker_vms = vms['workers']

            if not master_vms:
                print(f"✗ No master nodes found for cluster '{cluster_name}'")
                self.cluster_setup_results[cluster_name] = False
                return False

            # Initialize the first master node
            master_vm = master_vms[0]
            master_name = master_vm['name']
            print(f"\nInitializing Kubernetes cluster on master: {master_name}")

            # Get management IP (for SSH) and k8s node IP (for apiserver) separately
            master_mgmt_ip = self.get_vm_ip_with_retry(master_name)
            if not master_mgmt_ip:
                print(f"✗ Failed: Could not get management IP address for master {master_name}")
                self.cluster_setup_results[cluster_name] = False
                return False

            master_k8s_ip = master_vm.get('k8s_node_ip')
            if not master_k8s_ip:
                print(f"✗ Failed: No k8s_node_ip configured for master {master_name}")
                self.cluster_setup_results[cluster_name] = False
                return False

            print(f"  Master Management IP: {master_mgmt_ip}")
            print(f"  Master K8s Node IP: {master_k8s_ip}")

            # Get CNI type from config
            cni_type = cluster_config.get('cni', 'flannel').lower()
            print(f"  CNI Plugin: {cni_type}")

            # Setup br-ex bridge on all VMs first before K8s initialization (for OVN-Kubernetes)
            if cni_type == 'ovn-kubernetes':
                # Setup br-ex on master first
                if not self._setup_ovn_brex(master_mgmt_ip, master_name):
                    print(f"✗ Failed to setup br-ex bridge on master {master_name}")
                    self.cluster_setup_results[cluster_name] = False
                    return False

                # Setup br-ex on all worker nodes before K8s initialization
                for worker_vm in worker_vms:
                    worker_name = worker_vm['name']
                    worker_ip = self.get_vm_ip_with_retry(worker_name)

                    if not worker_ip:
                        print(f"✗ Failed: Could not get IP address for worker {worker_name}")
                        self.cluster_setup_results[cluster_name] = False
                        return False

                    if not self._setup_ovn_brex(worker_ip, worker_name):
                        print(f"✗ Failed to setup br-ex bridge on worker {worker_name}")
                        self.cluster_setup_results[cluster_name] = False
                        return False

            success, join_command = self._initialize_k8s_master(master_mgmt_ip, master_k8s_ip, master_name, pod_cidr, service_cidr)
            if not success:
                print(f"✗ Failed to initialize master node {master_name}")
                self.cluster_setup_results[cluster_name] = False
                return False

            # Install the selected CNI plugin
            if cni_type == 'ovn-kubernetes':
                if not self._install_ovn_kubernetes(master_mgmt_ip, master_k8s_ip, master_name, pod_cidr, service_cidr):
                    print(f"✗ Failed to install OVN-Kubernetes CNI on {master_name}")
                    self.cluster_setup_results[cluster_name] = False
                    return False

            elif cni_type == 'flannel':
                if not self._install_flannel(master_mgmt_ip, master_name, pod_cidr):
                    print(f"✗ Failed to install Flannel CNI on {master_name}")
                    self.cluster_setup_results[cluster_name] = False
                    return False

                # Install Multus CNI on top of Flannel
                # Multus wraps Flannel as the default network and enables multiple network interfaces
                if not self._install_multus(master_mgmt_ip, master_name):
                    print(f"✗ Failed to install Multus CNI on {master_name}")
                    self.cluster_setup_results[cluster_name] = False
                    return False
            else:
                print(f"✗ Unknown CNI type '{cni_type}' for cluster '{cluster_name}'")
                print(f"  Supported CNI types: 'flannel', 'ovn-kubernetes'")
                self.cluster_setup_results[cluster_name] = False
                return False

            # Join worker nodes
            if worker_vms and join_command:
                print(f"\nJoining {len(worker_vms)} worker node(s) to cluster '{cluster_name}'...")

                for worker_vm in worker_vms:
                    worker_name = worker_vm['name']
                    worker_ip = self.get_vm_ip_with_retry(worker_name)

                    if not worker_ip:
                        print(f"✗ Failed: Could not get IP address for worker {worker_name}")
                        return False

                    if not self._join_k8s_worker(worker_ip, worker_name, join_command):
                        print(f"✗ Failed to join worker {worker_name}")
                        return False

                    # For OVN-Kubernetes: wait for ovnkube-node pod and approve CSRs
                    if cni_type == 'ovn-kubernetes':
                        self._wait_for_ovnkube_node_and_approve_csrs(master_mgmt_ip, worker_name)
                    else:
                        # For other CNIs, just approve any pending CSRs
                        time.sleep(5)
                        self._approve_pending_csrs(master_mgmt_ip)

            # Wait a bit for nodes to register
            print("\n  Waiting for nodes to register...")
            time.sleep(5)

            # Display cluster status
            print(f"\n--- Cluster '{cluster_name}' Status ---")
            success, stdout, stderr = ssh_command(
                self.config, master_mgmt_ip, "kubectl get nodes",
                capture_output=True, timeout=30
            )
            if success and stdout:
                print(stdout)
            else:
                print(f"✗ Could not get cluster status: {stderr}")

            # Track cluster setup result
            self.cluster_setup_results[cluster_name] = True
            print(f"\n✓ Cluster '{cluster_name}' setup complete!")

        return True

    def install_all_vms(self, parallel: bool = False) -> None:
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

        # Setup Kubernetes cluster after all VMs are installed
        if all(self.results.values()):
            print("\n✓ All VMs installed successfully!")
            if not self.setup_k8s_cluster():
                print("✗ Kubernetes cluster setup failed")
                # Don't mark as complete failure since installations succeeded
        else:
            print("\n✗ Some VMs failed installation, skipping cluster setup")
            clusters = self.config.get('kubernetes', {}).get('clusters', [])
            for cluster in clusters:
                self.cluster_setup_results[cluster['name']] = False

        self.print_summary()

    def print_summary(self) -> None:
        """Print installation summary"""
        print("\n=== VM Software Installation Summary ===")

        all_success = True
        for vm_name in sorted(self.results.keys()):
            success = self.results[vm_name]
            status = "✓ Success" if success else "✗ Failed"

            # Get k8s role and cluster if available
            vm_config = next((vm for vm in self.config['vms'] if vm['name'] == vm_name), None)
            k8s_role = vm_config.get('k8s_role', 'N/A') if vm_config else 'N/A'
            k8s_cluster = vm_config.get('k8s_cluster', 'N/A') if vm_config else 'N/A'

            print(f"  {vm_name:<20} {status:<15} (cluster: {k8s_cluster}, role: {k8s_role})")
            if not success:
                all_success = False

        # Print cluster setup status if any clusters were configured
        if self.cluster_setup_results:
            print("\n=== Kubernetes Cluster Setup Summary ===")
            for cluster_name in sorted(self.cluster_setup_results.keys()):
                success = self.cluster_setup_results[cluster_name]
                status = "✓ Success" if success else "✗ Failed"
                print(f"  {cluster_name:<20} {status}")
                if not success:
                    all_success = False

        if all_success:
            print("\n✓ All VMs have been configured successfully!")

            if self.cluster_setup_results:
                print("✓ All Kubernetes clusters have been set up successfully!")

            # Find master nodes for next steps instructions
            master_nodes = [vm['name'] for vm in self.config['vms'] if vm.get('k8s_role') == 'master']

            print("\nHints:")
            if master_nodes:
                print(f"  1. Check cluster status: python3 vmctl.py exec {master_nodes[0]} 'kubectl get nodes'")
                print(f"  2. Check pods: python3 vmctl.py exec {master_nodes[0]} 'kubectl get pods -A'")
            print("  3. SSH into VM: python3 vmctl.py ssh <vm-name>")
        else:
            print("\n✗ Some VMs or clusters failed")
            print("\nTroubleshooting:")
            print("  - Check VM is running: python3 vmctl.py list")
            print("  - Check SSH access: python3 vmctl.py ssh <vm-name>")
            print("  - Retry installation: python3 install_software.py")

            if self.cluster_setup_results:
                # Show which clusters failed
                failed_clusters = [name for name, success in self.cluster_setup_results.items() if not success]
                if failed_clusters:
                    print(f"  - Failed cluster(s): {', '.join(failed_clusters)}")

    def cleanup(self) -> None:
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

