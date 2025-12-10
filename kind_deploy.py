#!/usr/bin/env python3
"""
Kind Cluster Deployment Script
Deploys Kubernetes clusters using Kind (Kubernetes in Docker) containers
"""

import sys
import subprocess
import tempfile
import argparse
import traceback
from pathlib import Path
from typing import Dict, Any, List

from cfg_utils import load_config, get_cluster_names
from kind_utils import (
    check_kind_installed,
    check_docker_running,
    cluster_exists,
    delete_cluster,
    get_cluster_nodes,
    get_node_ip,
    export_kubeconfig,
    wait_for_node_ready,
    generate_kind_config,
    configure_ipv6_on_nodes,
    is_ovn_kubernetes,
    patch_coredns_for_ovn,
    get_api_server_url
)
from ovn_kubernetes_deploy import prepare_ovn_kubernetes_deployment, load_ovn_image, install_ovn_kubernetes


class KindCleanup:
    """Cleanup handler for Kind (container) mode"""

    def __init__(self, config: dict, config_path: str = "config.yaml") -> None:
        self.config = config
        # Kubeconfig directory is relative to the config file location
        self.project_dir = Path(config_path).resolve().parent
        self.kubeconfig_dir = self.project_dir / 'kubeconfig'

    def cleanup_kubeconfigs(self, cluster_names: List[str]) -> None:
        """Remove kubeconfig files for the specified clusters

        Args:
            cluster_names: List of cluster names whose kubeconfigs should be removed
        """
        if not self.kubeconfig_dir.exists():
            return

        for cluster_name in cluster_names:
            kubeconfig_file = self.kubeconfig_dir / f'{cluster_name}.yaml'
            if kubeconfig_file.exists():
                print(f"  Removing kubeconfig: {kubeconfig_file.name}")
                kubeconfig_file.unlink()

        # Remove kubeconfig directory if empty
        if self.kubeconfig_dir.exists() and not any(self.kubeconfig_dir.iterdir()):
            self.kubeconfig_dir.rmdir()
            print("  Removed empty kubeconfig directory")

    def run(self, confirm: bool = True) -> bool:
        """Run Kind cluster cleanup

        Args:
            confirm: Whether to prompt for confirmation (default: True)

        Returns:
            True if cleanup was successful, False otherwise
        """
        if not check_kind_installed():
            print("✗ Kind is not installed")
            return False

        cluster_names = get_cluster_names(self.config)
        if not cluster_names:
            print("✗ No cluster names found in config")
            return False

        # Check which clusters actually exist
        existing_clusters = [name for name in cluster_names if cluster_exists(name)]

        # Check for existing kubeconfig files
        existing_kubeconfigs = []
        if self.kubeconfig_dir.exists():
            for name in cluster_names:
                if (self.kubeconfig_dir / f'{name}.yaml').exists():
                    existing_kubeconfigs.append(name)

        print("=== Kind Cluster Cleanup Starting ===\n")

        if not existing_clusters and not existing_kubeconfigs:
            print("No clusters to delete. The following clusters from config do not exist:")
            for name in cluster_names:
                print(f"  - {name}")
            return True

        if existing_clusters:
            print("WARNING: This will delete the following Kind cluster(s):")
            for name in existing_clusters:
                print(f"  - {name}")

        if existing_kubeconfigs:
            print("\nThe following kubeconfig files will be removed:")
            for name in existing_kubeconfigs:
                print(f"  - {name}.yaml")

        if len(cluster_names) > len(existing_clusters):
            non_existing = [name for name in cluster_names if name not in existing_clusters]
            if non_existing:
                print("\nThe following clusters from config do not exist (skipping):")
                for name in non_existing:
                    print(f"  - {name}")

        if confirm:
            response = input("\nAre you sure you want to continue? (yes/no): ")
            if response.lower() not in ['yes', 'y']:
                print("✗ Cleanup cancelled")
                return False

        all_success = True
        for cluster_name in existing_clusters:
            print(f"\nDeleting Kind cluster '{cluster_name}'...")
            if delete_cluster(cluster_name):
                print(f"✓ Cluster '{cluster_name}' deleted successfully")
            else:
                print(f"✗ Failed to delete cluster '{cluster_name}'")
                all_success = False

        # Cleanup kubeconfig files
        if existing_kubeconfigs:
            print("\nCleaning up kubeconfig files...")
            self.cleanup_kubeconfigs(existing_kubeconfigs)

        print("\n=== Kind Cluster Cleanup Complete ===")
        return all_success


class KindDeployer:
    """Deploys and manages Kind clusters for DPU simulation"""

    def __init__(self, config_path: str = "config.yaml") -> None:
        self.config_path = config_path
        self.config = load_config(config_path)
        self.clusters: List[str] = []  # Track created clusters
        # Store kubeconfig in project directory
        self.project_dir = Path(config_path).resolve().parent
        self.kubeconfig_dir = self.project_dir / 'kubeconfig'

    def check_prerequisites(self) -> bool:
        """Check all prerequisites are met

        Returns:
            True if all prerequisites are met, False otherwise
        """
        print("--- Checking prerequisites ---")

        # Check Docker
        if not check_docker_running():
            print("✗ Docker is not running. Please start Docker:")
            print("  sudo systemctl start docker")
            return False
        print("✓ Docker is running")

        # Check Kind
        if not check_kind_installed():
            print("✗ Kind is not installed. Please install Kind:")
            print("  # Using Go")
            print("  go install sigs.k8s.io/kind@latest")
            print("")
            print("  # Or download binary")
            print("  curl -Lo ./kind https://kind.sigs.k8s.io/dl/latest/kind-linux-amd64")
            print("  chmod +x ./kind")
            print("  sudo mv ./kind /usr/local/bin/kind")
            return False
        print("✓ Kind is installed")

        # Check kubectl
        try:
            result = subprocess.run(['kubectl', 'version', '--client'],
                                    capture_output=True, text=True)
            if result.returncode != 0:
                raise FileNotFoundError
            print("✓ kubectl is installed")
        except FileNotFoundError:
            print("✗ kubectl is not installed. Please install kubectl:")
            print("  curl -LO https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl")
            print("  chmod +x kubectl")
            print("  sudo mv kubectl /usr/local/bin/")
            return False

        return True

    def cleanup_all(self) -> bool:
        """Cleanup all Kind clusters defined in config

        Returns:
            True if cleanup was successful, False otherwise
        """
        cleanup_handler = KindCleanup(self.config, self.config_path)
        return cleanup_handler.run(confirm=False)

    def create_cluster(self, cluster_name: str, kind_config: Dict[str, Any],
                       cluster_config: Dict[str, Any]) -> bool:
        """Create a Kind cluster

        Args:
            cluster_name: Name for the cluster
            kind_config: Kind-specific configuration
            cluster_config: Kubernetes cluster configuration

        Returns:
            True if cluster was created successfully, False otherwise
        """
        print(f"\n--- Creating Kind cluster '{cluster_name}' ---")

        # Generate Kind config
        config_yaml = generate_kind_config(kind_config, cluster_config)
        print("Kind cluster configuration:")
        for line in config_yaml.split('\n'):
            print(f"  {line}")

        # Write config to temp file
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write(config_yaml)
            config_file = f.name

        try:
            # Create cluster
            print(f"\nCreating cluster (this may take a few minutes)...")
            result = subprocess.run(
                ['kind', 'create', 'cluster', '--name', cluster_name,
                 '--config', config_file],
                capture_output=True, text=True
            )

            if result.returncode != 0:
                print(f"✗ Failed to create cluster: {result.stderr}")
                return False

            print(f"✓ Cluster '{cluster_name}' created")

            # Export kubeconfig
            self.kubeconfig_dir.mkdir(parents=True, exist_ok=True)
            kubeconfig_path = self.kubeconfig_dir / f'{cluster_name}.yaml'
            if export_kubeconfig(cluster_name, str(kubeconfig_path)):
                print(f"✓ Kubeconfig saved to: {kubeconfig_path}")
            else:
                print(f"✗ Failed to save kubeconfig")
                return False

            # Nodes will not be ready for none kindnet CNI clusters
            if not is_ovn_kubernetes(cluster_config.get('cni', 'kindnet')):
                # Wait for nodes to be ready
                print("  Waiting for nodes to be ready...")
                if not wait_for_node_ready(str(kubeconfig_path), timeout=300):
                    print("✗ Nodes did not become ready in time")
                    return False
                print("✓ All nodes are ready")

            self.clusters.append(cluster_name)
            return True

        finally:
            # Cleanup temp file
            Path(config_file).unlink(missing_ok=True)

    def deploy_ovn_kubernetes(self, cluster_name: str, cluster_config: Dict[str, Any]) -> bool:
        """Install OVN-Kubernetes CNI

        Args:
            cluster_name: Name of the cluster
            cluster_config: Kubernetes cluster configuration

        Returns:
            True if OVN-Kubernetes was installed successfully, False otherwise
        """
        print(f"\n=== Installing OVN-Kubernetes on cluster '{cluster_name}' ===")

        kubeconfig_path = self.kubeconfig_dir / f'{cluster_name}.yaml'
        if not kubeconfig_path.exists():
            print(f"  ✗ Kubeconfig not found: {kubeconfig_path}")
            return False

        print("\nDisabling IPv6 on all nodes for OVN-Kubernetes...")
        if not configure_ipv6_on_nodes(cluster_name):
            print("✗ Failed to configure IPv6 on some nodes")
            return False
        print("✓ Disabling of IPv6 configured on all nodes")

        print("\nPatching CoreDNS for OVN-Kubernetes...")
        if not patch_coredns_for_ovn(str(kubeconfig_path)):
            print("✗ Failed to patch CoreDNS")
            return False
        print("✓ CoreDNS patched successfully")

        print("\nGetting API server URL...")
        api_server_url = get_api_server_url(cluster_name)
        if api_server_url:
            print(f"✓ API Server URL: {api_server_url}")
        else:
            print("✗ Could not retrieve API server URL")
            return False

        print("\nPreparing OVN-Kubernetes deployment...")
        if not prepare_ovn_kubernetes_deployment(str(kubeconfig_path), api_server_url, cluster_config):
            print("✗ Failed to prepare OVN-Kubernetes deployment")
            return False
        print("✓ OVN-Kubernetes deployment prepared successfully")

        print("\nLoading OVN-Kubernetes image...")
        if not load_ovn_image(cluster_name, local_registry=cluster_config.get('local_registry', None)):
            print("✗ Failed to load OVN-Kubernetes image")
            return False
        print("✓ OVN-Kubernetes image loaded successfully")

        print("\nInstalling OVN-Kubernetes...")
        if not install_ovn_kubernetes(str(kubeconfig_path)):
            print("✗ Failed to install OVN-Kubernetes")
            return False
        print("✓ OVN-Kubernetes installed successfully")


        # Wait for OVN pods to be ready
        print("  Waiting for OVN-Kubernetes pods to be ready...")
        wait_cmd = [
            'kubectl', '--kubeconfig', str(kubeconfig_path),
            'wait', '--for=condition=Ready',
            'pod', '-l', 'app.kubernetes.io/name=ovn-kubernetes',
            '-n', 'ovn-kubernetes',
            '--timeout=300s'
        ]

        result = subprocess.run(wait_cmd, capture_output=True, text=True)
        if result.returncode != 0:
            print(f"⚠ Some OVN pods may still be initializing: {result.stderr}")
        else:
            print("✓ OVN-Kubernetes pods are ready")

        # Display pod status
        print("\n  OVN-Kubernetes Pods:")
        result = subprocess.run(
            ['kubectl', '--kubeconfig', str(kubeconfig_path),
             'get', 'pods', '-n', 'ovn-kubernetes', '-o', 'wide'],
            capture_output=True, text=True
        )
        if result.returncode == 0:
            for line in result.stdout.split('\n'):
                print(f"    {line}")

        return True

    def display_cluster_info(self, cluster_name: str) -> None:
        """Display information about a Kind cluster

        Args:
            cluster_name: Name of the cluster
        """
        print(f"\n--- Cluster '{cluster_name}' Information ---")

        nodes = get_cluster_nodes(cluster_name)
        print(f"\nNodes ({len(nodes)} total):")
        print(f"  {'NAME':<40} {'ROLE':<15} {'IP ADDRESS':<20}")
        print(f"  {'-'*40} {'-'*15} {'-'*20}")

        for node in nodes:
            ip = get_node_ip(node['name']) or 'N/A'
            print(f"  {node['name']:<40} {node['role']:<15} {ip:<20}")

        # Get kubeconfig path
        kubeconfig_path = self.kubeconfig_dir / f'{cluster_name}.yaml'
        if kubeconfig_path.exists():
            print(f"\nKubeconfig: {kubeconfig_path}")
            print(f"\nTo use this cluster:")
            print(f"  export KUBECONFIG={kubeconfig_path}")
            print(f"  kubectl get nodes")

    def deploy(self, cleanup: bool = True) -> bool:
        """Main deployment workflow

        Args:
            cleanup: Whether to cleanup existing clusters before deployment

        Returns:
            True if deployment was successful, False otherwise
        """
        print("=== Kind Cluster Deployment Starting ===\n")

        # Check prerequisites
        if not self.check_prerequisites():
            return False

        # Check if Kind config exists
        kind_config = self.config.get('kind')
        if not kind_config:
            print("✗ No 'kind' configuration found in config file")
            print("  Please add a 'kind' section to your configuration")
            return False

        # Get cluster configurations
        clusters = self.config.get('kubernetes', {}).get('clusters', [])
        if not clusters:
            print("✗ No Kubernetes clusters defined in config")
            return False

        # Cleanup if requested
        if cleanup:
            self.cleanup_all()

        # Create each cluster
        for cluster_config in clusters:
            cluster_name = cluster_config['name']
            cni = cluster_config.get('cni', 'kindnet').lower()

            print(f"Setting up cluster: {cluster_name}")
            print(f"Using CNI: {cni}")

            # Create the Kind cluster
            if not self.create_cluster(cluster_name, kind_config, cluster_config):
                print(f"✗ Failed to create cluster '{cluster_name}'")
                return False

            # Install CNI if OVN-Kubernetes
            if is_ovn_kubernetes(cni):
                if not self.deploy_ovn_kubernetes(cluster_name, cluster_config):
                    print(f"✗ Failed to install OVN-Kubernetes on '{cluster_name}'")
                    return False
            elif cni != 'kindnet':
                print(f"  Note: CNI '{cni}' will need to be installed manually")

            # Display cluster info
            self.display_cluster_info(cluster_name)

        print(f"\n{'='*60}")
        print("✓ Kind Deployment Complete!")
        print(f"{'='*60}")

        print("\nNext steps:")
        for cluster_config in clusters:
            cluster_name = cluster_config['name']
            kubeconfig_path = self.kubeconfig_dir / f'{cluster_name}.yaml'
            print(f"\n  Cluster '{cluster_name}':")
            print(f"    export KUBECONFIG={kubeconfig_path}")
            print(f"    kubectl get nodes")
            print(f"    kubectl get pods -A")

        return True

    def cleanup(self) -> None:
        """Final cleanup (placeholder for resource cleanup)"""
        pass


def main():
    parser = argparse.ArgumentParser(
        description='Deploy Kind clusters for DPU simulation',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  python3 kind_deploy.py                       # Deploy with automatic cleanup
  python3 kind_deploy.py --no-cleanup          # Deploy without cleaning up
  python3 kind_deploy.py --config kind.yaml    # Use custom config file
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
        help='Skip cleanup of existing clusters before deployment'
    )
    parser.add_argument(
        '--cleanup-only',
        action='store_true',
        help='Only cleanup existing clusters, do not deploy'
    )

    args = parser.parse_args()

    # Validate config file exists
    config_path = Path(args.config)
    if not config_path.exists():
        print(f"✗ Error: Configuration file '{args.config}' not found!")
        sys.exit(1)

    deployer = KindDeployer(config_path=args.config)

    try:
        if args.cleanup_only:
            cleanup_handler = KindCleanup(deployer.config, args.config)
            success = cleanup_handler.run(confirm=True)
            sys.exit(0 if success else 1)

        success = deployer.deploy(cleanup=not args.no_cleanup)
        sys.exit(0 if success else 1)
    except KeyboardInterrupt:
        print("\n\n✗ Deployment interrupted by user")
        sys.exit(1)
    except Exception as e:
        print(f"\n✗ Error during deployment: {e}")
        traceback.print_exc()
        sys.exit(1)
    finally:
        deployer.cleanup()


if __name__ == '__main__':
    main()

