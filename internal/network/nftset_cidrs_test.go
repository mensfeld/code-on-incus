package network

import (
	"reflect"
	"testing"
)

// Real `nft -j list set` payloads captured from nftables v1.0.9: an interval
// (static) set with a bare address + two prefixes, and a timeout (dynamic) set
// with a bare address + a timeout-wrapped element.
func TestParseNftSetElemCIDRs(t *testing.T) {
	staticJSON := []byte(`{"nftables":[{"metainfo":{}},{"set":{"family":"ip","name":"s","table":"coi","type":"ipv4_addr","flags":["interval"],"elem":["8.8.8.8",{"prefix":{"addr":"10.0.0.0","len":8}},{"prefix":{"addr":"142.250.0.0","len":15}}]}}]}`)
	dynJSON := []byte(`{"nftables":[{"set":{"name":"d","table":"coi","type":"ipv4_addr","flags":["timeout"],"elem":["172.217.112.4",{"elem":{"val":"172.217.113.4","timeout":300,"expires":299}}]}}]}`)

	gotStatic, err := parseNftSetElemCIDRs(staticJSON)
	if err != nil {
		t.Fatalf("static parse: %v", err)
	}
	wantStatic := []string{"8.8.8.8/32", "10.0.0.0/8", "142.250.0.0/15"}
	if !reflect.DeepEqual(gotStatic, wantStatic) {
		t.Errorf("static: got %v want %v", gotStatic, wantStatic)
	}

	gotDyn, err := parseNftSetElemCIDRs(dynJSON)
	if err != nil {
		t.Fatalf("dynamic parse: %v", err)
	}
	wantDyn := []string{"172.217.112.4/32", "172.217.113.4/32"}
	if !reflect.DeepEqual(gotDyn, wantDyn) {
		t.Errorf("dynamic: got %v want %v", gotDyn, wantDyn)
	}

	// Empty / no-set output must yield nothing, not an error.
	got, err := parseNftSetElemCIDRs([]byte(`{"nftables":[{"metainfo":{}}]}`))
	if err != nil || len(got) != 0 {
		t.Errorf("empty: got %v err %v", got, err)
	}
}

func TestCurrentAllowlistCIDRs_EmptyIP(t *testing.T) {
	got, err := CurrentAllowlistCIDRs("")
	if err != nil || got != nil {
		t.Errorf("empty containerIP: got %v err %v", got, err)
	}
}
