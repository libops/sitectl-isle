package cmd

import (
	"strings"
	"testing"

	"github.com/libops/sitectl/pkg/plugin"
)

func TestCreateDefinitionUsesTemplateInitContract(t *testing.T) {
	t.Parallel()
	spec := createDefinition()
	if len(spec.DockerComposeInit) != 3 || !strings.Contains(spec.DockerComposeInit[0], "cp sample.env .env") || !strings.Contains(spec.DockerComposeInit[2], "docker compose run --rm") || !strings.Contains(spec.DockerComposeInit[2], "HOST_UID") {
		t.Fatalf("create init must use the shared Compose init service: %+v", spec.DockerComposeInit)
	}
	if !strings.Contains(spec.DockerComposeInit[0], "DRUPAL_HEALTHCHECK_START_PERIOD=5m") {
		t.Fatalf("create must allow enough startup time for development dependency installation: %+v", spec.DockerComposeInit)
	}
	if !strings.Contains(spec.DockerComposeInit[2], `"$attempt" -ge 3`) {
		t.Fatalf("create must tolerate transient certificate-tool download failures: %+v", spec.DockerComposeInit)
	}
	if len(spec.Images) != 1 || spec.Images[0].Service != "drupal" || spec.Images[0].Image != "libops/islandora:nginx-1.30.3-php84" || spec.Images[0].BuildPolicy != plugin.BuildPolicyAlways {
		t.Fatalf("the derived ISLE application image must always rebuild: %+v", spec.Images)
	}
	if len(spec.DockerComposeUp) != 1 || !strings.Contains(spec.DockerComposeUp[0], "--wait --wait-timeout 600") {
		t.Fatalf("create must wait for service health before reporting ready: %+v", spec.DockerComposeUp)
	}
	var foundTomcatPassword bool
	for _, artifact := range spec.InitArtifacts {
		foundTomcatPassword = foundTomcatPassword || artifact.Path == "secrets/TOMCAT_ADMIN_PASSWORD"
	}
	if !foundTomcatPassword {
		t.Fatalf("create init artifacts must include the Fedora Tomcat credential: %+v", spec.InitArtifacts)
	}
	rollout := strings.Join(spec.DockerComposeRollout, "\n")
	if !strings.Contains(rollout, "docker compose build --pull") || strings.Contains(rollout, "|| true") {
		t.Fatalf("rollout must rebuild and propagate failures:\n%s", rollout)
	}
}

func TestCreateDefinitionRolloutRunsBoundedDrupalMigrations(t *testing.T) {
	t.Parallel()

	rollout := createDefinition().DockerComposeRollout
	if len(rollout) != 7 {
		t.Fatalf("rollout commands = %d, want 7: %+v", len(rollout), rollout)
	}
	for _, index := range []int{6} {
		command := rollout[index]
		if !strings.Contains(command, "docker compose up") || !strings.Contains(command, "--wait --wait-timeout 600") || !strings.Contains(command, "-d") {
			t.Fatalf("rollout command %d must use a bounded detached health wait: %q", index, command)
		}
	}
	for index, want := range map[int]string{
		0: "docker compose pull",
		1: "docker compose build --pull",
		2: "docker compose up",
		3: "until test -f /installed",
		4: "docker compose exec -T --workdir /var/www/drupal drupal drush updb -y",
		5: "docker compose exec -T --workdir /var/www/drupal drupal drush cr",
		6: "docker compose up",
	} {
		if !strings.Contains(rollout[index], want) {
			t.Fatalf("rollout command %d = %q, want %q in order", index, rollout[index], want)
		}
	}
	if !strings.HasSuffix(rollout[2], " -d drupal") || strings.Contains(rollout[2], "--wait") {
		t.Fatalf("initial migration start must target Drupal without exposing the full stack: %q", rollout[2])
	}
	for _, index := range []int{4, 5} {
		if strings.Contains(rollout[index], "||") {
			t.Fatalf("Drupal migration command %d must fail hard: %q", index, rollout[index])
		}
	}
}
