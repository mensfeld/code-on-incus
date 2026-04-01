package cli

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b     string
		expected int
	}{
		// Equal versions
		{"0.7.0", "0.7.0", 0},
		{"1.0.0", "1.0.0", 0},

		// a < b
		{"0.7.0", "0.8.0", -1},
		{"0.7.0", "1.0.0", -1},
		{"0.7.9", "0.8.0", -1},
		{"0.9.0", "0.10.0", -1},
		{"1.2.3", "1.2.4", -1},

		// a > b
		{"0.8.0", "0.7.0", 1},
		{"1.0.0", "0.7.0", 1},
		{"0.10.0", "0.9.0", 1},
		{"1.2.4", "1.2.3", 1},

		// Different lengths
		{"0.7", "0.7.0", 0},
		{"0.7", "0.7.1", -1},
		{"0.7.1", "0.7", 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := compareVersions(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello world\n")
	// SHA256 of "hello world\n"
	checksumFile := []byte("a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447  test-binary\n")

	t.Run("valid checksum", func(t *testing.T) {
		err := verifyChecksum(data, checksumFile, "test-binary")
		if err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("wrong binary name", func(t *testing.T) {
		err := verifyChecksum(data, checksumFile, "wrong-name")
		if err == nil {
			t.Error("expected error for wrong binary name")
		}
	})

	t.Run("tampered data", func(t *testing.T) {
		tamperedData := []byte("tampered\n")
		err := verifyChecksum(tamperedData, checksumFile, "test-binary")
		if err == nil {
			t.Error("expected error for tampered data")
		}
	})

	t.Run("single space separator", func(t *testing.T) {
		checksumSingleSpace := []byte("a948904f2f0f479b8f8197694b30184b0d2ed1c1cd2a1ec0fb85d299a192a447 test-binary\n")
		err := verifyChecksum(data, checksumSingleSpace, "test-binary")
		if err != nil {
			t.Errorf("expected no error with single space separator, got: %v", err)
		}
	})

	t.Run("empty checksums file", func(t *testing.T) {
		err := verifyChecksum(data, []byte(""), "test-binary")
		if err == nil {
			t.Error("expected error for empty checksums file")
		}
	})
}
