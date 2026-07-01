package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
)

func TestProjectDetectClaimsISLETemplateWithoutComposerPackage(t *testing.T) {
	projectDir := t.TempDir()
	writeFileForTest(t, filepath.Join(projectDir, "docker-compose.yml"), "services:\n  alpaca:\n    image: libops/alpaca:2\n  drupal:\n    image: islandora/drupal:main\n")
	writeFileForTest(t, filepath.Join(projectDir, "composer.json"), `{"require": {}}`)

	data, err := os.ReadFile(filepath.Join(projectDir, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("ReadFile(docker-compose.yml) error = %v", err)
	}
	services := config.DetectComposeServices(projectDir)
	if !slices.Contains(services, "alpaca") || !slices.Contains(services, "drupal") {
		t.Fatalf("expected fixture services alpaca and drupal, got %v from:\n%s", services, string(data))
	}

	oldSDK := commandSDK
	previousDetector := config.SetProjectClaimDetector(nil)
	t.Cleanup(func() {
		commandSDK = oldSDK
		config.SetProjectClaimDetector(previousDetector)
	})

	sdk := plugin.NewSDK(plugin.Metadata{Name: "isle", Includes: []string{"drupal"}})
	RegisterCommands(sdk)

	result := runProjectDetectForTest(t, sdk, projectDir)
	if !result.Claimed {
		t.Fatalf("expected ISLE project detection to claim %s, got %#v", projectDir, result)
	}
	if result.Plugin != "isle" {
		t.Fatalf("expected isle claim, got %#v", result)
	}
}

func runProjectDetectForTest(t *testing.T, sdk *plugin.SDK, projectDir string) projectDetectResultForTest {
	t.Helper()

	req, err := plugin.NewProjectDetectRequest(projectDir)
	if err != nil {
		t.Fatalf("NewProjectDetectRequest() error = %v", err)
	}
	requestData, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal(RPCRequest) error = %v", err)
	}

	cmd := sdk.GetRPCCommand()
	var stdout bytes.Buffer
	cmd.SetIn(bytes.NewReader(requestData))
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var resp plugin.RPCResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal(RPCResponse) error = %v: %s", err, stdout.String())
	}
	if !resp.OK {
		t.Fatalf("project.detect failed: %+v", resp.Error)
	}
	var result projectDetectResultForTest
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("Unmarshal(project detect result) error = %v", err)
	}
	return result
}

type projectDetectResultForTest struct {
	Claimed    bool   `json:"claimed"`
	Plugin     string `json:"plugin"`
	ProjectDir string `json:"project_dir"`
	Reason     string `json:"reason"`
}
