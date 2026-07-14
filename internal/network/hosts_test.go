package network

import (
	"strings"
	"testing"
)

func TestRenderHostsBlock(t *testing.T) {
	got := renderHostsBlock(map[string][]string{
		"oauth2.googleapis.com": {"142.251.127.95"},
		"api.anthropic.com":     {"160.79.104.10"},
	})

	want := hostsBeginMarker + "\n" +
		"160.79.104.10\tapi.anthropic.com\n" +
		"142.251.127.95\toauth2.googleapis.com\n" +
		hostsEndMarker

	if got != want {
		t.Errorf("renderHostsBlock() =\n%s\n\nwant:\n%s", got, want)
	}
}

// TestRenderHostsBlock_EmitsEveryAddress is the point of the whole design.
//
// A rotating cloud frontend answers with several addresses, and the container may
// pick any of them. Every one it could pick must be in the hosts file — and the
// same set goes into the firewall — so there is no address the container can
// reach for that the firewall has not already been given. Emitting only the first
// address would recreate the original bug: the container would still work, until
// the one address it was given went away.
func TestRenderHostsBlock_EmitsEveryAddress(t *testing.T) {
	ips := []string{
		"172.217.112.4", "172.217.113.4", "172.217.114.4", "172.217.115.4",
		"172.217.116.4", "172.217.117.4", "172.217.118.4", "172.217.119.4",
	}
	got := renderHostsBlock(map[string][]string{"us-central1-aiplatform.googleapis.com": ips})

	for _, ip := range ips {
		if !strings.Contains(got, ip+"\tus-central1-aiplatform.googleapis.com") {
			t.Errorf("every address the resolver returned must be in the hosts file; %s is missing:\n%s", ip, got)
		}
	}
}

// TestRenderHostsBlock_IsDeterministic keeps the refresher from rewriting
// /etc/hosts (and disturbing anything reading it) when nothing has actually
// changed. Go map iteration order is random, so the sort is load-bearing.
func TestRenderHostsBlock_IsDeterministic(t *testing.T) {
	in := map[string][]string{
		"b.example.com": {"2.2.2.2", "1.1.1.1"},
		"a.example.com": {"4.4.4.4", "3.3.3.3"},
	}
	first := renderHostsBlock(in)
	for i := 0; i < 20; i++ {
		if got := renderHostsBlock(in); got != first {
			t.Fatalf("renderHostsBlock is not deterministic:\n%s\nvs\n%s", first, got)
		}
	}
}

func TestRenderHostsBlock_IsDelimited(t *testing.T) {
	got := renderHostsBlock(map[string][]string{"api.anthropic.com": {"160.79.104.10"}})

	// The markers are what let the block be replaced without disturbing the rest of
	// /etc/hosts — localhost, the container's own name — which the container needs
	// to keep working.
	if !strings.HasPrefix(got, hostsBeginMarker) {
		t.Errorf("block must start with the begin marker so it can be replaced in place:\n%s", got)
	}
	if !strings.HasSuffix(got, hostsEndMarker) {
		t.Errorf("block must end with the end marker:\n%s", got)
	}
}
