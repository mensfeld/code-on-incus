package cli

import (
	"strings"
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "-"},
		{-time.Second, "-"},
		{500 * time.Millisecond, "<1s"},
		{30 * time.Second, "30s"},
		{2 * time.Minute, "2m0s"},
		{2*time.Minute + 15*time.Second, "2m15s"},
		{4*time.Hour + 7*time.Minute, "4h7m"},
		{50*time.Hour + 12*time.Minute, "2d2h"},
	}
	for _, c := range cases {
		got := formatUptime(c.d)
		if got != c.want {
			t.Errorf("formatUptime(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestShortenID(t *testing.T) {
	cases := map[string]string{
		"":                                     "-",
		"abc":                                  "abc",
		"123456789012":                         "123456789012",
		"1234567890123":                        "123456789012",
		"abcdefghijklmnopqrstuvwxyz1234567890": "abcdefghijkl",
	}
	for in, want := range cases {
		if got := shortenID(in); got != want {
			t.Errorf("shortenID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortenPath(t *testing.T) {
	if got := shortenPath("", 16); got != "-" {
		t.Errorf("empty: got %q want -", got)
	}
	if got := shortenPath("/a/b", 16); got != "/a/b" {
		t.Errorf("short: got %q want /a/b", got)
	}
	in := "/home/alice/src/very/deep/project/path"
	got := shortenPath(in, 16)
	if len(got) != 16 {
		t.Errorf("len(%q) = %d, want 16", got, len(got))
	}
	if !strings.HasPrefix(got, "...") {
		t.Errorf("expected leading ellipsis: %q", got)
	}
	if !strings.HasSuffix(got, "project/path") {
		t.Errorf("expected trailing %q in %q", "project/path", got)
	}
}

func TestRenderOverviewEmpty(t *testing.T) {
	h := overviewHeader{Version: "0.7.0", Sessions: 0, Containers: 0}
	at := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	out := renderOverviewToString(h, nil, at, false)

	want := []string{
		"coi overview",
		"v0.7.0",
		"sessions=0",
		"running=0",
		"2026-05-05 14:30:00",
		"(no running sessions)",
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("expected %q in:\n%s", w, out)
		}
	}
	// No footer in non-interactive mode.
	if strings.Contains(out, "[Ctrl+C]") {
		t.Errorf("did not expect footer in non-interactive render:\n%s", out)
	}
}

func TestRenderOverviewRows(t *testing.T) {
	at := time.Date(2026, 5, 5, 14, 30, 0, 0, time.UTC)
	rows := []overviewSession{
		{
			ID:        "abcdef0123456789",
			Container: "coi-aaa-1",
			Workspace: "/home/alice/projects/redbot",
			Mode:      "persistent",
			Started:   at.Add(-3 * time.Hour),
			Uptime:    3 * time.Hour,
		},
		{
			ID:        "",
			Container: "coi-orphan-2",
			Workspace: "",
			Mode:      "",
			Started:   time.Time{},
			Uptime:    0,
		},
	}
	h := overviewHeader{Version: "0.7.0", Sessions: 1, Containers: 2}
	out := renderOverviewToString(h, rows, at, true)

	wantContains := []string{
		"SESSION", "CONTAINER", "WORKSPACE", "MODE", "STARTED", "UPTIME",
		"abcdef012345", // truncated session id
		"coi-aaa-1",
		"redbot", // tail of workspace path survives shortenPath
		"persistent",
		"3h0m",
		"coi-orphan-2",
		"-",             // placeholder for empty fields on orphan row
		"[Ctrl+C] quit", // footer present in interactive render
		"sessions=1",
		"running=2",
	}
	for _, w := range wantContains {
		if !strings.Contains(out, w) {
			t.Errorf("expected %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "abcdef0123456789") {
		t.Errorf("session id should be truncated, got full id in:\n%s", out)
	}
}

func TestCollectOverviewMaps(t *testing.T) {
	// Pure unit test of the row-shaping logic: we can't reach incus from
	// CI, so just verify formatStarted's zero-time handling here.
	if got := formatStarted(time.Time{}); got != "-" {
		t.Errorf("formatStarted(zero) = %q, want -", got)
	}
	now := time.Now()
	if got := formatStarted(now); got == "-" {
		t.Errorf("formatStarted(now) returned placeholder")
	}
}
