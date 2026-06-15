package network

import (
	"reflect"
	"testing"
)

func TestParseCOIIP6RuleContainers(t *testing.T) {
	output := `table ip6 coi {
	chain forward {
		type filter hook forward priority 10; policy accept;
		ct state established,related accept comment "coi-base" # handle 4
		iifname "veth0" drop comment "coi6-coi-abc12345-1" # handle 5
		iifname "veth1" drop comment "coi6-coi-def67890-2" # handle 6
		iifname "veth0" drop comment "coi6-coi-abc12345-1" # handle 7
	}
}`
	got := parseCOIIP6RuleContainers(output)
	want := []string{"coi-abc12345-1", "coi-def67890-2"} // deduped, order-preserved
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseCOIIP6RuleContainers() = %v, want %v", got, want)
	}
}

func TestParseCOIIP6RuleContainers_None(t *testing.T) {
	// Output with no coi6- comments yields no names (not nil-vs-empty sensitive).
	if got := parseCOIIP6RuleContainers("chain forward { ct state established accept }"); len(got) != 0 {
		t.Errorf("expected no names, got %v", got)
	}
	if got := parseCOIIP6RuleContainers(""); len(got) != 0 {
		t.Errorf("expected no names for empty input, got %v", got)
	}
}
