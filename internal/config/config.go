package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// DatabaseConfig holds connection parameters for a PostgreSQL instance.
type DatabaseConfig struct {
	Host     string
	Port     uint16
	User     string
	Password string
	DBName   string
}

// ParseURI parses a PostgreSQL connection URI (postgres://user:pass@host:port/dbname)
// into the DatabaseConfig fields, unconditionally setting each component found in the URI.
func (d *DatabaseConfig) ParseURI(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return fmt.Errorf("invalid connection URI: %w", err)
	}
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return fmt.Errorf("unsupported URI scheme %q (expected postgres or postgresql)", u.Scheme)
	}

	if u.Hostname() != "" {
		d.Host = u.Hostname()
	}
	if u.Port() != "" {
		p, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil {
			return fmt.Errorf("invalid port in URI: %w", err)
		}
		d.Port = uint16(p)
	}
	if u.User != nil {
		if username := u.User.Username(); username != "" {
			d.User = username
		}
		if password, ok := u.User.Password(); ok {
			d.Password = password
		}
	}
	dbname := strings.TrimPrefix(u.Path, "/")
	if dbname != "" {
		d.DBName = dbname
	}
	return nil
}

// DSN returns a standard PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(d.User, d.Password),
		Host:   fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:   d.DBName,
	}
	return u.String()
}

// ReplicationDSN returns a connection string with replication=database set.
func (d DatabaseConfig) ReplicationDSN() string {
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(d.User, d.Password),
		Host:     fmt.Sprintf("%s:%d", d.Host, d.Port),
		Path:     d.DBName,
		RawQuery: "replication=database",
	}
	return u.String()
}

// ReplicationConfig holds settings for the WAL replication stream.
type ReplicationConfig struct {
	SlotName     string
	Publication  string
	OutputPlugin string
	OriginID     string
}

// SnapshotConfig holds settings for the initial data copy.
type SnapshotConfig struct {
	Workers int
}

// LoggingConfig holds settings for structured logging.
type LoggingConfig struct {
	Level  string
	Format string // "json" or "console"
}

// FilterConfig holds table/schema filtering rules for migration.
type FilterConfig struct {
	IncludeSchemas    []string
	ExcludeSchemas    []string
	IncludeTables     []string
	ExcludeTables     []string
	ExcludeExtensions []string
}

// SchemaConfig holds settings for DDL handling.
type SchemaConfig struct {
	MaxErrors         int
	ExcludeExtensions []string
}

// Config is the top-level configuration for pgmanager.
type Config struct {
	Source      DatabaseConfig
	Dest        DatabaseConfig
	Replication ReplicationConfig
	Snapshot    SnapshotConfig
	Logging     LoggingConfig
	Filter      FilterConfig
	Schema      SchemaConfig
}

// Validate checks that required fields are present and values are sane.
func (c *Config) Validate() error {
	var errs []error

	if c.Source.Host == "" {
		errs = append(errs, errors.New("source host is required"))
	}
	if c.Source.DBName == "" {
		errs = append(errs, errors.New("source database name is required"))
	}
	if c.Dest.Host == "" {
		errs = append(errs, errors.New("destination host is required"))
	}
	if c.Dest.DBName == "" {
		errs = append(errs, errors.New("destination database name is required"))
	}
	if c.Replication.SlotName == "" {
		errs = append(errs, errors.New("replication slot name is required"))
	}
	if c.Replication.Publication == "" {
		errs = append(errs, errors.New("publication name is required"))
	}
	if c.Replication.OutputPlugin == "" {
		c.Replication.OutputPlugin = "pgoutput"
	}
	if c.Snapshot.Workers < 1 {
		c.Snapshot.Workers = 4
	}

	return errors.Join(errs...)
}
