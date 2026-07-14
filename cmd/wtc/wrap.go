package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/migueljfsc/wtc/internal/wrap"
)

func newWrapCmd(flags *clientFlags) *cobra.Command {
	var opts wrap.Options

	cmd := &cobra.Command{
		Use:   "wrap [flags] -- <command...>",
		Short: "Run a command and record its lifecycle as a change event",
		Long: `Records a started event, runs the command with inherited stdio, then
records succeeded/failed with the duration and exit code. helm upgrade/install
and terraform apply/destroy are recognized and enriched (service, namespace,
chart, image tag, change counts). A dead wtc server never blocks the command.`,
		Example: `  wtc wrap --env pr-123 -- helm upgrade pr-123 ./chart -n pr-123
  wtc wrap --env prod -- terraform apply -auto-approve -json`,
		DisableFlagsInUseLine: true,
		Args:                  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Everything after -- is the wrapped command.
			if dash := cmd.ArgsLenAtDash(); dash >= 0 {
				args = args[dash:]
			}
			code := wrap.Run(cmd.Context(), flags.resolve(), opts, args, os.Stdout, os.Stderr)
			if code != 0 {
				// Propagate the wrapped command's exit code transparently.
				os.Exit(code)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Env, "env", "", "environment the change targets (e.g. pr-123)")
	f.StringVar(&opts.Service, "service", "", "override the sniffed service name")
	f.StringVar(&opts.Title, "title", "", "override the event title (default: the command line)")
	return cmd
}
