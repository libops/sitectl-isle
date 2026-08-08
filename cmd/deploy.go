package cmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	rolloutPreflightScript         = "scripts/sitectl-rollout-preflight.sh"
	rolloutPreflightCommand        = "bash " + rolloutPreflightScript
	rolloutMediaStateScriptSource  = "scripts/drupal-media-storage-state.php"
	rolloutMediaStateScriptTarget  = "/var/www/drupal/drupal-media-storage-state.php"
	rolloutWaitScriptSource        = "scripts/drupal-wait-installed.sh"
	rolloutWaitScriptTarget        = "/usr/local/lib/sitectl/drupal-wait-installed.sh"
	rolloutComposeConfigCommand    = "docker compose config --quiet"
	rolloutMountedWaitProbeCommand = "docker compose run --rm --no-deps --entrypoint test drupal -r " + rolloutWaitScriptTarget
)

type rolloutBindContract struct {
	source string
	target string
}

var rolloutBindContracts = []rolloutBindContract{
	{source: rolloutMediaStateScriptSource, target: rolloutMediaStateScriptTarget},
	{source: rolloutWaitScriptSource, target: rolloutWaitScriptTarget},
}

type isleDeployRunner struct{}

func (isleDeployRunner) BindFlags(*cobra.Command) {}

func (isleDeployRunner) PreDown(cmd *cobra.Command, ctx *config.Context) error {
	runCtx := context.Background()
	if cmd != nil {
		runCtx = cmd.Context()
	}
	if err := validateRolloutTemplateContractContext(runCtx, ctx); err != nil {
		return err
	}
	stdout, stderr := io.Writer(io.Discard), io.Writer(io.Discard)
	if cmd != nil {
		stdout = cmd.OutOrStdout()
		stderr = cmd.ErrOrStderr()
	}
	if err := createRunComposeCommand(runCtx, ctx, ctx.ProjectDir, stdout, stderr, rolloutPreflightCommand); err != nil {
		return rolloutTemplateMigrationError(fmt.Sprintf(
			"the complete tracked preflight %s rejected one or more required rollout sources: %v",
			rolloutPreflightScript,
			err,
		))
	}
	if err := createRunComposeCommand(runCtx, ctx, ctx.ProjectDir, stdout, stderr, rolloutComposeConfigCommand); err != nil {
		return fmt.Errorf("validate effective ISLE Compose configuration before services were stopped: %w", err)
	}
	if err := createRunComposeCommand(runCtx, ctx, ctx.ProjectDir, stdout, stderr, rolloutMountedWaitProbeCommand); err != nil {
		return rolloutTemplateMigrationError(fmt.Sprintf("the effective drupal service cannot read %s", rolloutWaitScriptTarget))
	}
	return nil
}

func (isleDeployRunner) PostUp(*cobra.Command, *config.Context) error {
	return nil
}

func isleDeployDefinition() plugin.DeploySpec {
	return plugin.DeploySpec{
		Name:        "default",
		Description: "Validate the ISLE template contract before replacing running services",
		Default:     true,
	}
}

func validateRolloutTemplateContractContext(runCtx context.Context, ctx *config.Context) error {
	if ctx == nil {
		return fmt.Errorf("validate ISLE rollout contract: context is nil")
	}
	projectDir := strings.TrimSpace(ctx.ProjectDir)
	if projectDir == "" {
		return fmt.Errorf("validate ISLE rollout contract: context %q does not define a project directory", ctx.Name)
	}

	requiredPrograms := []string{rolloutPreflightScript}
	for _, contract := range rolloutBindContracts {
		requiredPrograms = append(requiredPrograms, contract.source)
	}
	for _, relativePath := range requiredPrograms {
		programPath := filepath.Join(projectDir, filepath.FromSlash(relativePath))
		exists, err := ctx.FileExists(programPath)
		if err != nil {
			return fmt.Errorf("inspect ISLE rollout program %q: %w", relativePath, err)
		}
		if !exists {
			return rolloutTemplateMigrationError(fmt.Sprintf("missing tracked %s", relativePath))
		}
		for _, probeArgs := range [][]string{{"-f", programPath}, {"!", "-L", programPath}} {
			if _, err := ctx.RunQuietCommandContext(runCtx, exec.Command("test", probeArgs...)); err != nil { // #nosec G204 -- fixed file-type probes receive context-scoped rollout paths as argv values.
				return rolloutTemplateMigrationError(fmt.Sprintf("%s must be a regular tracked file, not a directory or symbolic link", relativePath))
			}
		}
	}

	foundComposeFile := false
	for _, composePath := range rolloutComposePaths(ctx) {
		exists, err := ctx.FileExists(composePath)
		if err != nil {
			return fmt.Errorf("inspect Compose file %q: %w", composePath, err)
		}
		if !exists {
			continue
		}
		foundComposeFile = true
		data, err := ctx.ReadFile(composePath)
		if err != nil {
			return fmt.Errorf("read Compose file %q: %w", composePath, err)
		}
		matchesAll := true
		for _, contract := range rolloutBindContracts {
			matches, err := composeHasReadOnlyBind(data, projectDir, contract)
			if err != nil {
				return fmt.Errorf("parse Compose file %q: %w", composePath, err)
			}
			if !matches {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			return nil
		}
	}
	if !foundComposeFile {
		return rolloutTemplateMigrationError("no configured Compose file was found")
	}
	return rolloutTemplateMigrationError("the drupal service does not bind every required checked-in rollout program read-only at its template target")
}

func rolloutComposePaths(ctx *config.Context) []string {
	candidates := append([]string{}, ctx.ComposeFile...)
	candidates = append(candidates, "compose.yaml", "docker-compose.yml")
	paths := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		resolved := candidate
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(ctx.ProjectDir, resolved)
		}
		resolved = filepath.Clean(resolved)
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		paths = append(paths, resolved)
	}
	return paths
}

func composeHasReadOnlyBind(data []byte, projectDir string, contract rolloutBindContract) (bool, error) {
	var compose struct {
		Services map[string]struct {
			Volumes []yaml.Node `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return false, err
	}
	drupal, ok := compose.Services["drupal"]
	if !ok {
		return false, nil
	}
	for _, volume := range drupal.Volumes {
		if composeVolumeMatchesReadOnlyBind(volume, projectDir, contract) {
			return true, nil
		}
	}
	return false, nil
}

func composeVolumeMatchesReadOnlyBind(volume yaml.Node, projectDir string, contract rolloutBindContract) bool {
	if volume.Kind == yaml.AliasNode && volume.Alias != nil {
		return composeVolumeMatchesReadOnlyBind(*volume.Alias, projectDir, contract)
	}
	switch volume.Kind {
	case yaml.ScalarNode:
		parts := strings.SplitN(strings.TrimSpace(volume.Value), ":", 3)
		return len(parts) == 3 && rolloutBindSourceMatches(parts[0], projectDir, contract.source) && filepath.Clean(parts[1]) == contract.target && composeVolumeModeReadOnly(parts[2])
	case yaml.MappingNode:
		values := make(map[string]string, len(volume.Content)/2)
		for index := 0; index+1 < len(volume.Content); index += 2 {
			values[strings.TrimSpace(volume.Content[index].Value)] = strings.TrimSpace(volume.Content[index+1].Value)
		}
		volumeType := values["type"]
		return (volumeType == "" || volumeType == "bind") && strings.EqualFold(values["read_only"], "true") &&
			rolloutBindSourceMatches(values["source"], projectDir, contract.source) &&
			filepath.Clean(values["target"]) == contract.target
	default:
		return false
	}
}

func composeVolumeModeReadOnly(mode string) bool {
	for _, option := range strings.Split(mode, ",") {
		if strings.TrimSpace(option) == "ro" {
			return true
		}
	}
	return false
}

func rolloutBindSourceMatches(source, projectDir, expectedSource string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(projectDir, source)
	}
	want := filepath.Join(projectDir, filepath.FromSlash(expectedSource))
	return filepath.Clean(source) == filepath.Clean(want)
}

func rolloutTemplateMigrationError(detail string) error {
	return fmt.Errorf(
		"ISLE rollout compatibility check failed before services were stopped: %s; update the site checkout from %s at %s or newer so its checked-in preflight and read-only Drupal program mounts match the template contract, then rerun sitectl deploy",
		detail,
		defaultTemplateRepo,
		defaultTemplateBranch,
	)
}
