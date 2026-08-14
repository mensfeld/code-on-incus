package config

import (
	"reflect"
	"testing"
)

// mergeNetworkInto must carry the new egress-policy keys, so a trusted config or
// profile can set dns_servers / allowed_ports and have them survive the merge.
func TestMergeNetworkInto_CarriesEgressKeys(t *testing.T) {
	base := GetDefaultConfig()
	base.Merge(&Config{Network: NetworkConfig{
		Mode:         NetworkModeRestricted,
		DNSServers:   []string{"192.168.1.2"},
		AllowedPorts: []int{80, 443},
	}})

	if !reflect.DeepEqual(base.Network.DNSServers, []string{"192.168.1.2"}) {
		t.Errorf("dns_servers not merged, got %v", base.Network.DNSServers)
	}
	if !reflect.DeepEqual(base.Network.AllowedPorts, []int{80, 443}) {
		t.Errorf("allowed_ports not merged, got %v", base.Network.AllowedPorts)
	}
}

// A later merge with the keys unset must not clobber values an earlier layer set
// (nil source = "no opinion"), matching how the other network slices behave.
func TestMergeNetworkInto_UnsetDoesNotClobber(t *testing.T) {
	base := GetDefaultConfig()
	base.Network.DNSServers = []string{"192.168.1.2"}
	base.Network.AllowedPorts = []int{443}

	base.Merge(&Config{Network: NetworkConfig{Mode: NetworkModeRestricted}})

	if !reflect.DeepEqual(base.Network.DNSServers, []string{"192.168.1.2"}) {
		t.Errorf("dns_servers clobbered by unset merge, got %v", base.Network.DNSServers)
	}
	if !reflect.DeepEqual(base.Network.AllowedPorts, []int{443}) {
		t.Errorf("allowed_ports clobbered by unset merge, got %v", base.Network.AllowedPorts)
	}
}
