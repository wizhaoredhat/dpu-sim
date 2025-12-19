package cni

import (
	"github.com/wizhao/dpu-sim/pkg/config"
	"github.com/wizhao/dpu-sim/pkg/ssh"
)

// CNIManager manages CNI installations
type CNIManager struct {
	config    *config.Config
	sshClient *ssh.Client
}

// CNIType represents the type of CNI
type CNIType string

const (
	CNIFlannel       CNIType = "flannel"
	CNIOVNKubernetes CNIType = "ovn-kubernetes"
	CNIMultus        CNIType = "multus"
	CNIKindnet       CNIType = "kindnet"
)

// NewCNIManager creates a new CNI manager
func NewCNIManager(cfg *config.Config) *CNIManager {
	return &CNIManager{
		config:    cfg,
		sshClient: ssh.NewClient(&cfg.SSH),
	}
}
