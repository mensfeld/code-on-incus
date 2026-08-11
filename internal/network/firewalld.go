package network

import (
	"os"
	"regexp"
	"sort"
	"strings"
)

// vethNameRe matches veth interface names as they appear in firewalld's
// generated ruleset (iifname "veth3108f3f4" ...).
var vethNameRe = regexp.MustCompile(`"(veth[0-9a-zA-Z_.-]+)"`)

// FirewalldVethAudit reports on veth interfaces referenced by firewalld's
// generated nft table. NetworkManager enrolls each container's host-side veth
// into firewalld's default zone; when the container is deleted the veth
// vanishes but the zone registration can leak, and firewalld generates its
// FORWARD policy rules as the CROSS PRODUCT of zone interfaces — dead veths
// therefore grow the ruleset quadratically (145 leaked veths ≈ 101k rules on
// the #695 reporter's host) even with few containers running.
type FirewalldVethAudit struct {
	Present    bool     // firewalld's nft table exists and was readable
	Unreadable bool     // the table could not be read (sudo/nft unavailable, timeout) — NOT proof of absence
	RuleCount  int      // entries (body lines) in table inet firewalld — includes set elements, so approximate
	DeadVeths  []string // veths referenced by the table that no longer exist
	LiveVeths  int      // referenced veths that still exist
}

// AuditFirewalldVeths inspects `table inet firewalld` (via the same sudo nft
// path every other rule operation uses) and classifies each referenced veth by
// whether the interface still exists. A genuinely missing table yields the
// zero value (nothing to audit); any other failure to read (sudo disabled,
// nft missing, timeout) yields Unreadable=true so the caller can say
// "couldn't look" instead of asserting firewalld is absent.
func AuditFirewalldVeths() FirewalldVethAudit {
	out, err := runNFTCommand("list", "table", "inet", "firewalld")
	if err != nil {
		// Distinguish "table genuinely absent" from "could not look": the
		// pathological 100k-rule table this audit exists for produces the
		// slowest listing, so conflating a timeout (or disabled sudo) with
		// absence would report the worst-affected hosts as healthy with an
		// affirmative "no firewalld" message.
		if strings.Contains(err.Error(), "No such file or directory") {
			return FirewalldVethAudit{}
		}
		return FirewalldVethAudit{Unreadable: true}
	}
	return classifyFirewalldVeths(string(out), func(name string) bool {
		_, statErr := os.Stat("/sys/class/net/" + name)
		return statErr == nil
	})
}

// classifyFirewalldVeths is the pure half of AuditFirewalldVeths: it extracts
// the unique veth names from a firewalld table listing and splits them by the
// given existence predicate, counting rule lines along the way.
func classifyFirewalldVeths(ruleset string, exists func(string) bool) FirewalldVethAudit {
	audit := FirewalldVethAudit{Present: true}
	seen := map[string]bool{}
	for _, m := range vethNameRe.FindAllStringSubmatch(ruleset, -1) {
		seen[m[1]] = true
	}
	for name := range seen {
		if exists(name) {
			audit.LiveVeths++
		} else {
			audit.DeadVeths = append(audit.DeadVeths, name)
		}
	}
	sort.Strings(audit.DeadVeths)
	for _, line := range strings.Split(ruleset, "\n") {
		if len(line) > 2 && line[0] == '\t' && line[1] == '\t' {
			audit.RuleCount++
		}
	}
	return audit
}
