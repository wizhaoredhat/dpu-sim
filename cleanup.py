#!/usr/bin/env python3
"""
Cleanup Script
Removes VMs, network, and associated resources
"""

import sys
import yaml
import libvirt
from typing import Optional
import traceback
from vm_utils import cleanup_vms, cleanup_networks


class Cleanup:
    def __init__(self, config_path: str = "config.yaml") -> None:
        with open(config_path, 'r') as f:
            self.config = yaml.safe_load(f)

        self.conn: Optional[libvirt.virConnect] = None

    def connect(self) -> bool:
        """Connect to libvirt"""
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
        print("=== Cleanup Starting ===\n")
        print("WARNING: This will remove all VMs and networks defined in config.yaml")

        response = input("Are you sure you want to continue? (yes/no): ")
        if response.lower() not in ['yes', 'y']:
            print("✗ Cleanup cancelled")
            return False

        if not self.connect():
            return False

        cleanup_vms(self.config, self.conn)
        cleanup_networks(self.config, self.conn)

        print("\n=== Cleanup Complete ===")
        return True

    def cleanup_conn(self) -> None:
        """Cleanup libvirt connection"""
        if self.conn:
            self.conn.close()


def main():
    cleanup = Cleanup()

    try:
        success = cleanup.run()
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n\n✗Cleanup interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Error during cleanup: {e}")
        traceback.print_exc()
        sys.exit(1)
    finally:
        cleanup.cleanup_conn()


if __name__ == '__main__':
    main()

