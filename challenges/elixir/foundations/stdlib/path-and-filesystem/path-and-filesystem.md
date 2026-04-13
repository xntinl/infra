# Path and Filesystem: Building a Project Scaffolder

**Project**: `scaffold_gen` — generates a project directory tree with template files from a blueprint

---

## Why Path and File matter for a senior developer

Most "works on my machine" bugs trace back to filesystem assumptions: hardcoded
`/` separators, assuming a directory exists, stepping over user files with
`File.write!/2`, or resolving paths relative to the wrong cwd.

Elixir provides two complementary modules:

- **`Path`**: pure string manipulation. Joins, splits, expansions, and
  normalisations. Never touches the disk. Handles OS-specific separators.
- **`File`**: actual filesystem operations. `mkdir_p`, `ls`, `write`,
  `cp_r`, `exists?`. Every function has a `!` variant that raises and a
  non-bang variant that returns `{:ok, _} | {:error, reason}`.

Knowing which belongs where prevents both bugs and unnecessary IO.

---

## Why `Path` + `File` and not hardcoded strings + `:file`

Hardcoding `"/"` as a separator works on BEAM (it normalises internally) until you hand the string to an external tool — shell, Dockerfile generator, Git — that expects native separators and silently corrupts on Windows CI. Relative paths without `Path.expand/1` are interpreted against the cwd at call time, so a test that passes `./tmp` under the project root succeeds locally and fails when the same code runs from a systemd unit with a different cwd. `Path` handles both concerns with pure string ops and no syscalls; `File` layers the actual IO on top. Reaching directly for `:file` (Erlang) skips the cross-platform normalisation and returns charlists instead of binaries, which then leak into your logs and assertions.

---

## The business problem

When you start a new internal service at your company, you create the same
layout every time: `lib/`, `test/`, `config/`, a `.formatter.exs`, a
`.gitignore`, a README with the service name substituted in, and a Dockerfile.
Doing this by hand leads to drift.

Build a scaffolder that:

1. Accepts a target path and a service name
2. Validates the target (does not exist, or is empty)
3. Creates the directory tree with `File.mkdir_p/1`
4. Writes template files with placeholders substituted (`{{service_name}}`,
   `{{module_name}}`)
5. Returns a summary of everything created so callers can log or confirm

---

## Project structure

```
scaffold_gen/
├── lib/
│   └── scaffold_gen/
│       ├── blueprint.ex
│       ├── renderer.ex
│       └── generator.ex
├── script/
│   └── main.exs
├── test/
│   └── scaffold_gen/
│       ├── renderer_test.exs
│       └── generator_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Design decisions

**Option A — imperative generator: one function per file, `File.write!/2` inline**
- Pros: zero indirection; each file is visible at a glance; no template engine to learn.
- Cons: every new stack forks the generator; paths and templates are tangled; testing a path rendering requires a filesystem; easy to stomp on existing files.

**Option B — data-driven blueprint (`%{directories: [...], files: [...]}`) rendered by a pure renderer, materialised by a separate generator** (chosen)
- Pros: blueprints are testable in memory (no IO); new stacks = new blueprint functions, no generator changes; placeholder rendering has its own module and unit tests; validation happens before any write so partial scaffolds are impossible.
- Cons: three layers instead of one; plain `String.replace/3` templates can't express conditionals (EEx would) — fine for the current scope.

Chose **B** because the generator lives forever and new stacks arrive unpredictably. Separating "what the blueprint says" from "how we render it" from "how we write it" pays off the first time two devs add stacks in parallel.

---

## Implementation

### Step 1: Create the project

**Objective**: Path is pure string ops (OS-agnostic separators); File is actual IO — never hardcode "/" for portable code.

```bash
mix new scaffold_gen
cd scaffold_gen
```

### `mix.exs`
**Objective**: Boilerplate; focus on Path.expand/1 resolving relative paths against project root, not cwd.

```elixir
defmodule ScaffoldGen.MixProject do
  use Mix.Project

  def project do
    [
      app: :scaffold_gen,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 3: `.formatter.exs`

**Objective**: Formatter is opinionated; configure inputs glob + line length once; format is hermetic (no env deps).

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### `lib/scaffold_gen.ex`

```elixir
defmodule ScaffoldGen do
  @moduledoc """
  Path and Filesystem: Building a Project Scaffolder.

  Most "works on my machine" bugs trace back to filesystem assumptions: hardcoded.
  """
end
```

### `lib/scaffold_gen/blueprint.ex`

**Objective**: Data-driven blueprints (maps) are testable without IO; pure description separates intent from side effects.

```elixir
defmodule ScaffoldGen.Blueprint do
  @moduledoc """
  Declarative description of what a scaffold produces.

  A blueprint is a list of directories to create and a list of files
  with template content. Keeping this as data (not code) makes it easy
  to extend: new stacks = new blueprints, no generator changes.
  """

  @type file_spec :: {relative_path :: String.t(), template :: String.t()}
  @type t :: %{
          directories: [String.t()],
          files: [file_spec()]
        }

  @doc """
  Default Elixir service blueprint.

  Directory paths use forward slashes. Path.join/1 will translate to
  the host separator when the generator materialises them.
  """
  @spec elixir_service() :: t()
  def elixir_service do
    %{
      directories: [
        "lib/{{service_name}}",
        "test/{{service_name}}",
        "config"
      ],
      files: [
        {"mix.exs", mix_exs_template()},
        {"README.md", readme_template()},
        {".gitignore", gitignore_template()},
        {".formatter.exs", formatter_template()},
        {"lib/{{service_name}}.ex", main_module_template()},
        {"test/{{service_name}}_test.exs", main_test_template()},
        {"test/test_helper.exs", "ExUnit.start()\n"}
      ]
    }
  end

  # --- templates ---
  # These are deliberately plain strings with {{placeholders}} so the
  # renderer remains a single-pass string replacement. For complex
  # templating, reach for EEx — see the "when NOT to use" section.

  defp mix_exs_template do
    """
    defmodule {{module_name}}.MixProject do
      use Mix.Project

      def project do
        [
          app: :{{service_name}},
          version: "0.1.0",
          elixir: "~> 1.19",
          deps: deps()
        ]
      end

      def application do
        [extra_applications: [:logger]]
      end

      defp deps, do: []
    end
    """
  end

  defp readme_template do
    """
    # {{module_name}}

    Generated by scaffold_gen.

    ## Usage

        mix deps.get
        mix test
    """
  end

  defp gitignore_template do
    """
    /_build/
    /deps/
    /cover/
    /doc/
    /.elixir_ls/
    *.beam
    erl_crash.dump
    """
  end

  defp formatter_template do
    """
    [
      inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
      line_length: 98
    ]
    """
  end

  defp main_module_template do
    """
    defmodule {{module_name}} do
      @moduledoc \"\"\"
      Entry point for the {{service_name}} service.
      \"\"\"

      @spec hello() :: :world
      def hello, do: :world
    end
    """
  end

  defp main_test_template do
    """
    defmodule {{module_name}}Test do
      use ExUnit.Case, async: true
      doctest ScaffoldGen.MixProject

      describe "core functionality" do
        test "hello returns :world" do
          assert {{module_name}}.hello() == :world
        end
      end
      """
    end
      end
end
```

### `lib/scaffold_gen/renderer.ex`

**Objective**: Renderer is pure (String.replace); testing templates never touches disk — fast, repeatable, isolated.

```elixir
defmodule ScaffoldGen.Renderer do
  @moduledoc """
  Substitutes placeholders in paths and file contents.

  Pure string manipulation — no IO. Isolated so we can test rendering
  with a simple map of assigns.
  """

  @type assigns :: %{optional(String.t()) => String.t()}

  @doc """
  Replaces every `{{key}}` in `template` with `assigns[key]`.

  Unknown placeholders are left as-is rather than raising. That choice
  lets blueprints include literal `{{` in templates (e.g. Mustache
  examples in documentation) without escaping. If you want strict
  behaviour, see `render_strict/2`.
  """
  @spec render(String.t(), assigns()) :: String.t()
  def render(template, assigns) when is_binary(template) and is_map(assigns) do
    Enum.reduce(assigns, template, fn {key, value}, acc ->
      String.replace(acc, "{{#{key}}}", value)
    end)
  end

  @doc """
  Like render/2 but raises if any `{{...}}` placeholder remains after
  substitution.
  """
  @spec render_strict(String.t(), assigns()) :: String.t()
  def render_strict(template, assigns) do
    rendered = render(template, assigns)

    case Regex.run(~r/\{\{([^}]+)\}\}/, rendered) do
      nil -> rendered
      [_, missing] -> raise ArgumentError, "unresolved placeholder: #{missing}"
    end
  end

  @doc """
  Converts a service name like "order_service" into a module name
  "OrderService". We do this here (not in blueprints) so blueprints
  only need to know about one canonical assign (the service_name) and
  derivations live in one place.
  """
  @spec module_name(String.t()) :: String.t()
  def module_name(service_name) when is_binary(service_name) do
    service_name
    |> String.split(["_", "-"], trim: true)
    |> Enum.map_join("", &String.capitalize/1)
  end
end
```

### `lib/scaffold_gen/generator.ex`

**Objective**: File.mkdir_p validates target doesn't exist; File.write! only after validation — never partial scaffolds.

```elixir
defmodule ScaffoldGen.Generator do
  @moduledoc """
  Materialises a blueprint onto disk.

  Responsibilities:
    * Validate the target path is safe to write into.
    * Create directories with File.mkdir_p/1.
    * Render and write files with correct paths for the host OS.
    * Return an audit trail of what was created.
  """

  alias ScaffoldGen.{Blueprint, Renderer}

  @type result :: %{
          root: Path.t(),
          directories: [Path.t()],
          files: [Path.t()]
        }

  @doc """
  Generate a scaffold at `target_path` for `service_name`.

  `target_path` is expanded (~ is resolved, relative paths become
  absolute against the current cwd) before any IO happens so errors
  reference the final location.

  Returns `{:ok, result}` or `{:error, reason}`.
  """
  @spec generate(Path.t(), String.t(), Blueprint.t()) ::
          {:ok, result()} | {:error, String.t()}
  def generate(target_path, service_name, blueprint \\ Blueprint.elixir_service()) do
    with :ok <- validate_service_name(service_name),
         expanded = Path.expand(target_path),
         :ok <- validate_target(expanded) do
      assigns = %{
        "service_name" => service_name,
        "module_name" => Renderer.module_name(service_name)
      }

      dirs = create_directories(expanded, blueprint.directories, assigns)
      files = create_files(expanded, blueprint.files, assigns)

      {:ok, %{root: expanded, directories: dirs, files: files}}
    end
  end

  # --- private ---

  # Service names map directly to atoms (`:my_service`) and module
  # segments, so we constrain them up front. Catching this here gives
  # a clear error instead of a cryptic compile failure later.
  defp validate_service_name(name) when is_binary(name) do
    if Regex.match?(~r/^[a-z][a-z0-9_]*$/, name) do
      :ok
    else
      {:error, "service_name must match ^[a-z][a-z0-9_]*$, got: #{inspect(name)}"}
    end
  end

  defp validate_service_name(other),
    do: {:error, "service_name must be a binary, got: #{inspect(other)}"}

  # Refuse to write into an existing non-empty directory. Writing into
  # an empty dir is fine (common when users `mkdir foo && cd foo`).
  defp validate_target(path) do
    cond do
      not File.exists?(path) ->
        :ok

      not File.dir?(path) ->
        {:error, "target exists and is not a directory: #{path}"}

      match?({:ok, []}, File.ls(path)) ->
        :ok

      true ->
        {:error, "target directory is not empty: #{path}"}
    end
  end

  defp create_directories(root, dir_templates, assigns) do
    Enum.map(dir_templates, fn template ->
      rendered = Renderer.render(template, assigns)
      full_path = Path.join(root, rendered)
      File.mkdir_p!(full_path)
      full_path
    end)
  end

  defp create_files(root, file_specs, assigns) do
    Enum.map(file_specs, fn {path_template, content_template} ->
      rendered_path = Renderer.render(path_template, assigns)
      full_path = Path.join(root, rendered_path)

      # Guarantee the parent exists even if the blueprint did not list
      # it explicitly — this keeps blueprints DRY.
      File.mkdir_p!(Path.dirname(full_path))

      content = Renderer.render(content_template, assigns)
      File.write!(full_path, content)

      full_path
    end)
  end
end
```

**Why this works:**

- `Path.expand/1` canonicalises the target once at the start. Every later
  operation references the same absolute path, so log messages are unambiguous.
- `Path.join/2` uses the correct separator per OS. Hardcoded `"/"` would break
  on Windows tooling.
- `File.mkdir_p/1` is idempotent: it does not fail if the directory already
  exists. That is the right primitive for scaffolders and migrations.
- Validation happens before any write. A partial scaffold is worse than no
  scaffold — we fail fast.

### `test/scaffold_gen_test.exs`

**Objective**: Test path rendering and blueprint validation in memory; only test generator integration with temp dirs.

```elixir
defmodule ScaffoldGen.RendererTest do
  use ExUnit.Case, async: true
  doctest ScaffoldGen.Generator
  alias ScaffoldGen.Renderer

  describe "render/2" do
    test "replaces a single placeholder" do
      assert Renderer.render("hello {{name}}", %{"name" => "world"}) == "hello world"
    end

    test "replaces multiple placeholders" do
      template = "service {{service_name}} module {{module_name}}"
      assigns = %{"service_name" => "orders", "module_name" => "Orders"}
      assert Renderer.render(template, assigns) == "service orders module Orders"
    end

    test "leaves unknown placeholders untouched" do
      assert Renderer.render("{{known}} {{unknown}}", %{"known" => "ok"}) == "ok {{unknown}}"
    end
  end

  describe "render_strict/2" do
    test "raises if any placeholder remains" do
      assert_raise ArgumentError, ~r/unresolved placeholder: missing/, fn ->
        Renderer.render_strict("hello {{missing}}", %{})
      end
    end
  end

  describe "module_name/1" do
    test "snake_case to PascalCase" do
      assert Renderer.module_name("order_service") == "OrderService"
    end

    test "kebab-case to PascalCase" do
      assert Renderer.module_name("order-service") == "OrderService"
    end

    test "single word" do
      assert Renderer.module_name("orders") == "Orders"
    end
  end
end
```

```elixir
defmodule ScaffoldGen.GeneratorTest do
  use ExUnit.Case, async: true
  doctest ScaffoldGen.Generator
  alias ScaffoldGen.Generator

  @tmp_root Path.join(System.tmp_dir!(), "scaffold_gen_test")

  setup do
    # Each test gets its own subdir so they can run async: true.
    unique = "run_#{System.unique_integer([:positive])}"
    target = Path.join(@tmp_root, unique)
    File.mkdir_p!(@tmp_root)
    on_exit(fn -> File.rm_rf!(target) end)
    {:ok, target: target}
  end

  describe "core functionality" do
    test "creates the full directory tree", %{target: target} do
      assert {:ok, result} = Generator.generate(target, "orders")

      assert File.dir?(Path.join(target, "lib/orders"))
      assert File.dir?(Path.join(target, "test/orders"))
      assert File.dir?(Path.join(target, "config"))

      # result should reflect reality.
      assert length(result.directories) == 3
      assert length(result.files) == 7
    end

    test "renders placeholders in file content", %{target: target} do
      {:ok, _} = Generator.generate(target, "orders")

      mix_exs = File.read!(Path.join(target, "mix.exs"))
      assert mix_exs =~ "defmodule Orders.MixProject"
      assert mix_exs =~ "app: :orders"

      main = File.read!(Path.join(target, "lib/orders.ex"))
      assert main =~ "defmodule Orders do"
    end

    test "renders placeholders in file paths", %{target: target} do
      {:ok, _} = Generator.generate(target, "payments")
      assert File.exists?(Path.join(target, "lib/payments.ex"))
      assert File.exists?(Path.join(target, "test/payments_test.exs"))
    end

    test "rejects an invalid service name", %{target: target} do
      assert {:error, message} = Generator.generate(target, "BadName")
      assert message =~ "service_name must match"
    end

    test "rejects a non-empty target directory", %{target: target} do
      File.mkdir_p!(target)
      File.write!(Path.join(target, "existing.txt"), "hello")

      assert {:error, message} = Generator.generate(target, "orders")
      assert message =~ "not empty"
    end

    test "accepts an empty existing target directory", %{target: target} do
      File.mkdir_p!(target)
      assert {:ok, _} = Generator.generate(target, "orders")
    end

    test "rejects when target is an existing file", %{target: target} do
      File.mkdir_p!(Path.dirname(target))
      File.write!(target, "i am a file")

      assert {:error, message} = Generator.generate(target, "orders")
      assert message =~ "not a directory"
    end
  end
end
```

### Step 8: Run and verify

**Objective**: --warnings-as-errors catches unused blueprint keys; test coverage validates all template placeholders expand.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test --trace
mix format
```

### Why this works

`Path.expand/1` canonicalises the target once so every subsequent operation — mkdir, write, error logging — references the same absolute path. `Path.join/2` consults the host OS for the correct separator, which keeps the same blueprint correct on macOS, Linux, and Windows CI. Validation (`validate_service_name`, `validate_target`) runs before any IO, so a failed scaffold leaves the filesystem untouched; a half-written project is worse than none. `File.mkdir_p/1` is idempotent and recursive, so blueprints can list only the leaf directories and still work.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ScaffoldGen: demo ===\n")

    result_1 = ScaffoldGen.Renderer.render("hello {{name}}", %{"name" => "world"})
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = ScaffoldGen.Renderer.render(template, assigns)
    IO.puts("Demo 2: #{inspect(result_2)}")

    result_3 = ScaffoldGen.Renderer.render("{{known}} {{unknown}}", %{"known" => "ok"})
    IO.puts("Demo 3: #{inspect(result_3)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

Create a simple example demonstrating the key concepts:

```elixir
# Example code demonstrating module concepts
IO.puts("Example: Read the Implementation section above and run the code samples in iex")
```

## Benchmark

<!-- benchmark N/A: IO-bound scaffolder, dominated by filesystem latency, not Elixir code paths -->

---

## Trade-off analysis

| Aspect | Current (data-driven blueprints) | Hardcoded generator |
|--------|----------------------------------|---------------------|
| Add a new stack | New blueprint function | Edit generator code |
| Testability | Render + generate separately | Coupled |
| Template engine | Plain `String.replace/3` | EEx |
| File permissions | Default (0644/0755) | Can be customised |

| Aspect | `File.mkdir_p/1` | `File.mkdir/1` |
|--------|-----------------|----------------|
| Missing parents | Created recursively | Fails |
| Already exists | No-op | Returns `{:error, :eexist}` |
| Use case | Scaffolders, migrations | Strict one-step create |

---

## Common production mistakes

**1. Using `Path.join(["a", "/b"])` and expecting `a/b`**
`Path.join/1` preserves absolute components: `Path.join(["a", "/b"])` returns
`/b` because the second argument is absolute. Always render your relative
segments before joining.

**2. Forgetting `Path.expand/1`**
A path like `~/projects/foo` is not expanded automatically. `File.write!/2`
will create a literal `~` directory next to your cwd. Call `Path.expand/1`
at the boundary of your module.

**3. Writing without checking the target**
`File.write!/2` silently overwrites. A scaffolder that stomps on an existing
project is a disaster. Validate before writing.

**4. Ignoring OS path separators**
Hardcoded `"/"` in Elixir source is fine because BEAM normalises it, but it
breaks when you pass the string to external tools that expect native
separators. Prefer `Path.join/1`.

**5. `File.ls!/1` in a hot loop**
Every call is a syscall. For filesystem traversals beyond a few hundred files,
batch with `Path.wildcard/1` or reach for `File.stream!/3` when reading many
files.

---

## When NOT to use plain string placeholders

- When templates have conditionals, loops, or partials — use `EEx` from the
  standard library. It is compile-time safe and supports `<%= %>` expressions.
- When you need compiled templates cached in memory — use `EEx.SmartEngine`.
- When your templates are user-supplied — beware of injection. A plain
  `String.replace/3` here is safe because blueprints ship with the app, not
  from external input.

---

## Reflection

1. Your scaffolder refuses to write into a non-empty directory. A platform team now wants an "update" mode that patches existing scaffolds in place (add a new config file, refresh a gitignore). Would you add an `:overwrite` flag, split `generate/3` into `generate_new/3` and `apply_diff/3`, or move to a git-based workflow (clone template, merge changes)? What invariant does each approach preserve?
2. Blueprints today are plain Elixir functions compiled into the generator. A product manager wants users to upload their own blueprints as YAML. What changes in the trust model of `Renderer.render/2` and in the validator? At what point do you stop fighting and embed a real template engine (EEx with restricted bindings, Mustache)?

---

## Resources

- [Path module — HexDocs](https://hexdocs.pm/elixir/Path.html)
- [File module — HexDocs](https://hexdocs.pm/elixir/File.html)
- [EEx templating — HexDocs](https://hexdocs.pm/eex/EEx.html)
- [Mix.Generator (internal Mix scaffolding helpers)](https://hexdocs.pm/mix/Mix.Generator.html)

---

## Why Path and Filesystem matters

Mastering **Path and Filesystem** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts
### 1. `Path` Functions Work with Strings, Not Actual Paths
`Path.join` does string manipulation on paths without validation. `Path` does not check if paths exist or are accessible.

### 2. Cross-Platform Paths
`Path.join/2` uses the OS-specific separator. Always use `join` instead of string concatenation for portable code.

### 3. Path Components
`Path.dirname`, `Path.basename`, `Path.extname` parse paths into components without touching the filesystem.

---
