package session

import "testing"

func TestDecideUIDMapping(t *testing.T) {
	const code = 1000
	cases := []struct {
		name         string
		hostUID      int
		disableShift bool
		colima       bool
		wantShift    bool
		wantIdmap    string
	}{
		{"match, shift allowed → shift", 1000, false, false, true, ""},
		{"match, colima (host handles mapping) → no shift, no idmap", 1000, false, true, false, ""},

		// The issue #530 regression: a UID mismatch must set raw.idmap and turn
		// shift off — INCLUDING under Colima/Lima and explicit disable_shift,
		// which the old code gated out.
		{"mismatch 501 (macOS), plain → idmap", 501, false, false, false, "both 501 1000"},
		{"mismatch 1001 (CI runner), plain → idmap", 1001, false, false, false, "both 1001 1000"},
		{"mismatch 501, colima → idmap (#530)", 501, false, true, false, "both 501 1000"},
		{"mismatch 501, disable_shift → idmap (#530)", 501, true, false, false, "both 501 1000"},

		// The issue #667 gap: hostUID == codeUID with a manually configured
		// disable_shift (e.g. OrbStack, where shift is unsupported per #553)
		// still needs raw.idmap — the container's own default subuid range
		// doesn't cover hostUID just because the nominal code UID matches it.
		// This must NOT apply when hostHandlesUIDMapping is true (real
		// Colima/Lima, covered by the case above) — that guest already maps
		// the UID itself.
		{"match, manual disable_shift (no host mapping) → idmap (#667)", 1000, true, false, false, "both 1000 1000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shift, idmap := decideUIDMapping(c.hostUID, code, c.disableShift, c.colima)
			if shift != c.wantShift {
				t.Errorf("shift: got %v want %v", shift, c.wantShift)
			}
			if idmap != c.wantIdmap {
				t.Errorf("idmap: got %q want %q", idmap, c.wantIdmap)
			}
			// raw.idmap and shift are mutually exclusive.
			if idmap != "" && shift {
				t.Error("raw.idmap must never be combined with shift=true")
			}
		})
	}
}
