# 11. Makefile to Justfile Migration

<!--
difficulty: intermediate
concepts:
  - Makefile vs justfile syntax comparison
  - .PHONY elimination
  - variable syntax translation
  - shell command substitution
  - default argument values
  - confirm attribute
  - migration patterns and pitfalls
tools: [just]
estimated_time: 30 minutes
bloom_level: analyze
prerequisites:
  - just basics (exercises 1-8)
  - basic Makefile familiarity
-->

## Prerequisites

| Tool | Version | Check |
|------|---------|-------|
| just | >= 1.0 | `just --version` |
| make | any | `make --version` |

## Learning Objectives

- **Analyze** the structural differences between Makefiles and justfiles to identify migration patterns
- **Apply** systematic translation rules for variables, shell commands, targets, and dependencies
- **Implement** a complete migration from a real-world Makefile, leveraging just-specific features like `[confirm]`, default arguments, and `set shell`

## Why Migrate from Make to Just

Make is a build system designed for compiling C programs in 1976. When teams repurpose it as a task runner, they fight against its file-based dependency model. Targets named `test` conflict with directories named `test`, requiring `.PHONY` declarations. Variables use `$(VAR)` syntax that collides with shell variables. Tab-vs-space indentation causes invisible, maddening errors.

just is purpose-built for running commands. Every recipe is implicitly "phony" -- there is no file-based dependency system to work around. Variables use `{{var}}` syntax that never conflicts with `$SHELL_VAR`. Indentation is flexible (spaces or tabs). Error messages reference line numbers and recipe names.

The migration itself is mechanical once you know the patterns. This exercise walks through a realistic Makefile, translates it piece by piece, and highlights the features that only just provides. By the end, you will have a reference table you can consult for any future migration.

## Step 1 -- The Original Makefile

Here is a typical project Makefile combining build, test, deploy, and utility tasks.

### `Makefile`

```makefile
.PHONY: all build test lint clean deploy docker-build docker-push help

SHELL := /bin/bash
APP_NAME := myservice
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
REGISTRY := ghcr.io/myorg
IMAGE := $(REGISTRY)/$(APP_NAME)
ENV ?= dev

all: lint test build

build:
	@echo "Building $(APP_NAME) $(VERSION)..."
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(APP_NAME) .

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ dist/ cover/

docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

docker-push: docker-build
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

deploy:
	@echo "Deploying $(APP_NAME) to $(ENV)..."
	kubectl apply -k deploy/overlays/$(ENV)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
```

Note the pain points: `.PHONY` for every target, `$(shell ...)` for command substitution, `?=` for defaults, `$$` escaping in the help recipe, tab-only indentation.

## Step 2 -- Translation Reference Table

Before translating, study this mapping:

| Makefile | justfile | Notes |
|----------|----------|-------|
| `.PHONY: target` | _(not needed)_ | All recipes are "phony" by default |
| `SHELL := /bin/bash` | `set shell := ["bash", "-euo", "pipefail", "-c"]` | Gains `pipefail` and `errexit` |
| `VAR := value` | `var := "value"` | just uses `{{var}}`, not `$(VAR)` |
| `VAR ?= default` | Recipe argument: `recipe var="default":` | Or `env("VAR", "default")` |
| `$(shell cmd)` | `` `cmd` `` (backticks) | Evaluated once at startup |
| `$(VAR)` in recipe | `{{var}}` | Double braces |
| `$$VAR` in recipe | `$VAR` | No double-dollar escaping needed |
| `@echo "msg"` | `@echo "msg"` | Same suppression syntax |
| `target: dep1 dep2` | `recipe: dep1 dep2` | Same dependency syntax |
| `help:` with grep/awk | `just --list` | Built-in, no custom recipe needed |
| _(no equivalent)_ | `[confirm]` | Interactive confirmation prompt |
| _(no equivalent)_ | `[group('name')]` | Recipe organization |

## Step 3 -- The Translated Justfile

Now translate the Makefile piece by piece.

### `justfile`

```just
set shell := ["bash", "-euo", "pipefail", "-c"]
set export

# --- Variables ---
# Make: APP_NAME := myservice
app_name := "myservice"

# Make: VERSION := $(shell git describe ...)
# just: backtick expression
version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
git_sha := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`

# Make: REGISTRY := ghcr.io/myorg
registry := "ghcr.io/myorg"
image    := registry + "/" + app_name

# Make: all: lint test build
# just: same dependency syntax, no .PHONY needed
[group('ci')]
all: lint test build

# Make: build:
#     go build -ldflags "-X main.version=$(VERSION)" -o bin/$(APP_NAME) .
[group('build')]
build:
    @echo "Building {{app_name}} {{version}}..."
    go build -ldflags "-X main.version={{version}}" -o bin/{{app_name}} .

# Make: test:
#     go test -race -count=1 ./...
[group('test')]
test *args:
    go test -race -count=1 ./... {{args}}

# Make: lint:
#     golangci-lint run ./...
[group('lint')]
lint:
    golangci-lint run ./...

# Make: clean:
#     rm -rf bin/ dist/ cover/
[group('build')]
clean:
    rm -rf bin/ dist/ cover/

# Make: docker-build:
#     docker build -t $(IMAGE):$(VERSION) ...
[group('docker')]
docker-build:
    docker build -t {{image}}:{{version}} -t {{image}}:latest .

# Make: docker-push: docker-build
#     docker push $(IMAGE):$(VERSION)
[group('docker')]
[confirm("Push images to registry? (yes/no)")]
docker-push: docker-build
    docker push {{image}}:{{version}}
    docker push {{image}}:latest

# Make: deploy:
#     kubectl apply -k deploy/overlays/$(ENV)
# just: ENV becomes a recipe argument with default
[group('deploy')]
[confirm("Deploy to {{env}}? (yes/no)")]
deploy env="dev":
    @echo "Deploying {{app_name}} to {{env}}..."
    kubectl apply -k deploy/overlays/{{env}}

# Make: help: (20 lines of grep/awk)
# just: built-in!
[group('help')]
default:
    @just --list --unsorted
```

**Intermediate Verification:**

```bash
just
```

You should see a clean list of all recipes, organized by group. Compare this to the 5-line `help` recipe in the Makefile.

## Step 4 -- Key Improvements Over the Makefile

Review each improvement and understand why it matters.

### 4a. Arguments with Defaults Replace `?=`

In Make, `ENV ?= dev` is a global variable overridden with `make deploy ENV=staging`. In just, this becomes a recipe-scoped argument:

```just
deploy env="dev":
    kubectl apply -k deploy/overlays/{{env}}
```

Invoked as `just deploy staging`. The argument is scoped to the recipe -- it cannot accidentally affect other recipes.

### 4b. `[confirm]` Prevents Accidental Destructive Operations

```just
[confirm("Deploy to {{env}}? (yes/no)")]
deploy env="dev":
    kubectl apply -k deploy/overlays/{{env}}
```

No Make equivalent exists. Teams typically add custom `@read -p "Are you sure?"` hacks.

### 4c. No Dollar-Sign Escaping

In Make, using shell variables inside a recipe requires `$$`:

```makefile
deploy:
	for f in $$FILES; do echo $$f; done
```

In just, shell variables use a single `$`:

```just
deploy:
    for f in $FILES; do echo $f; done
```

### 4d. Flexible Error Handling

`set shell := ["bash", "-euo", "pipefail", "-c"]` means every recipe line fails fast on errors. In Make, each line runs in a separate shell, and errors are silently ignored unless you chain with `&&`.

## Step 5 -- Handling Advanced Make Patterns

Some Makefile patterns require more thought to translate.

### Pattern: Conditional Logic

```makefile
ifdef CI
  BUILD_FLAGS += -v
endif
```

In just, use `env()` or a shebang recipe:

```just
build:
    #!/usr/bin/env bash
    set -euo pipefail
    FLAGS=""
    if [ -n "${CI:-}" ]; then
        FLAGS="-v"
    fi
    go build $FLAGS -o bin/app .
```

### Pattern: Includes

```makefile
include tools.mk
```

In just, use the `import` keyword:

```just
import 'tools.just'
```

### Pattern: Wildcard/File Targets

```makefile
%.o: %.c
    gcc -c $< -o $@
```

This has no direct equivalent in just. Justfiles are for task running, not file-based build systems. For file compilation, keep using Make or a proper build tool.

## Common Mistakes

### Mistake 1: Using `$(var)` Syntax in Justfiles

**Wrong:**

```just
app_name := "myapp"
build:
    echo "Building $(app_name)"
```

**What happens:** just does not expand `$(app_name)`. The shell interprets it as a subshell, which likely produces nothing or an error.

**Fix:** Use double-brace syntax:

```just
build:
    echo "Building {{app_name}}"
```

### Mistake 2: Expecting Each Line to Share Shell State

**Wrong (in Make):**

```makefile
deploy:
	cd deploy/
	kubectl apply -f .
```

This fails in Make because each line is a separate shell. The same is true in just (by default). Use a shebang recipe or chain with `&&`:

**Fix:**

```just
deploy:
    cd deploy/ && kubectl apply -f .
```

## Verify What You Learned

```bash
# 1. Compare help output
just
# Expected: clean grouped list (no grep/awk hack)

# 2. Check that deploy has a default argument
just --show deploy
# Expected: recipe signature shows env="dev"

# 3. Verify no .PHONY needed
just --show test
# Expected: recipe body with no phony declaration

# 4. Check confirm attribute on dangerous recipes
just --show docker-push
# Expected: [confirm] attribute visible
```

## What's Next

In the next exercise, you will learn how to manage multiple environments using dotenv files, environment switching recipes, and validation patterns.

## Summary

- All Make targets translate to just recipes; `.PHONY` is eliminated entirely
- `$(shell cmd)` becomes backtick expressions; `$(VAR)` becomes `{{var}}`
- `?=` defaults become recipe arguments with default values
- Dollar-sign escaping (`$$`) is no longer needed in just
- `[confirm]` provides built-in safety for destructive operations
- `just --list` replaces custom help targets
- File-based build patterns (wildcards, `%.o: %.c`) do not translate -- keep Make for those

## Reference

- [just manual -- recipe parameters](https://just.systems/man/en/recipe-parameters.html)
- [just manual -- confirm attribute](https://just.systems/man/en/confirm.html)
- [just manual -- import](https://just.systems/man/en/import.html)
- [just manual -- settings](https://just.systems/man/en/settings.html)

## Additional Resources

- [just vs Make comparison (just README)](https://github.com/casey/just#what-are-the-differences-between-just-and-make)
- [Migrating from Make to just (community guide)](https://github.com/casey/just/discussions)
