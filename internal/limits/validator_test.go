package limits

import "testing"

// TestValidateDiskIO is the format matrix for disk I/O limit values. This is
// the authoritative coverage for the syntax; the e2e test
// (tests/limits/test_limits_validation.py) boots ONE container with a
// generous limit to prove end-to-end application, because booting under a
// pathological throttle (e.g. 100kB/s reads) only succeeds when the host page
// cache absorbs the boot I/O — a CI flake, not a validation check.
func TestValidateDiskIO(t *testing.T) {
	valid := []string{
		"", // empty = unlimited
		"10MB",
		"100kB", // SI kilo is lowercase k
		"1GB",
		"1TB",
		"10MiB",
		"100KiB", // IEC kibi is uppercase K
		"1GiB",
		"1TiB",
		"1000iops",
		"1iops",
	}
	for _, io := range valid {
		if err := ValidateDiskIO(io); err != nil {
			t.Errorf("ValidateDiskIO(%q) should be valid, got: %v", io, err)
		}
	}

	invalid := []string{
		"fast",
		"10MB/s", // the /s suffix is documentation-only, not config syntax
		"1000",   // bare number: ambiguous, must carry a unit or iops
		"abc",
		"100KB",  // SI kilo must be lowercase k
		"100kiB", // IEC kibi must be uppercase K
		"10mb",
		"iops",
		"10 MB",
		"-10MB",
		"10.5MB",
	}
	for _, io := range invalid {
		if err := ValidateDiskIO(io); err == nil {
			t.Errorf("ValidateDiskIO(%q) should be rejected", io)
		}
	}
}

func TestValidateAllReportsDiskFields(t *testing.T) {
	errs := ValidateAll(
		CPULimits{},
		MemoryLimits{},
		DiskLimits{Read: "fast", Write: "10MB/s", Max: "1000"},
		RuntimeLimits{},
	)
	for _, field := range []string{"disk.read", "disk.write", "disk.max"} {
		if errs[field] == nil {
			t.Errorf("ValidateAll should report %s as invalid", field)
		}
	}
	if errs["disk.priority"] != nil {
		t.Errorf("ValidateAll should accept default disk priority, got: %v", errs["disk.priority"])
	}
}
