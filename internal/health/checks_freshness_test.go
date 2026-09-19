package health

import (
	"strings"
	"testing"
	"time"
)

func TestParseKernelBuildDate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string // "" = expect no parse
	}{
		{
			"ubuntu",
			"Linux version 6.8.0-31-generic (buildd@lcy02-amd64-080) (x86_64-linux-gnu-gcc-13 (Ubuntu 13.2.0-23ubuntu4) 13.2.0, GNU ld (GNU Binutils for Ubuntu) 2.42) #31-Ubuntu SMP PREEMPT_DYNAMIC Sat Apr 20 00:40:06 UTC 2024",
			"2024-04-20",
		},
		{
			"fedora",
			"Linux version 6.6.9-100.fc38.x86_64 (mockbuild@...) (gcc ...) #1 SMP PREEMPT_DYNAMIC Wed Jan 3 20:11:03 UTC 2024",
			"2024-01-03",
		},
		{
			"debian iso date",
			"Linux version 6.1.0-18-amd64 (debian-kernel@lists.debian.org) (gcc-12 (Debian 12.2.0-14) 12.2.0) #1 SMP PREEMPT_DYNAMIC Debian 6.1.76-1 (2024-02-01)",
			"2024-02-01",
		},
		{
			"single digit day",
			"Linux version 6.5.0 (x@y) (gcc) #1 SMP Mon Sep 4 09:00:00 UTC 2023",
			"2023-09-04",
		},
		{
			"garbage",
			"Linux version 6.5.0 with no date at all",
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseKernelBuildDate(tt.in)
			if tt.want == "" {
				if ok {
					t.Fatalf("expected no parse, got %v", got)
				}
				return
			}
			if !ok {
				t.Fatal("expected a parse, got none")
			}
			if got.Format("2006-01-02") != tt.want {
				t.Errorf("got %s, want %s", got.Format("2006-01-02"), tt.want)
			}
		})
	}
}

func TestEvaluateKernelBuildAge(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fresh := "Linux version 6.14.0 (b@h) (gcc) #1 SMP Mon Jun 1 00:00:00 UTC 2026"
	stale := "Linux version 6.8.0 (b@h) (gcc) #1 SMP Sat Apr 20 00:40:06 UTC 2024"

	if c := evaluateKernelBuildAge(fresh, now); c.Status != StatusOK {
		t.Errorf("3-month-old kernel should be OK, got %s: %s", c.Status, c.Message)
	}
	if c := evaluateKernelBuildAge(stale, now); c.Status != StatusWarning {
		t.Errorf("2-year-old kernel should warn, got %s: %s", c.Status, c.Message)
	}
	if c := evaluateKernelBuildAge("no date here", now); c.Status != StatusOK {
		t.Errorf("unparseable /proc/version must degrade to OK, got %s", c.Status)
	}
}

const (
	osReleaseUbuntu2004 = "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"20.04\"\n"
	osReleaseUbuntu2404 = "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n"
)

func TestEvaluateDistroEOL(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	// Past EOL (Ubuntu 20.04 standard support ended 2025-05).
	if c := evaluateDistroEOL(osReleaseUbuntu2004, now); c.Status != StatusWarning ||
		!strings.Contains(c.Message, "end of standard support") {
		t.Errorf("EOL distro should warn, got %s: %s", c.Status, c.Message)
	}
	// Well within support.
	c := evaluateDistroEOL(osReleaseUbuntu2404, now)
	if c.Status != StatusOK {
		t.Errorf("supported distro should be OK, got %s: %s", c.Status, c.Message)
	}
	// oldstable note: newer_release must be the NEWEST release, chosen
	// deterministically (not a random map-order entry). For 20.04 that is 26.04,
	// and it must be identical across repeated calls.
	first := evaluateDistroEOL(osReleaseUbuntu2004, now).Details["newer_release"]
	if first != "26.04" {
		t.Errorf("newer_release = %v, want the newest release 26.04", first)
	}
	for i := 0; i < 50; i++ {
		if got := evaluateDistroEOL(osReleaseUbuntu2004, now).Details["newer_release"]; got != first {
			t.Fatalf("newer_release is nondeterministic: %v then %v", first, got)
		}
	}

	// The NEWEST release in the table must never report itself as a newer
	// release: 26.04 is the latest Ubuntu entry, so newer_release stays unset.
	if got := evaluateDistroEOL("ID=ubuntu\nVERSION_ID=\"26.04\"\n", now).Details["newer_release"]; got != nil {
		t.Errorf("newest release must not report a newer_release, got %v", got)
	}
	// Debian dates track END OF STANDARD (security-team) support, not the later
	// LTS end: Debian 12's security support ends mid-2026, so a check in 2027
	// must warn — not report "supported" off an LTS date.
	if c := evaluateDistroEOL("ID=debian\nVERSION_ID=\"12\"\n", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)); c.Status != StatusWarning {
		t.Errorf("Debian 12 in 2027 should warn (standard support ended mid-2026), got %s: %s", c.Status, c.Message)
	}
	// Approaching-EOL window: within 6 months of the cutoff.
	nearEOL := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC) // 22.04 EOL 2027-06-01
	if c := evaluateDistroEOL("ID=ubuntu\nVERSION_ID=\"22.04\"\n", nearEOL); c.Status != StatusWarning ||
		!strings.Contains(c.Message, "plan a host upgrade") {
		t.Errorf("near-EOL distro should warn, got %s: %s", c.Status, c.Message)
	}
	// Unknown distro/version degrade to OK.
	if c := evaluateDistroEOL("ID=arch\nVERSION_ID=\"rolling\"\n", now); c.Status != StatusOK {
		t.Errorf("unknown distro must degrade to OK, got %s", c.Status)
	}
	if c := evaluateDistroEOL("", now); c.Status != StatusOK {
		t.Errorf("empty os-release must degrade to OK, got %s", c.Status)
	}
}

func TestParseOSRelease(t *testing.T) {
	id, ver := parseOSRelease("PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\nVERSION_ID=\"12\"\n")
	if id != "debian" || ver != "12" {
		t.Errorf("got %q/%q, want debian/12", id, ver)
	}
}
