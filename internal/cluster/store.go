package cluster

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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
	AgentURL string   `json:"agent_url,omitempty"`
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

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
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

func (s *Store) Add(ctx context.Context, c Cluster) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
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
		return fmt.Errorf("insert cluster: %w", err)
	}

	for _, n := range c.Nodes {
		if err := insertNode(ctx, tx, c.ID, n); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
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

	_, err = tx.Exec(ctx, "DELETE FROM nodes WHERE cluster_id = $1", c.ID)
	if err != nil {
		return err
	}

	for _, n := range c.Nodes {
		if err := insertNode(ctx, tx, c.ID, n); err != nil {
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

	if err := insertNode(ctx, tx, clusterID, n); err != nil {
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
		`SELECT id, name, host, port, role, username, password, dbname, agent_url
		 FROM nodes WHERE cluster_id = $1 ORDER BY id`, clusterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		var user, pass, dbname, agentURL string
		if err := rows.Scan(&n.ID, &n.Name, &n.Host, &n.Port, &n.Role, &user, &pass, &dbname, &agentURL); err != nil {
			return nil, err
		}
		n.User = user
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

func insertNode(ctx context.Context, tx pgx.Tx, clusterID string, n Node) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO nodes (id, cluster_id, name, host, port, role, username, password, dbname, agent_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		n.ID, clusterID, n.Name, n.Host, n.Port, string(n.Role),
		n.User, n.Password, n.DBName, n.AgentURL)
	if err != nil {
		return fmt.Errorf("insert node %q: %w", n.ID, err)
	}
	return nil
}

func ValidateCluster(c Cluster) error {
	var errs []error
	if c.ID == "" {
		errs = append(errs, errors.New("cluster id is required"))
	}
	if c.Name == "" {
		errs = append(errs, errors.New("cluster name is required"))
	}
	if len(c.Nodes) == 0 {
		errs = append(errs, errors.New("at least one node is required"))
	}
	for _, n := range c.Nodes {
		if n.ID == "" {
			errs = append(errs, errors.New("node id is required"))
		}
		if n.Host == "" {
			errs = append(errs, errors.New("node host is required"))
		}
		if n.Port == 0 {
			errs = append(errs, fmt.Errorf("node %q port is required", n.ID))
		}
	}
	return errors.Join(errs...)
}
