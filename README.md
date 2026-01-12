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

Usage:
  sitectl isle [command]

Available Commands:
  completion  Generate the autocompletion script for the specified shell
  help        Help about any command
  migrate     Migration helpers

Flags:
      --context string     The sitectl context to use. See sitectl config --help for more info (default "local")
  -h, --help               help for sitectl-isle
      --log-level string   The logging level for the command (default "DEBUG")
  -v, --version            version for sitectl-isle

Use "sitectl isle [command] --help" for more information about a command.
```
