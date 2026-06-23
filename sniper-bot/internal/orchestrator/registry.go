package orchestrator

import "sort"

// StageEntry is a registered stage with its routing configuration.
type StageEntry struct {
	Group      string
	EventTypes []string
	Handler    StageHandler
}

// Registry holds all registered stage handlers indexed by worker group name.
// Each group handles one or more event types.
type Registry struct {
	entries map[string]*StageEntry
}

// NewRegistry creates an empty stage registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]*StageEntry)}
}

// Register adds a stage handler for a worker group.
// eventTypes is the list of event types this handler consumes.
// Panics if the same group is registered twice (programming error).
func (r *Registry) Register(group string, handler StageHandler, eventTypes ...string) {
	if _, exists := r.entries[group]; exists {
		panic("orchestrator: duplicate stage registration for group: " + group)
	}
	r.entries[group] = &StageEntry{
		Group:      group,
		EventTypes: eventTypes,
		Handler:    handler,
	}
}

// Entries returns all registered stage entries in deterministic (sorted) order.
func (r *Registry) Entries() []*StageEntry {
	keys := make([]string, 0, len(r.entries))
	for k := range r.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic ordering

	result := make([]*StageEntry, 0, len(keys))
	for _, k := range keys {
		result = append(result, r.entries[k])
	}
	return result
}

// Empty returns true if no stages are registered.
func (r *Registry) Empty() bool {
	return len(r.entries) == 0
}

// Len returns the number of registered stages.
func (r *Registry) Len() int {
	return len(r.entries)
}
