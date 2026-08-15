package workbench

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config contains the Workbench settings sitectl must validate before
// delegating an Islandora mutation.
type Config struct {
	Task string
	Host string

	settings map[string]yaml.Node
}

// ParseRollbackCSV validates an exact Workbench rollback artifact and returns
// its unique node IDs in artifact order.
func ParseRollbackCSV(r io.Reader) ([]uint64, error) {
	file, err := readCSVWithHeader(r, "rollback CSV", csvReadOptions{
		comment:          '#',
		fieldsPerRecord:  -1,
		trimLeadingSpace: true,
	})
	if err != nil {
		return nil, err
	}
	if file.header[0] != "node_id" {
		return nil, fmt.Errorf("rollback CSV first column is %q, want node_id", file.header[0])
	}

	seen := make(map[uint64]int)
	var ids []uint64
	for {
		record, recordNumber, readErr := file.readRecord()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read rollback CSV record %d: %w", recordNumber, readErr)
		}
		if len(record) != len(file.header) {
			return nil, fmt.Errorf("rollback CSV record %d has %d columns, want %d", recordNumber, len(record), len(file.header))
		}
		value := strings.TrimSpace(record[0])
		id, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil || id == 0 {
			return nil, fmt.Errorf("rollback CSV record %d has invalid node_id %q", recordNumber, value)
		}
		if firstRecord, ok := seen[id]; ok {
			return nil, fmt.Errorf("rollback CSV node_id %d is duplicated at records %d and %d", id, firstRecord, recordNumber)
		}
		seen[id] = recordNumber
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("rollback CSV contains no node IDs")
	}
	return ids, nil
}

// ValidateDeleteConfig verifies that a Workbench configuration is explicitly
// scoped to the destructive delete task.
func ValidateDeleteConfig(r io.Reader) error {
	return ValidateTaskConfig(r, "delete")
}

// ValidateTaskConfig verifies that a Workbench configuration is explicitly
// scoped to the expected task.
func ValidateTaskConfig(r io.Reader, expected string) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return fmt.Errorf("expected Workbench task is required")
	}
	cfg, err := ParseConfig(r)
	if err != nil {
		return err
	}
	if cfg.Task != expected {
		return fmt.Errorf("workbench config task is %q, want %s", cfg.Task, expected)
	}
	return nil
}

// ValidateGuardedExecutionConfig validates a Workbench config for sitectl's
// exact-input execution profile and rejects hooks, acquisition, and row
// selection settings that could expand or alter the requested operation.
func ValidateGuardedExecutionConfig(r io.Reader, expected string) (Config, error) {
	expected = strings.TrimSpace(expected)
	if expected != "add_media" && expected != "delete" {
		return Config{}, fmt.Errorf("guarded Workbench task must be add_media or delete")
	}
	cfg, err := ParseConfig(r)
	if err != nil {
		return Config{}, err
	}
	if cfg.Task != expected {
		return Config{}, fmt.Errorf("workbench config task is %q, want %s", cfg.Task, expected)
	}
	forbidden := []string{
		"bootstrap",
		"check_lock_file_path",
		"completion_message",
		"csv_row_filters",
		"csv_rows_to_process",
		"csv_start_row",
		"csv_start_row_skip",
		"csv_stop_row",
		"csv_stop_row_skip",
		"csv_field_templates",
		"csv_value_templates",
		"ignore_csv_columns",
		"input_data_zip_archives",
		"log_file_name_and_line_number",
		"log_response_body",
		"media_post_create",
		"node_post_create",
		"node_post_update",
		"preprocessors",
		"recovery_mode_starting_from_node_id",
		"remind_user_to_run_check",
		"remove_password_from_config_file",
		"run_scripts",
		"secondary_tasks",
		"shutdown",
		"user_prompts",
	}
	if expected == "add_media" {
		forbidden = append(forbidden, "additional_files")
	}
	if expected == "delete" {
		forbidden = append(forbidden, "prompt_user_before_delete_task")
	}
	for _, setting := range forbidden {
		if _, configured := cfg.settings[setting]; configured {
			return Config{}, fmt.Errorf("guarded Workbench %s config must omit %q because it can add side effects or alter the exact input scope", expected, setting)
		}
	}
	if delimiter, ok := cfg.settings["delimiter"]; ok {
		var value string
		if err := delimiter.Decode(&value); err != nil || value != "," {
			return Config{}, fmt.Errorf("guarded Workbench %s config delimiter must be comma", expected)
		}
	}
	return cfg, nil
}

// GuardedConfigSnapshot returns an execution-only copy of a validated config
// with both Workbench logging phases pinned to the caller-selected fresh log.
func GuardedConfigSnapshot(data []byte, expectedTask, logPath string) ([]byte, Config, error) {
	cfg, err := ValidateGuardedExecutionConfig(bytes.NewReader(data), expectedTask)
	if err != nil {
		return nil, Config{}, err
	}
	if !path.IsAbs(logPath) || path.Clean(logPath) != logPath || logPath == "/" {
		return nil, Config{}, fmt.Errorf("guarded Workbench log path must be an absolute canonical file path")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, Config{}, fmt.Errorf("decode Workbench config for snapshot: %w", err)
	}
	mapping := document.Content[0]
	setYAMLScalar(mapping, "log_file_path", "!!str", logPath)
	setYAMLScalar(mapping, "log_file_mode", "!!str", "w")
	output, err := yaml.Marshal(&document)
	if err != nil {
		return nil, Config{}, fmt.Errorf("encode guarded Workbench config snapshot: %w", err)
	}
	return output, cfg, nil
}

func setYAMLScalar(mapping *yaml.Node, key, tag, value string) {
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value},
	)
}

// ParseConfig reads the task and host from one Workbench YAML document.
func ParseConfig(r io.Reader) (Config, error) {
	decoder := yaml.NewDecoder(r)
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return Config{}, fmt.Errorf("decode Workbench config: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return Config{}, fmt.Errorf("workbench config must contain one YAML mapping")
	}
	mapping := document.Content[0]
	settings := make(map[string]yaml.Node, len(mapping.Content)/2)
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		name := strings.TrimSpace(mapping.Content[index].Value)
		if name == "" {
			return Config{}, fmt.Errorf("workbench config contains an empty setting name")
		}
		if _, duplicate := settings[name]; duplicate {
			return Config{}, fmt.Errorf("workbench config contains duplicate setting %q", name)
		}
		if name == "<<" || mapping.Content[index].Tag == "!!merge" {
			return Config{}, fmt.Errorf("workbench config YAML merge keys are not allowed in a guarded execution config")
		}
		if mapping.Content[index+1].Kind == yaml.AliasNode {
			return Config{}, fmt.Errorf("workbench config setting %q must not use a YAML alias", name)
		}
		settings[name] = *mapping.Content[index+1]
	}
	var decoded struct {
		Task string `yaml:"task"`
		Host string `yaml:"host"`
	}
	if err := mapping.Decode(&decoded); err != nil {
		return Config{}, fmt.Errorf("decode Workbench config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, fmt.Errorf("workbench config contains multiple YAML documents")
		}
		return Config{}, fmt.Errorf("decode trailing Workbench config: %w", err)
	}
	return Config{Task: strings.TrimSpace(decoded.Task), Host: strings.TrimSpace(decoded.Host), settings: settings}, nil
}

// NormalizeSiteURL returns the canonical HTTP(S) origin and base path used to
// compare a Workbench host with a sitectl-resolved Drupal endpoint.
func NormalizeSiteURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse URL %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("URL %q must use http or https", rawURL)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("URL %q must not contain credentials", rawURL)
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return "", fmt.Errorf("URL %q must contain a host", rawURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("URL %q must not contain a query or fragment", rawURL)
	}
	hostname := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if hostname == "" {
		return "", fmt.Errorf("URL %q must contain a host", rawURL)
	}
	port := parsed.Port()
	if port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return "", fmt.Errorf("URL %q has invalid port %q", rawURL, port)
		}
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	basePath := path.Clean("/" + strings.TrimSpace(parsed.EscapedPath()))
	if basePath == "/" || basePath == "/." {
		basePath = ""
	}
	basePath = strings.TrimSuffix(basePath, "/")
	return scheme + "://" + host + basePath, nil
}
