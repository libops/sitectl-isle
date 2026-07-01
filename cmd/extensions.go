package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/libops/sitectl/pkg/plugin/debugui"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var componentExtensionName string
var componentExtensionListName string

var componentExtensionCmd = &cobra.Command{
	Use:    "component",
	Short:  "Internal component extension command",
	Hidden: true,
}

var componentExtensionListCmd = &cobra.Command{
	Use:   "list [component]",
	Short: "Internal component list hook",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.TrimSpace(componentExtensionListName)
		if len(args) > 0 {
			name = strings.TrimSpace(args[0])
		}
		return corecomponent.WriteComponentCatalog(cmd.OutOrStdout(), "ISLE", componentCatalogDefinitions(), name)
	},
}

var componentExtensionDescribeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Internal component describe hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponentDescribe(cmd, componentDescribeOptionsFromGlobals(componentExtensionName, true))
	},
}

var componentExtensionReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Internal component reconcile hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponentReconcile(cmd, componentReconcileOptionsFromGlobals(componentExtensionName))
	},
}

var componentExtensionSetCmd = &cobra.Command{
	Use:   "set <name> [disposition]",
	Short: "Internal component set hook",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		stateValue, err := resolveComponentSetStateValue(cmd, args)
		if err != nil {
			return err
		}
		return runComponentSetWithOptions(cmd, args[0], stateValue, componentSetOptionsFromGlobals())
	},
}

// isleDebugRunner implements plugin.DebugRunner for the isle plugin.
type isleDebugRunner struct {
	codebaseRootfs string
	drupalRootfs   string
	verbose        bool
}

func (r *isleDebugRunner) BindFlags(cmd *cobra.Command) {
	addCodebaseRootfsFlags(cmd, &r.codebaseRootfs, &r.drupalRootfs, createpkg.DefaultDrupalRootfs)
	cmd.Flags().BoolVar(&r.verbose, "verbose", false, "Include verbose debug details")
}

func (r *isleDebugRunner) Render(cmd *cobra.Command, ctx *config.Context) (string, error) {
	rootfs, err := resolveCodebaseRootfsForContext(cmd, ctx, r.codebaseRootfs, r.drupalRootfs)
	if err != nil {
		return "", err
	}
	return renderISLEDebugBody(cmd.Context(), ctx, rootfs, r.verbose)
}

func init() {
	componentExtensionListCmd.Flags().StringVarP(&componentExtensionListName, "component", "c", "", "Specific component to list")

	componentExtensionDescribeCmd.Flags().StringVarP(&componentExtensionName, "component", "c", "", "Specific component to describe")
	componentExtensionDescribeCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	addCodebaseRootfsFlags(componentExtensionDescribeCmd, &statusCodebaseRootfs, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	componentExtensionDescribeCmd.Flags().BoolVar(&statusVerbose, "verbose", false, "Include verbose component details")
	componentExtensionDescribeCmd.Flags().StringVar(&statusFormat, "format", "", "Output format override")

	componentExtensionReconcileCmd.Flags().StringVarP(&componentExtensionName, "component", "c", "", "Specific component to reconcile")
	componentExtensionReconcileCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	addCodebaseRootfsFlags(componentExtensionReconcileCmd, &statusCodebaseRootfs, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	componentExtensionReconcileCmd.Flags().BoolVar(&componentReviewReport, "report", false, "Render a report instead of applying changes")
	componentExtensionReconcileCmd.Flags().BoolVar(&componentReviewVerbose, "verbose", false, "Include verbose component details")
	componentExtensionReconcileCmd.Flags().StringVar(&componentReviewFormat, "format", "", "Output format override")
	componentExtensionReconcileCmd.Flags().BoolVar(&componentReviewYolo, "yolo", false, "Apply without confirmation")

	componentExtensionSetCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	addCodebaseRootfsFlags(componentExtensionSetCmd, &statusCodebaseRootfs, &statusDrupalRootfs, createpkg.DefaultDrupalRootfs)
	componentExtensionSetCmd.Flags().StringVar(&componentSetState, "state", "", "Explicit state override")
	componentExtensionSetCmd.Flags().StringVar(&componentSetDisposition, "disposition", "", "Explicit disposition override")
	componentExtensionSetCmd.Flags().BoolVar(&componentSetYolo, "yolo", false, "Apply without confirmation")
	addComponentSetFollowUpFlags(componentExtensionSetCmd, managedComponentDefinitions())

	componentExtensionCmd.AddCommand(componentExtensionListCmd)
	componentExtensionCmd.AddCommand(componentExtensionDescribeCmd)
	componentExtensionCmd.AddCommand(componentExtensionReconcileCmd)
	componentExtensionCmd.AddCommand(componentExtensionSetCmd)
}

func renderISLEDebugBody(runCtx context.Context, ctx *config.Context, drupalRootfsOverride string, verbose bool) (string, error) {
	slog.Debug("starting plugin debug", "plugin", "isle")
	slog.Debug("resolved plugin context", "plugin", "isle", "context", ctx.Name, "project_dir", ctx.ProjectDir)

	rows := []debugui.Row{
		{Label: "Context", Value: ctx.Name},
		{Label: "Project dir", Value: ctx.ProjectDir},
		{Label: "Environment", Value: ctx.Environment},
		{Label: "Compose override", Value: resolveEnvironmentOverridePath(ctx)},
	}

	overrideSummary, err := renderOverrideEnvSummary(resolveEnvironmentOverridePath(ctx))
	if err != nil {
		return "", err
	}
	if overrideSummary != "" {
		rows = append(rows, debugui.Row{Label: "Service overrides", Value: overrideSummary})
	}
	slog.Debug("creating file accessor", "plugin", "isle")
	if commandSDK == nil {
		return "", fmt.Errorf("plugin sdk is not initialized")
	}
	files, err := commandSDK.GetFileAccessor()
	if err != nil {
		return "", err
	}
	defer files.Close()

	rootfs := strings.TrimSpace(drupalRootfsOverride)
	if rootfs == "" {
		rootfs = ctx.EffectiveDrupalRootfs()
	}
	slog.Debug("resolving drupal root", "plugin", "isle", "rootfs", rootfs)
	drupalRoot := ctx.ResolveProjectPath(rootfs)
	slog.Debug("resolved drupal root", "plugin", "isle", "drupal_root", drupalRoot)
	slog.Debug("rendering media storage", "plugin", "isle")
	mediaStorageRows, mediaStorageErr := renderMediaStorageRows(runCtx, files, drupalRoot)
	slog.Debug("rendering derivative actions", "plugin", "isle", "verbose", verbose)
	actionStorageRows, triggerRows, derivativeErr := renderDerivativeActionRows(runCtx, files, drupalRoot, verbose)

	slog.Debug("rendering component status", "plugin", "isle")
	componentRows := renderISLEComponentRows(ctx, rootfs)

	body := []string{
		debugui.Divider(),
		"",
		debugui.Title("General"),
		"",
		debugui.FormatRows(rows),
	}
	body = append(body, "", debugui.Divider(), "", debugui.Title("Components"), "", debugui.FormatRows(componentRows))
	body = append(body, "", debugui.Divider(), "", debugui.Title("Media Storage"), "")
	if mediaStorageErr != nil {
		body = append(body, debugui.FormatRows([]debugui.Row{{Label: "Status", Value: debugui.Status("warning")}, {Label: "Detail", Value: mediaStorageErr.Error()}}))
	} else {
		body = append(body, debugui.FormatRows(mediaStorageRows))
	}
	body = append(body, "", debugui.Divider(), "", debugui.Title("Action Storage"), "")
	if derivativeErr != nil {
		body = append(body, debugui.FormatRows([]debugui.Row{{Label: "Status", Value: debugui.Status("warning")}, {Label: "Detail", Value: derivativeErr.Error()}}))
	} else {
		body = append(body, debugui.FormatRows(actionStorageRows))
		body = append(body, "", debugui.Divider(), "", debugui.Title("Automatic Triggers"), "", debugui.FormatRows(triggerRows))
	}

	slog.Debug("finished plugin debug body", "plugin", "isle")
	return strings.Join(body, "\n"), nil
}

func renderISLEComponentRows(ctx *config.Context, drupalRootfs string) []debugui.Row {
	statuses, err := detectComponentViewsForContext(ctx, drupalRootfs)
	if err != nil {
		return []debugui.Row{
			{Label: "Status", Value: debugui.Status("warning")},
			{Label: "Detail", Value: err.Error()},
		}
	}
	rows := []debugui.Row{{Label: "Status", Value: debugui.Status("ok")}}
	anyDrifted := false
	for _, s := range statuses {
		value := string(s.State)
		if strings.TrimSpace(s.Detail) != "" {
			value = fmt.Sprintf("%s (%s)", value, s.Detail)
		}
		if s.State == "drifted" {
			anyDrifted = true
		}
		rows = append(rows, debugui.Row{Label: s.Name, Value: value})
	}
	if anyDrifted {
		rows[0].Value = debugui.Status("warning")
	}
	return rows
}

func renderOverrideEnvSummary(path string) (string, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- override path is resolved inside the selected ISLE project.
	if err != nil {
		if os.IsNotExist(err) {
			return "none", nil
		}
		return "", err
	}

	var compose struct {
		Services map[string]struct {
			Environment any `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return "", err
	}

	entries := make([]string, 0, len(compose.Services))
	for serviceName, service := range compose.Services {
		keys := extractEnvKeys(service.Environment)
		if len(keys) == 0 {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s=[%s]", serviceName, strings.Join(keys, ", ")))
	}
	if len(entries) == 0 {
		return "none", nil
	}
	sort.Strings(entries)
	return strings.Join(entries, "; "), nil
}

func extractEnvKeys(value any) []string {
	keys := []string{}
	switch typed := value.(type) {
	case map[string]any:
		for key := range typed {
			keys = append(keys, key)
		}
	case []any:
		for _, entry := range typed {
			text := strings.TrimSpace(fmt.Sprint(entry))
			key, _, ok := strings.Cut(text, "=")
			if !ok {
				continue
			}
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	sort.Strings(keys)
	return keys
}

type mediaFieldConfig struct {
	FieldName string `yaml:"field_name"`
	Bundle    string `yaml:"bundle"`
	FieldType string `yaml:"field_type"`
}

type mediaFieldStorageConfig struct {
	Settings struct {
		URIScheme string `yaml:"uri_scheme"`
	} `yaml:"settings"`
}

type actionConfig struct {
	ID            string `yaml:"id"`
	Configuration struct {
		Event  string `yaml:"event"`
		Scheme string `yaml:"scheme"`
	} `yaml:"configuration"`
}

type contextConfig struct {
	Name       string `yaml:"name"`
	Conditions any    `yaml:"conditions"`
	Reactions  struct {
		Derivative struct {
			Actions map[string]string `yaml:"actions"`
		} `yaml:"derivative"`
	} `yaml:"reactions"`
}

type derivativeActionTrigger struct {
	ActionID    string
	ContextName string
	Conditions  string
}

type derivativeActionInfo struct {
	ID     string
	Scheme string
}

func renderMediaStorageRows(runCtx context.Context, files *plugin.FileAccessor, drupalRoot string) ([]debugui.Row, error) {
	configDir := filepath.Join(drupalRoot, "config", "sync")
	if strings.TrimSpace(drupalRoot) == "" {
		return []debugui.Row{{Label: "Status", Value: debugui.Status("warning")}, {Label: "Detail", Value: "Drupal root could not be resolved"}}, nil
	}
	entries, err := files.MatchFilesInDir(configDir, "field.field.media.*.field_media_of.yml")
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return []debugui.Row{{Label: "Status", Value: debugui.Status("warning")}, {Label: "Detail", Value: "No media bundles with field_media_of were found"}}, nil
	}
	entryData, err := files.ReadFilesContext(runCtx, entries)
	if err != nil {
		return nil, err
	}

	rows := []debugui.Row{{Label: "Status", Value: debugui.Status("ok")}}
	for _, path := range entries {
		var mediaOf mediaFieldConfig
		if err := yaml.Unmarshal(entryData[path], &mediaOf); err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		fieldName, uriScheme, err := resolveBundleStorage(runCtx, files, configDir, mediaOf.Bundle)
		if err != nil {
			return nil, fmt.Errorf("bundle %s: %w", mediaOf.Bundle, err)
		}
		rows = append(rows, debugui.Row{
			Label: mediaOf.Bundle,
			Value: fmt.Sprintf("%s -> %s://", fieldName, uriScheme),
		})
	}
	return rows, nil
}

func resolveBundleStorage(runCtx context.Context, files *plugin.FileAccessor, configDir, bundle string) (string, string, error) {
	fieldPaths, err := files.MatchFilesInDir(configDir, fmt.Sprintf("field.field.media.%s.*.yml", bundle))
	if err != nil {
		return "", "", err
	}
	fieldData, err := files.ReadFilesContext(runCtx, fieldPaths)
	if err != nil {
		return "", "", err
	}
	for _, path := range fieldPaths {
		if strings.HasSuffix(path, ".field_media_of.yml") || strings.HasSuffix(path, ".field_media_use.yml") {
			continue
		}
		var field mediaFieldConfig
		if err := yaml.Unmarshal(fieldData[path], &field); err != nil {
			return "", "", err
		}
		if field.FieldType != "file" && field.FieldType != "image" {
			continue
		}
		storagePath := filepath.Join(configDir, fmt.Sprintf("field.storage.media.%s.yml", field.FieldName))
		var storage mediaFieldStorageConfig
		storageData, err := files.ReadFilesContext(runCtx, []string{storagePath})
		if err != nil {
			return "", "", err
		}
		if err := yaml.Unmarshal(storageData[storagePath], &storage); err != nil {
			return "", "", err
		}
		scheme := strings.TrimSpace(storage.Settings.URIScheme)
		if scheme == "" {
			scheme = "unknown"
		}
		return field.FieldName, scheme, nil
	}
	return "", "", fmt.Errorf("no file/image field found")
}

func renderDerivativeActionRows(runCtx context.Context, files *plugin.FileAccessor, drupalRoot string, verbose bool) ([]debugui.Row, []debugui.Row, error) {
	configDir := filepath.Join(drupalRoot, "config", "sync")
	if strings.TrimSpace(drupalRoot) == "" {
		rows := []debugui.Row{{Label: "Status", Value: debugui.Status("warning")}, {Label: "Detail", Value: "Drupal root could not be resolved"}}
		return rows, rows, nil
	}
	entries, err := files.MatchFilesInDir(configDir, "system.action.*.yml")
	if err != nil {
		return nil, nil, err
	}
	actions, err := loadDerivativeActions(runCtx, files, entries)
	if err != nil {
		return nil, nil, err
	}
	triggersByAction, err := loadDerivativeTriggers(runCtx, files, configDir)
	if err != nil {
		return nil, nil, err
	}

	storageRows := []debugui.Row{{Label: "Status", Value: debugui.Status("ok")}}
	triggerRows := []debugui.Row{{Label: "Status", Value: debugui.Status("ok")}}
	for _, action := range actions {
		storageRows = append(storageRows, debugui.Row{Label: action.ID, Value: action.Scheme})
		triggers := triggersByAction[action.ID]
		for _, trigger := range triggers {
			triggerRows = append(triggerRows, debugui.Row{Label: trigger.ActionID, Value: trigger.ContextName})
			if verbose && strings.TrimSpace(trigger.Conditions) != "" {
				triggerRows = append(triggerRows, debugui.Row{Label: "", Value: fmt.Sprintf("%s conditions:\n%s", trigger.ContextName, trigger.Conditions)})
			}
		}
	}
	if len(actions) == 0 {
		rows := []debugui.Row{{Label: "Status", Value: debugui.Status("warning")}, {Label: "Detail", Value: "No derivative-generating actions were found"}}
		return rows, rows, nil
	}
	if len(triggerRows) == 1 {
		triggerRows = append(triggerRows, debugui.Row{Label: "Detail", Value: "No automatic trigger contexts were found"})
	}
	return storageRows, triggerRows, nil
}

func loadDerivativeActions(runCtx context.Context, files *plugin.FileAccessor, paths []string) ([]derivativeActionInfo, error) {
	batch, err := files.ReadFilesContext(runCtx, paths)
	if err != nil {
		return nil, err
	}
	actions := make([]derivativeActionInfo, 0, len(paths))
	for _, path := range paths {
		var action actionConfig
		if err := yaml.Unmarshal(batch[path], &action); err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		if strings.TrimSpace(action.Configuration.Event) != "Generate Derivative" {
			continue
		}
		scheme := strings.TrimSpace(action.Configuration.Scheme)
		if scheme == "" {
			scheme = "unknown"
		}
		actions = append(actions, derivativeActionInfo{ID: action.ID, Scheme: scheme})
	}
	return actions, nil
}

func loadDerivativeTriggers(runCtx context.Context, files *plugin.FileAccessor, configDir string) (map[string][]derivativeActionTrigger, error) {
	contextPaths, err := files.MatchFilesInDir(configDir, "context.context.*.yml")
	if err != nil {
		return nil, err
	}
	batch, err := files.ReadFilesContext(runCtx, contextPaths)
	if err != nil {
		return nil, err
	}

	rows := map[string][]derivativeActionTrigger{}
	for _, path := range contextPaths {
		var ctx contextConfig
		if err := yaml.Unmarshal(batch[path], &ctx); err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		if len(ctx.Reactions.Derivative.Actions) == 0 {
			continue
		}
		conditions := ""
		if ctx.Conditions != nil {
			rendered, err := yaml.Marshal(ctx.Conditions)
			if err != nil {
				return nil, fmt.Errorf("marshal conditions for %s: %w", ctx.Name, err)
			}
			conditions = strings.TrimSpace(string(rendered))
		}
		for actionID := range ctx.Reactions.Derivative.Actions {
			rows[actionID] = append(rows[actionID], derivativeActionTrigger{
				ActionID:    actionID,
				ContextName: ctx.Name,
				Conditions:  conditions,
			})
		}
	}
	return rows, nil
}
