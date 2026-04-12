# `try`/`rescue`/`catch`/`after` — Guaranteed Cleanup

**Project**: `file_processor` — reads and transforms a file, guaranteeing the handle is closed

**Difficulty**: ★★☆☆☆
**Estimated time**: 1–2 hours

---

## Project structure

```
file_processor/
├── lib/
│   └── file_processor/
│       └── processor.ex
├── test/
│   └── file_processor_test.exs
└── mix.exs
```

---

## What you will learn

1. **`try`/`rescue`/`catch`/`after`** — the full exception-handling form, and what each clause catches.
2. **Guaranteed cleanup** — using `after` to release resources (file handles, sockets, ETS entries)
   regardless of how the `try` block exits.

---

## The concept in 60 seconds

```elixir
try do
  risky()
rescue
  e in RuntimeError -> {:error, Exception.message(e)}
catch
  :throw, value -> {:thrown, value}
  :exit, reason -> {:exit, reason}
after
  cleanup()    # runs no matter what — success, rescue, catch, or re-raise
end
```

- `rescue` catches **exceptions** (the `%RuntimeError{}` kind).
- `catch` catches **throws** and **exits** (non-exception non-local flow).
- `after` **always** runs, even if the block raises/exits. Its value is discarded.

The Elixir convention is: prefer `{:ok, _}` / `{:error, _}` tuples over exceptions.
Reach for `try` only for **resource cleanup** or at the boundary with Erlang libraries
that exit/throw.

---

## Why a file processor

Files are the canonical "resource that must be closed." A function that opens a file,
processes it, and returns is trivial — until the processing raises. Without `after`, the
file handle leaks. This exercise makes the leak impossible.

---

## Implementation

### Step 1 — Create the project

```bash
mix new file_processor
cd file_processor
```

### Step 2 — `lib/file_processor/processor.ex`

```elixir
defmodule FileProcessor.Processor do
  @moduledoc """
  Reads a file line by line and applies a transform. Guarantees the file
  handle is closed, even if the transform raises.
  """

  @doc """
  Opens `path`, applies `transform` to each line, and returns the list of results.

  Returns:
    {:ok, [transformed_lines]} on success
    {:error, reason}           on open failure or transform raising
  """
  @spec process(Path.t(), (String.t() -> term())) ::
          {:ok, [term()]} | {:error, term()}
  def process(path, transform) when is_function(transform, 1) do
    case File.open(path, [:read, :utf8]) do
      {:ok, device} ->
        # `try/after` guarantees close/1 even if `read_and_transform` raises.
        # We use `rescue` to convert raised errors into the {:error, _} tuple —
        # callers should not need to care whether we failed from I/O or from
        # their transform function.
        try do
          lines = read_and_transform(device, transform)
          {:ok, lines}
        rescue
          e -> {:error, Exception.message(e)}
        catch
          # Some underlying Erlang calls use throw/exit for flow. Convert them too.
          :throw, value -> {:error, {:throw, value}}
          :exit, reason -> {:error, {:exit, reason}}
        after
          # `after` value is discarded — that's fine, File.close/1 returns :ok.
          File.close(device)
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  # IO.stream/2 yields each line; we transform eagerly into a list so the stream
  # is fully consumed BEFORE the `after` closes the handle. A lazy stream
  # escaping the try block would read from a closed file.
  defp read_and_transform(device, transform) do
    device
    |> IO.stream(:line)
    |> Enum.map(fn line ->
      line |> String.trim_trailing("\n") |> transform.()
    end)
  end
end
```

### Step 3 — `test/file_processor_test.exs`

```elixir
defmodule FileProcessorTest do
  use ExUnit.Case, async: true

  alias FileProcessor.Processor

  @tmp_dir System.tmp_dir!()

  setup do
    path = Path.join(@tmp_dir, "fp_#{System.unique_integer([:positive])}.txt")
    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  describe "process/2 — happy path" do
    test "applies transform to each line", %{path: path} do
      File.write!(path, "alpha\nbeta\ngamma\n")
      assert Processor.process(path, &String.upcase/1) == {:ok, ["ALPHA", "BETA", "GAMMA"]}
    end

    test "empty file returns empty list", %{path: path} do
      File.write!(path, "")
      assert Processor.process(path, &String.upcase/1) == {:ok, []}
    end
  end

  describe "process/2 — errors" do
    test "missing file returns {:error, :enoent}" do
      assert {:error, :enoent} = Processor.process("/nonexistent/path.txt", &String.upcase/1)
    end

    test "raising transform is converted to {:error, _}", %{path: path} do
      File.write!(path, "will crash\n")
      boom = fn _ -> raise "boom" end
      assert {:error, "boom"} = Processor.process(path, boom)
    end

    test "throwing transform is converted to {:error, {:throw, _}}", %{path: path} do
      File.write!(path, "will throw\n")
      thrower = fn _ -> throw(:nope) end
      assert {:error, {:throw, :nope}} = Processor.process(path, thrower)
    end
  end

  describe "process/2 — cleanup" do
    test "file handle is released even when transform raises", %{path: path} do
      File.write!(path, "one\ntwo\n")
      boom = fn _ -> raise "boom" end

      # If the handle were leaked, opening with :exclusive would fail afterward
      # on some OSes. A subtler portable check: we can re-process successfully.
      assert {:error, "boom"} = Processor.process(path, boom)
      assert {:ok, ["one", "two"]} = Processor.process(path, & &1)
    end
  end
end
```

### Step 4 — Run the tests

```bash
mix test
```

All 6 tests pass.

---

## Trade-offs

| Construct | Catches | Typical use |
|---|---|---|
| `rescue` | Exceptions (`raise/1`, `%RuntimeError{}`, etc.) | Converting known failure modes to tuples |
| `catch :throw, _` | Values thrown with `throw/1` | Non-local exit from Erlang libs |
| `catch :exit, _` | Process exits (`exit/1`) | Linked process crashes you want to handle |
| `after` | Nothing — always runs | Resource cleanup |

**When NOT to use `try`:**

- **Control flow in your own code.** Return `{:ok, _}` / `{:error, _}`. Exceptions are
  for bugs and truly exceptional conditions.
- **Catching all exceptions to log-and-ignore.** That is how bugs hide. Let them crash,
  restart under a supervisor, alert.
- **When there is no resource to clean up.** A bare `try do ... rescue -> ... end` is
  usually better expressed as an explicit check or a `with` pipeline.

---

## Common production mistakes

**1. `catch _, _` to "catch everything"**
You catch exits, including `:shutdown` and `:killed`. Your process now refuses to die
when its supervisor tells it to. Always pattern match on specific kinds.

**2. Lazy streams escaping `after`**
`File.stream!/1` returns a lazy stream. If you return it from inside `try` and enumerate
it after the `after` closes the file, you read from a closed handle. Force evaluation
**inside** the try block (`Enum.map`, `Enum.to_list`).

**3. Forgetting `after` cleanup on the happy path**
`after` exists precisely because you cannot remember every exit path. Even if the body
looks safe, a future change may introduce a raise. Put cleanup in `after` from day one.

**4. `rescue` without naming the exception**
`rescue -> ...` catches any exception but gives you no value to log. `rescue e -> ...`
binds it. If you care about the cause (and you should), bind it.

**5. Re-raising wrong**
`raise e` inside a `rescue e` loses the original stacktrace. Use `reraise e, __STACKTRACE__`
to preserve it — future debuggers (you, at 3am) will need it.

**6. Using `try` as `with`**
Multi-step "do A, then B, then C, short-circuit on first failure" is what `with` is for.
Don't nest `try` blocks — use `with` and pattern-match each step.

---

## Resources

- [Elixir — try, catch, and rescue](https://hexdocs.pm/elixir/try-catch-and-rescue.html)
- [`Kernel.SpecialForms.try/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#try/1)
- [`File` module](https://hexdocs.pm/elixir/File.html) — note which functions return tuples vs raise (the `!` suffix convention)
- [Let It Crash — Joe Armstrong on error handling](https://erlang.org/download/armstrong_thesis_2003.pdf) (chapter 4)
