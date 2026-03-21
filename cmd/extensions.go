package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var (
	componentExtensionName     string
	debugExtensionDrupalRootfs string
	debugExtensionVerbose      bool
)

var (
	debugPanelStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#112235")).
			Padding(1, 2)
	debugTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#98C1D9"))
	debugSectionDividerStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#29425E"))
	debugStatusOKStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#7BD389"))
	debugStatusWarningStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F4C95D"))
	debugStatusFailedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F28482"))
	debugMutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9FB3C8"))
	debugRowStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#112235"))
)

var componentExtensionCmd = &cobra.Command{
	Use:    "__component",
	Short:  "Internal component extension command",
	Hidden: true,
}

var componentExtensionDescribeCmd = &cobra.Command{
	Use:   "describe",
	Short: "Internal component describe hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponentDescribe(cmd, componentExtensionName, true)
	},
}

var componentExtensionReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Internal component reconcile hook",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponentReconcile(cmd, componentExtensionName)
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
		return runComponentSet(cmd, args[0], stateValue)
	},
}

var debugExtensionCmd = &cobra.Command{
	Use:    "__debug",
	Short:  "Internal debug extension command",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		rendered, err := renderISLEDebug(cmd.Context())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), rendered)
		return err
	},
}

func init() {
	componentExtensionDescribeCmd.Flags().StringVarP(&componentExtensionName, "component", "c", "", "Specific component to describe")
	componentExtensionDescribeCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	componentExtensionDescribeCmd.Flags().StringVar(&statusDrupalRootfs, "drupal-rootfs", createpkg.DefaultDrupalRootfs, "Drupal rootfs path override")
	componentExtensionDescribeCmd.Flags().BoolVar(&statusVerbose, "verbose", false, "Include verbose component details")
	componentExtensionDescribeCmd.Flags().StringVar(&statusFormat, "format", "", "Output format override")

	componentExtensionReconcileCmd.Flags().StringVarP(&componentExtensionName, "component", "c", "", "Specific component to reconcile")
	componentExtensionReconcileCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	componentExtensionReconcileCmd.Flags().StringVar(&statusDrupalRootfs, "drupal-rootfs", createpkg.DefaultDrupalRootfs, "Drupal rootfs path override")
	componentExtensionReconcileCmd.Flags().BoolVar(&componentReviewReport, "report", false, "Render a report instead of applying changes")
	componentExtensionReconcileCmd.Flags().BoolVar(&componentReviewVerbose, "verbose", false, "Include verbose component details")
	componentExtensionReconcileCmd.Flags().StringVar(&componentReviewFormat, "format", "", "Output format override")

	componentExtensionSetCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	componentExtensionSetCmd.Flags().StringVar(&statusDrupalRootfs, "drupal-rootfs", createpkg.DefaultDrupalRootfs, "Drupal rootfs path override")
	componentExtensionSetCmd.Flags().StringVar(&componentSetState, "state", "", "Explicit state override")
	componentExtensionSetCmd.Flags().StringVar(&componentSetDisposition, "disposition", "", "Explicit disposition override")
	componentExtensionSetCmd.Flags().StringVar(&componentSetTLSMode, "tls-mode", "", "TLS mode override")
	componentExtensionSetCmd.Flags().BoolVar(&componentSetYolo, "yolo", false, "Apply without confirmation")

	componentExtensionCmd.AddCommand(componentExtensionDescribeCmd)
	componentExtensionCmd.AddCommand(componentExtensionReconcileCmd)
	componentExtensionCmd.AddCommand(componentExtensionSetCmd)

	debugExtensionCmd.Flags().StringVar(&statusPath, "path", "", "Project path override")
	debugExtensionCmd.Flags().StringVar(&debugExtensionDrupalRootfs, "drupal-rootfs", createpkg.DefaultDrupalRootfs, "Drupal rootfs path override")
	debugExtensionCmd.Flags().BoolVar(&debugExtensionVerbose, "verbose", false, "Include verbose debug details")
}

func renderISLEDebug(runCtx context.Context) (string, error) {
	slog.Debug("starting plugin debug", "plugin", "isle")
	ctx, err := resolveStatusContext()
	if err != nil {
		return "", err
	}
	slog.Debug("resolved plugin context", "plugin", "isle", "context", ctx.Name, "project_dir", ctx.ProjectDir)

	rows := []debugRow{
		{Label: "Context", Value: ctx.Name},
		{Label: "Project dir", Value: ctx.ProjectDir},
		{Label: "Environment", Value: ctx.Environment},
		{Label: "Compose override", Value: resolveEnvironmentOverridePath(ctx)},
	}

	envPath := filepath.Join(ctx.ProjectDir, ".env")
	if envSummary := renderInterestingEnv(envPath); envSummary != "" {
		rows = append(rows, debugRow{Label: "Environment", Value: envSummary})
	}

	overrideSummary, err := renderOverrideEnvSummary(resolveEnvironmentOverridePath(ctx))
	if err != nil {
		return "", err
	}
	if overrideSummary != "" {
		rows = append(rows, debugRow{Label: "Service overrides", Value: overrideSummary})
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

	slog.Debug("resolving drupal root", "plugin", "isle", "rootfs", debugExtensionDrupalRootfs)
	drupalRoot := resolveDrupalRoot(files, ctx.ProjectDir, debugExtensionDrupalRootfs)
	slog.Debug("resolved drupal root", "plugin", "isle", "drupal_root", drupalRoot)
	slog.Debug("rendering media storage", "plugin", "isle")
	mediaStorageRows, mediaStorageErr := renderMediaStorageRows(runCtx, files, drupalRoot)
	slog.Debug("rendering derivative actions", "plugin", "isle", "verbose", debugExtensionVerbose)
	actionStorageRows, triggerRows, derivativeErr := renderDerivativeActionRows(runCtx, files, drupalRoot, debugExtensionVerbose)

	body := []string{
		debugDivider(),
		"",
		debugTitleStyle.Render("General"),
		"",
		formatDebugRows(rows),
	}
	body = append(body, "", debugDivider(), "", debugTitleStyle.Render("Media Storage"), "")
	if mediaStorageErr != nil {
		body = append(body, formatDebugRows([]debugRow{{Label: "Status", Value: renderStatus("warning")}, {Label: "Detail", Value: mediaStorageErr.Error()}}))
	} else {
		body = append(body, formatDebugRows(mediaStorageRows))
	}
	body = append(body, "", debugDivider(), "", debugTitleStyle.Render("Action Storage"), "")
	if derivativeErr != nil {
		body = append(body, formatDebugRows([]debugRow{{Label: "Status", Value: renderStatus("warning")}, {Label: "Detail", Value: derivativeErr.Error()}}))
	} else {
		body = append(body, formatDebugRows(actionStorageRows))
		body = append(body, "", debugDivider(), "", debugTitleStyle.Render("Automatic Triggers"), "", formatDebugRows(triggerRows))
	}

	rendered := renderDebugPanel("isle", strings.Join(body, "\n"))
	if commandSDK == nil {
		return rendered, nil
	}

	for _, include := range commandSDK.Metadata.Includes {
		slog.Debug("running included plugin debug", "plugin", "isle", "include", include, "command", "__debug")
		output, err := commandSDK.InvokeIncludedPluginCommand(include, []string{"__debug"}, plugin.CommandExecOptions{
			Context: runCtx,
			Capture: true,
		})
		if err != nil {
			rendered += "\n\n" + renderDebugPanel(include, formatDebugRows([]debugRow{
				{Label: "Status", Value: renderStatus("warning")},
				{Label: "Detail", Value: err.Error()},
			}))
			continue
		}
		if strings.TrimSpace(output) == "" {
			slog.Debug("included plugin returned empty debug output", "plugin", "isle", "include", include)
			continue
		}
		slog.Debug("included plugin debug completed", "plugin", "isle", "include", include)
		rendered += "\n\n" + strings.TrimSpace(output)
	}

	slog.Debug("finished plugin debug", "plugin", "isle")
	return rendered, nil
}

func renderInterestingEnv(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	keys := []string{"DOMAIN", "BASE_DOMAIN", "URI_SCHEME", "TLS_PROVIDER"}
	values := make([]string, 0, len(keys))
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		for _, interesting := range keys {
			if key != interesting {
				continue
			}
			values = append(values, fmt.Sprintf("%s=%s", key, strings.Trim(strings.TrimSpace(value), `"`)))
		}
	}
	if len(values) == 0 {
		return ""
	}
	sort.Strings(values)
	return strings.Join(values, ", ")
}

func renderOverrideEnvSummary(path string) (string, error) {
	data, err := os.ReadFile(path)
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

type debugRow struct {
	Label string
	Value string
}

func renderDebugPanel(title, body string) string {
	header := debugTitleStyle.Render(strings.TrimSpace(title))
	content := header
	if strings.TrimSpace(body) != "" {
		content += "\n\n" + body
	}
	return debugPanelStyle.Width(debugPanelWidth()).Render(content)
}

func formatDebugRows(rows []debugRow) string {
	labelWidth := 0
	for _, row := range rows {
		if width := len(strings.TrimSpace(row.Label)); width > labelWidth {
			labelWidth = width
		}
	}
	lines := make([]string, 0, len(rows))
	rowWidth := debugContentWidth()
	for _, row := range rows {
		label := strings.TrimSpace(row.Label)
		value := strings.TrimSpace(row.Value)
		if label == "" {
			lines = append(lines, renderDebugRow(rowWidth, "", value))
			continue
		}
		lines = append(lines, renderDebugRow(rowWidth, fmt.Sprintf("%-*s", labelWidth, label), value))
	}
	return strings.Join(lines, "\n")
}

func renderDebugRow(width int, label, value string) string {
	valueWidth := max(0, width-lipgloss.Width(label)-2)
	row := label
	if strings.TrimSpace(label) != "" {
		row += "  "
	}
	row += lipgloss.NewStyle().
		Width(valueWidth).
		Background(lipgloss.Color("#112235")).
		Render(value)
	return debugRowStyle.Width(width).Render(row)
}

func renderStatus(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "ok":
		return debugStatusOKStyle.Render("OK")
	case "warning":
		return debugStatusWarningStyle.Render("WARNING")
	case "failed":
		return debugStatusFailedStyle.Render("FAILED")
	default:
		return debugMutedStyle.Render(strings.ToUpper(strings.TrimSpace(state)))
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func debugPanelWidth() int {
	if columns, err := strconv.Atoi(strings.TrimSpace(os.Getenv("COLUMNS"))); err == nil && columns > 0 {
		return max(40, columns)
	}
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		return max(40, width)
	}
	return 100
}

func debugContentWidth() int {
	return max(20, debugPanelWidth()-4)
}

func debugDivider() string {
	return debugSectionDividerStyle.Width(debugContentWidth()).Render(strings.Repeat("─", debugContentWidth()))
}

func resolveDrupalRoot(files *plugin.FileAccessor, projectDir, drupalRootPath string) string {
	candidates := []string{}
	if trimmed := strings.TrimSpace(drupalRootPath); trimmed != "" {
		if filepath.IsAbs(trimmed) {
			candidates = append(candidates, filepath.Clean(trimmed))
		} else {
			candidates = append(candidates, filepath.Join(projectDir, trimmed))
		}
	}
	if strings.TrimSpace(projectDir) != "" {
		candidates = append(candidates, projectDir)
	}
	for _, candidate := range candidates {
		if _, err := files.ReadFile(filepath.Join(candidate, "config", "sync", "core.extension.yml")); err == nil {
			return candidate
		}
	}
	return ""
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

func renderMediaStorageRows(runCtx context.Context, files *plugin.FileAccessor, drupalRoot string) ([]debugRow, error) {
	configDir := filepath.Join(drupalRoot, "config", "sync")
	if strings.TrimSpace(drupalRoot) == "" {
		return []debugRow{{Label: "Status", Value: renderStatus("warning")}, {Label: "Detail", Value: "Drupal root could not be resolved"}}, nil
	}
	entries, err := files.MatchFilesInDir(configDir, "field.field.media.*.field_media_of.yml")
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return []debugRow{{Label: "Status", Value: renderStatus("warning")}, {Label: "Detail", Value: "No media bundles with field_media_of were found"}}, nil
	}
	entryData, err := files.ReadFilesContext(runCtx, entries)
	if err != nil {
		return nil, err
	}

	rows := []debugRow{{Label: "Status", Value: renderStatus("ok")}}
	for _, path := range entries {
		var mediaOf mediaFieldConfig
		if err := yaml.Unmarshal(entryData[path], &mediaOf); err != nil {
			return nil, fmt.Errorf("read %s: %w", filepath.Base(path), err)
		}
		fieldName, uriScheme, err := resolveBundleStorage(runCtx, files, configDir, mediaOf.Bundle)
		if err != nil {
			return nil, fmt.Errorf("bundle %s: %w", mediaOf.Bundle, err)
		}
		rows = append(rows, debugRow{
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

func renderDerivativeActionRows(runCtx context.Context, files *plugin.FileAccessor, drupalRoot string, verbose bool) ([]debugRow, []debugRow, error) {
	configDir := filepath.Join(drupalRoot, "config", "sync")
	if strings.TrimSpace(drupalRoot) == "" {
		rows := []debugRow{{Label: "Status", Value: renderStatus("warning")}, {Label: "Detail", Value: "Drupal root could not be resolved"}}
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

	storageRows := []debugRow{{Label: "Status", Value: renderStatus("ok")}}
	triggerRows := []debugRow{{Label: "Status", Value: renderStatus("ok")}}
	for _, action := range actions {
		storageRows = append(storageRows, debugRow{Label: action.ID, Value: action.Scheme})
		triggers := triggersByAction[action.ID]
		for _, trigger := range triggers {
			triggerRows = append(triggerRows, debugRow{Label: trigger.ActionID, Value: trigger.ContextName})
			if verbose && strings.TrimSpace(trigger.Conditions) != "" {
				triggerRows = append(triggerRows, debugRow{Label: "", Value: fmt.Sprintf("%s conditions:\n%s", trigger.ContextName, trigger.Conditions)})
			}
		}
	}
	if len(actions) == 0 {
		rows := []debugRow{{Label: "Status", Value: renderStatus("warning")}, {Label: "Detail", Value: "No derivative-generating actions were found"}}
		return rows, rows, nil
	}
	if len(triggerRows) == 1 {
		triggerRows = append(triggerRows, debugRow{Label: "Detail", Value: "No automatic trigger contexts were found"})
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
