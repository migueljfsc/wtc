package main

import (
	"cmp"
	"os"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/client"
)

// version is stamped via -ldflags "-X main.version=..." at release time.
var version = "dev"

const defaultServer = "http://localhost:8484"

type clientFlags struct {
	server string
	token  string
}

// resolve builds the API client with flag > env > default precedence.
func (f *clientFlags) resolve() *client.Client {
	return client.New(
		cmp.Or(f.server, os.Getenv("WTC_SERVER"), defaultServer),
		cmp.Or(f.token, os.Getenv("WTC_API_TOKEN")),
	)
}

func newRootCmd() *cobra.Command {
	flags := &clientFlags{}

	root := &cobra.Command{
		Use:           "wtc",
		Short:         "wtc — what the change: a vendor-neutral change ledger",
		Long:          "wtc ingests change events (CI builds, GitOps reconciles, manual changes)\ninto one timeline and answers: what changed, where is this commit,\nand how do two environments differ.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringVar(&flags.server, "server", "",
		"wtc server URL (default WTC_SERVER or "+defaultServer+")")
	root.PersistentFlags().StringVar(&flags.token, "token", "",
		"API bearer token (default WTC_API_TOKEN)")

	root.AddCommand(
		newServeCmd(),
		newRecordCmd(flags),
		newLogCmd(flags),
		newWhereCmd(flags),
		newDiffCmd(flags),
		newHandoffCmd(flags),
		newAroundCmd(flags),
		newBlastCmd(flags),
		newDoraCmd(flags),
		newChangesCmd(flags),
		newExplainCmd(flags),
		newExportCmd(flags),
		newBackupCmd(flags),
		newWrapCmd(flags),
		newInitCmd(),
		newDoctorCmd(flags),
		newConfigCmd(flags),
		newDemoCmd(flags),
		newMigrateCmd(),
	)
	return root
}
