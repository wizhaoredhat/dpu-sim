# VM Deployment with Libvirt + Kubernetes + OVN for emulating DPUs

This project automates the deployment of multiple VMs with a custom libvirt network, pre-configured with Kubernetes and Open vSwitch for container networking experiments.

## Features

- 🚀 Automated deployment of multiple VMs
- ☸️ Kubernetes (kubeadm, kubelet, kubectl) pre-installed on all VMs
- 🔀 Open vSwitch (OVS) used for networking between hosts and DPUs
- 🌐 Multiple network support (NAT, Layer 2 Bridge)
- 🔌 Configurable NIC models (virtio, igb)
- 🖥️ Q35 machine type VM with PCIe and IOMMU support (SR-IOV ready)
- 🔑 SSH key-based authentication
- 💻 Easy VM access via SSH and console
- 🎛️ Full VM lifecycle management (start, stop, reboot)
- ✅ Verification script to check installations
- 🧹 Cleanup script

## Prerequisites

### System Requirements
- Fedora/RHEL/CentOS Linux
- KVM/QEMU virtualization support
- At least 12GB RAM (for all 4 VMs)
- At least 100GB free disk space

### Required Packages

```bash

subscription-manager repos --enable=codeready-builder-for-rhel-9-$(arch)-rpms
subscription-manager repos --enable=fast-datapath-for-rhel-9-$(arch)-rpms
subscription-manager repos --enable=openstack-17-for-rhel-9-$(arch)-rpms

# Install required system packages
sudo dnf install -y \
    qemu-kvm \
    qemu-img \
    python3.12 \
    python3.12-devel \
    libvirt \
    libvirt-devel \
    virt-install \
    genisoimage \
    openvswitch \
    wget
```

Start required services:

```bash
# Start and enable libvirt sockets
sudo systemctl start virtqemud.socket virtnetworkd.socket
sudo systemctl enable virtqemud.socket virtnetworkd.socket

# Start and enable Open vSwitch
sudo systemctl start openvswitch
sudo systemctl enable openvswitch

# Verify services are active
sudo systemctl status virtqemud.socket
sudo systemctl status virtnetworkd.socket
sudo systemctl status openvswitch

# Add user to libvirt group
sudo usermod -a -G libvirt $USER
newgrp libvirt
```

### Python Dependencies

```bash
python3.12 -m venv dpu-sim-venv

source dpu-sim-venv/bin/activate

pip3 install -r requirements.txt
```

### SSH Key Setup

Generate SSH keys if you don't have them:

```bash
ssh-keygen -t rsa -b 4096 -f ~/.ssh/id_rsa
```

## Configuration

Edit `config.yaml` to customize your deployment:

```yaml
networks:
  # Management network with internet access
  - name: "primary-k8s-network"
    bridge_name: "virbr-k8s"
    gateway: "192.168.100.1"
    subnet_mask: "255.255.255.0"
    dhcp_start: "192.168.100.10"
    dhcp_end: "192.168.100.100"
    mode: "nat"
    nic_model: "virtio"  # virtio for management network
    attach_to: "any"  # Attach to all VMs: "dpu", "host", or "any"

  # Pure Layer 2 data network with OVS (no IP/DHCP)
  - name: "data-l2-network"
    bridge_name: "ovs-data"
    mode: "l2-bridge"
    nic_model: "igb"  # Intel 82576 emulated NIC
    use_ovs: true  # Use Open vSwitch (supports OpenFlow, flow tables, etc.)
    attach_to: "dpu"  # Attach to all VMs: "dpu", "host", or "any"

vms:
  - name: "host-1"
    type: "host"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB
    ip: "192.168.100.12"

  - name: "dpu-1"
    type: "dpu"
    host: "host-1"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB
    ip: "192.168.100.13"
```

### Network Modes

- **`nat`**: VMs can communicate with each other AND access the internet via NAT (requires gateway, subnet_mask, dhcp_start, dhcp_end)
- **`l2-bridge`**: Pure Layer 2 bridge - VMs connected like a switch, no IP/DHCP management (configure IPs manually in VMs)
  - Set `use_ovs: true` to use Open vSwitch instead of Linux bridge
  - OVS provides: OpenFlow support, flow tables, VLAN tagging, port mirroring, QoS, and more

### Network Attachment

The `attach_to` field controls which VM types a network should attach to:

- **`any`** (default): Attach to all VMs regardless of type
- **`host`**: Only attach to VMs with `type: host`
- **`dpu`**: Only attach to VMs with `type: dpu`

Example use case: You might want a management network attached to all VMs, but a specific data plane network only attached to DPU VMs.

### NIC Models

- **`virtio`**: High-performance paravirtualized NIC (recommended for management)
- **`igb`**: Intel 82576 Gigabit Ethernet emulation (good for testing Intel drivers)
- **`e1000`**: Intel PRO/1000 emulation (widely compatible)
- **`e1000e`**: Intel 82574 emulation (newer than e1000)
- **`rtl8139`**: Realtek 8139 emulation

### VM Architecture

All VMs use the **Q35 machine type** which provides:
- **PCIe bus**
- **IOMMU support** (Intel VT-d emulation)
- **SR-IOV ready** architecture

This makes the VMs suitable for:
- Testing SR-IOV devices
- DPU emulation by interconnecting VMs with networks (OvS or Linux Bridge)
- Testing Kubernetes features with emulating hardware

## Usage

### Deploy VMs

**Step 1:** Deploy all Host and DPU VMs and the network:

```bash
python3 deploy.py
```

This will:
1. **Clean up any existing VMs and networks** (idempotent deployment - can be run multiple times safely)
2. Download Cloud Base image (if not present) - We recommend to download from Fedora.
3. Create custom libvirt networks. All Host and DPUs, the
4. Create and start all Host and DPU VMs with cloud-init configuration
5. Wait for VMs to boot and get IP addresses

**Note:** The deploy script is idempotent by default - it automatically cleans up existing resources before deploying. You can run it multiple times safely. If you want to skip cleanup for some reason, use `python3 deploy.py --no-cleanup`

**Step 2:** Install Kubernetes and OVS on all VMs:

```bash
# Install sequentially (recommended for first time)
python3 install_software.py

# Or install in parallel (faster but output is harder to read)
python3 install_software.py --parallel

# Or install on a specific VM only
python3 install_software.py --vm host-1
```

This will SSH into each VM and:
- Disable swap (required for Kubernetes)
- Configure kernel modules (overlay, br_netfilter)
- Install and configure containerd
- Install Open vSwitch
- Install Kubernetes components (kubeadm, kubelet, kubectl)
- Configure firewall rules

**Note:** Installation takes 3-5 minutes per VM (faster in parallel mode).

**Step 3:** Verify Installation

After installation, verify that Kubernetes and OVS are properly installed:

```bash
python3 verify_setup.py
```

This will check all VMs and report the status of:
- Kubernetes components (kubeadm, kubelet, kubectl, containerd)
- Open vSwitch installation and service status
- Network connectivity
- Required kernel modules

### Manage VMs

List all VMs:
```bash
python3 vmctl.py list
```

SSH into a VM:
```bash
python3 vmctl.py ssh host-1
python3 vmctl.py ssh dpu-1
```

Access serial console:
```bash
python3 vmctl.py console host-1
```

Start/Stop VMs:
```bash
python3 vmctl.py start host-1
python3 vmctl.py stop host-1
python3 vmctl.py reboot host-1
```

Execute commands remotely:
```bash
python3 vmctl.py exec host-1 "uname -a"
python3 vmctl.py exec dpu-1 "ip addr show"
```

### Cleanup

Remove all VMs and networks:

```bash
python3 cleanup.py
```

**Warning:** This will permanently delete all VMs and their disks.

**Note:** The deploy script automatically cleans up before deploying, so you typically don't need to run cleanup manually unless you just want to remove everything without redeploying.

## VM Access Details

### SSH Access
- **User:** Specified in the config file
- **Authentication:** SSH key (from `~/.ssh/id_rsa`)
- **No password required**

### Console Access (Emergency)
- **User:** Specified in the config file
- **Password:** Specified in the config file (default: "redhat")
- Use console access if SSH is not working

## File Structure

```
.

├── bridge_utils.py       # For Bridge and networking naming utilities.
├── cfg_utils.py          # For Configuration utilities
├── cleanup.py            # For removing VMs, network, and associated resources
├── config.yaml           # Configuration file
├── deploy.py             # VM (Host and DPU) and VM Networking deployment script
├── install_software.py   # For K8s and OVS installation script
├── quickstart.sh         # For quick setup of dependencies
├── README.md             # This file
├── requirements.txt      # For Python dependencies
├── ssh_utils.py          # For accessing VMs with SSH utilies
├── verify_setup.py       # For verification of installed components
├── vm_utils.py           # For VM (libvirt) utilities
└── vmctl.py              # VM management utility
```

## Troubleshooting

### VMs not getting IP addresses

Wait 1-2 minutes for VMs to boot. Check VM status:
```bash
python3 vmctl.py list
```

### Cannot connect via SSH

1. Verify VM is running: `python3 vmctl.py list`
2. Check VM has IP address
3. Try console access: `python3 vmctl.py console host-1`
4. Verify SSH key exists: `ls -la ~/.ssh/id_rsa*`

### Permission denied errors

Make sure your user is in the `libvirt` group:
```bash
groups | grep libvirt
```

If not, add yourself and log out/in:
```bash
sudo usermod -a -G libvirt $USER
```

### Cannot download cloud image

The download may take time depending on your connection. If it fails:
1. Check internet connectivity
2. Verify the image URL in `config.yaml` is correct
3. Manually download to `/var/lib/libvirt/images/`

## Kubernetes Cluster Setup

After the VMs are deployed and verified, you can set up a Kubernetes cluster:

### Initialize Kubernetes Master (on vm1)

```bash
# SSH into the first VM
python3 vmctl.py ssh host-1

# Initialize the cluster
sudo kubeadm init --pod-network-cidr=10.244.0.0/16

# Set up kubectl for regular user
mkdir -p $HOME/.kube
sudo cp -i /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config

# Save the join command that is displayed
```

### Join Worker Nodes (on vm2, vm3, vm4)

```bash
# SSH into each worker VM
python3 vmctl.py ssh host-2

# Run the kubeadm join command from the master
sudo kubeadm join <master-ip>:6443 --token <token> --discovery-token-ca-cert-hash sha256:<hash>
```

### Install CNI Plugin (on master)

For OVN-Kubernetes (OVS-based networking):
```bash
# On the master node (vm1)
kubectl apply -f https://raw.githubusercontent.com/ovn-org/ovn-kubernetes/master/dist/images/ovnkube-db.yaml
kubectl apply -f https://raw.githubusercontent.com/ovn-org/ovn-kubernetes/master/dist/images/ovnkube-master.yaml
kubectl apply -f https://raw.githubusercontent.com/ovn-org/ovn-kubernetes/master/dist/images/ovnkube-node.yaml
```

Or use Flannel (simpler):
```bash
kubectl apply -f https://raw.githubusercontent.com/flannel-io/flannel/master/Documentation/kube-flannel.yml
```

### Verify Cluster

```bash
# Check nodes
kubectl get nodes

# Check pods
kubectl get pods -A
```

## Open vSwitch Usage

### OVS Data Bridge (on Host)

If you configured a network with `use_ovs: true`, an OVS bridge is created on the host that connects all VMs:

```bash
# Check OVS bridge status on host
sudo ovs-vsctl show

# View the data bridge
sudo ovs-vsctl list-br

# Show all ports on the bridge
sudo ovs-vsctl list-ports ovs-data

# View OpenFlow rules
sudo ovs-ofctl dump-flows ovs-data

# Add OpenFlow rules (example: drop all traffic)
sudo ovs-ofctl add-flow ovs-data priority=100,actions=drop

# Delete all flows
sudo ovs-ofctl del-flows ovs-data

# Enable port mirroring (mirror eth1 traffic to eth2)
sudo ovs-vsctl -- set Bridge ovs-data mirrors=@m \
  -- --id=@eth1 get Port eth1 \
  -- --id=@eth2 get Port eth2 \
  -- --id=@m create Mirror name=mirror0 select-all=true output-port=@eth2
```

### OVS Inside VMs

Each VM also has OVS installed for custom networking inside the VM:

```bash
# SSH into any VM
python3 vmctl.py ssh host-1

# Check OVS status
sudo ovs-vsctl show

# Create a bridge
sudo ovs-vsctl add-br br0

# List bridges
sudo ovs-vsctl list-br

# Add a port
sudo ovs-vsctl add-port br0 eth1
```

## Advanced Usage

### Using Different Cloud Image Versions

Update the `operating_system.image_url` in `config.yaml` to point to a different Cloud image:

For Fedora visit the downloads website https://download.fedoraproject.org/pub/fedora/linux/releases/ and pick the version that is required.

```yaml
operating_system:
  image_url: https://mirror.xenyth.net/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2
  image_name: "Fedora-x86_64.qcow2"
  cloud_init_iso_name: "Fedora-x86_64-cloud-init.iso"
```

### Adding More VMs

Add more VM entries to `config.yaml`:

```yaml
vms:
  - name: "host-1"
    type: "host"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB
    ip: "192.168.100.12"

  - name: "dpu-1"
    type: "dpu"
    host: "host-1"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB
    ip: "192.168.100.13"

  - name: "host-2"
    type: "host"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB
    ip: "192.168.100.14"

  - name: "dpu-2"
    type: "dpu"
    host: "host-1"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB
    ip: "192.168.100.15"
```

## License

This project is provided as-is for educational and development purposes.

## Contributing

Feel free to submit issues and enhancement requests!

