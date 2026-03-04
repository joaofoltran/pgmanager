package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jfoltran/pgmanager/internal/appconfig"
	"github.com/jfoltran/pgmanager/internal/db"
	ms "github.com/jfoltran/pgmanager/internal/migrationstore"
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Manage migrations from the command line",
}

var migrateCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new migration",
	RunE:  runMigrateCreate,
}

var migrateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all migrations",
	RunE:  runMigrateList,
}

var migrateStartCmd = &cobra.Command{
	Use:   "start <migration-id>",
	Short: "Start a migration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateStart,
}

var migrateStopCmd = &cobra.Command{
	Use:   "stop <migration-id>",
	Short: "Stop a running migration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateStop,
}

var migrateStatusCmd = &cobra.Command{
	Use:   "status <migration-id>",
	Short: "Show migration status",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateStatus,
}

var migrateDeleteCmd = &cobra.Command{
	Use:   "delete <migration-id>",
	Short: "Delete a migration",
	Args:  cobra.ExactArgs(1),
	RunE:  runMigrateDelete,
}

var (
	flagName          string
	flagSourceCluster string
	flagDestCluster   string
	flagSourceNode    string
	flagDestNode      string
	flagMode          string
	flagFallback      bool
	flagWorkers       int
)

func init() {
	migrateCreateCmd.Flags().StringVar(&flagName, "name", "", "Migration name (required)")
	migrateCreateCmd.Flags().StringVar(&flagSourceCluster, "source-cluster", "", "Source cluster ID (required)")
	migrateCreateCmd.Flags().StringVar(&flagDestCluster, "dest-cluster", "", "Destination cluster ID (required)")
	migrateCreateCmd.Flags().StringVar(&flagSourceNode, "source-node", "", "Source node ID (required)")
	migrateCreateCmd.Flags().StringVar(&flagDestNode, "dest-node", "", "Destination node ID (required)")
	migrateCreateCmd.Flags().StringVar(&flagMode, "mode", "clone_and_follow", "Migration mode: clone_only, clone_and_follow, clone_follow_switchover")
	migrateCreateCmd.Flags().BoolVar(&flagFallback, "fallback", false, "Enable fallback replication")
	migrateCreateCmd.Flags().IntVar(&flagWorkers, "workers", 4, "Parallel copy workers")
	migrateCreateCmd.MarkFlagRequired("name")
	migrateCreateCmd.MarkFlagRequired("source-cluster")
	migrateCreateCmd.MarkFlagRequired("dest-cluster")
	migrateCreateCmd.MarkFlagRequired("source-node")
	migrateCreateCmd.MarkFlagRequired("dest-node")

	migrateCmd.AddCommand(migrateCreateCmd)
	migrateCmd.AddCommand(migrateListCmd)
	migrateCmd.AddCommand(migrateStartCmd)
	migrateCmd.AddCommand(migrateStopCmd)
	migrateCmd.AddCommand(migrateStatusCmd)
	migrateCmd.AddCommand(migrateDeleteCmd)
}

func openDB(ctx context.Context) (*db.DB, error) {
	cfg, err := appconfig.Load(configPath)
	if err != nil {
		return nil, err
	}
	database, err := db.Open(ctx, cfg.Database.URL, silentLogger())
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	return database, nil
}

func runMigrateCreate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	database, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	store := ms.NewStore(database.Pool)

	id := fmt.Sprintf("%s-to-%s", flagSourceCluster, flagDestCluster)
	m := ms.Migration{
		ID:              id,
		Name:            flagName,
		SourceClusterID: flagSourceCluster,
		DestClusterID:   flagDestCluster,
		SourceNodeID:    flagSourceNode,
		DestNodeID:      flagDestNode,
		Mode:            ms.Mode(flagMode),
		Fallback:        flagFallback,
		SlotName:        "pgmanager_" + id,
		Publication:     "pgmanager_pub_" + id,
		CopyWorkers:     flagWorkers,
	}

	if err := ms.ValidateMigration(m); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	if err := store.Create(ctx, m); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Migration %q created.\n", m.ID)
	return nil
}

func runMigrateList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	database, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	store := ms.NewStore(database.Pool)
	list, err := store.List(ctx)
	if err != nil {
		return err
	}

	if len(list) == 0 {
		fmt.Println("No migrations.")
		return nil
	}

	for _, m := range list {
		fmt.Fprintf(os.Stdout, "%-30s %-20s %-12s %s → %s\n",
			m.ID, m.Name, m.Status, m.SourceClusterID, m.DestClusterID)
	}
	return nil
}

func runMigrateStart(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	database, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	store := ms.NewStore(database.Pool)
	if err := store.UpdateStatus(ctx, args[0], ms.StatusRunning, "starting", ""); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Migration %q marked as running. Start the pgmanager server to execute it.\n", args[0])
	return nil
}

func runMigrateStop(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	database, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	store := ms.NewStore(database.Pool)
	if err := store.UpdateStatus(ctx, args[0], ms.StatusStopped, "stopped", ""); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Migration %q marked as stopped.\n", args[0])
	return nil
}

func runMigrateStatus(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	database, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	store := ms.NewStore(database.Pool)
	m, ok, err := store.Get(ctx, args[0])
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("migration %q not found", args[0])
	}

	data, _ := json.MarshalIndent(m, "", "  ")
	fmt.Println(string(data))
	return nil
}

func runMigrateDelete(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	database, err := openDB(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	store := ms.NewStore(database.Pool)
	if err := store.Delete(ctx, args[0]); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Migration %q deleted.\n", args[0])
	return nil
}
