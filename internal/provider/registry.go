package provider

import (
	"fmt"
	"sort"
)

type IndexReport struct {
	GeneratedAt    string       `json:"generated_at"`
	DatapanVersion string       `json:"datapan_version,omitempty"`
	AdapterCount   int          `json:"adapter_count"`
	HostCount      int          `json:"host_count"`
	Adapters       []IndexEntry `json:"adapters"`
}

type IndexEntry struct {
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	Hosts        []string `json:"hosts"`
	Capabilities []string `json:"capabilities"`
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
			Capabilities: []string{"verification"},
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
	}
}
