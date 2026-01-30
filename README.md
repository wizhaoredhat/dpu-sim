# DPU Simulator - VM and Container-based Kubernetes + OVN-Kubernetes CNI Environment

This project automates the deployment of DPU simulation environments using either **VMs (libvirt)** or **containers (Kind)**, pre-configured with Kubernetes and OVN-Kubernetes (or other CNIs) for container networking experiments/development/CI/CD.

DPUs are being used in data centers to accelerate different workloads such as AI (Artificial Intelligence), NFs (Network Functions) and many use cases. This DPU simulation's goal is to bring the DPU into developer's hands without needing the hardware. DPU hardware has limitations such as ease of provisioning, hardware availability, cost, embedded CPU capacity, and others, the DPU simulation tools here using Virtual Machines or Containers should lower the barrier of entry to move fast in developing features in Kubernetes, CNIs, APIs, etc... The second objective is to use this simulation in upstream CI/CD for CNIs that support offloading to DPUs such as OVN-Kubernetes

These are the list of DPUs that this simulation will try to emulate:
- NVIDIA BlueField 3
- Marvell Octeon 10
- Intel NetSec Accelerator
- Intel IPU

All these DPUs have common simularities, some we can emulate better than others. As this DPU simulation project grows there would a increased interest and need to simulate the hardware closely (e.g. eSwitch) in QEMU drivers.

## Status: 🚧 Active Development
 - `dpu-sim` is functional for VM & Kind mode.
 - `vmctl` is functional for managing VMs created by dpu-sim.

## Features

### Core Features
- 🚀 **Multiple deployment modes**: VMs (libvirt) or Containers (Kind)
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

## Prerequisites

### System Requirements
- Fedora/RHEL/CentOS Linux
- **For VM Mode**: KVM/QEMU virtualization support, at least 12GB RAM, 100GB disk
- **For Kind Mode**: Container support, at least 8GB RAM

### Dependencies
Runtime dependencies are automatically installed by dpu-sim. For example the dpu-sim binary will output the following if all depencies are meet on the system:
```bash
=== Checking Dependencies ===
✓ Detected Linux distribution: rhel 9.6 (package manager: dnf, architecture: x86_64)
✓ wget is installed
✓ pip3 is installed
✓ jinjanator is installed
✓ git is installed
✓ openvswitch is installed
✓ libvirt is installed
✓ qemu-kvm is installed
✓ qemu-img is installed
✓ libvirt-devel is installed
✓ virt-install is installed
✓ genisoimage is installed
✓ All dependencies are available
```
Seperate dependencies are checked whether the provided configuration file is deploying VM vs. Kind modes.

### Required Packages

The dpu-sim should install all dependecies by detecting the system's Linux distribution. However some distributions require enabling subscriptions to allow the installation of some packages. This is outside the scope of dpu-sim; however depending on the distribution, dpu-sim will try to enable repositories.

### Required Services

Although dpu-sim tries to install dependencies, the user may be required to start required services. This can potentially go away once the handles these required servers in its entirety.

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

### Required SSH Key Setup

Generate SSH keys if you don't have them:

```bash
ssh-keygen -t rsa -b 4096 -f ~/.ssh/id_rsa
```

### Compiling Binaries

In order to use the dpu-sim binary, the binaries must be built. To compile the GO binaries, golang compilers must be installed.

```bash
$ go version
go version go1.25.3 (Red Hat 1.25.3-1.el9_7) linux/amd64
$ make build
Building binaries...
  Building dpu-sim...
  Building vmctl...
Build complete! Binaries are in bin/
```
### Makefile Commands

```bash
make                # Show help
make build          # Build all binaries
make test           # Run tests
make test-coverage  # Run tests with HTML coverage report
make clean          # Clean build artifacts
make install        # Install binaries to $GOPATH/bin
make fmt            # Format code
make vet            # Run go vet
make lint           # Run golangci-lint
make check          # Run fmt, vet, and test
make build-all      # Cross-compile for multiple platforms
make deps           # Download dependencies
```

## Configuration

The simulator supports two deployment modes, configured via different sections in the YAML config file:

- **VM Mode**: Uses `vms` section (libvirt-based VMs)
- **Kind Mode**: Uses `kind` section (Docker containers)

### Kind Mode Configuration (config-kind.yaml)

```yaml
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
      service_cidr: "10.245.0.0/16"
      cni: "ovn-kubernetes"
```

### VM Mode Configuration (config.yaml)

Edit `config.yaml` to customize your deployment:

```yaml
networks:
  # Management network with internet access
  - name: "mgmt-network"
    type: "mgmt"
    bridge_name: "virbr-mgmt"
    gateway: "192.168.120.1"
    subnet_mask: "255.255.255.0"
    dhcp_start: "192.168.120.10"
    dhcp_end: "192.168.120.100"
    mode: "nat"
    nic_model: "virtio"  # virtio for management network
    attach_to: "any"  # Attach to all VMs: "dpu", "host", or "any"

  - name: "ovn-network"
    type: "k8s"
    bridge_name: "ovn"
    gateway: "192.168.123.1"
    subnet_mask: "255.255.255.0"
    dhcp_start: "192.168.123.50"
    dhcp_end: "192.168.123.100"
    mode: "nat"
    nic_model: "igb"  # Intel 82576 emulated NIC
    use_ovs: false
    attach_to: "any"

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
    k8s_cluster: "cluster-1"
    k8s_role: "master"
    k8s_node_mac: "52:54:00:00:01:11"
    k8s_node_ip: "192.168.123.11"
    memory: 4096  # MB
    vcpus: 2
    disk_size: 20  # GB

  - name: "host-1"
    type: "host"
    k8s_cluster: "cluster-1"
    k8s_role: "worker"
    k8s_node_mac: "52:54:00:00:01:12"
    k8s_node_ip: "192.168.123.12"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB

  - name: "dpu-1"
    type: "dpu"
    k8s_cluster: "cluster-1"
    k8s_role: "worker"
    k8s_node_mac: "52:54:00:00:01:13"
    k8s_node_ip: "192.168.123.13"
    host: "host-1"
    memory: 2048  # MB
    vcpus: 2
    disk_size: 20  # GB

operating_system:
  # Download from https://download.fedoraproject.org/pub/fedora/linux/releases/
  image_url: https://mirror.xenyth.net/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2
  image_name: "Fedora-x86_64.qcow2"

ssh:
  user: "root"
  key_path: "~/.ssh/id_rsa"
  password: "redhat"  # Default password for console/SSH access

kubernetes:
  version: "1.33"
  kubeconfig_dir: "kubeconfig"
  clusters:
    - name: "cluster-1"
      pod_cidr: "10.244.0.0/16"
      service_cidr: "10.245.0.0/16"
      cni: "ovn-kubernetes"
```

### Network Types

Network types change the behaviour of dpu-sim on how they treat the network. For example "k8s" network shouldn't be used to access machines, rather the "mgmt" network should be used (more stable/non-changing)

- **`mgmt`**: A non-changing network to provide SSH access to the machine
- **`k8s`**: A network that the CNI would have access to. For example OVN-Kubernetes would have control of this network and it's interfaces.
- **`layer2`**: A network that is layer 2 connection between 2 machines. Currently dpu-sim does not modify this network beyond configuring it.

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

Kubernetes is the choice for orchestrating DPU deployment. Hence kubernetes installation and usage is assumed. Although you might choose to simulate DPUs without Kubernetes, which currently means to pass the `--skip-k8s` flag to dpu-sim.

If Kubernetes is needed then by default dpu-sim will perform those operations automatically.

Each VM must specify which cluster it belongs to using the `k8s_cluster` field, which references a cluster name defined in the `kubernetes.clusters` section.

Each VM in `config.yaml` must have a `k8s_role` field with two supported values:
- **master**: Kubernetes control plane node
- **worker**: Kubernetes worker node

Everything Kubernetes related is in the `kubernetes` section. By default version `1.33 Kubernetes` is used however this can be overwritten in the `kubernetes.version` field. The resulting config files are generated and written into the kubeconfig directory by default, but this can be overwritten with `kubeconfig_dir`. Each cluster definition includes:
- **name**: Unique identifier for the cluster
- **pod_cidr**: Default is 10.244.0.0/16. This is the custom pod network CIDR
- **service_cidr**: Default is 10.245.0.0/16. This is the custom service CIDR.
- **cni**: Selects which CNI should be used in the cluster such as ovn-kubernetes

Multiple cluster configuration example:
```yaml
kubernetes:
  version: "1.33"
  kubeconfig_dir: "kubeconfig"
  clusters:
    - name: "cluster-1"
      pod_cidr: "10.244.0.0/16" # First cluster pod network
      service_cidr: "10.245.0.0/16"
      cni: "ovn-kubernetes"
    - name: "cluster-2"
      pod_cidr: "10.246.0.0/16" # Second cluster pod network
      service_cidr: "10.247.0.0/16"
      cni: "ovn-kubernetes"

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
  # Download from https://download.fedoraproject.org/pub/fedora/linux/releases/
  image_url: https://mirror.xenyth.net/fedora/linux/releases/43/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-43-1.6.x86_64.qcow2
  image_name: "Fedora-x86_64.qcow2"
```

## Usage

The `dpu-sim` executable automatically detects whether to use VM or Kind mode based on your config file.

### Deployment Options

```bash
# Auto-detect mode from default config (default: config.yaml = VM mode)
$ ./bin/dpu-sim

# Use Kind mode explicitly
$ ./bin/dpu-sim --config config-kind.yaml

# Skip cleanup (for incremental changes)
$ ./bin/dpu-sim --skip-cleanup

# Cleanup only
$ ./bin/dpu-sim --cleanup

# Review all avaialable options
$ ./bin/dpu-sim --help
DPU Simulator automates deployment of DPU simulation environments
using either VMs (libvirt) or containers (Kind), pre-configured with
Kubernetes and CNI for container networking experiments.

This is the main orchestrator that runs the complete deployment workflow:
  1. Install dependencies
  2. Clean up existing resources (Idempotent deployment - can be run multiple times safely)
  3. Deploy infrastructure (VMs or Kind clusters)
  4. Install Kubernetes and CNI components

Usage:
  dpu-sim [flags]

Flags:
      --cleanup            Only cleanup existing resources, do not deploy
      --config string      Path to configuration file (default "config.yaml")
  -h, --help               help for dpu-sim
      --log-level string   Log level (error, warn, info, debug) (default "info")
      --skip-cleanup       Skip cleanup of existing resources
      --skip-deploy        Skip VM/Kind deployment
      --skip-deps          Skip dependency checks
      --skip-k8s           Skip Kubernetes (VM only) and CNI installation

# After deployment, use the cluster
$ export KUBECONFIG=kubeconfig/cluster-1.kubeconfig
$ kubectl get nodes
$ kubectl get pods -A
```

### VM/Kind Mode Usage

#### Step 1: Ensuring dpu-sim is compiled

Binaries are located by default in `bin`. Make sure dpu-sim compiles sucessfully with the go compiler.

#### Step 2a: Deploy (VM)

Deploy all Host and DPU VMs and the network:

```bash
$ ./bin/dpu-sim
╔═══════════════════════════════════════════════╗
║               DPU Simulator                   ║
╚═══════════════════════════════════════════════╝
Configuration: config.yaml
Deployment mode: vm

=== Checking Dependencies ===
✓ Detected Linux distribution: rhel 9.6 (package manager: dnf, architecture: x86_64)
✓ wget is installed
✓ pip3 is installed
✓ jinjanator is installed
✓ git is installed
✓ openvswitch is installed
✓ libvirt is installed
✓ qemu-kvm is installed
✓ qemu-img is installed
✓ libvirt-devel is installed
✓ virt-install is installed
✓ genisoimage is installed
✓ All dependencies are available

=== Cleaning up K8s ===
✓ Kubeconfig file removed: kubeconfig/cluster-1.kubeconfig

╔═══════════════════════════════════════════════╗
║       VM-Based Deployment Workflow            ║
╚═══════════════════════════════════════════════╝
=== Cleaning up VMs ===
✓ Deleted disk: /var/lib/libvirt/images/master-1.qcow2
✓ Deleted cloud-init ISO: /var/lib/libvirt/images/master-1-cloud-init.iso
✓ Cleaned up VM: master-1
✓ Deleted disk: /var/lib/libvirt/images/host-1.qcow2
✓ Deleted cloud-init ISO: /var/lib/libvirt/images/host-1-cloud-init.iso
✓ Cleaned up VM: host-1
✓ Deleted disk: /var/lib/libvirt/images/dpu-1.qcow2
✓ Deleted cloud-init ISO: /var/lib/libvirt/images/dpu-1-cloud-init.iso
✓ Cleaned up VM: dpu-1
=== Cleaning up Networks ===
✓ Removed network mgmt-network
✓ Removed network ovn-network
✓ Removed host-to-DPU network h2d-host-1-dpu-1 (bridge: h2d-83d76b0d2f2)

=== Deploying VMs ===
=== Creating Networks ===
✓ Created network: mgmt-network
✓ Created network: ovn-network
✓ Created OVS bridge: h2d-83d76b0d2f2
✓ Created host-to-DPU network: h2d-host-1-dpu-1 (bridge: h2d-83d76b0d2f2)
✓ All networks created successfully
=== Creating All VMs ===
=== Creating VM: master-1 ===
✓ Image already exists at /var/lib/libvirt/images/Fedora-x86_64.qcow2, skipping download
✓ Created disk for master-1: /var/lib/libvirt/images/master-1.qcow2
✓ Created cloud-init ISO: /var/lib/libvirt/images/master-1-cloud-init.iso
✓ Created and started VM: master-1
=== Creating VM: host-1 ===
✓ Image already exists at /var/lib/libvirt/images/Fedora-x86_64.qcow2, skipping download
✓ Created disk for host-1: /var/lib/libvirt/images/host-1.qcow2
✓ Created cloud-init ISO: /var/lib/libvirt/images/host-1-cloud-init.iso
✓ Created and started VM: host-1
=== Creating VM: dpu-1 ===
✓ Image already exists at /var/lib/libvirt/images/Fedora-x86_64.qcow2, skipping download
✓ Created disk for dpu-1: /var/lib/libvirt/images/dpu-1.qcow2
✓ Created cloud-init ISO: /var/lib/libvirt/images/dpu-1-cloud-init.iso
✓ Created and started VM: dpu-1
✓ All VMs created successfully

=== Waiting for VMs to boot and get IPs ===
Waiting for master-1 to get an IP address...
✓ master-1 IP: 192.168.120.62
Waiting for SSH on master-1...
✓ SSH ready on master-1
Waiting for host-1 to get an IP address...
✓ host-1 IP: 192.168.120.36
Waiting for SSH on host-1...
✓ SSH ready on host-1
Waiting for dpu-1 to get an IP address...
✓ dpu-1 IP: 192.168.120.77
Waiting for SSH on dpu-1...
✓ SSH ready on dpu-1

=== Installing Kubernetes and CNI ===
=== Installing Kubernetes on VM-based deployment ===
--- Installing Kubernetes on master-1 (192.168.120.62) ---
Installing Kubernetes on master-1 (ssh://root@192.168.120.62)...
✓ Hostname set to master-1
✓ Detected Linux distribution: fedora 43 (package manager: dnf, architecture: x86_64)
✓ Disable firewalld is installed
Installing missing dependencies: Swap Off, K8s Kernel Modules, crio, openvswitch, NetworkManager-ovs, Kubelet Tools
Installing Swap Off for fedora on ssh://root@192.168.120.62...
✓ Swap Off installed
Installing K8s Kernel Modules for fedora on ssh://root@192.168.120.62...
✓ K8s Kernel Modules installed
Installing crio for fedora on ssh://root@192.168.120.62...
✓ crio installed
Installing openvswitch for fedora on ssh://root@192.168.120.62...
✓ openvswitch installed
Installing NetworkManager-ovs for fedora on ssh://root@192.168.120.62...
✓ NetworkManager-ovs installed
Installing Kubelet Tools for fedora on ssh://root@192.168.120.62...
✓ Kubelet Tools installed
✓ All dependencies are available
✓ Kubernetes 1.33 installed on master-1
--- Installing Kubernetes on host-1 (192.168.120.36) ---
Installing Kubernetes on host-1 (ssh://root@192.168.120.36)...
✓ Hostname set to host-1
✓ Detected Linux distribution: fedora 43 (package manager: dnf, architecture: x86_64)
✓ Disable firewalld is installed
Installing missing dependencies: Swap Off, K8s Kernel Modules, crio, openvswitch, NetworkManager-ovs, Kubelet Tools
Installing Swap Off for fedora on ssh://root@192.168.120.36...
✓ Swap Off installed
Installing K8s Kernel Modules for fedora on ssh://root@192.168.120.36...
✓ K8s Kernel Modules installed
Installing crio for fedora on ssh://root@192.168.120.36...
✓ crio installed
Installing openvswitch for fedora on ssh://root@192.168.120.36...
✓ openvswitch installed
Installing NetworkManager-ovs for fedora on ssh://root@192.168.120.36...
✓ NetworkManager-ovs installed
Installing Kubelet Tools for fedora on ssh://root@192.168.120.36...
✓ Kubelet Tools installed
✓ All dependencies are available
✓ Kubernetes 1.33 installed on host-1
--- Installing Kubernetes on dpu-1 (192.168.120.77) ---
Installing Kubernetes on dpu-1 (ssh://root@192.168.120.77)...
✓ Hostname set to dpu-1
✓ Detected Linux distribution: fedora 43 (package manager: dnf, architecture: x86_64)
✓ Disable firewalld is installed
Installing missing dependencies: Swap Off, K8s Kernel Modules, crio, openvswitch, NetworkManager-ovs, Kubelet Tools
Installing Swap Off for fedora on ssh://root@192.168.120.77...
✓ Swap Off installed
Installing K8s Kernel Modules for fedora on ssh://root@192.168.120.77...
✓ K8s Kernel Modules installed
Installing crio for fedora on ssh://root@192.168.120.77...
✓ crio installed
Installing openvswitch for fedora on ssh://root@192.168.120.77...
✓ openvswitch installed
Installing NetworkManager-ovs for fedora on ssh://root@192.168.120.77...
✓ NetworkManager-ovs installed
Installing Kubelet Tools for fedora on ssh://root@192.168.120.77...
✓ Kubelet Tools installed
✓ All dependencies are available
✓ Kubernetes 1.33 installed on dpu-1

=== Setting up Kubernetes cluster cluster-1 ===
--- Setting up OVN br-ex on 192.168.120.62 (ssh://root@192.168.120.62) ---
Mgmt Interface information: Interface: enp1s0 (index: 2)
  State: UP
  MAC: 52:54:00:dd:a4:6c
  MTU: 1500
  Link Type: ether
  Flags: [BROADCAST MULTICAST UP LOWER_UP]
  Addresses:
    - 192.168.120.62/24 (inet, scope: global)
    - fe80::5054:ff:fedd:a46c/64 (inet6, scope: link)
K8s Interface information: Interface: enp2s0 (index: 3)
  State: UP
  MAC: 52:54:00:00:01:11
  MTU: 1500
  Link Type: ether
  Flags: [BROADCAST MULTICAST UP LOWER_UP]
  Addresses:
    - 192.168.123.11/24 (inet, scope: global)
    - fe80::5054:ff:fe00:111/64 (inet6, scope: link)
--- Setting up OVN br-ex on 192.168.120.36 (ssh://root@192.168.120.36) ---
Mgmt Interface information: Interface: enp1s0 (index: 2)
  State: UP
  MAC: 52:54:00:54:1c:7b
  MTU: 1500
  Link Type: ether
  Flags: [BROADCAST MULTICAST UP LOWER_UP]
  Addresses:
    - 192.168.120.36/24 (inet, scope: global)
    - fe80::5054:ff:fe54:1c7b/64 (inet6, scope: link)
K8s Interface information: Interface: enp2s0 (index: 3)
  State: UP
  MAC: 52:54:00:00:01:12
  MTU: 1500
  Link Type: ether
  Flags: [BROADCAST MULTICAST UP LOWER_UP]
  Addresses:
    - 192.168.123.12/24 (inet, scope: global)
    - fe80::5054:ff:fe00:112/64 (inet6, scope: link)
--- Setting up OVN br-ex on 192.168.120.77 (ssh://root@192.168.120.77) ---
Mgmt Interface information: Interface: enp1s0 (index: 2)
  State: UP
  MAC: 52:54:00:32:e2:06
  MTU: 1500
  Link Type: ether
  Flags: [BROADCAST MULTICAST UP LOWER_UP]
  Addresses:
    - 192.168.120.77/24 (inet, scope: global)
    - fe80::5054:ff:fe32:e206/64 (inet6, scope: link)
K8s Interface information: Interface: enp2s0 (index: 3)
  State: UP
  MAC: 52:54:00:00:01:13
  MTU: 1500
  Link Type: ether
  Flags: [BROADCAST MULTICAST UP LOWER_UP]
  Addresses:
    - 192.168.123.13/24 (inet, scope: global)
    - fe80::5054:ff:fe00:113/64 (inet6, scope: link)

=== Initializing first control plane node: master-1 ===
Initializing control plane on master-1 (ssh://root@192.168.120.62)...
K8s IP: 192.168.123.11 Pod CIDR: 10.244.0.0/16, Service CIDR: 10.245.0.0/16
Setting up kubectl on master-1 (ssh://root@192.168.120.62)...
✓ Control plane initialized on master-1
Worker join command: kubeadm join 192.168.123.11:6443 --token q9t6nf.78gs3khhyijayi6i --discovery-token-ca-cert-hash sha256:d29e8e5e7071d93ab7cf5766ca0b062139e23fe816a0200cd1bbed11942d02c0
Control plane join command: kubeadm join 192.168.123.11:6443 --token q9t6nf.78gs3khhyijayi6i --discovery-token-ca-cert-hash sha256:d29e8e5e7071d93ab7cf5766ca0b062139e23fe816a0200cd1bbed11942d02c0 --control-plane --certificate-key cd9798ce677b9122d32117d8d10f71cc2e39f7596887b3215d28b62fb2e0f107
API server endpoint: https://192.168.123.11:6443
✓ Kubeconfig saved to: kubeconfig/cluster-1.kubeconfig

=== Installing ovn-kubernetes CNI on cluster cluster-1 ===
For OVN-Kubernetes installation, using Pod CIDR: 10.244.0.0/16, Service CIDR: 10.245.0.0/16, API Server URL: https://192.168.123.11:6443
Patching CoreDNS configmap for OVN-Kubernetes compatibility, dns server: 8.8.8.8
✓ CoreDNS configmap patched successfully
Running daemonset.sh to generate manifests...
✓ daemonset.sh completed successfully
Applying OVN-Kubernetes CRD manifests...
Applying external CRD manifests...
Applying OVN-Kubernetes setup manifests...
✓ Applied setup manifest ovn-setup.yaml
✓ Applied setup manifest rbac-ovnkube-identity.yaml
✓ Applied setup manifest rbac-ovnkube-cluster-manager.yaml
✓ Applied setup manifest rbac-ovnkube-master.yaml
✓ Applied setup manifest rbac-ovnkube-node.yaml
✓ Applied setup manifest rbac-ovnkube-db.yaml
✓ Master nodes labeled for OVN-Kubernetes HA
Applying OVN-Kubernetes deployment manifests...
✓ Applied deployment manifest ovnkube-identity.yaml
✓ Applied deployment manifest ovnkube-db.yaml
✓ Applied deployment manifest ovnkube-master.yaml
✓ Applied deployment manifest ovnkube-node.yaml
Waiting for all pods in namespace: ovn-kubernetes to be ready...
✓ All Pods in namespace: ovn-kubernetes are ready
✓ OVN-Kubernetes pods are ready, installed successfully!
✓ Deleted DaemonSet kube-system/kube-proxy
=== Joining worker nodes ===
✓ Worker node joined to Kubernetes cluster: host-1
✓ Worker node joined to Kubernetes cluster: dpu-1
✓ Kubernetes cluster cluster-1 setup complete

╔═══════════════════════════════════════════════╗
║         Deployment Completed Successfully!    ║
╚═══════════════════════════════════════════════╝

✓ VM deployment complete!

Your DPU simulation environment is ready:
  • VMs are running and accessible
  • Kubernetes is installed and configured
  • CNI is deployed and ready

Useful commands:
  vmctl list                    # List all VMs
  vmctl ssh <vm-name>           # SSH into a VM
  kubectl --kubeconfig kubeconfig/<cluster>.kubeconfig get nodes

Kubeconfig files: kubeconfig
For more information, see README.md
```

This will:
1. **Clean up any existing VMs and networks** (idempotent deployment - can be run multiple times safely)
2. Download Cloud Base image (if not present) - We recommend to download from Fedora.
3. Create custom libvirt networks. All Host and DPUs will have a dedicated connection between them to simulate a DPU's general design.
4. Create and start all Host and DPU VMs with cloud-init configuration
5. Wait for VMs to boot and get IP addresses
6. Kubernetes gets installed on all VMs.
  a. Disable swap (required for Kubernetes)
  b. Configure kernel modules (overlay, br_netfilter)
  c. Install and configure CRI-O
  d. Install Open vSwitch
  e. Installs Kubernetes components (`kubeadm`, `kubelet`, `kubectl`)
  f. Disable the firewall
7. One master is chosen to bootstrap the cluster with `kubeadm`
  a. Additional masters also join the cluster
8. All workers join the cluster with `kubeadm`
7. CNI gets installed on the cluster.
9. Workload pods can now be deployed on the cluster once `dpu-sim` runs to completion sucessfully.

**Note:** The `dpu-sim` application is idempotent by default - it automatically cleans up existing resources before deploying. You can run it multiple times safely. If you want to skip cleanup for some reason, use `dpu-sim --skip-cleanup`

#### Step 2b: Deploy (Kind)

Deploy all Host and DPU Containers and the network:

```bash
$ ./bin/dpu-sim --config=config-kind.yaml
╔═══════════════════════════════════════════════╗
║               DPU Simulator                   ║
╚═══════════════════════════════════════════════╝
Configuration: config-kind.yaml
Deployment mode: kind

=== Checking Dependencies ===
✓ Detected Linux distribution: rhel 9.6 (package manager: dnf, architecture: x86_64)
✓ wget is installed

✓ pip3 is installed
✓ jinjanator is installed
✓ git is installed
✓ openvswitch is installed
✓ kubectl is installed
✓ Container Runtime is installed
✓ kind is installed
✓ All dependencies are available

=== Cleaning up K8s ===
✓ Kubeconfig file removed: kubeconfig/dpu-sim-kind.kubeconfig

╔═══════════════════════════════════════════════╗
║      Kind-Based Deployment Workflow           ║
╚═══════════════════════════════════════════════╝

=== Cleaning up existing kind clusters ===
Deleting Kind cluster: dpu-sim-kind
✓ Deleted Kind cluster: dpu-sim-kind

=== Deploying Kind clusters ===

=== Creating Kind Clusters ===
Creating Kind cluster: dpu-sim-kind
✓ Created Kind cluster: dpu-sim-kind
✓ Kubeconfig saved to: kubeconfig/dpu-sim-kind.kubeconfig

Cluster: dpu-sim-kind
  Status: running
  Nodes:
    - dpu-sim-kind-control-plane (control-plane) [NotReady]
    - dpu-sim-kind-worker (worker) [NotReady]
    - dpu-sim-kind-worker2 (worker) [NotReady]
✓ Detected Linux distribution: debian 12 (package manager: apt, architecture: x86_64)
Installing missing dependencies: IPv6
Installing IPv6 for debian on docker://dpu-sim-kind-control-plane...
✓ IPv6 installed
✓ All dependencies are available
✓ Detected Linux distribution: debian 12 (package manager: apt, architecture: x86_64)
Installing missing dependencies: IPv6
Installing IPv6 for debian on docker://dpu-sim-kind-worker...
✓ IPv6 installed
✓ All dependencies are available
✓ Detected Linux distribution: debian 12 (package manager: apt, architecture: x86_64)
Installing missing dependencies: IPv6
Installing IPv6 for debian on docker://dpu-sim-kind-worker2...
✓ IPv6 installed
✓ All dependencies are available

=== Installing CNI ===

=== Installing CNI on Kind clusters ===

--- Installing CNI on cluster dpu-sim-kind ---
Pulling image ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master...
Emulate Docker CLI using podman. Create /etc/containers/nodocker to quiet msg.
Trying to pull ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master...
Getting image source signatures
Copying blob d153d8a925e7 skipped: already exists
Copying blob a6951c5915c1 skipped: already exists
Copying blob face38820b68 skipped: already exists
Copying blob 6ef53945944f skipped: already exists
Copying blob fdf401b6ab97 skipped: already exists
Copying blob f4e66d6497fe skipped: already exists
Copying blob 5bbdd5d536e2 skipped: already exists
Copying blob 323f405d2067 skipped: already exists
Copying blob cf6f6c0342d3 skipped: already exists
Copying blob a47b6925e710 skipped: already exists
Copying blob cffe8ba4d37d skipped: already exists
Copying blob 5316e83967f7 skipped: already exists
Copying blob 30711b0192a3 skipped: already exists
Copying blob 4f4fb700ef54 skipped: already exists
Copying blob c5b5a69d5870 skipped: already exists
Copying blob 779a2c0f7fcb skipped: already exists
Copying blob cd14aa180a1b skipped: already exists
Copying blob 66cab4415f1d skipped: already exists
Copying blob df11fa02b635 skipped: already exists
Copying config bf057c168f done   |
Writing manifest to image destination
bf057c168f88c40eedaa1dea9966d7d880cfae752595d210642aa22adf9068f3
✓ Pulled image: ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master
Loading image ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master into cluster dpu-sim-kind...
enabling experimental podman provider
Image: "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master" with ID "bf057c168f88c40eedaa1dea9966d7d880cfae752595d210642aa22adf9068f3" not yet present on node "dpu-sim-kind-control-plane", loading...
Image: "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master" with ID "bf057c168f88c40eedaa1dea9966d7d880cfae752595d210642aa22adf9068f3" not yet present on node "dpu-sim-kind-worker", loading...
Image: "ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master" with ID "bf057c168f88c40eedaa1dea9966d7d880cfae752595d210642aa22adf9068f3" not yet present on node "dpu-sim-kind-worker2", loading...
✓ Loaded image: ghcr.io/ovn-kubernetes/ovn-kubernetes/ovn-kube-fedora:master
Internal API server IP for cluster dpu-sim-kind: 10.89.0.84

=== Installing ovn-kubernetes CNI on cluster dpu-sim-kind ===
For OVN-Kubernetes installation, using Pod CIDR: 10.244.0.0/16, Service CIDR: 10.245.0.0/16, API Server URL: https://10.89.0.84:6443
Patching CoreDNS configmap for OVN-Kubernetes compatibility, dns server: 8.8.8.8
✓ CoreDNS configmap patched successfully
Running daemonset.sh to generate manifests...
✓ daemonset.sh completed successfully
Applying OVN-Kubernetes CRD manifests...
Applying external CRD manifests...
Applying OVN-Kubernetes setup manifests...
✓ Applied setup manifest ovn-setup.yaml
✓ Applied setup manifest rbac-ovnkube-identity.yaml
✓ Applied setup manifest rbac-ovnkube-cluster-manager.yaml
✓ Applied setup manifest rbac-ovnkube-master.yaml
✓ Applied setup manifest rbac-ovnkube-node.yaml
✓ Applied setup manifest rbac-ovnkube-db.yaml
✓ Master nodes labeled for OVN-Kubernetes HA
Applying OVN-Kubernetes deployment manifests...
✓ Applied deployment manifest ovnkube-identity.yaml
✓ Applied deployment manifest ovs-node.yaml
✓ Applied deployment manifest ovnkube-db.yaml
✓ Applied deployment manifest ovnkube-master.yaml
✓ Applied deployment manifest ovnkube-node.yaml
Waiting for all pods in namespace: ovn-kubernetes to be ready...
✓ All Pods in namespace: ovn-kubernetes are ready
✓ OVN-Kubernetes pods are ready, installed successfully!
DaemonSet kube-system/kube-proxy does not exist, skipping deletion

✓ CNI installation complete on Kind clusters

╔═══════════════════════════════════════════════╗
║         Deployment Completed Successfully!    ║
╚═══════════════════════════════════════════════╝

✓ Kind deployment complete!

Your DPU simulation environment is ready:
  • Kind clusters are running
  • CNI is deployed and ready

Useful commands:
  kind get clusters             # List all clusters
  kubectl --kubeconfig kubeconfig/<cluster>.kubeconfig get nodes

Kubeconfig files: kubeconfig
For more information, see README.md
```


#### Key Features

- **Automatic Cluster Initialization**: No manual `kubeadm` commands needed
- **Multiple Cluster Support**: Deploy multiple independent K8s clusters in one configuration
- **Custom Pod Network CIDRs**: Each cluster can have its own pod and/or service network CIDR
- **Automatic CNI Installation**: Flannel or OVN-Kubernetes is automatically installed and configured
- **Role-Based Assignment**: VMs/Kind containers are assigned as `master` or `worker` nodes
- **Network Isolation**: Different clusters use different overlay networks

After Installation finished, you should expect these software packages to be running:
- CRI-O container runtime
- `kubelet` (Kubernetes node agent)
- Flannel and other containers are running, for example:
```bash
$ kubectl get pods -A -o wide
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
$ kubectl get pods -o wide -A
NAMESPACE        NAME                               READY   STATUS    RESTARTS   AGE     IP               NODE       NOMINATED NODE   READINESS GATES
kube-system      coredns-674b8bbfcf-c7ccg           1/1     Running   0          2m26s   10.85.0.2        master-1   <none>           <none>
kube-system      coredns-674b8bbfcf-k2pkk           1/1     Running   0          2m26s   10.85.0.3        master-1   <none>           <none>
kube-system      etcd-master-1                      1/1     Running   0          2m32s   192.168.120.62   master-1   <none>           <none>
kube-system      kube-apiserver-master-1            1/1     Running   0          2m32s   192.168.120.62   master-1   <none>           <none>
kube-system      kube-controller-manager-master-1   1/1     Running   0          2m33s   192.168.120.62   master-1   <none>           <none>
kube-system      kube-scheduler-master-1            1/1     Running   0          2m32s   192.168.120.62   master-1   <none>           <none>
ovn-kubernetes   ovnkube-db-69bc9dff88-5lf94        2/2     Running   0          2m9s    192.168.120.62   master-1   <none>           <none>
ovn-kubernetes   ovnkube-identity-p7pw4             1/1     Running   0          2m10s   192.168.120.62   master-1   <none>           <none>
ovn-kubernetes   ovnkube-master-84f8dbf89-9m2bn     2/2     Running   0          2m8s    192.168.120.62   master-1   <none>           <none>
ovn-kubernetes   ovnkube-node-2j8jz                 1/3     Running   0          36s     192.168.120.36   host-1     <none>           <none>
ovn-kubernetes   ovnkube-node-7n2qp                 3/3     Running   0          2m8s    192.168.120.62   master-1   <none>           <none>
ovn-kubernetes   ovnkube-node-qbskf                 1/3     Running   0          33s     192.168.120.77   dpu-1      <none>           <none>
```

- On kind with OVN-Kubernetes, it looks like this:
```bash
$ kubectl get pods -A -o wide
NAMESPACE            NAME                                                 READY   STATUS    RESTARTS   AGE     IP           NODE                         NOMINATED NODE   READINESS GATES
kube-system          coredns-7d764666f9-5vj9p                             1/1     Running   0          8m43s   10.244.2.4   dpu-sim-kind-worker2         <none>           <none>
kube-system          coredns-7d764666f9-z8pxf                             1/1     Running   0          8m43s   10.244.2.3   dpu-sim-kind-worker2         <none>           <none>
kube-system          etcd-dpu-sim-kind-control-plane                      1/1     Running   0          8m50s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
kube-system          kube-apiserver-dpu-sim-kind-control-plane            1/1     Running   0          8m50s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
kube-system          kube-controller-manager-dpu-sim-kind-control-plane   1/1     Running   0          8m50s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
kube-system          kube-scheduler-dpu-sim-kind-control-plane            1/1     Running   0          8m50s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
local-path-storage   local-path-provisioner-67b8995b4b-w27g7              1/1     Running   0          8m43s   10.244.2.5   dpu-sim-kind-worker2         <none>           <none>
ovn-kubernetes       ovnkube-db-74b65f65b9-sfmg6                          2/2     Running   0          7m57s   10.89.0.85   dpu-sim-kind-worker          <none>           <none>
ovn-kubernetes       ovnkube-identity-76bmx                               1/1     Running   0          7m58s   10.89.0.83   dpu-sim-kind-worker2         <none>           <none>
ovn-kubernetes       ovnkube-identity-hldd5                               1/1     Running   0          7m58s   10.89.0.85   dpu-sim-kind-worker          <none>           <none>
ovn-kubernetes       ovnkube-identity-qd6bg                               1/1     Running   0          7m58s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
ovn-kubernetes       ovnkube-master-7f6dd4ffcc-dpmhs                      2/2     Running   0          7m57s   10.89.0.83   dpu-sim-kind-worker2         <none>           <none>
ovn-kubernetes       ovnkube-node-5g558                                   3/3     Running   0          7m56s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
ovn-kubernetes       ovnkube-node-rvk5q                                   3/3     Running   0          7m56s   10.89.0.83   dpu-sim-kind-worker2         <none>           <none>
ovn-kubernetes       ovnkube-node-xp9rd                                   3/3     Running   0          7m56s   10.89.0.85   dpu-sim-kind-worker          <none>           <none>
ovn-kubernetes       ovs-node-9l4sm                                       1/1     Running   0          7m58s   10.89.0.85   dpu-sim-kind-worker          <none>           <none>
ovn-kubernetes       ovs-node-vmv8f                                       1/1     Running   0          7m58s   10.89.0.83   dpu-sim-kind-worker2         <none>           <none>
ovn-kubernetes       ovs-node-zjvrq                                       1/1     Running   0          7m58s   10.89.0.84   dpu-sim-kind-control-plane   <none>           <none>
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
$ export KUBECONFIG=./kubeconfig/cluster-1.kubeconfig
$ kubectl get nodes
NAME       STATUS   ROLES           AGE   VERSION
dpu-1      Ready    <none>          13m   v1.33.6
host-1     Ready    <none>          13m   v1.33.6
master-1   Ready    control-plane   13m   v1.33.6

# Check all pods
$ kubectl get pods -A
...

# Check Flannel CNI
$ kubectl get pods -n kube-flannel

# Or check OVN-Kubernetes CNI
$ kubectl get pods -n ovn-kubernetes

...
```

#### Multiple Clusters

```bash
# Check cluster-1
$ export KUBECONFIG=./kubeconfig/cluster-1.kubeconfig
$ kubectl get nodes

# Check cluster-2
$ export KUBECONFIG=./kubeconfig/cluster-2.kubeconfig
$ kubectl get nodes

# Verify different pod CIDRs
$ export KUBECONFIG=./kubeconfig/cluster-1.kubeconfig
$ kubectl get nodes -o jsonpath="{.items[0].spec.podCIDR}"

$ export KUBECONFIG=./kubeconfig/cluster-1.kubeconfig
$ kubectl get nodes -o jsonpath="{.items[0].spec.podCIDR}"
```

### Manage VMs

List all VMs:
```bash
$ ./bin/vmctl list
VM Name              State           IP Address      vCPUs    Memory
--------------------------------------------------------------------------------
master-1             Running         192.168.120.74  2        4096MB
host-1               Running         192.168.120.66  2        2048MB
dpu-1                Running         192.168.120.69  2        2048MB

```

SSH into a VM:
```bash
$ ./bin/vmctl ssh host-1
Connecting to host-1 (192.168.120.66) as root...
[systemd]
Failed Units: 3
  cloud-final.service
  cloud-init-main.service
  NetworkManager-wait-online.service
[root@host-1 ~]#
```

Start/Stop VMs:
```bash
$ ./bin/vmctl list
VM Name              State           IP Address      vCPUs    Memory
--------------------------------------------------------------------------------
master-1             Running         192.168.120.74  2        4096MB
host-1               Running         192.168.120.66  2        2048MB
dpu-1                Running         192.168.120.69  2        2048MB
$ oc get nodes
NAME       STATUS   ROLES           AGE     VERSION
dpu-1      Ready    <none>          5h49m   v1.33.7
host-1     Ready    <none>          5h49m   v1.33.7
master-1   Ready    control-plane   5h51m   v1.33.7
$ ./bin/vmctl stop dpu-1
✓ Shutting down VM 'dpu-1'...
$ oc get nodes
NAME       STATUS     ROLES           AGE     VERSION
dpu-1      NotReady   <none>          5h49m   v1.33.7
host-1     Ready      <none>          5h49m   v1.33.7
master-1   Ready      control-plane   5h51m   v1.33.7
$ ./bin/vmctl list
VM Name              State           IP Address      vCPUs    Memory
--------------------------------------------------------------------------------
master-1             Running         192.168.120.74  2        4096MB
host-1               Running         192.168.120.66  2        2048MB
dpu-1                Shut off        N/A             2        2048MB
$ ./bin/vmctl start dpu-1
✓ Started VM 'dpu-1'
$ oc get nodes
NAME       STATUS   ROLES           AGE     VERSION
dpu-1      Ready    <none>          5h50m   v1.33.7
host-1     Ready    <none>          5h50m   v1.33.7
master-1   Ready    control-plane   5h52m   v1.33.7
$ ./bin/vmctl list
VM Name              State           IP Address      vCPUs    Memory
--------------------------------------------------------------------------------
master-1             Running         192.168.120.74  2        4096MB
host-1               Running         192.168.120.66  2        2048MB
dpu-1                Running         192.168.120.69  2        2048MB

```

Execute commands remotely:
```bash
$ ./bin/vmctl exec dpu-1 "uname -a"
Linux dpu-1 6.17.1-300.fc43.x86_64 #1 SMP PREEMPT_DYNAMIC Mon Oct  6 15:37:21 UTC 2025 x86_64 GNU/Linux
$ ./bin/vmctl exec dpu-1 "ip link show br-ex"
9: br-ex: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UNKNOWN mode DEFAULT group default qlen 1000
    link/ether 52:54:00:00:01:13 brd ff:ff:ff:ff:ff:ff

```

### Cleanup

Remove all VMs and networks:

```bash
$ ./bin/dpu-sim --cleanup
╔═══════════════════════════════════════════════╗
║               DPU Simulator                   ║
╚═══════════════════════════════════════════════╝
Configuration: config.yaml
Deployment mode: vm

=== Checking Dependencies ===
✓ Detected Linux distribution: rhel 9.6 (package manager: dnf, architecture: x86_64)
✓ wget is installed
✓ pip3 is installed
✓ jinjanator is installed
✓ git is installed
✓ openvswitch is installed
✓ libvirt is installed
✓ qemu-kvm is installed
✓ qemu-img is installed
✓ libvirt-devel is installed
✓ virt-install is installed
✓ genisoimage is installed
✓ All dependencies are available

=== Cleaning up K8s ===
✓ Kubeconfig file removed: kubeconfig/cluster-1.kubeconfig

╔═══════════════════════════════════════════════╗
║       VM-Based Deployment Workflow            ║
╚═══════════════════════════════════════════════╝
=== Cleaning up VMs ===
✓ Deleted disk: /var/lib/libvirt/images/master-1.qcow2
✓ Deleted cloud-init ISO: /var/lib/libvirt/images/master-1-cloud-init.iso
✓ Cleaned up VM: master-1
✓ Deleted disk: /var/lib/libvirt/images/host-1.qcow2
✓ Deleted cloud-init ISO: /var/lib/libvirt/images/host-1-cloud-init.iso
✓ Cleaned up VM: host-1
✓ Deleted disk: /var/lib/libvirt/images/dpu-1.qcow2
✓ Deleted cloud-init ISO: /var/lib/libvirt/images/dpu-1-cloud-init.iso
✓ Cleaned up VM: dpu-1
=== Cleaning up Networks ===
✓ Removed network mgmt-network
✓ Removed network ovn-network
✓ Removed host-to-DPU network h2d-host-1-dpu-1 (bridge: h2d-83d76b0d2f2)

✓ Cleanup complete. No deployment performed.
```

**Warning:** This will permanently delete all VMs and their disks.

**Note:** The `dpu-sim` application automatically cleans up before deploying, so you typically don't need to run cleanup manually unless you just want to remove everything without redeploying.

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
├── cmd/dpu-sim
│   ├── main.go           # The main application execution for deploying the simulation
├── cmd/vmctl
│   ├── main.go           # The helper application to manage virtual machines
├── pkg/cni
│   ├── flannel.go        # Functions to install Flannel CNI
│   ├── install.go        # Delagates CNI installation
│   ├── multus.go         # Functions to install Multus CNI
│   ├── ovn_kubernetes.go # Functions to install OVN-Kubernetes CNI
│   ├── types.go          # Types related to CNI
├── pkg/config
│   ├── config.go         # Functions to manage configuration YAML files
│   ├── config_test.go    # Unit tests for configuration files
│   ├── types.go          # Types related to Config
├── pkg/k8s
│   ├── cleanup.go
│   ├── client.go
│   ├── install.go
│   ├── types.go
├── pkg/kind
│   ├── cleanup.go
│   ├── cluster.go
│   ├── config.go
│   ├── types.go
├── pkg/linux
│   ├── linux.go
├── pkg/log
│   ├── log.go
├── pkg/network
│   ├── network.go       # Bridge name generation & Networking helper functions
│   ├── network_test.go
├── pkg/platform
│   ├── deps.go
│   ├── distro.go
│   ├── distro_test.go
│   ├── executor.go
│   ├── executor_test.go
│   ├── types.go
├── pkg/requirements
│   ├── requirements.go
├── pkg/ssh
│   ├── ssh.go           # Execute commands on remote hosts
│   ├── ssh_test.go
├── pkg/vm
│   ├── cleanup.go
│   ├──  create.go
│   ├── disk.go
│   ├──  info.go
│   ├── install.go
│   ├── lifecycle.go
│   ├──  network.go
│   ├──  types.go
```

## Testing

### Unit Tests

Each package has unit tests:

```bash
# Run all tests
make test

# Run specific package
go test ./pkg/config/

# Run with coverage
go test -cover ./pkg/config/
```

## Troubleshooting

### VMs not getting IP addresses

Wait 1-2 minutes for VMs to boot. Check VM status:
```bash
$ ./bin/vmctl list
VM Name              State           IP Address      vCPUs    Memory
--------------------------------------------------------------------------------
master-1             Running         192.168.120.74  2        4096MB
host-1               Running         192.168.120.66  2        2048MB
dpu-1                Running         192.168.120.69  2        2048MB
```

### Cannot connect via SSH

1. Verify VM is running: `./bin/vmctl list`
2. Check VM has IP address
3. Try SSH access: `./bin/vmctl ssh host-1`
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

### View Cluster Logs

```bash
# Check kubelet logs on any node
./bin/vmctl exec <vm-name> 'sudo journalctl -u kubelet -n 50'

# Check kubeadm init logs on master
./bin/vmctl exec <vm-master-name> 'cat /tmp/kubeadm-init.log'
```

## Open vSwitch Usage

### OVS Data Bridge (on Host system)

If you configured a network with `use_ovs: true`, an OVS bridge is created on the host system that connects all VMs:

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
./bin/vmctl ssh host-1

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
