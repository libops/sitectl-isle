package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
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

func resolveCodebaseRootfsForContext(cmd *cobra.Command, ctx *config.Context, codebaseRootfs, drupalRootfs string) (string, error) {
	codebaseChanged := cmd != nil && cmd.Flags().Changed("codebase-rootfs")
	drupalChanged := cmd != nil && cmd.Flags().Changed("drupal-rootfs")
	if codebaseChanged || drupalChanged {
		return resolveCodebaseRootfsFlag(cmd, codebaseRootfs, drupalRootfs)
	}
	if ctx != nil {
		if rootfs := strings.TrimSpace(ctx.DrupalRootfs); rootfs != "" {
			return rootfs, nil
		}
		if rootfs := detectCodebaseRootfs(ctx.ProjectDir, codebaseRootfs, drupalRootfs); rootfs != "" {
			return rootfs, nil
		}
	}
	return resolveCodebaseRootfsFlag(cmd, codebaseRootfs, drupalRootfs)
}

func detectCodebaseRootfs(projectDir string, candidates ...string) string {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return ""
	}
	seen := map[string]bool{}
	for _, candidate := range append(candidates, "drupal/rootfs/var/www/drupal", "drupal", corecomponent.DefaultDrupalRootfs) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if codebaseRootfsExists(projectDir, candidate) {
			return candidate
		}
	}
	return ""
}

func codebaseRootfsExists(projectDir, rootfs string) bool {
	layout := corecomponent.ResolveDrupalLayout(projectDir, rootfs)
	for _, path := range []string{
		layout.ConfigSyncDir(),
		layout.ComposerJSONPath(),
		filepath.Join(layout.Root, "web", "robots.txt"),
	} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}
