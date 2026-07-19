package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/config"
	"github.com/migueljfsc/wtc/internal/store"
)

// newMigrateCmd is the one-shot sqlite→postgres ledger migration. It is
// the deliberate exception to "the CLI never opens the DB file": an offline
// admin operation run with serve stopped, like serve itself.
func newMigrateCmd() *cobra.Command {
	var configPath, from, to string

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Copy a sqlite ledger into postgres (offline, idempotent)",
		Long: "Copies events, poller watermarks, and config overrides from the sqlite\n" +
			"ledger into a postgres database, creating the schema if needed. Run with\n" +
			"`wtc serve` STOPPED so the copy is the final consistent snapshot. Safe to\n" +
			"re-run: existing rows are skipped (ON CONFLICT DO NOTHING).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			explicit := cmd.Flags().Changed("config")
			cfg, err := config.Load(configPath, !explicit)
			if err != nil {
				return err
			}
			if from == "" {
				from = cfg.Server.DB
			}
			if to == "" {
				to = cfg.Storage.DSN
			}
			if to == "" {
				return fmt.Errorf("no postgres DSN: set --to or storage.dsn in %s", configPath)
			}

			res, err := store.MigrateLedger(cmd.Context(), from, to)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(),
				"migrated %s → postgres:\n  events copied:  %d\n  events skipped: %d (already present)\n  watermarks:     %d\n  overrides:      %d\n",
				from, res.Events, res.EventsSkipped, res.Watermarks, res.Overrides)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(),
				"next: set storage.backend=postgres (and storage.dsn) in wtc.yaml, then start serve")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "wtc.yaml", "path to wtc.yaml (supplies defaults for --from/--to)")
	cmd.Flags().StringVar(&from, "from", "", "sqlite ledger path (default server.db from config)")
	cmd.Flags().StringVar(&to, "to", "", "postgres DSN (default storage.dsn from config)")
	return cmd
}
