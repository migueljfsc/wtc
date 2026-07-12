package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Phase 0 ships init and doctor as stubs; they land in later phases
// (init: P1 wiring checklists; doctor: P1 source health). See docs/PLAN.md.

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold wtc.yaml and print wiring checklists (not implemented yet)",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("wtc init is not implemented yet — lands with Phase 1 (see docs/PLAN.md)")
		},
	}
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report ingest source health (not implemented yet)",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("wtc doctor is not implemented yet — lands with Phase 1 (see docs/PLAN.md)")
		},
	}
}
