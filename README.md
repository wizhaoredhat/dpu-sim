# DPU Simulator - VM and Container-based Kubernetes + OVN-Kubernetes CNI Environment

This project automates the deployment of DPU simulation environments using either **VMs (libvirt)** or **containers (Kind)**, pre-configured with Kubernetes and OVN-Kubernetes (or other CNIs) for container networking experiments/development/CI/CD.

DPUs are being used in data centers to accelerate different workloads such as AI (Artificial Intelligence), NFs (Network Functions) and many use cases. This DPU simulation's goal is to bring the DPU into developer's hands without needing the hardware. DPU hardware has limitations such as ease of provisioning, hardware availability, cost, embedded CPU capacity, and others, the DPU simulation tools here using Virtual Machines or Containers should lower the barrier of entry to move fast in developing features in Kubernetes, CNIs, APIs, etc... The second objective is to use this simulation in upstream CI/CD for CNIs that support offloading to DPUs such as OVN-Kubernetes

These are the list of DPUs that this simulation will try to emulate:
- NVIDIA BlueField 3
- Marvell Octeon 10
- Intel NetSec Accelerator
- Intel IPU

All these DPUs have common simularities, some we can emulate better than others. As this DPU simulation project grows there would a increased interest and need to simulate the hardware closely (e.g. eSwitch) in QEMU drivers.

## Features

### Core Features
- 🚀 **Two deployment modes**: VMs (libvirt) or Containers (Kind)
- ☸️ Kubernetes (kubeadm, kubelet, kubectl) pre-installed
- 🔀 OVN-Kubernetes or Flannel CNI support
- 🌐 Multiple network support (NAT, Layer 2 Bridge)
- ✅ Automatic cluster setup and CNI installation
- 🧹 Cleanup scripts for both modes

### VM Mode Features
- 🔌 Configurable NIC models (virtio, igb)
- 🖥️ Q35 machine type with PCIe and IOMMU support (SR-IOV ready)
- 🔑 SSH key-based authentication
- 💻 Easy VM access via SSH and console
- 🎛️ Full VM lifecycle management (start, stop, reboot)
- 🔀 Open vSwitch (OVS) for host-to-DPU networking

### Kind Mode Features
- ⚡ **Fast iteration** - clusters deploy in seconds
- 🐳 Uses Docker containers instead of VMs
- 💾 Lower resource usage than VMs
- 🔄 Easy cluster recreation for testing

## Quick Start

### Python Dependencies

```bash
dnf -y install python3 python3-devel

python3 -m venv dpu-sim-venv

source dpu-sim-venv/bin/activate

pip3 install -r requirements.txt
```

### Kind Mode

```bash
# Enable k8s repo
sudo tee /etc/yum.repos.d/kubernetes.repo > /dev/null <<EOF
[kubernetes]
name=Kubernetes
baseurl=https://pkgs.k8s.io/core:/stable:/v1.28/rpm/
enabled=1
gpgcheck=1
gpgkey=https://pkgs.k8s.io/core:/stable:/v1.28/rpm/repodata/repomd.xml.key
EOF

# Install prerequisites
sudo dnf install -y podman kubectl
curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64
chmod +x ./kind && sudo mv ./kind /usr/local/bin/kind

# Start Docker
sudo systemctl enable podman
sudo systemctl start podman

# Deploy Kind cluster with OVN-Kubernetes
python3 dpu-sim.py --config config-kind.yaml
```

### VM Mode

```bash
# Run quickstart script
./quickstart.sh

# Deploy VMs and install Kubernetes
python3 dpu-sim.py
```

## Prerequisites

### System Requirements
- Fedora/RHEL/CentOS Linux
- **For VM Mode**: KVM/QEMU virtualization support, at least 12GB RAM, 100GB disk
- **For Kind Mode**: Docker installed and running, at least 8GB RAM

### Required Packages

```bash

subscription-manager repos --enable=codeready-builder-for-rhel-9-$(arch)-rpms
subscription-manager repos --enable=fast-datapath-for-rhel-9-$(arch)-rpms
subscription-manager repos --enable=openstack-17-for-rhel-9-$(arch)-rpms

# Install required system packages
sudo dnf install -y \
    gcc \
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

### SSH Key Setup

Generate SSH keys if you don't have them:

```bash
ssh-keygen -t rsa -b 4096 -f ~/.ssh/id_rsa
```

## Configuration

The simulator supports two deployment modes, configured via different sections in the YAML config file:

- **VM Mode**: Uses `vms` section (libvirt-based VMs)
- **Kind Mode**: Uses `kind` section (Docker containers)

### Kind Mode Configuration (config-kind.yaml)

```yaml
# Kind cluster configuration
kind:
  nodes:
    - role: control-plane
    - role: worker
    - role: worker

kubernetes:
  version: "1.33"
  clusters:
    - name: "dpu-sim-kind"
      pod_cidr: "10.244.0.0/16"
      service_cidr: "10.96.0.0/16"
      cni: "ovn-kubernetes"  # or 'kindnet' (default)
```

### VM Mode Configuration (config.yaml)

Edit `config.yaml` to customize your deployment:

```yaml
networks:
  # Management network with internet access
  - name: "mgmt-network"
    type: "mgmt"
    bridge_name: "virbr-mgmt"
    gateway: "192.168.100.1"
    subnet_mask: "255.255.255.0"
    dhcp_start: "192.168.100.10"
    dhcp_end: "192.168.100.100"
    mode: "nat"
    nic_model: "virtio"  # virtio for management network
    attach_to: "any"  # Attach to all VMs: "dpu", "host", or "any"

  # Pure Layer 2 data network with OVS (no IP/DHCP)
  - name: "data-l2-network"
    type: "layer2"
    bridge_name: "ovs-data"
    mode: "l2-bridge"
    nic_model: "igb"  # Intel 82576 emulated NIC
    use_ovs: true  # Use Open vSwitch (supports OpenFlow, flow tables, etc.)
    attach_to: "dpu"  # Attach to all VMs: "dpu", "host", or "any"

vms:
  - name: "master-1"
    type: "host"
    k8s_cluster: "cluster-1" # Cluster assignment
    k8s_role: "master"       # Control plane node
    memory: 2048
    vcpus: 2
    disk_size: 20
    ip: "192.168.100.12"

  - name: "host-1"
    type: "host"
    k8s_cluster: "cluster-1"
    k8s_role: "worker"       # Worker node
    memory: 2048
    vcpus: 2
    disk_size: 20
    ip: "192.168.100.13"

  - name: "dpu-1"
    type: "dpu"
    k8s_cluster: "cluster-1"
    k8s_role: "worker"
    host: "host-1"
    memory: 2048
    vcpus: 2
    disk_size: 20
    ip: "192.168.100.14"

operating_system:
  # Download from https://download.fedoraproject.org/pub/fedora/linux/releases/
  image_url: https://mirror.xenyth.net/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2
  image_name: "Fedora-x86_64.qcow2"
  cloud_init_iso_name: "Fedora-x86_64-cloud-init.iso"

ssh:
  user: "root"
  key_path: "~/.ssh/id_rsa"
  password: "redhat"  # Default password for console/SSH access

kubernetes:
  version: "1.33"
  clusters:
    - name: "cluster-1"
      pod_cidr: "10.244.0.0/16"
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

### Kubernetes

Kubernetes is the choice for orchestrating DPU deployment. Hence kubernetes installation and usage is assumed. Although you might choose to simulate DPUs without Kubernetes, which currently means to run the `deploy.py` script only.

If Kubernetes is needed then the `install_software.py` script would need to be run or `dpu-sim.py` which calls both `deploy.py` and `install_software.py`.

Each VM must specify which cluster it belongs to using the `k8s_cluster` field, which references a cluster name defined in the `kubernetes.clusters` section.

Each VM in `config.yaml` must have a `k8s_role` field with two supported values:
- **master**: Kubernetes control plane node
- **worker**: Kubernetes worker node

Everything Kubernetes related is in the `kubernetes` section. By default version 1.33 Kubernetes version is used however this can be overwritten in the `kubernetes.version` field. Each cluster definition includes:
- **name**: Unique identifier for the cluster
- **pod_cidr**: Custom pod network CIDR

Multiple cluster configuration example:
```yaml
kubernetes:
  version: "1.33"
  clusters:
    - name: "cluster-1"
      pod_cidr: "10.244.0.0/16"  # First cluster pod network
    - name: "cluster-2"
      pod_cidr: "10.245.0.0/16"  # Second cluster pod network

vms:
  - name: "master-1"
    k8s_cluster: "cluster-1"
    k8s_role: "master"
    ...

  - name: "master-2"
    k8s_cluster: "cluster-2"
    k8s_role: "master"
    ...

  - name: "host-1"
    k8s_cluster: "cluster-1"
    k8s_role: "worker"
    ...

  - name: "dpu-1"
    k8s_cluster: "cluster-2"
    k8s_role: "worker"
    ...
```

### Using Different Cloud Image Versions

Update the `operating_system.image_url` in `config.yaml` to point to a different Cloud image:

For Fedora visit the downloads website https://download.fedoraproject.org/pub/fedora/linux/releases/ and pick the version that is required.

```yaml
operating_system:
  image_url: https://mirror.xenyth.net/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2
  image_name: "Fedora-x86_64.qcow2"
  cloud_init_iso_name: "Fedora-x86_64-cloud-init.iso"
```

## Usage

The `dpu-sim.py` script automatically detects whether to use VM or Kind mode based on your config file.

### Deployment Options

```bash
# Auto-detect mode from config (default: config.yaml = VM mode)
python3 dpu-sim.py

# Use Kind mode explicitly
python3 dpu-sim.py --config config-kind.yaml

# Force a specific mode
python3 dpu-sim.py --mode kind
python3 dpu-sim.py --mode vm

# Skip cleanup (for incremental changes)
python3 dpu-sim.py --no-cleanup

# Parallel installation (VM mode only)
python3 dpu-sim.py --parallel
```

### Kind Mode Usage

```bash
# Deploy Kind cluster
python3 kind_deploy.py --config config-kind.yaml

# Cleanup only
python3 kind_deploy.py --cleanup-only

# After deployment, use the cluster
export KUBECONFIG=kubeconfig/dpu-sim-kind.yaml
kubectl get nodes
kubectl get pods -A
```

### VM Mode Usage

#### Step 0: Choosing the right script

You can choose to deploy just the VMs with `deploy.py` or deploy the software installation with `install_software.py`. The `dpu-sim.py` script will run both for your convenience.

#### Step 1: Deploy VMs

Deploy all Host and DPU VMs and the network:

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

### Step 2: Install Software

The project supports **fully automated Kubernetes cluster setup** with support for **multiple independent clusters**, each with custom pod network CIDRs. The `install_software.py` script handles all aspects of software installation, cluster initialization, CNI installation, and node joining automatically.

```bash
python3 install_software.py
```

#### Key Features

- **Automatic Cluster Initialization**: No manual `kubeadm` commands needed
- **Multiple Cluster Support**: Deploy multiple independent K8s clusters in one configuration
- **Custom Pod Network CIDRs**: Each cluster can have its own pod network CIDR(default: `10.244.0.0/16`)
- **Automatic CNI Installation**: Flannel or OVN-Kubernetes is automatically installed and configured
- **Role-Based Assignment**: VMs are assigned as `master` or `worker` nodes
- **Network Isolation**: Different clusters use different overlay networks

#### Automated Setup

The `install_software.py` script automatically SSH into each VM to install components:

For Software Installation on all VMs
1. Disable swap (required for Kubernetes)
2. Configure kernel modules (overlay, br_netfilter)
3. Install and configure CRI-O
4. Install Open vSwitch
5. Add the upstream Kubernetes repo
6. Installs Kubernetes components (kubeadm, kubelet, kubectl)
7. Disable the firewall
For Kubernetes Install on selected or all VMs
1. First the script groups VMs by cluster assignment
3. Initializes each cluster on its master node with the custom (or default) pod CIDR
4. Installs and configures a Flannel or OVN-Kubernetes CNI deployment for each cluster
5. Joins all worker nodes to their respective clusters

Simply run:

```bash
# Sequential installation (recommended for debugging)
python3 install_software.py

# Parallel installation (faster)
python3 install_software.py --parallel
```

After Installation finished, you should expect these software packages to be running:
- CRI-O container runtime
- kubelet (Kubernetes node agent)
- Flannel and other containers are running, for example:
```bash
[root@master-1 ~]# kubectl get pods -A -o wide
NAMESPACE      NAME                               READY   STATUS    RESTARTS   AGE   IP               NODE       NOMINATED NODE   READINESS GATES
kube-flannel   kube-flannel-ds-btnhv              1/1     Running   0          11m   192.168.100.86   dpu-1      <none>           <none>
kube-flannel   kube-flannel-ds-t7d44              1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
kube-flannel   kube-flannel-ds-vdhjz              1/1     Running   0          11m   192.168.100.23   host-1     <none>           <none>
kube-system    coredns-674b8bbfcf-2g6tz           1/1     Running   0          11m   10.85.0.3        master-1   <none>           <none>
kube-system    coredns-674b8bbfcf-qhsw7           1/1     Running   0          11m   10.85.0.2        master-1   <none>           <none>
kube-system    etcd-master-1                      1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
kube-system    kube-apiserver-master-1            1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
kube-system    kube-controller-manager-master-1   1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
kube-system    kube-multus-ds-jh2l5               1/1     Running   0          11m   192.168.100.86   dpu-1      <none>           <none>
kube-system    kube-multus-ds-rzqj2               1/1     Running   0          11m   192.168.100.23   host-1     <none>           <none>
kube-system    kube-multus-ds-vn4bv               1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
kube-system    kube-proxy-69q6s                   1/1     Running   0          11m   192.168.100.23   host-1     <none>           <none>
kube-system    kube-proxy-9fq5x                   1/1     Running   0          11m   192.168.100.86   dpu-1      <none>           <none>
kube-system    kube-proxy-kc9fd                   1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
kube-system    kube-scheduler-master-1            1/1     Running   0          11m   192.168.100.14   master-1   <none>           <none>
```

- OVN-Kubernetes and other containers are running, for example:
```bash
[root@master-1 ~]# kubectl get pods -A
NAMESPACE        NAME                               READY   STATUS    RESTARTS   AGE   IP               NODE       NOMINATED NODE   READINESS GATES
kube-system      coredns-674b8bbfcf-lsfbl           1/1     Running   0          26m   10.85.0.3        master-1   <none>           <none>
kube-system      coredns-674b8bbfcf-xzstj           1/1     Running   0          26m   10.85.0.2        master-1   <none>           <none>
kube-system      etcd-master-1                      1/1     Running   0          26m   192.168.120.72   master-1   <none>           <none>
kube-system      kube-apiserver-master-1            1/1     Running   0          26m   192.168.120.72   master-1   <none>           <none>
kube-system      kube-controller-manager-master-1   1/1     Running   0          26m   192.168.120.72   master-1   <none>           <none>
kube-system      kube-scheduler-master-1            1/1     Running   0          26m   192.168.120.72   master-1   <none>           <none>
ovn-kubernetes   ovnkube-db-68b8c896c6-dbtfm        2/2     Running   0          26m   192.168.120.72   master-1   <none>           <none>
ovn-kubernetes   ovnkube-identity-6pcqr             1/1     Running   0          26m   192.168.120.72   master-1   <none>           <none>
ovn-kubernetes   ovnkube-master-77c5fd869f-2pzqw    2/2     Running   0          25m   192.168.120.72   master-1   <none>           <none>
ovn-kubernetes   ovnkube-node-6xk2k                 3/3     Running   0          23m   192.168.120.90   host-1     <none>           <none>
ovn-kubernetes   ovnkube-node-dtclz                 3/3     Running   0          22m   192.168.120.65   dpu-1      <none>           <none>
ovn-kubernetes   ovnkube-node-gfjfm                 3/3     Running   0          24m   192.168.120.72   master-1   <none>           <none>
```

#### Kuberenetes Use Cases with DPU Simulation

With cluster support, you can:

1. **DPU workloads**: Deploy workloads to test DPU offloading
2. **Open vSwitch**: Configure OVS bridges for data plane traffic
3. **Testing**: Test the deployment of DPU-accelerated services

With the multi-cluster support, you can:

1. **Multi-Tenancy Scenarios**: Simulate multiple independent Kubernetes environments
2. **DPU Testing**: Test DPU nodes in either single or dual cluster deployments
3. **Cross-Cluster Communication**: Experiment with DPU Operator orchestration like https://github.com/openshift/dpu-operator which uses OPI APIs

#### Verify Cluster Setup

After installation completes, verify your cluster(s):

#### Single Cluster

```bash
# Check node status
python3 vmctl.py exec master-1 'kubectl get nodes'
NAME       STATUS   ROLES           AGE   VERSION
dpu-1      Ready    <none>          13m   v1.33.6
host-1     Ready    <none>          13m   v1.33.6
master-1   Ready    control-plane   13m   v1.33.6

# Check all pods
python3 vmctl.py exec master-1 'kubectl get pods -A'
...

# Check Flannel CNI
python3 vmctl.py exec master-1 'kubectl get pods -n kube-flannel'

# Or check OVN-Kubernetes CNI
python3 vmctl.py exec master-1 'kubectl get pods -n ovn-kubernetes'

...
```

#### Multiple Clusters

```bash
# Check cluster-1
python3 vmctl.py exec master-1 'kubectl get nodes'

# Check cluster-2
python3 vmctl.py exec master-2 'kubectl get nodes'

# Verify different pod CIDRs
python3 vmctl.py exec master-1 'kubectl get nodes -o jsonpath="{.items[0].spec.podCIDR}"'

python3 vmctl.py exec master-2 'kubectl get nodes -o jsonpath="{.items[0].spec.podCIDR}"'
```

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

## Project File Structure

```
├── bridge_utils.py       # Bridge and networking naming utilities
├── cfg_utils.py          # Configuration utilities
├── cleanup.py            # For removing VMs, networks, and resources
├── config-2-cluster.yaml # Configuration for 2-cluster deployment
├── config-kind.yaml      # Configuration for Kind (container) mode
├── config.yaml           # Default configuration (VM mode)
├── deploy.py             # VM and networking deployment script
├── dpu-sim.py            # Main entry point (supports both modes)
├── install_software.py   # K8s and OVS installation for VMs
├── kind_deploy.py        # Kind cluster deployment script
├── kind_utils.py         # Kind-specific utilities
├── quickstart.sh         # Quick setup of dependencies
├── README.md             # This file
├── requirements.txt      # Python dependencies
├── ssh_utils.py          # SSH utilities for VMs
├── vm_utils.py           # VM (libvirt) utilities
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

### Check Cluster Status

```bash
# View detailed node information
python3 vmctl.py exec master-1 'kubectl get nodes -o wide'

# Check system pod status
python3 vmctl.py exec master-1 'kubectl get pods -A'
```

### View Cluster Logs

```bash
# Check kubelet logs on any node
python3 vmctl.py exec <vm-name> 'sudo journalctl -u kubelet -n 50'

# Check kubeadm init logs on master
python3 vmctl.py exec master-1 'cat /tmp/kubeadm-init.log'
```

### Reset and Reinitialize the Cluster

If you need to reset a cluster:

```bash
# On master node
python3 vmctl.py exec master-1 'sudo kubeadm reset -f'

# On worker nodes
python3 vmctl.py exec host-1 'sudo kubeadm reset -f'
python3 vmctl.py exec dpu-1 'sudo kubeadm reset -f'

# Re-run installation
python3 install_software.py
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

## License

This project is provided as-is for educational and development purposes.

## Contributing

Feel free to submit issues and enhancement requests!
