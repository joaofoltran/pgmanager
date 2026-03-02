package filter

import "testing"

func TestFilterAllowNoConfig(t *testing.T) {
	f := New(Config{})
	if !f.Allow("public", "users") {
		t.Fatal("empty filter should allow everything")
	}
}

func TestFilterExcludeTables(t *testing.T) {
	f := New(Config{ExcludeTables: []string{"public.audit_log", "sessions"}})
	if f.Allow("public", "audit_log") {
		t.Fatal("should exclude public.audit_log")
	}
	if f.Allow("public", "sessions") {
		t.Fatal("should exclude sessions by unqualified name")
	}
	if !f.Allow("public", "users") {
		t.Fatal("should allow users")
	}
}

func TestFilterIncludeTables(t *testing.T) {
	f := New(Config{IncludeTables: []string{"public.users", "public.orders"}})
	if !f.Allow("public", "users") {
		t.Fatal("should include users")
	}
	if f.Allow("public", "sessions") {
		t.Fatal("should not include sessions")
	}
}

func TestFilterExcludeSchemas(t *testing.T) {
	f := New(Config{ExcludeSchemas: []string{"pg_catalog", "information_schema"}})
	if f.Allow("pg_catalog", "pg_class") {
		t.Fatal("should exclude pg_catalog tables")
	}
	if !f.Allow("public", "users") {
		t.Fatal("should allow public tables")
	}
}

func TestFilterIncludeSchemas(t *testing.T) {
	f := New(Config{IncludeSchemas: []string{"app"}})
	if !f.Allow("app", "users") {
		t.Fatal("should include app schema")
	}
	if f.Allow("public", "users") {
		t.Fatal("should not include public schema")
	}
}

func TestFilterExcludeOverridesInclude(t *testing.T) {
	f := New(Config{
		IncludeSchemas: []string{"public"},
		ExcludeTables:  []string{"public.audit_log"},
	})
	if f.Allow("public", "audit_log") {
		t.Fatal("exclude should override include")
	}
	if !f.Allow("public", "users") {
		t.Fatal("should allow non-excluded tables")
	}
}

func TestConfigIsEmpty(t *testing.T) {
	if !(Config{}).IsEmpty() {
		t.Fatal("default should be empty")
	}
	if (Config{ExcludeTables: []string{"foo"}}).IsEmpty() {
		t.Fatal("should not be empty")
	}
}
