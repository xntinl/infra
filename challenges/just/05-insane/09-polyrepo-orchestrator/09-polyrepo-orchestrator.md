# 47. Polyrepo Orchestrator

<!--
difficulty: insane
concepts: [multi-repo-management, dependency-ordering, parallel-git-operations, version-compatibility, synchronized-release, changelog-generation]
tools: [just, git, bash, jq, semver]
estimated_time: 3h-4h
bloom_level: create
prerequisites: [exercises 1-38]
-->

## Prerequisites

- Completed exercises 1-38 or equivalent experience
- Git installed with familiarity in branching, tagging, and log formatting
- Understanding of semantic versioning and multi-project dependency management
- Access to at least 3 Git repositories (local or remote) for testing — can be created
  as part of setup

## Learning Objectives

After completing this challenge, you will be able to:

- **Architect** a centralized management system for operations spanning multiple
  independent Git repositories with inter-repo dependencies
- **Evaluate** version compatibility constraints across repositories and design
  synchronized release strategies that either fully succeed or fully roll back

## The Challenge

Build a polyrepo orchestrator: a single justfile that manages operations across 5 or
more independent Git repositories. In a polyrepo architecture, each service or library
lives in its own repository. This provides isolation and independent deployment, but
introduces coordination complexity: how do you run tests across all repos? How do you
ensure a library update does not break its consumers? How do you release a coordinated
set of changes across repos atomically?

Your orchestrator must first manage the basics: clone all repos (or update them if
already cloned), switch branches, run commands across repos. These repos are defined in
a manifest file that lists each repo's URL, local path, default branch, and
dependencies on other repos (e.g., "service-api depends on shared-lib v2.x"). The
manifest is the single source of truth for the polyrepo topology.

Dependency ordering is where complexity emerges. If `service-api` depends on
`shared-lib`, and `shared-lib` depends on `core-types`, then building must proceed in
order: `core-types` -> `shared-lib` -> `service-api`. Your orchestrator must compute
this order, detect cycles, and parallelize where possible (repos at the same dependency
level can build concurrently). This is the same topological sort concept from exercise
41, but applied to Git repositories with real version constraints.

Version compatibility checking goes deeper. Each repo declares which versions of its
dependencies it supports (in its manifest entry or a local version file). Before running
a coordinated build, the orchestrator must verify that the checked-out version of each
repo is compatible with all repos that depend on it. If `service-api` requires
`shared-lib >=2.0 <3.0` but `shared-lib` is at `3.1.0`, this must be flagged before
any build is attempted. This prevents wasted build time on configurations that will
fail at link or runtime.

Synchronized release is the ultimate challenge. When releasing a new version of
`shared-lib`, all consuming repos should be updated to reference the new version. The
orchestrator must: tag `shared-lib` with the new version, update version references in
all dependent repos, create commits in those repos, run tests across all affected repos,
and either complete the release (tag everything) or roll back (reset all repos) based on
test results. This is a distributed transaction across Git repositories — it must be
all-or-nothing.

Unified changelog generation pulls commit messages from all repos since the last
synchronized release, organizes them by repo and by conventional commit type (feat, fix,
chore, docs, refactor), and produces a single changelog covering the entire system.
This gives stakeholders a single document showing everything that changed across the
entire product, not fragmented per-repo changelogs.

## Requirements

1. Define a YAML manifest format listing repos with: name, git URL, local clone path,
   default branch, dependencies (list of `{repo: name, version: "semver-range"}`), and
   optional build/test commands specific to each repo

2. Implement `setup` recipe that clones all repos from the manifest (or pulls latest if
   already cloned), reporting progress per repo and any clone/pull failures

3. Implement `run-all` recipe that executes an arbitrary command in every repo in
   parallel, collecting and summarizing results (exit codes, first line of output,
   duration)

4. Implement `run-ordered` recipe that executes a command across repos in dependency
   order, running each dependency level in parallel but waiting for all repos at one
   level to complete before proceeding to the next level

5. Compute dependency ordering via topological sort; detect and report circular
   dependencies with the specific cycle path (e.g., "A -> B -> C -> A")

6. Implement `check-versions` recipe that verifies all inter-repo version constraints
   are satisfied by the currently checked-out versions, reporting specific violations
   with expected vs actual version

7. Implement `status` recipe showing for each repo: current branch, last commit hash
   and subject, uncommitted changes count, and whether it is ahead/behind its remote
   tracking branch

8. Implement `release` recipe for synchronized multi-repo release: bump versions in
   dependency order, update version references in dependents, commit changes, run
   tests, and tag all repos if tests pass — or rollback all changes if any test fails

9. Implement `rollback-release` recipe that undoes a synchronized release: delete tags,
   reset commits across all affected repos, restoring them to their pre-release state

10. Implement `changelog` recipe that generates a unified changelog from all repos
    since the last release tag, organized by repo and categorized by conventional commit
    type (feat, fix, docs, chore, refactor, test)

11. Support branch operations across repos: `just branch-all name=feature/xyz` creates
    a branch with the same name in all repos, `just checkout-all branch=main` switches
    all repos to the specified branch

12. Implement `diff-since` recipe that shows all changes across all repos since a given
    date or tag, summarized per repo with file count, insertion count, and deletion
    count

## Hints

- `git -C path/to/repo` lets you run Git commands in a specific directory without `cd`
  — essential for managing multiple repos from one justfile without changing the working
  directory

- For topological sort with parallelism, group repos by "depth" in the dependency
  graph: depth-0 repos have no dependencies (build first, in parallel), depth-1 repos
  depend only on depth-0 (build next, in parallel), and so on — this maximizes
  concurrency while respecting ordering

- Semantic version comparison in bash is non-trivial; consider extracting
  major.minor.patch with `IFS='.' read -r major minor patch <<< "$version"` and
  comparing numerically, or use a helper script

- For synchronized release rollback, `git tag -d tagname` and `git reset --hard HEAD~1`
  in each repo undoes the release — but you must track which repos were modified to
  know where to rollback, and skip repos that were not touched

- `git log --oneline --format="%s" v1.0.0..HEAD` in each repo gives you commit messages
  since the last tag — filter by conventional commit prefix (`feat:`, `fix:`, etc.) to
  categorize

## Success Criteria

1. `just setup` clones all repos defined in the manifest, or updates them if already
   present, reporting per-repo status and any errors

2. `just run-all cmd="git status --short"` executes in all repos in parallel and
   displays a summary table with results from each repo

3. `just run-ordered cmd="make build"` builds repos in correct dependency order, with
   repos at the same level running in parallel

4. `just check-versions` correctly identifies version constraint violations: e.g., if
   repo A requires B >=2.0 but B is at 1.9.3, this is reported with both versions

5. `just release version=2.1.0` performs a coordinated release: version bumps, reference
   updates, commits, tests, and tagging — all in correct dependency order

6. If a test fails during `just release`, all repos are rolled back to their pre-release
   state with no dangling tags or partial commits remaining

7. `just changelog since=v2.0.0` produces a unified, categorized changelog covering
   commits from all repos since v2.0.0, organized by repo and commit type

8. `just status` provides a clear overview of all repos' states showing branch, last
   commit, uncommitted changes, and remote sync status

## Research Resources

- [Just Manual - Shell Recipes](https://just.systems/man/en/chapter_44.html)
  -- multi-line shell blocks for complex Git orchestration across repos

- [Semantic Versioning Specification](https://semver.org/)
  -- version format, comparison rules, and range specification

- [Conventional Commits](https://www.conventionalcommits.org/)
  -- commit message format for automated changelog categorization

- [Git Tagging](https://git-scm.com/book/en/v2/Git-Basics-Tagging)
  -- creating and managing release tags for version tracking

- [Just Manual - Backtick Expressions](https://just.systems/man/en/chapter_34.html)
  -- capturing command output in variables for version extraction and comparison

- [Topological Sort - Wikipedia](https://en.wikipedia.org/wiki/Topological_sorting)
  -- dependency ordering algorithms for build and release sequencing

## What's Next

Proceed to exercise 48, where you will build a comprehensive development environment
manager that handles tool installation, service management, and project scaffolding.

## Summary

- **Polyrepo orchestration** -- coordinating operations across multiple independent Git repositories from a single command center
- **Version compatibility** -- checking semantic version constraints across inter-repo dependencies before builds or releases
- **Synchronized release** -- atomic multi-repo version bumps, testing, and tagging with full rollback on failure
