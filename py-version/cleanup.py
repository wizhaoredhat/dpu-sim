#!/usr/bin/env python3
"""
Cleanup Script
Removes VMs, network, and associated resources (VM mode)
Or removes Kind clusters (container mode)
"""

import sys
import argparse
import yaml
from typing import Optional, Union
import traceback

from kind_deploy import KindCleanup


def is_kind_config(config: dict) -> bool:
    """Check if the config is for Kind (container) mode"""
    return 'kind' in config and 'vms' not in config


class VMCleanup:
    """Cleanup handler for VM mode"""

    def __init__(self, config: dict) -> None:
        self.config = config
        self.conn = None

    def connect(self) -> bool:
        """Connect to libvirt"""
        import libvirt
        try:
            self.conn = libvirt.open('qemu:///system')
            if self.conn is None:
                print('Failed to open connection to qemu:///system')
                return False
            return True
        except libvirt.libvirtError as e:
            print(f"Failed to connect to libvirt: {e}")
            return False

    def run(self) -> bool:
        """Main cleanup workflow"""
        from vm_utils import cleanup_vms, cleanup_networks

        print("=== VM Cleanup Starting ===\n")
        print("WARNING: This will remove all VMs and networks defined in config")

        response = input("Are you sure you want to continue? (yes/no): ")
        if response.lower() not in ['yes', 'y']:
            print("✗ Cleanup cancelled")
            return False

        if not self.connect():
            return False

        cleanup_vms(self.config, self.conn)
        cleanup_networks(self.config, self.conn)

        print("\n=== VM Cleanup Complete ===")
        return True

    def cleanup_conn(self) -> None:
        """Cleanup libvirt connection"""
        if self.conn:
            self.conn.close()


class Cleanup:
    def __init__(self, config_path: str = "config.yaml") -> None:
        self.config_path = config_path
        with open(config_path, 'r') as f:
            self.config = yaml.safe_load(f)

        self.handler: Optional[Union[VMCleanup, KindCleanup]] = None

    def run(self) -> bool:
        """Main cleanup workflow - delegates to appropriate handler"""
        if is_kind_config(self.config):
            print(f"Detected Kind (container) mode from {self.config_path}\n")
            self.handler = KindCleanup(self.config, self.config_path)
        else:
            print(f"Detected VM mode from {self.config_path}\n")
            self.handler = VMCleanup(self.config)
        return self.handler.run()

    def cleanup_conn(self) -> None:
        """Cleanup connections"""
        if isinstance(self.handler, VMCleanup):
            self.handler.cleanup_conn()


def main():
    parser = argparse.ArgumentParser(description='Cleanup DPU simulator resources')
    parser.add_argument('--config', '-c', default='config.yaml',
                        help='Path to configuration file (default: config.yaml)')
    args = parser.parse_args()

    cleanup = Cleanup(args.config)

    try:
        success = cleanup.run()
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n\n✗ Cleanup interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Error during cleanup: {e}")
        traceback.print_exc()
        sys.exit(1)
    finally:
        cleanup.cleanup_conn()


if __name__ == '__main__':
    main()

