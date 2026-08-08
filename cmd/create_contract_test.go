package cmd

import (
	"os"
	"strings"
	"testing"

	createpkg "github.com/libops/sitectl-isle/pkg/create"
	"github.com/libops/sitectl/pkg/plugin"
)

func TestCreateRuntimeDoesNotEmbedNontrivialLifecycleShellPrograms(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("create.go")
	if err != nil {
		t.Fatalf("ReadFile(create.go) error = %v", err)
	}
	source := string(data)
	if !strings.Contains(source, "EnsureComposeTemplateCheckoutContext") {
		t.Fatal("remote template checkout must delegate to the sitectl SDK")
	}
	for _, forbidden := range []string{
		`"bash", "-lc"`,
		"shellEnvPrefix(",
		"func startupCommand()",
		"attempt=1; until docker compose",
		"if [ ! -f .env ]",
		"until test -f /installed",
		"mv docker-compose.yml compose.yaml && sed",
		"if [ -d %s ] && [ -n",
		"cloneCommand := strings.Join",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("create runtime must delegate checked-in programs instead of containing %q", forbidden)
		}
	}
}

func TestCreateDefinitionUsesCheckedInTemplatePrograms(t *testing.T) {
	t.Parallel()
	spec := createDefinition()
	if spec.DockerComposeRepo != "https://github.com/libops/isle" || spec.DockerComposeBranch != "v1.3.0" {
		t.Fatalf("unexpected template contract: repo=%q branch=%q", spec.DockerComposeRepo, spec.DockerComposeBranch)
	}
	if len(spec.DockerComposeInit) != 3 || spec.DockerComposeInit[0] != "bash scripts/sitectl-prepare-init.sh" || spec.DockerComposeInit[1] != `docker compose run --rm -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" init` || spec.DockerComposeInit[2] != "bash scripts/sitectl-rollout-preflight.sh" {
		t.Fatalf("create init must separate checked-in preparation from the context-aware Compose command: %+v", spec.DockerComposeInit)
	}
	if len(spec.DockerComposeBuild) != 3 || spec.DockerComposeBuild[0] != "bash scripts/sitectl-prepare-build.sh" || spec.DockerComposeBuild[1] != "docker compose pull --ignore-buildable --ignore-pull-failures" || spec.DockerComposeBuild[2] != "docker compose build" {
		t.Fatalf("create build must separate checked-in preparation from context-aware Compose commands: %+v", spec.DockerComposeBuild)
	}
	if len(spec.Images) != 1 || spec.Images[0].Service != "drupal" || spec.Images[0].Image != createpkg.DefaultDrupalBaseImageRef || spec.Images[0].BuildPolicy != plugin.BuildPolicyAlways {
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
	if !strings.Contains(rollout, "sh /usr/local/lib/sitectl/drupal-wait-installed.sh") || strings.Contains(rollout, "until test -f /installed") {
		t.Fatalf("rollout must invoke the mounted Drupal readiness program:\n%s", rollout)
	}
	if !strings.Contains(rollout, "bash scripts/sitectl-rollout-preflight.sh") {
		t.Fatalf("rollout must fail safely on incompatible template checkouts:\n%s", rollout)
	}
	if !strings.Contains(rollout, "docker compose config --quiet") || !strings.Contains(rollout, "docker compose run --rm --no-deps --entrypoint test drupal -r "+rolloutWaitScriptTarget) {
		t.Fatalf("rollout must validate the effective context-aware Compose mount:\n%s", rollout)
	}
}

func TestCreateDefinitionRolloutRunsBoundedDrupalMigrations(t *testing.T) {
	t.Parallel()

	rollout := createDefinition().DockerComposeRollout
	if len(rollout) != 10 {
		t.Fatalf("rollout commands = %d, want 10: %+v", len(rollout), rollout)
	}
	for _, index := range []int{9} {
		command := rollout[index]
		if !strings.Contains(command, "docker compose up") || !strings.Contains(command, "--wait --wait-timeout 600") || !strings.Contains(command, "-d") {
			t.Fatalf("rollout command %d must use a bounded detached health wait: %q", index, command)
		}
	}
	for index, want := range map[int]string{
		0: "docker compose pull",
		1: "docker compose build --pull",
		2: "bash scripts/sitectl-rollout-preflight.sh",
		3: "docker compose config --quiet",
		4: "docker compose run --rm --no-deps --entrypoint test drupal -r /usr/local/lib/sitectl/drupal-wait-installed.sh",
		5: "docker compose up",
		6: "docker compose exec -T drupal sh /usr/local/lib/sitectl/drupal-wait-installed.sh",
		7: "docker compose exec -T --workdir /var/www/drupal drupal drush updb -y",
		8: "docker compose exec -T --workdir /var/www/drupal drupal drush cr",
		9: "docker compose up",
	} {
		if !strings.Contains(rollout[index], want) {
			t.Fatalf("rollout command %d = %q, want %q in order", index, rollout[index], want)
		}
	}
	if !strings.HasSuffix(rollout[5], " -d drupal") || strings.Contains(rollout[5], "--wait") || !strings.Contains(rollout[5], "--force-recreate") {
		t.Fatalf("initial migration start must target Drupal without exposing the full stack: %q", rollout[5])
	}
	for _, index := range []int{7, 8} {
		if strings.Contains(rollout[index], "||") {
			t.Fatalf("Drupal migration command %d must fail hard: %q", index, rollout[index])
		}
	}
}
