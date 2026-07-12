package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/ingest/generic"
	"github.com/migueljfsc/wtc/internal/model"
)

func newRecordCmd(flags *clientFlags) *cobra.Command {
	var req generic.Request

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record a change event by hand (posts to /ingest/generic)",
		Example: `  wtc record --kind manual --env prod --service api \
    --title "rotated db credentials" --actor alice`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if req.Source == "" {
				req.Source = string(model.SourceManual)
			}
			// Stable client-side key so a retried command updates rather
			// than duplicates (SPEC: wrap/record derive local:<id>).
			if req.DedupKey == "" {
				req.DedupKey = "local:" + model.NewID()
			}

			resp, err := flags.resolve().IngestGeneric(cmd.Context(), req)
			if err != nil {
				return err
			}
			if resp.Deduped {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "updated %s (dedup_key %s)\n", resp.ID, req.DedupKey)
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "recorded %s\n", resp.ID)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&req.Kind, "kind", "", "event kind: build|merge|push|deploy|config_change|infra_change|rollback|manual (required)")
	f.StringVar(&req.Title, "title", "", "one-line human-readable description (required)")
	f.StringVar(&req.Env, "env", "", "environment (prod, staging, dev, pr-123, ...)")
	f.StringVar(&req.Service, "service", "", "service name")
	f.StringVar(&req.Cluster, "cluster", "", "cluster name")
	f.StringVar(&req.Namespace, "namespace", "", "kubernetes namespace")
	f.StringVar(&req.Actor, "actor", "", "who made the change (default: nobody)")
	f.StringVar(&req.Ref, "ref", "", "git sha / revision")
	f.StringVar(&req.Artifact, "artifact", "", "primary artifact, e.g. registry/app:tag")
	f.StringVar(&req.Status, "status", "", "started|succeeded|failed|unknown (default unknown)")
	f.StringVar(&req.TS, "ts", "", "event time, RFC3339 (default now)")
	f.StringVar(&req.URL, "url", "", "deep link into the source system")
	f.StringVar(&req.DedupKey, "dedup-key", "", "stable idempotency key (default local:<ulid>)")
	f.StringVar(&req.Source, "source", "", "event source (default manual)")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("title")

	return cmd
}
