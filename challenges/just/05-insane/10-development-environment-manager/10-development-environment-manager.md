# 48. Development Environment Manager

<!--
difficulty: insane
concepts: [environment-as-code, tool-management, service-orchestration, version-pinning, health-monitoring, scaffolding]
tools: [just, bash, curl, docker, openssl, dnsmasq]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Docker installed for background service management
- `curl` for downloading tools
- Familiarity with development toolchains across at least two languages
- Understanding of local service orchestration concepts (databases, caches, message
  queues)

## Learning Objectives

After completing this challenge, you will be able to:

- **Create** a comprehensive development environment manager that codifies tool
  installation, service lifecycle, and project scaffolding into reproducible recipes
- **Design** a health monitoring dashboard that provides at-a-glance visibility into all
  components of a local development environment

## The Challenge

Build a "development environment as code" system — a single justfile that sets up,
manages, and tears down everything a developer needs to work on a project. This goes
far beyond `docker-compose up`: it manages tool installation with version pinning,
scaffolds new projects, runs background services, provides health monitoring, handles
local DNS and SSL certificates, and tears down cleanly.

Tool management is the foundation. Your system must install specific versions of
development tools (language runtimes, CLIs, formatters, linters) into an isolated
directory (not polluting the system-wide installation). Think of it as a lightweight
`asdf` or `mise` built with just recipes. A `tools.yaml` manifest declares required
tools with exact versions and optional SHA256 checksums. The `install-tools` recipe
downloads and installs each tool, verifying checksums where provided. A `check-tools`
recipe verifies all required tools are installed at the correct versions and reports
mismatches. Tools should be installed to `.tools/` and made available via PATH
manipulation within just recipes.

Project scaffolding generates new project structures from templates. Given a language
and project type (e.g., `rust-api`, `python-cli`, `node-webapp`, `go-lib`), the system
generates a complete project skeleton: directory structure, build configuration (Cargo.toml,
package.json, go.mod, etc.), CI pipeline (.github/workflows/ci.yml), Dockerfile,
justfile for the new project, .gitignore, and a starter README. Templates should be
organized so that adding support for a new language or project type is a matter of
adding a template directory, not modifying the scaffolding engine.

Background service management is where the real orchestration lives. Development
projects need databases, caches, message queues, and other infrastructure running
locally. Your system must start these services (via Docker containers), manage their
lifecycle (start, stop, restart, logs, status), handle port allocation (detect and
avoid conflicts with already-bound ports), persist data across restarts (Docker volume
management), and provide connection strings as environment variables. Services should
be defined declaratively in a `services.yaml` file with dependencies between them
(e.g., the API gateway starts after the database is healthy).

The health dashboard pulls it all together. A `dashboard` recipe displays an
on-demand overview: which tools are installed and at correct versions (with a
checkmark or X), which services are running and healthy (with port mappings), disk
usage of persistent Docker volumes, uptime per service, and any warnings (services
using more than 80% of allocated memory, tools out of date). This is the developer's
single pane of glass for their local environment.

Local SSL certificate generation enables HTTPS development. Your system must generate
a local Certificate Authority (CA), create signed certificates for development domains
(e.g., `*.dev.local`, `api.dev.local`), and output instructions for trusting the CA
in the browser/system trust store. This is essential for testing OAuth flows, secure
WebSocket connections, and other HTTPS-dependent features locally without browser
warnings.

## Requirements

1. Define a `tools.yaml` schema for declaring required tools: name, version, download
   URL template (with `{version}` and `{os}` placeholders), optional SHA256 checksum,
   and binary name; implement `install-tools` to download and install each to `.tools/`

2. Implement `check-tools` recipe that verifies all required tools are installed at the
   pinned versions, reporting mismatches (expected vs found), missing tools, and
   checksum failures

3. Add the `.tools/bin` directory to PATH within just recipes using `export PATH`
   so all subsequent recipes automatically use the pinned tool versions instead of
   system versions

4. Define a `services.yaml` schema for background services: name, Docker image with
   tag, port mappings, environment variables, volumes, health-check command, startup
   dependencies on other services, and memory limit

5. Implement `services-up` recipe that starts all services in dependency order using
   Docker, waits for each service's health check to pass (with timeout), and reports
   connection details (host, port, credentials, connection URL)

6. Implement `services-down` recipe that stops all services gracefully, and
   `services-restart name=<service>` for individual service restart without affecting
   other services

7. Implement port conflict detection: before starting a service, check if its target
   port is already in use and provide a clear error with the conflicting process name
   and PID

8. Implement `scaffold` recipe accepting `lang` (rust, python, go, node) and `type`
   (api, cli, webapp, lib) that generates a complete project skeleton with language-
   appropriate build config, Dockerfile, CI pipeline, justfile, and .gitignore

9. Implement `ssl-setup` recipe that generates a local Certificate Authority, creates
   signed certificates for configurable development domains (defaulting to
   `*.dev.local`), and outputs the CA certificate path with trust-store import
   instructions for macOS and Linux

10. Implement `dashboard` recipe displaying: tool status table (name, expected version,
    installed version, status), service status table (name, status, ports, health,
    uptime, memory usage), disk usage of Docker volumes, and any warnings

11. Implement `teardown` recipe that stops all services, optionally removes Docker
    volumes (with confirmation prompt listing data that will be lost), and optionally
    removes installed tools — providing a complete clean slate

12. Implement `export-env` recipe that outputs shell export statements for all service
    connection strings (`DATABASE_URL`, `REDIS_URL`, `AMQP_URL`, etc.) for sourcing
    into the developer's shell: `eval $(just export-env)`

## Hints

- `docker inspect --format '{{.State.Health.Status}}' container_name` gives you
  container health status if a HEALTHCHECK is defined in the image; for containers
  without one, `docker exec container_name <command>` serves as an ad-hoc health check
  (e.g., `pg_isready` for PostgreSQL)

- For tool installation, `curl -fSL -o /path/to/binary "$url" && chmod +x /path/to/binary`
  handles most single-binary tools; for archives, pipe through
  `tar xz --strip-components=1 -C .tools/` to extract to the tools directory

- `lsof -i :$port` or `ss -tlnp | grep :$port` detects port conflicts before service
  startup — parse the output to extract the PID and process name for a helpful error
  message

- `openssl req -x509 -new -nodes -key ca.key -sha256 -days 365 -out ca.pem` creates a
  CA certificate; `openssl req -new -key domain.key | openssl x509 -req -CA ca.pem -CAkey ca.key`
  signs domain certificates — store all certs in a `.certs/` directory

- For the dashboard, `printf '%-20s %-10s %-10s\n'` with format strings creates clean
  terminal tables; ANSI color codes (`\033[32m` for green, `\033[31m` for red`) add
  visual distinction between healthy and unhealthy states

## Success Criteria

1. `just install-tools` downloads and installs all tools declared in `tools.yaml` to
   `.tools/`, and `just check-tools` reports all versions match with no mismatches

2. `just services-up` starts all declared services in dependency order, waits for
   health checks, and displays connection details (URLs, credentials) for each service

3. Port conflict detection prevents starting a service on an occupied port, reporting
   the conflicting process name and PID clearly

4. `just scaffold lang=rust type=api` generates a complete Rust API project skeleton
   with `Cargo.toml`, `src/main.rs`, `Dockerfile`, `.github/workflows/ci.yml`, and
   a working justfile

5. `just ssl-setup` generates a local CA and domain certificates that can be verified
   with `openssl verify -CAfile .certs/ca.pem .certs/dev.local.pem`

6. `just dashboard` displays a comprehensive, formatted view of all tools and services
   with their status, ports, health, and resource usage

7. `eval $(just export-env)` sets environment variables for all service connection
   strings in the current shell, usable by application code immediately

8. `just teardown` stops all services, cleans up volumes (with confirmation), and
   leaves the system in a clean state with no running containers or orphaned volumes

## Research Resources

- [Just Manual - Environment Variables and Export](https://just.systems/man/en/chapter_40.html)
  -- exporting PATH modifications and service connection strings

- [Docker CLI Reference](https://docs.docker.com/reference/cli/docker/)
  -- container lifecycle management, health checking, and volume operations

- [OpenSSL Cookbook](https://www.feistyduck.com/library/openssl-cookbook/)
  -- certificate generation, CA creation, and certificate signing procedures

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- complex multi-step installation, service management, and setup logic

- [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/latest/)
  -- conventions for local tool and data storage paths

- [12-Factor App: Dev/Prod Parity](https://12factor.net/dev-prod-parity)
  -- philosophy behind local development environments that mirror production

## What's Next

Proceed to exercise 49, where you will build a compliance audit system that scans
infrastructure configurations against security benchmarks.

## Summary

- **Environment as code** -- declaratively defining tools, services, and configurations for reproducible development setups
- **Service orchestration** -- managing Docker containers with dependency ordering, health checks, and port conflict detection
- **Developer experience** -- scaffolding, SSL certificates, connection string export, and health dashboards for frictionless development
