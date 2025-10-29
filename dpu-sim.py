#!/usr/bin/env python3

"""
DPU Simulator - Main Entry Point
Orchestrates VM deployment and software installation
"""

import sys
import argparse
import traceback
from pathlib import Path

# Import the main classes from our modules
from deploy import VMDeployer
from install_software import SoftwareInstaller

def deploy_vms(config_path, cleanup=True):
    """Deploy VMs using deploy.py"""
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


def run_full_setup(config_path="config.yaml", cleanup=True, parallel=False):
    """Run full setup: deploy VMs + install software"""
    print("=== DPU Simulator Setup ===")

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


def main():
    parser = argparse.ArgumentParser(
        description='DPU Simulator - Complete setup orchestration (deploys VMs and installs software)',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Full setup (deploy VMs + install software):
  python3 dpu-sim.py

  # Full setup with parallel installation:
  python3 dpu-sim.py --parallel

  # Deploy without cleanup (not recommended):
  python3 dpu-sim.py --no-cleanup

  # Use custom config file:
  python3 dpu-sim.py --config my-config.yaml

To run individual steps separately:
  python3 deploy.py              # Deploy VMs only
  python3 install_software.py    # Install software only
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
        help='Skip cleanup of existing VMs/networks before deployment'
    )
    parser.add_argument(
        '--parallel', '-p',
        action='store_true',
        help='Install software on all VMs in parallel (faster but harder to debug)'
    )

    args = parser.parse_args()

    config_path = Path(args.config)
    if not config_path.exists():
        print(f"✗ Error: Configuration file '{args.config}' not found!")
        sys.exit(1)

    try:
        # Run full setup
        success = run_full_setup(
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

