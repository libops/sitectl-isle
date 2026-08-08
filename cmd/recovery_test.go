package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestRecoveryCommandExposesCompleteOperatorWorkflow(t *testing.T) {
	names := make([]string, 0, len(recoveryCmd.Commands()))
	for _, command := range recoveryCmd.Commands() {
		names = append(names, command.Name())
	}
	if got := strings.Join(names, ","); got != "backup,plan,restore,validate" {
		t.Fatalf("recovery subcommands = %q", got)
	}
	var output bytes.Buffer
	recoveryPlanCmd.SetOut(&output)
	if err := recoveryPlanCmd.RunE(recoveryPlanCmd, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Authoritative (backed up)", "Rebuildable (not backed up)", "Vault backup", "sitectl verify --strict", "RPO/RTO"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("recovery plan missing %q:\n%s", want, output.String())
		}
	}
}
