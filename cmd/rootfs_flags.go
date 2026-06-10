package cmd

import (
	"fmt"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/spf13/cobra"
)

func addCodebaseRootfsFlags(cmd *cobra.Command, codebaseTarget, drupalTarget *string, defaultValue string) {
	if defaultValue == "" {
		defaultValue = corecomponent.DefaultDrupalRootfs
	}
	cmd.Flags().StringVar(codebaseTarget, "codebase-rootfs", defaultValue, "Codebase rootfs relative to --path. Used to resolve composer.json and config/sync")
	cmd.Flags().StringVar(drupalTarget, "drupal-rootfs", defaultValue, "Drupal rootfs relative to --path. Deprecated alias for --codebase-rootfs")
	corecomponent.MarkCodebaseRootfsFlag(cmd, "codebase-rootfs")
}

func resolveCodebaseRootfsFlag(cmd *cobra.Command, codebaseRootfs, drupalRootfs string) (string, error) {
	codebaseRootfs = strings.TrimSpace(codebaseRootfs)
	drupalRootfs = strings.TrimSpace(drupalRootfs)
	codebaseChanged := cmd != nil && cmd.Flags().Changed("codebase-rootfs")
	drupalChanged := cmd != nil && cmd.Flags().Changed("drupal-rootfs")
	if codebaseChanged && drupalChanged && codebaseRootfs != drupalRootfs {
		return "", fmt.Errorf("--codebase-rootfs and --drupal-rootfs cannot be combined with different values")
	}
	if codebaseChanged {
		return codebaseRootfs, nil
	}
	if drupalChanged {
		return drupalRootfs, nil
	}
	if codebaseRootfs != "" {
		return codebaseRootfs, nil
	}
	return drupalRootfs, nil
}
