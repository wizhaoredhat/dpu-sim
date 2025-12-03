#!/usr/bin/env python3

"""
DPU Simulator - Main Entry Point
Orchestrates deployment using VMs or Kind containers
"""

import sys
import argparse
import traceback
from pathlib import Path

from cfg_utils import load_config


def get_deployment_mode(config_path: str) -> str:
    """Determine deployment mode based on config file

    Args:
        config_path: Path to configuration file

    Returns:
        'vm' for VM-based deployment, 'kind' for container-based deployment
    """
    config = load_config(config_path)

    has_vms = 'vms' in config and config['vms']
    has_kind = 'kind' in config and config['kind']

    if has_kind and has_vms:
        print("✗ Error: Both 'vms' and 'kind' sections found in config.")
        print("  Please remove one of the sections to use only one deployment mode.")
        sys.exit(1)
    elif has_kind:
        return 'kind'
    elif has_vms:
        return 'vm'
    else:
        print("✗ Error: Neither 'vms' nor 'kind' section found in config!")
        print("  Please add either a 'vms' or 'kind' section to your configuration.")
        sys.exit(1)


def deploy_vms(config_path, cleanup=True):
    """Deploy VMs using deploy.py"""
    from deploy import VMDeployer

    print("=== Deploying Virtual Machines ===")

    deployer = VMDeployer(config_path)

    try:
        success = deployer.deploy(cleanup=cleanup)
        if not success:
            print("\n✗ VM deployment failed!")
            return False

        print("\n✓ VM deployment completed successfully!")
        return True
    finally:
        deployer.cleanup()


def install_software(config_path, parallel=False):
    """Install software on VMs using install_software.py"""
    from install_software import SoftwareInstaller

    print("=== Installing Software on VMs ===")

    installer = SoftwareInstaller(config_path)

    try:
        if not installer.connect():
            print("\n✗ Failed to connect to libvirt!")
            return False

        installer.install_all_vms(parallel=parallel)

        all_success = all(installer.results.values())
        if not all_success:
            print("\n✗ Software installation failed on some VMs!")
            return False

        print("\n✓ Software installation completed successfully!")
        return True
    finally:
        installer.cleanup()


def deploy_kind(config_path, cleanup=True):
    """Deploy Kind clusters using kind_deploy.py"""
    from kind_deploy import KindDeployer

    print("=== Deploying Kind Cluster ===")

    deployer = KindDeployer(config_path)

    try:
        success = deployer.deploy(cleanup=cleanup)
        if not success:
            print("\n✗ Kind deployment failed!")
            return False

        print("\n✓ Kind deployment completed successfully!")
        return True
    finally:
        deployer.cleanup()


def run_vm_setup(config_path="config.yaml", cleanup=True, parallel=False):
    """Run full VM setup: deploy VMs + install software"""
    print("=== DPU Simulator Setup (VM Mode) ===")

    if not deploy_vms(config_path, cleanup=cleanup):
        return False

    if not install_software(config_path, parallel=parallel):
        return False

    # Success!
    print("\n" + "=" * 70)
    print(" " * 15 + "✓ DPU Simulator Setup Complete!")
    print("=" * 70)
    print("\nYour DPU simulation environment is ready!")
    print("\nNext steps:")
    print("  • List VMs:        python3 vmctl.py list")
    print("  • SSH to VM:       python3 vmctl.py ssh <vm-name>")
    print("=" * 70 + "\n")

    return True


def run_kind_setup(config_path="config.yaml", cleanup=True):
    """Run Kind setup: create Kind cluster + install CNI"""
    print("=== DPU Simulator Setup (Kind Mode) ===")

    if not deploy_kind(config_path, cleanup=cleanup):
        return False

    # Success!
    config = load_config(config_path)
    clusters = config.get('kubernetes', {}).get('clusters', [])
    project_dir = Path(config_path).resolve().parent
    kubeconfig_dir = project_dir / 'kubeconfig'

    print("\n" + "=" * 70)
    print(" " * 15 + "✓ DPU Simulator Setup Complete!")
    print("=" * 70)
    print("\nYour Kind-based simulation environment is ready!")
    print("\nNext steps:")
    for cluster in clusters:
        cluster_name = cluster['name']
        kubeconfig = kubeconfig_dir / f"{cluster_name}.yaml"
        print(f"\n  Cluster '{cluster_name}':")
        print(f"    export KUBECONFIG={kubeconfig}")
        print(f"    kubectl get nodes")
        print(f"    kubectl get pods -A")
    print("=" * 70 + "\n")

    return True


def run_full_setup(config_path="config.yaml", cleanup=True, parallel=False):
    """Run full setup based on deployment mode"""
    mode = get_deployment_mode(config_path)

    if mode == 'kind':
        return run_kind_setup(config_path, cleanup=cleanup)
    else:
        return run_vm_setup(config_path, cleanup=cleanup, parallel=parallel)


def main():
    parser = argparse.ArgumentParser(
        description='DPU Simulator - Complete setup orchestration (supports VMs and Kind containers)',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Deployment Modes:
  VM Mode:   Uses libvirt VMs (when config has 'vms' section)
  Kind Mode: Uses Kind containers (when config has 'kind' section)

Examples:
  # Full setup (auto-detects mode from config):
  python3 dpu-sim.py

  # Use Kind containers (faster):
  python3 dpu-sim.py --config config-kind.yaml

  # Full setup with parallel installation (VM mode):
  python3 dpu-sim.py --parallel

  # Deploy without cleanup (not recommended):
  python3 dpu-sim.py --no-cleanup

  # Use custom config file:
  python3 dpu-sim.py --config my-config.yaml

To run individual steps separately:
  # VM Mode:
  python3 deploy.py              # Deploy VMs only
  python3 install_software.py    # Install software only

  # Kind Mode:
  python3 kind_deploy.py         # Deploy Kind cluster only
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
        help='Skip cleanup of existing VMs/networks/clusters before deployment'
    )
    parser.add_argument(
        '--parallel', '-p',
        action='store_true',
        help='Install software on all VMs in parallel (VM mode only, faster but harder to debug)'
    )
    parser.add_argument(
        '--mode',
        choices=['auto', 'vm', 'kind'],
        default='auto',
        help='Deployment mode: auto (detect from config), vm, or kind (default: auto)'
    )

    args = parser.parse_args()

    config_path = Path(args.config)
    if not config_path.exists():
        print(f"✗ Error: Configuration file '{args.config}' not found!")
        sys.exit(1)

    try:
        # Determine deployment mode
        if args.mode == 'auto':
            mode = get_deployment_mode(args.config)
        else:
            mode = args.mode

        print(f"Deployment mode: {mode}")
        print(f"Configuration: {args.config}\n")

        # Run appropriate setup
        if mode == 'kind':
            success = run_kind_setup(
                config_path=args.config,
                cleanup=not args.no_cleanup
            )
        else:
            success = run_vm_setup(
                config_path=args.config,
                cleanup=not args.no_cleanup,
                parallel=args.parallel
            )

        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n\n✗ Setup interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Error during setup: {e}")
        traceback.print_exc()
        sys.exit(1)


if __name__ == '__main__':
    main()
