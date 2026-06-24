package provider

import (
	"fmt"
	"sort"
)

type IndexReport struct {
	GeneratedAt    string         `json:"generated_at"`
	DatapanVersion string         `json:"datapan_version,omitempty"`
	AdapterCount   int            `json:"adapter_count"`
	HostCount      int            `json:"host_count"`
	Adapters       []IndexEntry   `json:"adapters"`
	SplitReadiness SplitReadiness `json:"split_readiness"`
}

type IndexEntry struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Hosts        []string `json:"hosts"`
	Capabilities []string `json:"capabilities"`
}

type SplitReadiness struct {
	Ready                        bool     `json:"ready"`
	Status                       string   `json:"status"`
	AdapterCount                 int      `json:"adapter_count"`
	VerificationCapableAdapters  int      `json:"verification_capable_adapters"`
	CallCapableAdapters          int      `json:"call_capable_adapters"`
	RequiredAdapters             int      `json:"required_adapters"`
	RequiredVerificationAdapters int      `json:"required_verification_adapters"`
	RequiredCallAdapters         int      `json:"required_call_adapters"`
	Reasons                      []string `json:"reasons"`
	Recommendation               string   `json:"recommendation"`
}

type Registry struct {
	adapters []Adapter
	hosts    map[string]string
}

func NewRegistry(adapters ...Adapter) (Registry, error) {
	reg := Registry{hosts: map[string]string{}}
	for _, adapter := range adapters {
		if err := reg.Register(adapter); err != nil {
			return Registry{}, err
		}
	}
	return reg, nil
}

func (r *Registry) Register(adapter Adapter) error {
	if adapter == nil {
		return fmt.Errorf("provider adapter is nil")
	}
	name := normalizeHost(adapter.Name())
	if name == "" {
		return fmt.Errorf("provider adapter name is empty")
	}
	if r.hosts == nil {
		r.hosts = map[string]string{}
	}
	for _, host := range adapter.Hosts() {
		host = normalizeHost(host)
		if host == "" {
			continue
		}
		if owner := r.hosts[host]; owner != "" && owner != name {
			return fmt.Errorf("provider host %q already registered by %s", host, owner)
		}
		r.hosts[host] = name
	}
	r.adapters = append(r.adapters, adapter)
	return nil
}

func (r Registry) MatchHost(host string) (Adapter, bool) {
	host = normalizeHost(host)
	if host == "" {
		return nil, false
	}
	for _, adapter := range r.adapters {
		if adapter.MatchHost(host) {
			return adapter, true
		}
	}
	return nil, false
}

func (r Registry) Adapters() []Adapter {
	out := make([]Adapter, len(r.adapters))
	copy(out, r.adapters)
	return out
}

func (r Registry) Hosts() []string {
	hosts := make([]string, 0, len(r.hosts))
	for host := range r.hosts {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func (r Registry) IndexReport(generatedAt, datapanVersion string) IndexReport {
	entries := make([]IndexEntry, 0, len(r.adapters))
	for _, adapter := range r.adapters {
		hosts := append([]string(nil), adapter.Hosts()...)
		for idx := range hosts {
			hosts[idx] = normalizeHost(hosts[idx])
		}
		sort.Strings(hosts)
		entries = append(entries, IndexEntry{
			Name:         normalizeHost(adapter.Name()),
			Status:       "registered",
			Hosts:        hosts,
			Capabilities: adapterCapabilities(adapter),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})
	return IndexReport{
		GeneratedAt:    generatedAt,
		DatapanVersion: datapanVersion,
		AdapterCount:   len(entries),
		HostCount:      len(r.Hosts()),
		Adapters:       entries,
		SplitReadiness: splitReadiness(entries),
	}
}

func adapterCapabilities(adapter Adapter) []string {
	capabilities := []string{"verification"}
	if reporter, ok := adapter.(CapabilityReporter); ok {
		capabilities = append(capabilities, reporter.Capabilities()...)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		capability = normalizeHost(capability)
		if capability == "" || seen[capability] {
			continue
		}
		seen[capability] = true
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

func splitReadiness(entries []IndexEntry) SplitReadiness {
	const requiredAdapters = 2
	const requiredVerificationAdapters = 2
	const requiredCallAdapters = 1
	verificationCapable := 0
	callCapable := 0
	for _, entry := range entries {
		if hasCapability(entry.Capabilities, "verification") {
			verificationCapable++
		}
		if hasCapability(entry.Capabilities, "call") {
			callCapable++
		}
	}
	reasons := []string{}
	if len(entries) < requiredAdapters {
		reasons = append(reasons, "need_at_least_two_external_adapters")
	}
	if verificationCapable < requiredVerificationAdapters {
		reasons = append(reasons, "need_at_least_two_verification_capable_adapters")
	}
	if callCapable < requiredCallAdapters {
		reasons = append(reasons, "need_at_least_one_call_capable_adapter")
	}
	ready := len(reasons) == 0
	status := "not_ready"
	recommendation := "keep provider adapters inside datapan-cli until call behavior is proven by at least one external adapter"
	if ready {
		status = "ready"
		recommendation = "adapter boundary has enough exercised surface to consider a datapan-providers split"
	}
	return SplitReadiness{
		Ready:                        ready,
		Status:                       status,
		AdapterCount:                 len(entries),
		VerificationCapableAdapters:  verificationCapable,
		CallCapableAdapters:          callCapable,
		RequiredAdapters:             requiredAdapters,
		RequiredVerificationAdapters: requiredVerificationAdapters,
		RequiredCallAdapters:         requiredCallAdapters,
		Reasons:                      reasons,
		Recommendation:               recommendation,
	}
}

func hasCapability(capabilities []string, want string) bool {
	want = normalizeHost(want)
	for _, capability := range capabilities {
		if normalizeHost(capability) == want {
			return true
		}
	}
	return false
}
