#!/bin/bash
# Quick Start Script for VM Deployment

set -e

echo "=== DPU Simulation VM Deployment Quick Start ==="
echo

# Check if running on Linux
if [[ "$OSTYPE" != "linux-gnu"* ]]; then
    echo "Error: This script is designed for Linux systems"
    exit 1
fi

# Check if running Fedora/RHEL/CentOS
if ! command -v dnf &> /dev/null; then
    echo "Warning: dnf not found. This script is optimized for Fedora/RHEL/CentOS"
    echo "You may need to adapt package installation commands for your distribution"
fi

# Function to check if command exists
command_exists() {
    command -v "$1" &> /dev/null
}

echo "Step 1: Checking system requirements..."

# Check virtualization support
if ! grep -E '(vmx|svm)' /proc/cpuinfo &> /dev/null; then
    echo "Error: CPU does not support virtualization (VT-x/AMD-V)"
    exit 1
fi
echo "✓ Virtualization support detected"

# Check if user is in libvirt group
if ! groups | grep -q libvirt; then
    echo "Warning: User $USER is not in 'libvirt' group"
    echo "Run: sudo usermod -a -G libvirt $USER"
    echo "Then log out and log back in"
fi

echo
echo "Step 2: Checking required packages..."

MISSING_PACKAGES=()

if ! command_exists virsh; then
    MISSING_PACKAGES+=("libvirt" "libvirt-daemon-kvm" "qemu-kvm")
fi

if ! command_exists genisoimage; then
    MISSING_PACKAGES+=("genisoimage")
fi

if ! command_exists wget; then
    MISSING_PACKAGES+=("wget")
fi

if [ ${#MISSING_PACKAGES[@]} -ne 0 ]; then
    echo "Missing packages: ${MISSING_PACKAGES[*]}"
    read -p "Install missing packages? (requires sudo) [y/N]: " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo dnf install -y "${MISSING_PACKAGES[@]}"
        sudo systemctl enable --now libvirtd
        echo "✓ Packages installed"
    else
        echo "Please install required packages manually"
        exit 1
    fi
else
    echo "✓ All required packages are installed"
fi

echo
echo "Step 3: Checking libvirt service..."
if systemctl is-active --quiet libvirtd; then
    echo "✓ libvirtd is running"
else
    echo "Starting libvirtd..."
    sudo systemctl start libvirtd
    echo "✓ libvirtd started"
fi

echo
echo "Step 4: Checking SSH keys..."
if [ ! -f ~/.ssh/id_rsa ]; then
    echo "SSH key not found. Generating..."
    ssh-keygen -t rsa -b 4096 -f ~/.ssh/id_rsa -N ""
    echo "✓ SSH key generated"
else
    echo "✓ SSH key exists"
fi

echo
echo "Step 5: Installing Python dependencies..."
if command_exists pip3; then
    pip3 install -r requirements.txt --user
    echo "✓ Python dependencies installed"
else
    echo "Warning: pip3 not found. Please install Python dependencies manually:"
    echo "  pip3 install -r requirements.txt"
fi

echo
echo "=== Setup Complete ==="
echo
