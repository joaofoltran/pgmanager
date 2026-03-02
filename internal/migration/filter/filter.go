package filter

import (
	"strings"
)

// Config defines table/schema filtering rules for migration.
type Config struct {
	IncludeSchemas []string
	ExcludeSchemas []string
	IncludeTables  []string
	ExcludeTables  []string
}

// IsEmpty returns true if no filter rules are configured.
func (c Config) IsEmpty() bool {
	return len(c.IncludeSchemas) == 0 &&
		len(c.ExcludeSchemas) == 0 &&
		len(c.IncludeTables) == 0 &&
		len(c.ExcludeTables) == 0
}

// Filter checks whether a given schema.table qualifies for replication.
type Filter struct {
	includeSchemas map[string]bool
	excludeSchemas map[string]bool
	includeTables  map[string]bool
	excludeTables  map[string]bool
}

// New creates a Filter from the given Config.
func New(cfg Config) *Filter {
	return &Filter{
		includeSchemas: toSet(cfg.IncludeSchemas),
		excludeSchemas: toSet(cfg.ExcludeSchemas),
		includeTables:  toSet(cfg.IncludeTables),
		excludeTables:  toSet(cfg.ExcludeTables),
	}
}

// Allow returns true if the given namespace.table should be included.
func (f *Filter) Allow(namespace, table string) bool {
	qualified := namespace + "." + table

	if len(f.excludeTables) > 0 {
		if f.excludeTables[qualified] || f.excludeTables[table] {
			return false
		}
	}
	if len(f.excludeSchemas) > 0 && f.excludeSchemas[namespace] {
		return false
	}

	if len(f.includeTables) > 0 {
		return f.includeTables[qualified] || f.includeTables[table]
	}
	if len(f.includeSchemas) > 0 {
		return f.includeSchemas[namespace]
	}

	return true
}

func toSet(items []string) map[string]bool {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[strings.TrimSpace(s)] = true
	}
	return m
}
