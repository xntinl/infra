# The process dictionary: legitimate uses and why you almost never want it

**Project**: `process_dict_lab` — explore `Process.put/get`, its niche legitimate uses, and the refactor back to explicit state.

---

## Project context

Every BEAM process has a built-in mutable key/value store called the
*process dictionary*, accessed via `Process.put/2` and `Process.get/1`.
It is the closest thing Erlang has to global mutable state — scoped to
one process, invisible to everyone else, and therefore deeply tempting.

It is also, in 95% of cases, the wrong tool. It breaks referential
transparency inside a single function, hides data flow from readers,
resists testing, and the OTP team has spent decades warning against it.
Yet it remains in the language because a handful of use cases genuinely
need it: ancestor tracking (`$ancestors`), logger metadata, seed state
for `:rand`, caller chains for debugging, and a few performance-critical
hot paths inside Erlang itself.

This exercise does two things: (1) show the legitimate pattern — logger
metadata that implicitly rides with the current process — and (2) show
the same functionality implemented with explicit state, so you can feel
the readability difference in your bones.

Project structure:

```
process_dict_lab/
├── lib/
│   ├── process_dict_lab.ex
│   ├── process_dict_lab/implicit.ex
│   └── process_dict_lab/explicit.ex
├── test/
│   └── process_dict_lab_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For process dictionary erlang, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. What the process dictionary actually is

Each BEAM process has a private hash table. `Process.put(k, v)` stores;
`Process.get(k)` reads; `Process.delete(k)` removes. Keys and values are
arbitrary terms. The dictionary dies with the process — no sharing,
no leak across processes. So far, so innocent.

### 2. Why it is dangerous

```
explicit:  fn(state, input) -> {new_state, output}   — pure, testable
implicit:  fn(input) -> output (reads hidden dict)    — what lives where?
```

A function that reads the dictionary has *invisible inputs*. Nothing in
its signature tells a reader that `compute_discount/1` depends on a
currency stored minutes ago by an unrelated caller. Tests must prepare
the dictionary; concurrent callers interfere; refactoring breaks silently.

### 3. Legitimate uses

- **Logger metadata** (`Logger.metadata/1`): attaches `request_id`,
  `user_id` to every log line from the current process without threading
  it through every call.
- **`$ancestors` / `$callers`**: OTP writes these so `Task.async/1` knows
  its parent tree for debugging.
- **Seed state for `:rand`**: kept per-process so you can reset it.
- **Telemetry spans**: active span IDs, often stored via the dictionary.

The pattern: **cross-cutting context that every function in a call stack
might want to log but none should be forced to accept as an argument**.

### 4. The refactor to explicit state

If the context is not cross-cutting, thread it through function
arguments or a GenServer's state. "Is it cross-cutting?" is the test.
Business logic is almost never cross-cutting.

---

## Design decisions

**Option A — store context in process dictionary**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — pass state explicitly (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because process dictionary is invisible to tests and crashes; explicit state is greppable and reviewable.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new process_dict_lab
cd process_dict_lab
```

### Step 2: `lib/process_dict_lab/implicit.ex`

```elixir
defmodule ProcessDictLab.Implicit do
  @moduledoc """
  Logger-metadata-style API. `set_context/1` attaches a request_id to
  the current process; `log/1` reads it implicitly. This is the one
  shape where the process dictionary is the least-bad option.
  """

  @key :__pd_lab_request_id__

  @doc "Attach `request_id` to the current process. Survives until the process dies or you clear it."
  @spec set_context(String.t()) :: :ok
  def set_context(request_id) when is_binary(request_id) do
    Process.put(@key, request_id)
    :ok
  end

  @doc "Clears the current context."
  @spec clear_context() :: :ok
  def clear_context do
    Process.delete(@key)
    :ok
  end

  @doc """
  Returns a log line with the current context prefixed. Notice that the
  function signature does NOT mention request_id — it's an invisible input.
  That is exactly the readability cost: convenient here, lethal in business logic.
  """
  @spec log(String.t()) :: String.t()
  def log(message) when is_binary(message) do
    case Process.get(@key) do
      nil -> "[no-ctx] " <> message
      id  -> "[req=#{id}] " <> message
    end
  end
end
```

### Step 3: `lib/process_dict_lab/explicit.ex`

```elixir
defmodule ProcessDictLab.Explicit do
  @moduledoc """
  The same API, rewritten with explicit state. Every function declares
  its inputs; the data flow is readable top-to-bottom. This is what
  99% of your code should look like.
  """

  defmodule Context do
    @moduledoc false
    @enforce_keys [:request_id]
    defstruct [:request_id]
    @type t :: %__MODULE__{request_id: String.t()}
  end

  @doc "Builds a context. Pure — no side effects."
  @spec new_context(String.t()) :: Context.t()
  def new_context(request_id) when is_binary(request_id) do
    %Context{request_id: request_id}
  end

  @doc "Log requires the context explicitly. Nothing hidden."
  @spec log(Context.t() | nil, String.t()) :: String.t()
  def log(nil, message), do: "[no-ctx] " <> message
  def log(%Context{request_id: id}, message), do: "[req=#{id}] " <> message
end
```

### Step 4: `lib/process_dict_lab.ex`

```elixir
defmodule ProcessDictLab do
  @moduledoc """
  A side-by-side demo: the same "log with request context" feature
  implemented implicitly (process dictionary) and explicitly (struct
  threaded through).

  See `ProcessDictLab.Implicit` and `ProcessDictLab.Explicit`.
  """

  @doc """
  Enumerates process-dictionary keys currently set on the calling process.
  Useful for understanding what already lives in the dict before you add
  anything.
  """
  @spec introspect() :: [term()]
  def introspect do
    Process.get()
    |> Enum.map(fn {k, _v} -> k end)
  end
end
```

### Step 5: `test/process_dict_lab_test.exs`

```elixir
defmodule ProcessDictLabTest do
  use ExUnit.Case, async: true

  alias ProcessDictLab.{Implicit, Explicit}

  describe "Implicit (process dictionary)" do
    test "log reads implicit context" do
      Implicit.set_context("req-42")
      assert Implicit.log("hello") == "[req=req-42] hello"
    end

    test "no context returns the no-ctx sentinel" do
      Implicit.clear_context()
      assert Implicit.log("hello") == "[no-ctx] hello"
    end

    test "context is per-process — it does not leak across processes" do
      Implicit.set_context("parent-ctx")

      # Spawn a child and check its context. A fresh process has no dict entry.
      parent = self()

      spawn(fn ->
        send(parent, {:child_log, Implicit.log("from child")})
      end)

      assert_receive {:child_log, msg}, 500
      assert msg == "[no-ctx] from child"
    end
  end

  describe "Explicit (struct)" do
    test "log takes context as an argument — no hidden state" do
      ctx = Explicit.new_context("req-99")
      assert Explicit.log(ctx, "hello") == "[req=req-99] hello"
    end

    test "nil context is also explicit" do
      assert Explicit.log(nil, "hello") == "[no-ctx] hello"
    end

    test "two contexts can be used concurrently without a dict" do
      ctx_a = Explicit.new_context("A")
      ctx_b = Explicit.new_context("B")
      # Pure function, no side effects — interleaving is safe by construction.
      assert Explicit.log(ctx_a, "x") == "[req=A] x"
      assert Explicit.log(ctx_b, "x") == "[req=B] x"
    end
  end

  describe "introspect/0" do
    test "lists whatever is currently in the dictionary" do
      Implicit.set_context("probe")
      keys = ProcessDictLab.introspect()
      assert :__pd_lab_request_id__ in keys
    end
  end
end
```

### Step 6: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. The dictionary hides inputs — call-graph tools cannot see them**
Dialyzer, compiler warnings, and your IDE will not tell you that a
function depends on a dictionary key. A refactor that deletes the
`Process.put` elsewhere silently breaks the reader. This is the single
biggest cost and the reason the Erlang style guide discourages it.

**2. It does not survive `Task.async` unless you opt in**
`Task.async/1` spawns a fresh process with a fresh (empty) dictionary.
If you rely on dict values as context, use `Logger.metadata/1` — Logger
copies metadata across Tasks — or pass the values explicitly.

**3. Keys collide — namespace them**
There is no namespace. `Process.put(:user, ...)` by you and
`Process.put(:user, ...)` by a library clobber each other silently.
Always use module-qualified atoms like `:"Elixir.MyApp.Cache.counter"` or
a prefix convention like `:__myapp_user__`.

**4. Tests become order-dependent**
Implicit state in `async: true` tests causes rare flaky failures when
the same process (e.g. in `setup`) is reused across examples. Either
clear the dict in every `setup`/`on_exit` or use explicit state.

**5. The legitimate niche: cross-cutting logger-like context**
`Logger.metadata/1`, OpenTelemetry spans, and `$ancestors` share the
property that they are observed *everywhere* and threading them through
would touch every function. That is the real test: "would I have to add
this parameter to 80% of my functions?" If yes, a process dictionary
(or `Logger.metadata`) is defensible. If no, use explicit state.

**6. When NOT to use the process dictionary**
Any business logic. Any state shared between requests. Any function
whose correctness depends on what's there. Any place a GenServer state,
a struct argument, or a `Registry` lookup would work. Which is — to a
first approximation — everywhere you were about to reach for it.

---


## Reflection

- Describí un caso real donde el process dictionary sigue siendo la herramienta correcta en 2026.

## Resources

- [`Process.put/2` & `Process.get/1` — Elixir stdlib](https://hexdocs.pm/elixir/Process.html#put/2)
- [`Logger.metadata/1` — the canonical legitimate user](https://hexdocs.pm/logger/Logger.html#metadata/1)
- [Erlang Efficiency Guide — process dictionary notes](https://www.erlang.org/doc/efficiency_guide/processes.html)
- Fred Hébert, ["A pipeline made of airbags"](https://ferd.ca/a-pipeline-made-of-airbags.html) — classic argument against implicit state
- [Ulf Wiger — The `$ancestors` key](https://www.erlang.org/doc/man/erlang.html#process_info-1) — how OTP itself uses the dictionary
