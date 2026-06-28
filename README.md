# sitectl-isle

`sitectl-isle` adds Islandora create metadata, component changes, Fedora and Blazegraph topology options, derivative service controls, cache helpers, sync and migration tools, validation, and health checks to [`sitectl`](https://sitectl.libops.io). It works with the [LibOps ISLE template](https://github.com/libops/isle).

Documentation: https://sitectl.libops.io/plugins/isle

## Requirements

- [`sitectl`](https://sitectl.libops.io/install).
- Docker with the Compose v2 plugin for local ISLE sites.
- [`sitectl-drupal`](https://github.com/libops/sitectl-drupal), because ISLE includes and extends the Drupal plugin surface.

## Quick Start

Create a local ISLE site from the matching template:

```bash
sitectl create isle/default \
  --template-repo https://github.com/libops/isle \
  --path ./my-isle-site \
  --type local \
  --checkout-source template \
  --default-context
```

The template README is at https://github.com/libops/isle.

## Basic Operations

Use [`sitectl compose`](https://sitectl.libops.io/commands/compose) to start or inspect the stack:

```bash
sitectl compose up --remove-orphans -d
```

Use [`sitectl healthcheck`](https://sitectl.libops.io/commands/healthcheck) and [`sitectl validate`](https://sitectl.libops.io/commands/validate) to check the site:

```bash
sitectl healthcheck
sitectl validate
```

Use [`sitectl image`](https://sitectl.libops.io/commands/image) for local image or build-arg overrides:

```bash
sitectl image set --tag drupal=nginx-1.30.3-php84 --tag solr=9
```

Use [`sitectl set`](https://sitectl.libops.io/commands/set) and [`sitectl converge`](https://sitectl.libops.io/commands/converge) for component changes:

```bash
sitectl set bot-mitigation on
sitectl converge
```

See the [ISLE plugin docs](https://sitectl.libops.io/plugins/isle) for Fedora, Blazegraph, IIIF, derivatives, sync, migration, cache, TLS, and bot mitigation details.

## License

`sitectl-isle` is licensed under the MIT License.
