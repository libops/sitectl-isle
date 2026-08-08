package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/spf13/cobra"
)

func TestIsleDeployRunnerPreDownChecksEffectiveComposeBeforeOutage(t *testing.T) {
	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - ./scripts/drupal-media-storage-state.php:/var/www/drupal/drupal-media-storage-state.php:ro,z
      - ./scripts/drupal-wait-installed.sh:/usr/local/lib/sitectl/drupal-wait-installed.sh:ro,z
`)
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}

	oldRunCompose := createRunComposeCommand
	t.Cleanup(func() {
		createRunComposeCommand = oldRunCompose
	})
	var commands []string
	createRunComposeCommand = func(runCtx context.Context, gotCtx *config.Context, gotProjectDir string, stdout, stderr io.Writer, command string) error {
		if gotCtx != ctx || gotProjectDir != projectDir {
			t.Fatalf("unexpected rollout context: ctx=%#v project=%q", gotCtx, gotProjectDir)
		}
		commands = append(commands, command)
		return nil
	}

	cmd := &cobra.Command{Use: "pre-down"}
	cmd.SetContext(t.Context())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := (isleDeployRunner{}).PreDown(cmd, ctx); err != nil {
		t.Fatalf("PreDown() error = %v", err)
	}
	want := []string{rolloutPreflightCommand, rolloutComposeConfigCommand, rolloutMountedWaitProbeCommand}
	if len(commands) != len(want) {
		t.Fatalf("effective Compose commands = %#v, want %#v", commands, want)
	}
	for index := range want {
		if commands[index] != want[index] {
			t.Fatalf("effective Compose command %d = %q, want %q", index, commands[index], want[index])
		}
	}
}

func TestIsleDeployRunnerPreDownStopsWhenCompletePreflightRejectsRequiredSource(t *testing.T) {
	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - ./scripts/drupal-media-storage-state.php:/var/www/drupal/drupal-media-storage-state.php:ro,z
      - ./scripts/drupal-wait-installed.sh:/usr/local/lib/sitectl/drupal-wait-installed.sh:ro,z
`)
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}

	oldRunCompose := createRunComposeCommand
	t.Cleanup(func() {
		createRunComposeCommand = oldRunCompose
	})
	var commands []string
	createRunComposeCommand = func(_ context.Context, _ *config.Context, _ string, _, _ io.Writer, command string) error {
		commands = append(commands, command)
		if command == rolloutPreflightCommand {
			return errors.New("missing required source certs/rootCA.pem")
		}
		return nil
	}

	err := (isleDeployRunner{}).PreDown(&cobra.Command{Use: "pre-down"}, ctx)
	assertActionableRolloutContractError(t, err, "complete tracked preflight")
	if len(commands) != 1 || commands[0] != rolloutPreflightCommand {
		t.Fatalf("commands after rejected complete preflight = %#v, want only %#v", commands, []string{rolloutPreflightCommand})
	}
}

func TestValidateRolloutTemplateContractAcceptsMountedCheckedInPrograms(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - type: bind
        source: ./scripts/drupal-media-storage-state.php
        target: /var/www/drupal/drupal-media-storage-state.php
        read_only: true
      - type: bind
        source: ./scripts/drupal-wait-installed.sh
        target: /usr/local/lib/sitectl/drupal-wait-installed.sh
        read_only: true
`)
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	if err := validateRolloutTemplateContractContext(t.Context(), ctx); err != nil {
		t.Fatalf("validateRolloutTemplateContractContext() error = %v", err)
	}
}

func TestValidateRolloutTemplateContractAcceptsShortBindSyntax(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - ./scripts/drupal-media-storage-state.php:/var/www/drupal/drupal-media-storage-state.php:ro,z
      - ./scripts/drupal-wait-installed.sh:/usr/local/lib/sitectl/drupal-wait-installed.sh:ro,z
`)
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	if err := validateRolloutTemplateContractContext(t.Context(), ctx); err != nil {
		t.Fatalf("validateRolloutTemplateContractContext() error = %v", err)
	}
}

func TestValidateRolloutTemplateContractRejectsMissingWaitProgramBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, "services:\n  drupal: {}\n")
	if err := os.Remove(filepath.Join(projectDir, "scripts", "drupal-wait-installed.sh")); err != nil {
		t.Fatalf("Remove(wait program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "missing tracked scripts/drupal-wait-installed.sh")
}

func TestValidateRolloutTemplateContractRejectsMissingPreflightBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, "services:\n  drupal: {}\n")
	if err := os.Remove(filepath.Join(projectDir, "scripts", "sitectl-rollout-preflight.sh")); err != nil {
		t.Fatalf("Remove(preflight program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "missing tracked scripts/sitectl-rollout-preflight.sh")
}

func TestValidateRolloutTemplateContractRejectsMissingMediaStateProgramBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, "services:\n  drupal: {}\n")
	if err := os.Remove(filepath.Join(projectDir, "scripts", "drupal-media-storage-state.php")); err != nil {
		t.Fatalf("Remove(media state program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "missing tracked scripts/drupal-media-storage-state.php")
}

func TestValidateRolloutTemplateContractRejectsMissingBindBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, "services:\n  drupal: {}\n")
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "does not bind")
}

func TestValidateRolloutTemplateContractRejectsWritableBindBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - type: bind
        source: ./scripts/drupal-media-storage-state.php
        target: /var/www/drupal/drupal-media-storage-state.php
        read_only: true
      - type: bind
        source: ./scripts/drupal-wait-installed.sh
        target: /usr/local/lib/sitectl/drupal-wait-installed.sh
        read_only: false
`)
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "does not bind")
}

func TestValidateRolloutTemplateContractRejectsSymlinkedWaitProgramBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - type: bind
        source: ./scripts/drupal-media-storage-state.php
        target: /var/www/drupal/drupal-media-storage-state.php
        read_only: true
      - type: bind
        source: ./scripts/drupal-wait-installed.sh
        target: /usr/local/lib/sitectl/drupal-wait-installed.sh
        read_only: true
`)
	waitProgram := filepath.Join(projectDir, "scripts", "drupal-wait-installed.sh")
	if err := os.Remove(waitProgram); err != nil {
		t.Fatalf("Remove(wait program) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(projectDir, "compose.yaml"), waitProgram); err != nil {
		t.Fatalf("Symlink(wait program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "must be a regular tracked file")
}

func TestValidateRolloutTemplateContractRejectsSymlinkedMediaStateProgramBeforeOutage(t *testing.T) {
	t.Parallel()

	projectDir := writeRolloutContractFixture(t, `services:
  drupal:
    volumes:
      - ./scripts/drupal-media-storage-state.php:/var/www/drupal/drupal-media-storage-state.php:ro,z
      - ./scripts/drupal-wait-installed.sh:/usr/local/lib/sitectl/drupal-wait-installed.sh:ro,z
`)
	mediaStateProgram := filepath.Join(projectDir, "scripts", "drupal-media-storage-state.php")
	if err := os.Remove(mediaStateProgram); err != nil {
		t.Fatalf("Remove(media state program) error = %v", err)
	}
	if err := os.Symlink(filepath.Join(projectDir, "compose.yaml"), mediaStateProgram); err != nil {
		t.Fatalf("Symlink(media state program) error = %v", err)
	}
	ctx := &config.Context{DockerHostType: config.ContextLocal, ProjectDir: projectDir}
	err := validateRolloutTemplateContractContext(t.Context(), ctx)
	assertActionableRolloutContractError(t, err, "must be a regular tracked file")
}

func writeRolloutContractFixture(t *testing.T, compose string) string {
	t.Helper()
	projectDir := t.TempDir()
	scriptsDir := filepath.Join(projectDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(scripts) error = %v", err)
	}
	for name, contents := range map[string]string{
		"drupal-media-storage-state.php": "<?php\n",
		"drupal-wait-installed.sh":       "#!/bin/sh\n",
		"sitectl-rollout-preflight.sh":   "#!/usr/bin/env bash\n",
	} {
		if err := os.WriteFile(filepath.Join(scriptsDir, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(projectDir, "compose.yaml"), []byte(compose), 0o644); err != nil {
		t.Fatalf("WriteFile(compose.yaml) error = %v", err)
	}
	return projectDir
}

func assertActionableRolloutContractError(t *testing.T, err error, detail string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected rollout contract failure")
	}
	for _, want := range []string{
		"before services were stopped",
		detail,
		"https://github.com/libops/isle",
		"v1.3.0",
		"then rerun sitectl deploy",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error to contain %q, got %v", want, err)
		}
	}
}
