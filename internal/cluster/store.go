package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jfoltran/pgmanager/internal/idgen"
	"github.com/jfoltran/pgmanager/internal/secret"
)

type NodeRole string

const (
	RolePrimary NodeRole = "primary"
	RoleReplica NodeRole = "replica"
	RoleStandby NodeRole = "standby"
)

type Node struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Host     string   `json:"host"`
	Port     uint16   `json:"port"`
	Role     NodeRole `json:"role"`
	User     string   `json:"user,omitempty"`
	Password string   `json:"password,omitempty"`
	DBName   string   `json:"dbname,omitempty"`
	AgentURL          string `json:"agent_url,omitempty"`
	MonitoringEnabled bool   `json:"monitoring_enabled"`
}

func (n Node) DSN() string {
	user := n.User
	if user == "" {
		user = "postgres"
	}
	port := n.Port
	if port == 0 {
		port = 5432
	}
	dbname := n.DBName
	if dbname == "" {
		dbname = "postgres"
	}
	if n.Password != "" {
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s", user, n.Password, n.Host, port, dbname)
	}
	return fmt.Sprintf("postgres://%s@%s:%d/%s", user, n.Host, port, dbname)
}

type Cluster struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Nodes      []Node    `json:"nodes"`
	Tags       []string  `json:"tags,omitempty"`
	BackupPath string    `json:"backup_path,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const RedactedPassword = "********"

func (c Cluster) Redacted() Cluster {
	out := c
	out.Nodes = make([]Node, len(c.Nodes))
	copy(out.Nodes, c.Nodes)
	for i := range out.Nodes {
		if out.Nodes[i].Password != "" {
			out.Nodes[i].Password = RedactedPassword
		}
	}
	return out
}

type Store struct {
	pool   *pgxpool.Pool
	cipher *secret.Box
}

func NewStore(pool *pgxpool.Pool, cipher *secret.Box) *Store {
	return &Store{pool: pool, cipher: cipher}
}

func (s *Store) List(ctx context.Context) ([]Cluster, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT id, name, tags, backup_path, created_at, updated_at FROM clusters ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.Tags, &c.BackupPath, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		clusters = append(clusters, c)
	}
	if clusters == nil {
		clusters = []Cluster{}
	}

	for i := range clusters {
		nodes, err := s.listNodes(ctx, clusters[i].ID)
		if err != nil {
			return nil, err
		}
		clusters[i].Nodes = nodes
	}
	return clusters, nil
}

func (s *Store) Get(ctx context.Context, id string) (Cluster, bool, error) {
	var c Cluster
	err := s.pool.QueryRow(ctx,
		"SELECT id, name, tags, backup_path, created_at, updated_at FROM clusters WHERE id = $1", id,
	).Scan(&c.ID, &c.Name, &c.Tags, &c.BackupPath, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Cluster{}, false, nil
		}
		return Cluster{}, false, err
	}

	nodes, err := s.listNodes(ctx, id)
	if err != nil {
		return Cluster{}, false, err
	}
	c.Nodes = nodes
	return c, true, nil
}

func (s *Store) Add(ctx context.Context, c Cluster) (Cluster, error) {
	if c.ID == "" {
		c.ID = idgen.NewClusterID()
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Cluster{}, err
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()
	tags := c.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO clusters (id, name, tags, backup_path, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		c.ID, c.Name, tags, c.BackupPath, now, now)
	if err != nil {
		return Cluster{}, fmt.Errorf("insert cluster: %w", err)
	}

	for i := range c.Nodes {
		if c.Nodes[i].ID == "" {
			c.Nodes[i].ID = idgen.NewNodeID()
		}
		if err := insertNode(ctx, tx, c.ID, c.Nodes[i], s.cipher); err != nil {
			return Cluster{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Cluster{}, err
	}

	c.Tags = tags
	c.CreatedAt = now
	c.UpdatedAt = now
	return c, nil
}

func (s *Store) Update(ctx context.Context, c Cluster) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tags := c.Tags
	if tags == nil {
		tags = []string{}
	}
	tag, err := tx.Exec(ctx,
		`UPDATE clusters SET name = $2, tags = $3, backup_path = $4, updated_at = now() WHERE id = $1`,
		c.ID, c.Name, tags, c.BackupPath)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("cluster %q not found", c.ID)
	}

	oldNodes, err := s.listNodes(ctx, c.ID)
	if err != nil {
		return fmt.Errorf("fetch existing nodes: %w", err)
	}
	oldPasswords := make(map[string]string, len(oldNodes))
	for _, n := range oldNodes {
		oldPasswords[n.ID] = n.Password
	}

	_, err = tx.Exec(ctx, "DELETE FROM nodes WHERE cluster_id = $1", c.ID)
	if err != nil {
		return err
	}

	for i := range c.Nodes {
		if c.Nodes[i].ID == "" {
			c.Nodes[i].ID = idgen.NewNodeID()
		}
		if c.Nodes[i].Password == RedactedPassword {
			c.Nodes[i].Password = oldPasswords[c.Nodes[i].ID]
		}
		if err := insertNode(ctx, tx, c.ID, c.Nodes[i], s.cipher); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) Remove(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM clusters WHERE id = $1", id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("cluster %q not found", id)
	}
	return nil
}

func (s *Store) AddNode(ctx context.Context, clusterID string, n Node) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := insertNode(ctx, tx, clusterID, n, s.cipher); err != nil {
		return err
	}

	_, err = tx.Exec(ctx, "UPDATE clusters SET updated_at = now() WHERE id = $1", clusterID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) RemoveNode(ctx context.Context, clusterID, nodeID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		"DELETE FROM nodes WHERE cluster_id = $1 AND id = $2", clusterID, nodeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node %q not found in cluster %q", nodeID, clusterID)
	}

	_, err = tx.Exec(ctx, "UPDATE clusters SET updated_at = now() WHERE id = $1", clusterID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) listNodes(ctx context.Context, clusterID string) ([]Node, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, host, port, role, username, password, dbname, agent_url, monitoring_enabled
		 FROM nodes WHERE cluster_id = $1 ORDER BY id`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var user, pass, dbname, agentURL string
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.Role, &user, &pass, &dbname, &agentURL, &n.MonitoringEnabled); err != nil {
			return nil, err
		}
		n.User = user
		if s.cipher != nil && pass != "" {
			dec, err := s.cipher.Decrypt(pass)
			if err != nil {
				return nil, fmt.Errorf("decrypt password for node %s: %w", n.ID, err)
			}
			pass = dec
		}
		n.Password = pass
		n.DBName = dbname
		n.AgentURL = agentURL
		nodes = append(nodes, n)
	}
	if nodes == nil {
		nodes = []Node{}
	}
	return nodes, nil
}

func insertNode(ctx context.Context, tx pgx.Tx, clusterID string, n Node, cipher *secret.Box) error {
	password := n.Password
	if cipher != nil && password != "" {
		enc, err := cipher.Encrypt(password)
		if err != nil {
			return fmt.Errorf("encrypt password for node %q: %w", n.ID, err)
		}
		password = enc
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO nodes (id, cluster_id, name, host, port, role, username, password, dbname, agent_url, monitoring_enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		n.ID, clusterID, n.Name, n.Host, n.Port, string(n.Role),
		n.User, password, n.DBName, n.AgentURL, n.MonitoringEnabled)
	if err != nil {
		return fmt.Errorf("insert node %q: %w", n.ID, err)
	}
	return nil
}

func (s *Store) SetNodeMonitoring(ctx context.Context, clusterID, nodeID string, enabled bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE nodes SET monitoring_enabled = $3 WHERE cluster_id = $1 AND id = $2`,
		clusterID, nodeID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("node %q not found in cluster %q", nodeID, clusterID)
	}
	_, err = s.pool.Exec(ctx, "UPDATE clusters SET updated_at = now() WHERE id = $1", clusterID)
	return err
}

func (s *Store) ListMonitoredClusters(ctx context.Context) ([]Cluster, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT c.id, c.name, c.tags, c.backup_path, c.created_at, c.updated_at
		 FROM clusters c JOIN nodes n ON n.cluster_id = c.id
		 WHERE n.monitoring_enabled = true
		 ORDER BY c.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var clusters []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.Tags, &c.BackupPath, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		clusters = append(clusters, c)
	}

	for i := range clusters {
		nodes, err := s.listNodes(ctx, clusters[i].ID)
		if err != nil {
			return nil, err
		}
		clusters[i].Nodes = nodes
	}
	return clusters, nil
}

func (s *Store) EncryptExistingPasswords(ctx context.Context) (int, error) {
	if s.cipher == nil {
		return 0, nil
	}
	rows, err := s.pool.Query(ctx,
		"SELECT cluster_id, id, password FROM nodes WHERE password != ''")
	if err != nil {
		return 0, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	type row struct {
		clusterID, nodeID, password string
	}
	var plaintext []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.clusterID, &r.nodeID, &r.password); err != nil {
			return 0, err
		}
		if !secret.IsEncrypted(r.password) {
			plaintext = append(plaintext, r)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, r := range plaintext {
		enc, err := s.cipher.Encrypt(r.password)
		if err != nil {
			return 0, fmt.Errorf("encrypt password for node %s: %w", r.nodeID, err)
		}
		_, err = s.pool.Exec(ctx,
			"UPDATE nodes SET password = $1 WHERE cluster_id = $2 AND id = $3",
			enc, r.clusterID, r.nodeID)
		if err != nil {
			return 0, fmt.Errorf("update node %s: %w", r.nodeID, err)
		}
	}
	return len(plaintext), nil
}

func ValidateCluster(c Cluster) error {
	var errs []error
	if c.Name == "" {
		errs = append(errs, errors.New("cluster name is required"))
	}
	if len(c.Nodes) == 0 {
		errs = append(errs, errors.New("at least one node is required"))
	}
	for i, n := range c.Nodes {
		if n.Host == "" {
			errs = append(errs, fmt.Errorf("node %d: host is required", i))
		}
		if n.Port == 0 {
			errs = append(errs, fmt.Errorf("node %d: port is required", i))
		}
	}
	return errors.Join(errs...)
}
