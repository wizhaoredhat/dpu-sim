package cni

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wizhao/dpu-sim/pkg/config"
)

func TestResolveAddonInstallOrderInjectsWhereaboutsBeforeMultus(t *testing.T) {
	addons := []config.AddonType{config.AddonMultus, config.AddonCertManager}
	ordered := resolveAddonInstallOrder(addons)
	require.Equal(t, []config.AddonType{config.AddonWhereabouts, config.AddonMultus, config.AddonCertManager}, ordered)
}

func TestResolveAddonInstallOrderDoesNotDuplicateWhereabouts(t *testing.T) {
	addons := []config.AddonType{config.AddonWhereabouts, config.AddonMultus, config.AddonCertManager}
	ordered := resolveAddonInstallOrder(addons)
	require.Equal(t, addons, ordered)
}
