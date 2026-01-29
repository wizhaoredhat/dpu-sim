package network

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateBridgeName(t *testing.T) {
	tests := []struct {
		hostName string
		dpuName  string
	}{
		{"host-1", "dpu-1"},
		{"host-2", "dpu-2"},
		{"master-1", "worker-1"},
	}

	for _, tt := range tests {
		t.Run(tt.hostName+"-"+tt.dpuName, func(t *testing.T) {
			name := GenerateBridgeName(tt.hostName, tt.dpuName)

			// Bridge name should start with 'h'
			assert.True(t, name[0] == 'h', "Bridge name should start with 'h'")

			// Bridge name should be <= 15 characters
			assert.LessOrEqual(t, len(name), 15, "Bridge name too long")

			// Same inputs should generate same name (deterministic)
			name2 := GenerateBridgeName(tt.hostName, tt.dpuName)
			assert.Equal(t, name, name2, "Bridge name should be deterministic")

			// Different inputs should generate different names
			name3 := GenerateBridgeName("different", "pair")
			assert.NotEqual(t, name, name3, "Different inputs should generate different names")
		})
	}
}

func TestGetHostToDPUNetworkName(t *testing.T) {
	tests := []struct {
		hostName string
		dpuName  string
		expected string
	}{
		{"host-1", "dpu-1", "h2d-host-1-dpu-1"},
		{"master", "worker", "h2d-master-worker"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			name := GetHostToDPUNetworkName(tt.hostName, tt.dpuName)
			assert.Equal(t, tt.expected, name)
		})
	}
}

func TestSanitizeBridgeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"valid-name", "valid-name"},
		{"with_underscore", "with_underscore"},
		{"with spaces", "with-spaces"},
		{"with@special#chars", "with-special-ch"},
		{"toolongnamemorethan15chars", "toolongnamemore"},
		{"trailing-dash-", "trailing-dash"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SanitizeBridgeName(tt.input)
			assert.Equal(t, tt.expected, result)

			// Verify result is valid
			err := ValidateBridgeName(result)
			assert.NoError(t, err, "Sanitized name should be valid")
		})
	}
}

func TestValidateBridgeName(t *testing.T) {
	tests := []struct {
		name        string
		expectError bool
	}{
		{"valid-name", false},
		{"valid_name", false},
		{"ValidName123", false},
		{"", true},                                // Empty
		{"toolongnamemorethan15characters", true}, // Too long
		{"invalid name", true},                    // Contains space
		{"invalid@name", true},                    // Contains @
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBridgeName(tt.name)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestInterfaceInfoParsing(t *testing.T) {
	// Sample JSON output from 'ip -j addr show'
	sampleJSON := `[
		{
			"ifindex": 1,
			"ifname": "lo",
			"flags": ["LOOPBACK", "UP", "LOWER_UP"],
			"mtu": 65536,
			"qdisc": "noqueue",
			"operstate": "UNKNOWN",
			"group": "default",
			"txqlen": 1000,
			"link_type": "loopback",
			"address": "00:00:00:00:00:00",
			"broadcast": "00:00:00:00:00:00",
			"addr_info": [
				{
					"family": "inet",
					"local": "127.0.0.1",
					"prefixlen": 8,
					"scope": "host",
					"label": "lo"
				},
				{
					"family": "inet6",
					"local": "::1",
					"prefixlen": 128,
					"scope": "host"
				}
			]
		},
		{
			"ifindex": 2,
			"ifname": "eth0",
			"flags": ["BROADCAST", "MULTICAST", "UP", "LOWER_UP"],
			"mtu": 1500,
			"qdisc": "fq_codel",
			"operstate": "UP",
			"group": "default",
			"txqlen": 1000,
			"link_type": "ether",
			"address": "52:54:00:12:34:56",
			"broadcast": "ff:ff:ff:ff:ff:ff",
			"addr_info": [
				{
					"family": "inet",
					"local": "192.168.1.100",
					"prefixlen": 24,
					"broadcast": "192.168.1.255",
					"scope": "global",
					"label": "eth0"
				}
			]
		}
	]`

	var interfaces []InterfaceInfo
	err := json.Unmarshal([]byte(sampleJSON), &interfaces)
	assert.NoError(t, err)
	assert.Len(t, interfaces, 2)

	// Test loopback interface
	lo := interfaces[0]
	assert.Equal(t, "lo", lo.Name)
	assert.Equal(t, 1, lo.Index)
	assert.Equal(t, 65536, lo.MTU)
	assert.Equal(t, "UNKNOWN", lo.State)
	assert.Equal(t, "00:00:00:00:00:00", lo.MAC)
	assert.Equal(t, "loopback", lo.LinkType)
	assert.Contains(t, lo.Flags, "LOOPBACK")
	assert.Len(t, lo.Addresses, 2)
	assert.Equal(t, "127.0.0.1", lo.Addresses[0].Local)
	assert.Equal(t, 8, lo.Addresses[0].Prefixlen)
	assert.Equal(t, "inet", lo.Addresses[0].Family)

	// Test eth0 interface
	eth0 := interfaces[1]
	assert.Equal(t, "eth0", eth0.Name)
	assert.Equal(t, 2, eth0.Index)
	assert.Equal(t, 1500, eth0.MTU)
	assert.Equal(t, "UP", eth0.State)
	assert.Equal(t, "52:54:00:12:34:56", eth0.MAC)
	assert.Equal(t, "ether", eth0.LinkType)
	assert.Contains(t, eth0.Flags, "BROADCAST")
	assert.Len(t, eth0.Addresses, 1)
	assert.Equal(t, "192.168.1.100", eth0.Addresses[0].Local)
	assert.Equal(t, 24, eth0.Addresses[0].Prefixlen)
	assert.Equal(t, "global", eth0.Addresses[0].Scope)
}

func TestInterfaceInfoString(t *testing.T) {
	iface := InterfaceInfo{
		Name:     "eth0",
		Index:    2,
		MTU:      1500,
		State:    "UP",
		MAC:      "52:54:00:12:34:56",
		LinkType: "ether",
		Flags:    []string{"BROADCAST", "MULTICAST", "UP"},
		Addresses: []IPAddr{
			{
				Family:    "inet",
				Local:     "192.168.1.100",
				Prefixlen: 24,
				Scope:     "global",
			},
		},
	}

	str := iface.String()
	assert.Contains(t, str, "eth0")
	assert.Contains(t, str, "UP")
	assert.Contains(t, str, "52:54:00:12:34:56")
	assert.Contains(t, str, "1500")
	assert.Contains(t, str, "192.168.1.100/24")
}

func TestFindInterfaceByIP(t *testing.T) {
	// Sample JSON output from 'ip -j addr show'
	sampleJSON := `[
		{
			"ifindex": 1,
			"ifname": "lo",
			"flags": ["LOOPBACK"],
			"mtu": 65536,
			"operstate": "UNKNOWN",
			"link_type": "loopback",
			"address": "00:00:00:00:00:00",
			"broadcast": "00:00:00:00:00:00",
			"addr_info": [
				{"family": "inet", "local": "127.0.0.1", "prefixlen": 8, "scope": "host"}
			]
		},
		{
			"ifindex": 2,
			"ifname": "eth0",
			"flags": ["BROADCAST", "UP"],
			"mtu": 1500,
			"operstate": "UP",
			"link_type": "ether",
			"address": "52:54:00:12:34:56",
			"broadcast": "ff:ff:ff:ff:ff:ff",
			"addr_info": [
				{"family": "inet", "local": "192.168.1.100", "prefixlen": 24, "scope": "global"}
			]
		},
		{
			"ifindex": 3,
			"ifname": "eth1",
			"flags": ["BROADCAST", "UP"],
			"mtu": 1500,
			"operstate": "UP",
			"link_type": "ether",
			"address": "52:54:00:78:9a:bc",
			"broadcast": "ff:ff:ff:ff:ff:ff",
			"addr_info": [
				{"family": "inet", "local": "10.0.0.50", "prefixlen": 16, "scope": "global"}
			]
		}
	]`

	var interfaces []InterfaceInfo
	err := json.Unmarshal([]byte(sampleJSON), &interfaces)
	assert.NoError(t, err)

	// Helper function to simulate finding interface by IP
	findByIP := func(searchIP string) *InterfaceInfo {
		for _, iface := range interfaces {
			for _, addr := range iface.Addresses {
				if addr.Local == searchIP {
					iface.TargetIP = searchIP
					return &iface
				}
			}
		}
		return nil
	}

	// Test finding eth0 by IP
	found := findByIP("192.168.1.100")
	assert.NotNil(t, found)
	assert.Equal(t, "eth0", found.Name)
	assert.Equal(t, "192.168.1.100", found.TargetIP)

	// Test finding eth1 by IP
	found = findByIP("10.0.0.50")
	assert.NotNil(t, found)
	assert.Equal(t, "eth1", found.Name)

	// Test finding loopback
	found = findByIP("127.0.0.1")
	assert.NotNil(t, found)
	assert.Equal(t, "lo", found.Name)

	// Test IP not found
	found = findByIP("10.10.10.10")
	assert.Nil(t, found)
}
