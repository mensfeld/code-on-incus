package network

import "testing"

const sampleFirewalldTable = `table inet firewalld {
	chain filter_FORWARD {
		iifname "veth11111111" oifname "veth22222222" jump filter_FWD_public
		iifname "veth11111111" oifname "veth22222222" reject with icmpx admin-prohibited
		iifname "vethlive0001" oifname "veth22222222" return
		oifname "vethlive0001" accept
	}
}
`

// classifyFirewalldVeths must dedupe interface names across the cross-product
// rules, split them by existence, and count rule lines — the inputs the #695
// health warning is built from.
func TestClassifyFirewalldVeths(t *testing.T) {
	exists := func(name string) bool { return name == "vethlive0001" }
	audit := classifyFirewalldVeths(sampleFirewalldTable, exists)

	if !audit.Present {
		t.Error("audit of a readable table must be Present")
	}
	if audit.LiveVeths != 1 {
		t.Errorf("live veths = %d, want 1", audit.LiveVeths)
	}
	if len(audit.DeadVeths) != 2 {
		t.Fatalf("dead veths = %v, want the two dead interfaces", audit.DeadVeths)
	}
	if audit.DeadVeths[0] != "veth11111111" || audit.DeadVeths[1] != "veth22222222" {
		t.Errorf("dead veths = %v, want sorted [veth11111111 veth22222222]", audit.DeadVeths)
	}
	if audit.RuleCount != 4 {
		t.Errorf("rule count = %d, want 4", audit.RuleCount)
	}
}

// An empty/veth-free table is Present but healthy.
func TestClassifyFirewalldVethsEmpty(t *testing.T) {
	audit := classifyFirewalldVeths("table inet firewalld {\n}\n", func(string) bool { return false })
	if !audit.Present || audit.RuleCount != 0 || len(audit.DeadVeths) != 0 {
		t.Errorf("empty table should be present and clean, got %+v", audit)
	}
}
