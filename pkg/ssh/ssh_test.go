package ssh

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wizhao/dpu-sim/pkg/config"
)

func TestNewClient(t *testing.T) {
	cfg := &config.SSHConfig{
		User:     "root",
		KeyPath:  "/home/user/.ssh/id_rsa",
		Password: "password",
	}

	client := NewClient(cfg)
	assert.NotNil(t, client)
	assert.Equal(t, cfg, client.config)
}

func TestBuildSSHCommand(t *testing.T) {
	cfg := &config.SSHConfig{
		User:    "root",
		KeyPath: "/home/user/.ssh/id_rsa",
	}

	tests := []struct {
		name     string
		ip       string
		command  string
		expected []string
	}{
		{
			name:    "Without command (interactive)",
			ip:      "192.168.1.100",
			command: "",
			expected: []string{
				"ssh",
				"-i", "/home/user/.ssh/id_rsa",
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "LogLevel=ERROR",
				"-o", "ConnectTimeout=5",
				"root@192.168.1.100",
			},
		},
		{
			name:    "With command",
			ip:      "192.168.1.100",
			command: "ls -la",
			expected: []string{
				"ssh",
				"-i", "/home/user/.ssh/id_rsa",
				"-o", "StrictHostKeyChecking=no",
				"-o", "UserKnownHostsFile=/dev/null",
				"-o", "LogLevel=ERROR",
				"-o", "ConnectTimeout=5",
				"root@192.168.1.100",
				"ls -la",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := BuildSSHCommand(cfg, tt.ip, tt.command)
			assert.Equal(t, tt.expected, cmd)
		})
	}
}
