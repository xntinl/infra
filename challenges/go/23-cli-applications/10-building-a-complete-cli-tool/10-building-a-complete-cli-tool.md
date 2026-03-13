# 10. Building a Complete CLI Tool

<!--
difficulty: insane
concepts: [cli-architecture, cobra-full-app, config-management, persistent-storage, output-formatting, error-handling, shell-completion, interactive-mode]
tools: [go, cobra, sqlite]
estimated_time: 90m
bloom_level: create
prerequisites: [cobra-commands-flags-args, interactive-prompts, progress-bars-and-spinners, output-formatting, config-loading, shell-completion-generation]
-->

## The Challenge

Build `notectl` -- a fully-featured command-line note-taking application. This tool combines every CLI pattern from the preceding exercises into a single, production-quality application: subcommands, flags, configuration files, persistent storage, multiple output formats, interactive prompts, shell completion, and graceful error handling.

The tool should feel as polished as `gh`, `kubectl`, or `docker` -- consistent flag naming, helpful error messages, discoverable subcommands, and Tab completion.

## Requirements

### Core CRUD Operations

1. `notectl add "Title" --tag=work --tag=urgent` -- create a note with tags
2. `notectl list` -- list all notes (table format by default)
3. `notectl list --tag=work` -- filter by tag
4. `notectl list --format=json` -- output as JSON
5. `notectl list --format=csv` -- output as CSV
6. `notectl show <id>` -- display a single note with full content
7. `notectl edit <id>` -- open note in `$EDITOR` for editing
8. `notectl delete <id>` -- delete a note (with confirmation prompt)
9. `notectl search <query>` -- full-text search across titles and bodies

### Organization

10. `notectl tag list` -- list all tags with note counts
11. `notectl tag rename <old> <new>` -- rename a tag across all notes
12. `notectl export --format=json > backup.json` -- export all notes
13. `notectl import < backup.json` -- import notes from JSON

### Configuration

14. Load config from `~/.config/notectl/config.yaml` with defaults
15. Config includes: database path, default output format, editor, color preference
16. `notectl config show` -- display current configuration
17. `notectl config set <key> <value>` -- update a config value

### Polish

18. All commands support `--no-color` and `--quiet` flags (via persistent flags on root)
19. Shell completion for bash, zsh, and fish via `notectl completion <shell>`
20. Dynamic completion for note IDs, tag names, and config keys
21. All errors produce helpful, actionable messages (not stack traces)
22. `notectl version` shows version, commit hash, and build date (set via ldflags)

### Storage

23. SQLite database with a `notes` table and a `note_tags` join table
24. Automatic database creation and schema migration on first run
25. WAL mode enabled for concurrent read safety

## Hints

<details>
<summary>Hint 1: Project structure</summary>

```
notectl/
  main.go
  cmd/
    root.go       -- root command, persistent flags, config loading
    add.go        -- add subcommand
    list.go       -- list subcommand
    show.go       -- show subcommand
    edit.go       -- edit subcommand
    delete.go     -- delete subcommand
    search.go     -- search subcommand
    tag.go        -- tag subcommand group
    export.go     -- export subcommand
    import.go     -- import subcommand
    config.go     -- config subcommand group
    completion.go -- completion subcommand
    version.go    -- version subcommand
  internal/
    db/
      db.go       -- database setup, migrations
      notes.go    -- note CRUD operations
      tags.go     -- tag operations
    config/
      config.go   -- config loading, defaults, persistence
    format/
      table.go    -- table output formatter
      json.go     -- JSON output formatter
      csv.go      -- CSV output formatter
    model/
      note.go     -- Note and Tag types
```
</details>

<details>
<summary>Hint 2: Root command with persistent flags</summary>

```go
var rootCmd = &cobra.Command{
    Use:   "notectl",
    Short: "A command-line note-taking tool",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // Load config, open database
        cfg, err := config.Load()
        if err != nil {
            return err
        }
        database, err := db.Open(cfg.DatabasePath)
        if err != nil {
            return err
        }
        cmd.SetContext(withDB(cmd.Context(), database))
        return nil
    },
}

func init() {
    rootCmd.PersistentFlags().Bool("no-color", false, "disable colored output")
    rootCmd.PersistentFlags().BoolP("quiet", "q", false, "suppress non-essential output")
    rootCmd.PersistentFlags().StringP("format", "f", "table", "output format (table|json|csv)")
}
```
</details>

<details>
<summary>Hint 3: Dynamic completion for note IDs</summary>

```go
deleteCmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
    database := dbFromContext(cmd.Context())
    if database == nil {
        return nil, cobra.ShellCompDirectiveNoFileComp
    }
    notes, _ := database.ListNotes(cmd.Context())
    var completions []string
    for _, n := range notes {
        completions = append(completions, fmt.Sprintf("%d\t%s", n.ID, n.Title))
    }
    return completions, cobra.ShellCompDirectiveNoFileComp
}
```
</details>

<details>
<summary>Hint 4: Version with ldflags</summary>

```go
var (
    version = "dev"
    commit  = "none"
    date    = "unknown"
)

// Build with:
// go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```
</details>

## Success Criteria

- [ ] `notectl add "Meeting notes" --tag=work` creates a note and prints its ID
- [ ] `notectl list` displays a formatted table with ID, title, tags, and creation date
- [ ] `notectl list --tag=work --format=json` filters and outputs valid JSON
- [ ] `notectl show 1` displays the full note content
- [ ] `notectl edit 1` opens `$EDITOR` (or configured editor) and saves changes
- [ ] `notectl delete 1` prompts for confirmation before deleting
- [ ] `notectl search "meeting"` finds notes by title and body content
- [ ] `notectl tag list` shows all tags with their note counts
- [ ] `notectl tag rename work professional` renames across all notes
- [ ] `notectl export --format=json | notectl import` round-trips correctly
- [ ] `notectl config show` displays current configuration
- [ ] `notectl config set editor vim` persists the setting
- [ ] `notectl completion bash` generates a valid bash completion script
- [ ] Tab completion suggests note IDs and tag names dynamically
- [ ] `--no-color` disables all ANSI escape codes in output
- [ ] Invalid commands produce helpful error messages, not panics
- [ ] `notectl version` shows version, commit, and build date
- [ ] Database is created automatically on first run
- [ ] All operations work correctly after `go build -o notectl .`
- [ ] `go test ./...` passes with meaningful test coverage

## Research Resources

- [Cobra documentation](https://cobra.dev/)
- [Cobra user guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md)
- [Viper configuration](https://github.com/spf13/viper) -- config file loading
- [go-sqlite3 documentation](https://github.com/mattn/go-sqlite3)
- [lipgloss](https://github.com/charmbracelet/lipgloss) -- terminal styling
- [tablewriter](https://github.com/olekukonko/tablewriter) -- table output
- [gh CLI source code](https://github.com/cli/cli) -- production CLI reference
