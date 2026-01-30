#!/usr/bin/env python3
"""
SSH utilities for VM management
"""

import subprocess
from pathlib import Path
from typing import Dict, List, Tuple, Any, Optional


def build_ssh_command(config: Dict[str, Any], ip: str, command: Optional[str] = None) -> List[str]:
    """Build SSH command array

    Args:
        config: Configuration dictionary from cfg file containing SSH settings
        ip: IP address of the target VM
        command: Optional command to execute (None for interactive shell)

    Returns:
        SSH command as a list of strings
    """
    ssh_user = config['ssh']['user']
    ssh_key = Path(config['ssh']['key_path']).expanduser()

    # In a lab environment, we want to disable StrictHostKeyChecking and UserKnownHostsFile
    # to avoid cleaning up known hosts file when the VM is destroyed and recreated with the same IP address.
    ssh_cmd = [
        'ssh',
        '-i', str(ssh_key),
        '-o', 'StrictHostKeyChecking=no',
        '-o', 'UserKnownHostsFile=/dev/null',
        '-o', 'LogLevel=ERROR',
        '-o', 'ConnectTimeout=5',
        f'{ssh_user}@{ip}'
    ]

    if command:
        ssh_cmd.append(command)

    return ssh_cmd


def ssh_command(config: Dict[str, Any], ip: str, command: str,
                capture_output: bool = True, timeout: int = 10) -> Tuple[bool, str, str]:
    """Execute command on VM via SSH

    Args:
        config: Configuration dictionary containing SSH settings
        ip: IP address of the target VM
        command: Command to execute
        capture_output: Whether to capture stdout/stderr (False for interactive)
        timeout: Command timeout in seconds

    Returns:
        Tuple of (success, stdout, stderr)
    """
    ssh_cmd = build_ssh_command(config, ip, command)

    try:
        result = subprocess.run(
            ssh_cmd,
            capture_output=capture_output,
            text=True,
            timeout=timeout
        )
        return (result.returncode == 0, result.stdout if capture_output else "",
                result.stderr if capture_output else "")
    except subprocess.TimeoutExpired:
        return (False, "", "SSH command timed out")
    except Exception as e:
        return (False, "", str(e))

