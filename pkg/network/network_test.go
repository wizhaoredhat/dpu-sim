package network

import (
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
		{"host-1", "dpu-1", "host-1-to-dpu-1"},
		{"master", "worker", "master-to-worker"},
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
		{"", true},                               // Empty
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
