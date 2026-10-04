//go:build windows

package network

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// The NRPT (Name Resolution Policy Table) is registry state read by the DNS client: one subkey
// per rule, the same place Add-DnsClientNrptRule writes to. Writing it directly costs a few
// registry calls instead of a PowerShell launch plus a batch of cmdlets against the DNS Client
// service, which took seconds on a slow machine and had to be given a 15s budget.

// nrptRuleKeyPrefix names the registry key of our first rule; later rules append an index.
// The DNS client treats the key name as an opaque label — the GUID shape is what the
// PowerShell cmdlets and other VPNs on Windows use, so our rules read the same in
// Get-DnsClientNrptRule.
const nrptRuleKeyPrefix = "{9a3d1e4c-6b52-47ad-b0f1-2c8e5d6f7a10}"

// nrptRuleKeyName is the registry key name of rule i.
func nrptRuleKeyName(i int) string {
	if i <= 0 {
		return nrptRuleKeyPrefix
	}
	return fmt.Sprintf("%s-%d", nrptRuleKeyPrefix, i)
}

// nrptServerList is the GenericDNSServers value: the rule's resolvers, semicolon-separated.
func nrptServerList(r nrptRule) string {
	return strings.Join(r.nameServers, ";")
}

const nrptPolicyKey = `SYSTEM\CurrentControlSet\Services\Dnscache\Parameters\DnsPolicyConfig`

// nrptRuleComment tags every NRPT rule this app creates. Rules written by older builds
// through Add-DnsClientNrptRule carry the same tag, which is how they are still found for
// removal; rules owned by group policy or other software are never touched.
const nrptRuleComment = "JorbiqueVPN"

// The values a rule is made of. Version 1 with ConfigOptions "generic DNS servers" is the
// minimal shape the DNS client accepts; Comment is ours, ignored by the resolver.
const (
	nrptVersion              = 1
	nrptConfigOptionsServers = 0x8
)

// writeNrptRulesNative replaces our rule set: stale rules first (repeat connects, a crashed
// session), then the current rules, then a resolver-cache flush so nothing answers from a
// pre-NRPT cache entry.
// One rule that will not write is not a reason to leave the rest out. The PowerShell path this
// replaced ran Add-DnsClientNrptRule with -ErrorAction SilentlyContinue and added every rule it
// could; returning at the first failure would leave the machine on a partial rule set and skip
// the flush below, so the resolver would keep answering from entries cached before any of this.
// Write what can be written, flush either way, report the first failure to the caller.
func writeNrptRulesNative(rules []nrptRule) error {
	firstErr := removeNrptRulesNative()
	for i, r := range rules {
		key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, nrptPolicyKey+`\`+nrptRuleKeyName(i), registry.SET_VALUE)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		err = writeNrptRuleValues(key, r)
		key.Close()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	flushDNSCacheNative()
	return firstErr
}

func writeNrptRuleValues(key registry.Key, r nrptRule) error {
	if err := key.SetDWordValue("Version", nrptVersion); err != nil {
		return err
	}
	if err := key.SetStringsValue("Name", []string{r.namespace}); err != nil {
		return err
	}
	if err := key.SetStringValue("GenericDNSServers", nrptServerList(r)); err != nil {
		return err
	}
	if err := key.SetDWordValue("ConfigOptions", nrptConfigOptionsServers); err != nil {
		return err
	}
	return key.SetStringValue("Comment", nrptRuleComment)
}

// removeNrptRulesNative deletes every rule this app owns and flushes the cache. State-free and
// idempotent: safe after a crash where no session state was persisted.
func removeNrptRulesNative() error {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, nrptPolicyKey, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		if err == registry.ErrNotExist {
			return nil // no NRPT rules on this machine at all
		}
		return err
	}
	names, err := root.ReadSubKeyNames(-1)
	root.Close()
	if err != nil {
		return err
	}
	var firstErr error
	for _, name := range names {
		if !nrptRuleIsOurs(name) {
			continue
		}
		if err := registry.DeleteKey(registry.LOCAL_MACHINE, nrptPolicyKey+`\`+name); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	flushDNSCacheNative()
	return firstErr
}

// nrptRuleIsOurs matches the rules we write by key name, and the ones older builds wrote
// through Add-DnsClientNrptRule — those carry a generated GUID name, so they are recognised by
// their Comment.
func nrptRuleIsOurs(name string) bool {
	if strings.HasPrefix(name, nrptRuleKeyPrefix) {
		return true
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, nrptPolicyKey+`\`+name, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	comment, _, err := key.GetStringValue("Comment")
	return err == nil && comment == nrptRuleComment
}
