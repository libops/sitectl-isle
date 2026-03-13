package cmd

import "github.com/spf13/cobra"

var componentCmd = &cobra.Command{
	Use:   "component",
	Short: "Inspect and manage ISLE components",
}

func init() {
	componentCmd.AddCommand(statusCmd)
}
