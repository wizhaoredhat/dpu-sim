#!/usr/bin/env python3
"""
VM Control Utility
Easy access and management of VMs
"""

import sys
import libvirt
import subprocess
import argparse
import traceback
from vm_utils import connect_libvirt, get_vm_ip
from cfg_utils import load_config
from ssh_utils import ssh_command, build_ssh_command


class VMController:
    def __init__(self, config_path="config.yaml"):
        self.config = load_config(config_path)
        self.conn = None

    def connect(self):
        """Connect to libvirt"""
        self.conn = connect_libvirt()
        return self.conn is not None

    def get_vm_ip(self, vm_name):
        """Get IP address of a VM"""
        return get_vm_ip(self.conn, vm_name)

    def list_vms(self):
        """List all VMs with their status and IPs"""
        print(f"{'VM Name':<20} {'State':<15} {'IP Address':<15} {'vCPUs':<8} {'Memory':<10}")
        print("-" * 78)

        for vm_config in self.config['vms']:
            vm_name = vm_config['name']

            try:
                vm = self.conn.lookupByName(vm_name)

                # Get state
                state, _ = vm.state()
                state_str = {
                    libvirt.VIR_DOMAIN_RUNNING: "Running",
                    libvirt.VIR_DOMAIN_BLOCKED: "Blocked",
                    libvirt.VIR_DOMAIN_PAUSED: "Paused",
                    libvirt.VIR_DOMAIN_SHUTDOWN: "Shutdown",
                    libvirt.VIR_DOMAIN_SHUTOFF: "Shut off",
                    libvirt.VIR_DOMAIN_CRASHED: "Crashed",
                }.get(state, "Unknown")

                # Get IP
                ip = self.get_vm_ip(vm_name) if state == libvirt.VIR_DOMAIN_RUNNING else "N/A"

                # Get info
                info = vm.info()
                vcpus = info[3]
                memory_mb = info[2] // 1024

                print(f"{vm_name:<20} {state_str:<15} {ip if ip else 'N/A':<15} {vcpus:<8} {memory_mb}MB")

            except libvirt.libvirtError:
                print(f"{vm_name:<20} {'Not Found':<15} {'N/A':<15} {'N/A':<8} {'N/A':<10}")

    def ssh_vm(self, vm_name):
        """SSH into a VM interactively"""
        ip = self.get_vm_ip(vm_name)

        if not ip:
            print(f"Could not get IP address for VM '{vm_name}'")
            print("Make sure the VM is running.")
            return False

        ssh_user = self.config['ssh']['user']
        print(f"Connecting to {vm_name} ({ip}) as {ssh_user}...")

        try:
            ssh_cmd = build_ssh_command(self.config, ip)
            subprocess.run(ssh_cmd)
            return True
        except Exception as e:
            print(f"Failed to SSH: {e}")
            return False

    def console_vm(self, vm_name):
        """Access VM console"""
        try:
            vm = self.conn.lookupByName(vm_name)

            print(f"Opening console for {vm_name}...")
            print("Press Ctrl+] to exit console")

            subprocess.run(['virsh', 'console', vm_name])
            return True

        except libvirt.libvirtError as e:
            print(f"Error accessing console: {e}")
            return False

    def start_vm(self, vm_name):
        """Start a VM"""
        try:
            vm = self.conn.lookupByName(vm_name)

            if vm.isActive():
                print(f"VM '{vm_name}' is already running")
                return True

            vm.create()
            print(f"Started VM '{vm_name}'")
            return True

        except libvirt.libvirtError as e:
            print(f"Failed to start VM: {e}")
            return False

    def stop_vm(self, vm_name):
        """Stop a VM"""
        try:
            vm = self.conn.lookupByName(vm_name)

            if not vm.isActive():
                print(f"VM '{vm_name}' is already stopped")
                return True

            vm.shutdown()
            print(f"Shutting down VM '{vm_name}'...")
            return True

        except libvirt.libvirtError as e:
            print(f"Failed to stop VM: {e}")
            return False

    def destroy_vm(self, vm_name):
        """Force stop a VM"""
        try:
            vm = self.conn.lookupByName(vm_name)

            if vm.isActive():
                vm.destroy()
                print(f"Force stopped VM '{vm_name}'")

            return True

        except libvirt.libvirtError as e:
            print(f"Failed to destroy VM: {e}")
            return False

    def reboot_vm(self, vm_name):
        """Reboot a VM"""
        try:
            vm = self.conn.lookupByName(vm_name)

            if not vm.isActive():
                print(f"VM '{vm_name}' is not running")
                return False

            vm.reboot()
            print(f"Rebooting VM '{vm_name}'...")
            return True

        except libvirt.libvirtError as e:
            print(f"Failed to reboot VM: {e}")
            return False

    def exec_command(self, vm_name, command):
        """Execute a command on a VM via SSH"""
        ip = self.get_vm_ip(vm_name)

        if not ip:
            print(f"Could not get IP address for VM '{vm_name}'")
            return False

        try:
            success, _, _ = ssh_command(self.config, ip, command, capture_output=False)
            return success
        except Exception as e:
            print(f"Failed to execute command: {e}")
            return False

    def cleanup(self):
        """Cleanup resources"""
        if self.conn:
            self.conn.close()


def main():
    parser = argparse.ArgumentParser(
        description='VM Control Utility',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s list                  # List all VMs
  %(prog)s ssh host-1            # SSH into VM
  %(prog)s console host-1        # Open serial console
  %(prog)s start host-1          # Start VM
  %(prog)s stop host-1           # Stop VM
  %(prog)s reboot host-1         # Reboot VM
  %(prog)s exec host-1 "uptime"  # Execute command on VM
        """
    )

    parser.add_argument('action',
                       choices=['list', 'ssh', 'console', 'start', 'stop', 'destroy', 'reboot', 'exec'],
                       help='Action to perform')
    parser.add_argument('vm_name', nargs='?', help='Name of the VM')
    parser.add_argument('command', nargs='?', help='Command to execute (for exec action)')

    args = parser.parse_args()

    # Validate arguments
    if args.action != 'list' and not args.vm_name:
        parser.error(f"VM name is required for '{args.action}' action")

    if args.action == 'exec' and not args.command:
        parser.error("Command is required for 'exec' action")

    controller = VMController()

    try:
        if not controller.connect():
            sys.exit(1)

        # Execute action
        if args.action == 'list':
            controller.list_vms()
        elif args.action == 'ssh':
            controller.ssh_vm(args.vm_name)
        elif args.action == 'console':
            controller.console_vm(args.vm_name)
        elif args.action == 'start':
            controller.start_vm(args.vm_name)
        elif args.action == 'stop':
            controller.stop_vm(args.vm_name)
        elif args.action == 'destroy':
            controller.destroy_vm(args.vm_name)
        elif args.action == 'reboot':
            controller.reboot_vm(args.vm_name)
        elif args.action == 'exec':
            controller.exec_command(args.vm_name, args.command)

    except KeyboardInterrupt:
        print("\n\nInterrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\nError: {e}")
        traceback.print_exc()
        sys.exit(1)
    finally:
        controller.cleanup()


if __name__ == '__main__':
    main()

