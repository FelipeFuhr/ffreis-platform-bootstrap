package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build information",
	Run: func(cmd *cobra.Command, _ []string) {
		v := strings.TrimSpace(version)
		if v == "" {
			v = "dev"
		}
		c := strings.TrimSpace(commit)
		if c == "" {
			c = "unknown"
		}
		t := strings.TrimSpace(buildTime)
		if t == "" {
			t = "unknown"
		}

		fmt.Fprintf(cmd.OutOrStdout(), "%s (commit=%s built=%s)\n", v, c, t)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
