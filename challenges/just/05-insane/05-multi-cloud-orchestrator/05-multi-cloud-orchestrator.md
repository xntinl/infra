# 43. Multi-Cloud Orchestrator

<!--
difficulty: insane
concepts: [multi-cloud, cli-abstraction, parallel-provisioning, resource-unification, graceful-degradation]
tools: [just, aws-cli, gcloud, az, bash, jq]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- At least one cloud CLI installed (`aws`, `gcloud`, or `az`) — the system must work
  with any subset including none (dry-run/simulation mode)
- `jq` installed for JSON processing
- Understanding of basic cloud concepts: compute instances, storage, networking, IAM

## Learning Objectives

After completing this challenge, you will be able to:

- **Architect** a cloud-agnostic abstraction layer that maps unified commands to
  cloud-specific CLI invocations
- **Evaluate** multi-cloud provisioning strategies, including parallel execution, error
  handling, and graceful degradation when a cloud provider is unavailable

## The Challenge

Build a single justfile that manages infrastructure across AWS, GCP, and Azure
simultaneously. The core idea is a unified resource abstraction: you define what you
want (a compute instance, a storage bucket, a DNS record) and the system translates it
into the correct CLI commands for each target cloud, provisions in parallel, and
presents a unified view of your multi-cloud estate.

The first major challenge is CLI detection and authentication. Not every developer has
all three CLIs installed, and not every environment is authenticated to all three
clouds. Your system must detect which CLIs are available (`command -v aws`, etc.),
verify authentication status for each (e.g., `aws sts get-caller-identity`), and
gracefully skip unavailable or unauthenticated clouds rather than failing entirely. A
`status` recipe should report: which clouds are available, authenticated, and which
region/project/subscription is active for each.

The second challenge is resource abstraction. You need a unified naming and tagging
convention that works across all three clouds. Given a logical resource name like
`web-server-01`, the system must construct appropriate names for each cloud (respecting
each cloud's naming constraints), apply consistent tags/labels, and store a mapping
between logical names and cloud-specific resource identifiers. A single YAML or JSON
definition should describe the desired resources with cloud-agnostic attributes that
get translated into cloud-specific parameters.

Each cloud has different naming rules, different API semantics, and different ways of
expressing the same concept. AWS uses "tags," GCP uses "labels," Azure uses "tags" but
with different key/value constraints. AWS identifies resources by ARN, GCP by resource
path, Azure by resource ID. Compute instance sizes have different names: `t3.micro` vs
`e2-micro` vs `Standard_B1s`. Your abstraction layer must handle all of these
translations transparently.

Parallel provisioning is the third challenge. When deploying to all three clouds,
operations should run concurrently. But cloud APIs have different speeds, rate limits,
and failure modes. Your system must handle the case where AWS provisioning succeeds in
30 seconds, GCP takes 2 minutes, and Azure fails entirely — reporting partial success
clearly and offering retry for failed clouds without re-running successful ones.

Cost awareness ties it together. Implement a recipe that queries each cloud's pricing
(or uses hardcoded pricing tables if API access is unavailable) to estimate the monthly
cost of provisioned resources, presenting a side-by-side comparison across clouds. This
helps teams make informed decisions about where to place workloads.

## Requirements

1. Implement cloud CLI detection: check for `aws`, `gcloud`, and `az` binaries and
   report their versions; skip unavailable clouds gracefully throughout all recipes

2. Implement authentication verification for each available cloud:
   `aws sts get-caller-identity`, `gcloud auth print-identity-token`,
   `az account show` — report authenticated status per cloud

3. Create a `status` recipe that displays a dashboard: for each cloud, show
   availability, auth status, active region/project/subscription, and number of
   managed resources

4. Define a unified resource schema in YAML: logical name, resource type (compute,
   storage, database), size/tier, region preference, tags — with optional cloud-specific
   attribute overrides

5. Implement `provision` recipe that reads the resource schema and provisions resources
   to all available clouds in parallel, collecting results from each into a unified
   report

6. Generate cloud-specific resource names from logical names following each cloud's
   naming conventions (AWS: hyphens allowed, max 63 chars; GCP: lowercase+hyphens,
   max 63; Azure: varies by resource type)

7. Implement a resource registry (JSON file) mapping logical names to cloud-specific
   identifiers (ARNs, GCP resource paths, Azure resource IDs) with creation timestamps
   and provisioning status per cloud

8. Handle partial failure: if provisioning fails on one cloud but succeeds on others,
   report the mixed result clearly and support `just retry cloud=azure` to re-attempt
   only the failed cloud without touching successful ones

9. Implement `teardown` recipe that destroys all managed resources across all clouds,
   respecting dependency order, with a confirmation prompt that shows what will be
   destroyed

10. Implement `cost-estimate` recipe that estimates monthly cost per resource per cloud,
    presenting a comparison table with totals per cloud (use hardcoded pricing tables
    if API access is unavailable)

11. Implement `list` recipe showing all managed resources across clouds in a unified
    table: logical name, cloud, type, status, region, resource ID, and cost estimate

12. Support targeting specific clouds: `CLOUDS="aws,gcp" just provision` limits
    operations to only the specified clouds, ignoring Azure even if available

## Hints

- `command -v aws >/dev/null 2>&1` is more portable than `which` for detecting
  available CLIs, and it works in both bash and POSIX shells

- For parallel provisioning across clouds, run each cloud's commands as a background
  process with output redirected to cloud-specific log files, then `wait` for all and
  collect exit codes — this gives you per-cloud success/failure tracking

- Consider using a `clouds` just variable with a default value of `"auto"` (detect
  available) that can be overridden to a comma-separated list for targeting specific
  clouds

- AWS uses `--output json`, GCP uses `--format=json`, and Azure uses `--output json` —
  normalizing output to JSON with `jq` makes cross-cloud response parsing feasible

- The resource registry file is your source of truth for teardown and retry: iterate
  over it to find what needs to be destroyed or re-provisioned, and update entries as
  operations complete

## Success Criteria

1. `just status` correctly detects available CLIs, authentication status, and active
   configuration for each cloud, gracefully noting unavailable clouds without error

2. Running `just provision` with a multi-resource YAML config provisions to all
   available clouds in parallel, completing in roughly the time of the slowest cloud
   (not the sum of all)

3. The resource registry correctly maps logical names to cloud-specific resource
   identifiers after provisioning, including per-cloud status

4. If one cloud fails during provisioning, the others still succeed, and the failure is
   clearly reported with the cloud name, error message, and retry instructions

5. `just retry cloud=azure` re-provisions only the resources that failed on Azure
   without affecting resources already successfully provisioned on other clouds

6. `just teardown` removes all managed resources from all clouds and clears the
   resource registry, with a confirmation step listing everything to be destroyed

7. `just cost-estimate` displays a per-resource, per-cloud cost comparison table with
   monthly totals

8. `CLOUDS="aws" just provision` provisions only to AWS, ignoring GCP and Azure even
   if their CLIs are available and authenticated

## Research Resources

- [Just Manual - Environment Variables](https://just.systems/man/en/chapter_40.html)
  -- using env vars to control cloud targeting and configuration

- [AWS CLI Command Reference](https://awscli.amazonaws.com/v2/documentation/api/latest/index.html)
  -- AWS resource provisioning commands and output formatting

- [Google Cloud CLI Reference](https://cloud.google.com/sdk/gcloud/reference)
  -- GCP resource provisioning commands and JSON output

- [Azure CLI Reference](https://learn.microsoft.com/en-us/cli/azure/reference-index)
  -- Azure resource provisioning commands and output formatting

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- multi-line shell blocks for complex provisioning logic

- [jq Cookbook](https://github.com/stedolan/jq/wiki/Cookbook)
  -- JSON manipulation patterns for normalizing multi-cloud API responses

## What's Next

Proceed to exercise 44, where you will build a self-testing justfile that validates its
own recipes through an integrated test suite.

## Summary

- **Multi-cloud abstraction** -- mapping unified resource definitions to cloud-specific CLI commands across AWS, GCP, and Azure
- **Graceful degradation** -- handling missing CLIs, authentication failures, and partial provisioning failures without crashing
- **Parallel provisioning** -- concurrent cross-cloud operations with per-cloud result tracking and retry support
