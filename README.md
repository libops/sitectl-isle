# sitectl-isle

A [sitectl](https://github.com/libops/sitectl) plugin for Islandora (ISLE) utilities and migration tools.

## Install

### Homebrew

You can install `sitectl-isle` using homebrew

```bash
brew tap libops/homebrew https://github.com/libops/homebrew
brew install libops/homebrew/sitectl-isle
```

### Download Binary

Instead of homebrew, you can download a binary for your system from [the latest release of sitectl](https://github.com/libops/sitectl/releases/latest) and [this plugin](https://github.com/libops/sitectl-isle/releases/latest)

Then put the binary in a directory that is in your `$PATH`

## Usage

```bash
$ sitectl isle --help

  Islandora (ISLE) utilities and migration tools

  USAGE


    sitectl isle [command] [--flags]


  COMMANDS

    cache [command]       Cache warmer commands to speed up your ISLE site page load times
    completion [command]  Generate the autocompletion script for the specified shell
    component [command]   Inspect and manage ISLE components
    create [--flags]      Create a a new ISLE install
    help [command]        Help about any command
    migrate [command]     Migration helpers

  FLAGS

    --context             The sitectl context to use. See sitectl config --help for more info (isle-local)
    -h --help             Help for sitectl isle
    --log-level           The logging level for the command (DEBUG)
    -v --version          Version for sitectl isle
```

## Development

### sitectl

If you need to make code changes to sitectl, which provides a lot of helpers this plugin uses, you can use a local/altered copy of sitectl with go.work files. Use `make work` to create a local `go.work` that points this plugin at `../sitectl`. The file is intentionally gitignored so local development can use unreleased sitectl features without affecting CI or releases.

Use `make lint test` locally for the same lint and test invocation used in GitHub Actions.

Use `make integration-test FCREPO_STATE=off ISLE_FILE_SYSTEM_URI=public SITECTL_CONTEXT=isle-test` to run the end-to-end `create` test locally. This is the same script the GitHub Actions integration workflow runs.

`create` now supports `--template-repo`, `--template-branch`, and `--git-remote-url`. By default it clones `https://github.com/islandora-devops/isle-site-template` from `main`, creates a local sitectl context for the chosen working directory, asks the component questions, and then applies the requested component state. If `--git-remote-url` is provided, the template remote is kept as `upstream` and your repository is added as `origin`.

Both `create` and `component status` accept `--drupal-rootfs`. The shared `sitectl` component SDK defaults this to `./`, and the ISLE plugin overrides the default to `./drupal/rootfs/var/www/drupal` so Drupal-specific paths like `composer.json` and `config/sync` resolve correctly for the site template layout.

Use `sitectl isle component status --path /path/to/project` to inspect whether the currently supported components are on, off, or drifted.


### Component States

Some ISLE capabilities span more than one file or service. Changing a feature or stack state may require updates to:

- Service(s), volume(s), secret(s), service environment variables in `docker-compose.yml`
- Drupal `config/sync` YAML
- related follow-up actions such as config import
- helper text to make clear what turning a component on/off will impact future repository operations and maintenance

So components are useful for cases like `fcrepo` and `blazegraph`, but also for stack choices such as a minimal Drupal install versus a fuller stack with services like Solr, Memcached, Redis, or a specific database backend.

The default path is still the upstream project as-is. Component state changes are an advanced, explicit override for users who want to customize that baseline.

Because these changes can be destructive, reconciliation is local-only and protected by a confirmation gate. Use `--yolo` only for automation or when you have already reviewed the impact.

Each component definition also records operational metadata so commands can explain whether a change is idempotent, whether it requires a backfill, whether it requires a hard data migration before it is safe to apply, and which Drupal modules must be present when the component is enabled. Module dependencies can be marked as strict or enable-only so future component transitions do not assume every disabled component must uninstall its modules.

### Planned Components

Current components:

- `fcrepo`
- `blazegraph`

Planned components include:

- PostgreSQL
- Memcached
- Redis
- self-managed TLS certificates
- Let's Encrypt TLS certificates
- load balancer support
- `mergepdf`

These will be added incrementally in separate PRs as their component definitions and migration requirements are finalized.
