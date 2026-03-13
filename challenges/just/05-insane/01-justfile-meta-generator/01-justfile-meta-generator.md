# 39. Justfile Meta-Generator

<!--
difficulty: insane
concepts: [meta-programming, code-generation, template-rendering, yaml-parsing, validation]
tools: [just, yq, sed, awk, bash]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- `yq` (v4+) installed for YAML parsing
- Familiarity with code generation patterns and template systems
- Understanding of different language ecosystems (Rust, Go, Python, Node)

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a meta-programming system that generates syntactically valid justfiles from declarative configuration
- **Design** a template architecture that cleanly separates recipe logic from project-specific customization
- **Evaluate** the correctness of generated build files through automated validation pipelines

## The Challenge

Build a justfile that is a factory for other justfiles. Given a YAML configuration
file describing a project — its language, enabled features, deployment targets, and
custom variables — your generator must produce a complete, ready-to-use justfile
tailored to that project.

This is fundamentally a meta-programming problem: you are writing just recipes that
emit just syntax. The difficulty lies in the layered quoting, conditional section
inclusion, and ensuring the generated output is not just textually correct but
semantically valid. A generated justfile for a Rust project with Docker and CI enabled
should look fundamentally different from a Python project with only linting — yet both
must be produced by the same generator logic.

Your YAML config file should support at minimum the following structure: a
`project_name` field, a `type` field (one of `rust`, `go`, `python`, `node`), a
`features` list (any combination of `docker`, `ci`, `deploy`, `lint`, `test`, `bench`),
a `vars` map for custom variables (like registry URLs or deploy targets), and an
optional `recipes` list for project-specific custom recipes with name, body, and
dependencies. The generator must read this YAML, decide which recipe blocks to include,
render variables into templates, and write the final justfile.

The hardest part is correctness. Just has specific formatting rules, indentation
requirements (recipes use either tabs or consistent spaces for body lines), and syntax
constraints. Your generated justfile must pass `just --fmt --check --unstable` without
modifications. This means your generator must respect just's formatting expectations
precisely — wrong indentation, missing blank lines between recipes, or malformed
variable assignments will cause validation failure.

Consider also composability: if both `docker` and `deploy` features are enabled, the
deploy recipes should reference docker image tags. If `ci` is enabled alongside `test`,
CI recipes should invoke test recipes as dependencies. The generator must reason about
feature interactions, not just concatenate independent blocks.

Finally, consider extensibility. The template system should be structured so that adding
support for a new language or a new feature does not require rewriting existing
templates. A well-designed generator has a clear separation between the rendering engine
(the recipe that reads YAML and assembles output) and the template fragments (the
per-language, per-feature recipe blocks). Aim for a design where adding Go support
means adding a Go template block, not touching the Python or Rust templates.

## Requirements

1. Define a YAML schema for project configuration supporting `project_name`, `type`
   (rust|go|python|node), `features` list, `vars` map, and optional custom `recipes`

2. Create a `generate` recipe that reads the YAML config and produces a complete
   justfile to stdout or a specified output path

3. Include language-specific recipe sets: `build`, `test`, `run`, `clean` must use the
   correct toolchain commands for each language (`cargo` for Rust, `go` for Go,
   `pip`/`pytest` for Python, `npm` for Node)

4. Feature-conditional sections: `docker` adds Dockerfile build/push/run recipes; `ci`
   adds lint+test+build pipeline recipes; `deploy` adds deployment recipes; `lint` adds
   language-appropriate linter invocations; `bench` adds benchmark recipes

5. Generated justfiles must include proper variable declarations at the top (project
   name, version, default shell configuration) and a `default` recipe listing available
   commands

6. Feature interactions must be handled: `deploy` + `docker` means deploy pushes a
   container image; `ci` + `test` means CI invokes the test recipe as a dependency;
   `lint` + `ci` means CI runs linting before tests

7. Custom recipes from the YAML config must be injected into the generated justfile
   with correct formatting, including their dependencies and parameters

8. The generated justfile must pass `just --fmt --check --unstable` validation without
   any manual edits

9. Implement a `validate` recipe that generates the justfile and immediately runs
   format-check plus a dry-run parse (`just --list` on the generated file)

10. Implement a `preview` recipe that generates and displays the justfile with syntax
    highlighting (if `bat` or `highlight` is available, falling back to plain `cat`)

11. Support an `--overwrite` flag (via just variable) that controls whether an existing
    output file is replaced or an error is raised

12. Include at least 4 sample YAML configs (one per language) in an `examples/`
    directory alongside the justfile, each demonstrating different feature combinations

## Hints

- The `just --fmt --check --unstable` command returns exit code 0 only if the file is
  already properly formatted — use this as your validation gate after every generation

- Remember that just recipe bodies must be indented; when generating multi-line recipe
  bodies from shell, pay close attention to how you emit leading whitespace — mixing
  tabs and spaces will cause parser errors

- `yq` can extract nested YAML fields as plain text, making it straightforward to
  branch on `type` and iterate over `features` without writing a full parser — for
  example, `yq '.features[]' config.yaml` gives you one feature per line

- Consider using heredocs within your generator recipes to define template blocks for
  each feature — this keeps the templates readable compared to long chains of `echo`
  statements

- `just --list --justfile <path>` will parse a justfile and list its recipes, serving
  as a quick semantic validation beyond just format-checking — a file that passes
  `--fmt --check` but fails `--list` has a syntax error

## Success Criteria

1. Running `just generate config=examples/rust-full.yaml` produces a justfile with
   Rust-specific `build`, `test`, `run`, `clean` recipes using `cargo` commands

2. The generated justfile for a Node project with `docker` and `deploy` features
   contains `docker-build`, `docker-push`, and `deploy` recipes where deploy depends
   on docker-push

3. `just --fmt --check --unstable --justfile <generated-file>` exits with code 0 for
   every generated justfile from every example config

4. `just --list --justfile <generated-file>` successfully parses and lists all recipes
   without errors for every generated justfile

5. Generating from a config with custom recipes correctly injects those recipes with
   proper dependencies and body formatting

6. Running `just validate config=examples/python-minimal.yaml` completes without
   errors, confirming both format and parse validity

7. The generator handles all four language types and all six feature flags in any
   combination without producing invalid output — including edge cases like all
   features enabled or no features enabled

8. Feature interactions are correctly resolved — enabling `deploy` + `docker` produces
   deploy recipes that reference docker image tags, not standalone deploy commands

## Research Resources

- [Just Manual - Variables and Substitution](https://just.systems/man/en/chapter_36.html)
  -- understanding variable declaration syntax your generator must produce

- [Just Manual - Recipe Parameters](https://just.systems/man/en/chapter_37.html)
  -- generating recipes that accept arguments correctly

- [Just Manual - Formatting](https://just.systems/man/en/chapter_55.html)
  -- the formatting rules your generated output must satisfy

- [yq Documentation](https://mikefarah.gitbook.io/yq/)
  -- YAML parsing from shell scripts for reading project configs

- [Just Manual - Shell Functions](https://just.systems/man/en/chapter_43.html)
  -- using shell blocks for complex generation logic

- [Heredoc Syntax in Bash](https://www.gnu.org/software/bash/manual/bash.html#Here-Documents)
  -- managing multi-line template output cleanly

## What's Next

Proceed to exercise 40, where you will tackle distributed execution across remote
machines via SSH orchestration.

## Summary

- **Meta-programming** -- generating syntactically and semantically valid build files from declarative configuration
- **Template composition** -- conditionally assembling recipe blocks based on feature flags and language type
- **Automated validation** -- using just's own tooling to verify generated output correctness
