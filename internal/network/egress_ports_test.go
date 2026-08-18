package network

import (
	"reflect"
	"testing"
)

func TestParsePortSpec(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []portRange
		wantErr bool
	}{
		{"single", "443", []portRange{{443, 443}}, false},
		{"list", "80,443", []portRange{{80, 80}, {443, 443}}, false},
		{"range", "8000-8100", []portRange{{8000, 8100}}, false},
		{"mixed sorts", "443,80,8000-8100", []portRange{{80, 80}, {443, 443}, {8000, 8100}}, false},
		{"dedupes", "443,443", []portRange{{443, 443}}, false},
		{"single-port range ok", "443-443", []portRange{{443, 443}}, false},
		{"whitespace tolerated", " 80 , 443 ", []portRange{{80, 80}, {443, 443}}, false},
		{"rejects zero", "0", nil, true},
		{"rejects over-max", "70000", nil, true},
		{"rejects inverted range", "443-80", nil, true},
		{"rejects empty element", "80,,443", nil, true},
		{"rejects non-numeric", "https", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePortSpec(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePortSpec(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePortSpec(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitDestPorts(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantDest  string
		wantPorts []portRange
		wantHas   bool
		wantErr   bool
	}{
		{"bare hostname", "github.com", "github.com", nil, false, false},
		{"hostname with port", "github.com:443", "github.com", []portRange{{443, 443}}, true, false},
		{"ip with port", "192.168.1.50:8080", "192.168.1.50", []portRange{{8080, 8080}}, true, false},
		{"cidr with port", "10.0.0.0/8:22", "10.0.0.0/8", []portRange{{22, 22}}, true, false},
		{"port list", "svc:80,443", "svc", []portRange{{80, 80}, {443, 443}}, true, false},
		{"port range", "svc:8000-8100", "svc", []portRange{{8000, 8100}}, true, false},
		{"empty dest", ":443", "", nil, false, true},
		{"bad port", "svc:0", "", nil, false, true},
		{"extra colon is a bad port", "svc:80:90", "", nil, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest, ports, has, err := splitDestPorts(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("splitDestPorts(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if dest != tt.wantDest || has != tt.wantHas || !reflect.DeepEqual(ports, tt.wantPorts) {
				t.Errorf("splitDestPorts(%q) = (%q, %v, %v), want (%q, %v, %v)",
					tt.in, dest, ports, has, tt.wantDest, tt.wantPorts, tt.wantHas)
			}
		})
	}
}

func TestResolvePorts(t *testing.T) {
	entry := []portRange{{443, 443}}
	global := []portRange{{80, 80}}
	all := allPortsRange()

	if got := resolvePorts(entry, global); !reflect.DeepEqual(got, entry) {
		t.Errorf("entry ports must win, got %v", got)
	}
	if got := resolvePorts(nil, global); !reflect.DeepEqual(got, global) {
		t.Errorf("bare entry must inherit global, got %v", got)
	}
	if got := resolvePorts(nil, nil); !reflect.DeepEqual(got, all) {
		t.Errorf("no entry + no global must be all ports, got %v", got)
	}
}

func TestPortRangeNftValueAndTupleElem(t *testing.T) {
	if got := (portRange{443, 443}).nftValue(); got != "443" {
		t.Errorf("single port nftValue = %q, want 443", got)
	}
	if got := (portRange{8000, 8100}).nftValue(); got != "8000-8100" {
		t.Errorf("range nftValue = %q, want 8000-8100", got)
	}
	if got := portTupleElem("192.168.1.50/32", portRange{443, 443}); got != "192.168.1.50/32 . 443" {
		t.Errorf("portTupleElem = %q", got)
	}
}
