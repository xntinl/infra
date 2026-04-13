# The process dictionary — why it's an anti-pattern and what to use instead

**Project**: `request_context` — shows a problematic implementation using `Process.put/get`, then the correct refactor to an `Agent` (and a note on GenServer).

---

## Project structure

```
request_context/
├── lib/
│   ├── bad_context.ex     # the anti-pattern
│   └── good_context.ex    # the refactor
├── test/
│   └── request_context_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

---

## The business problem
Every Elixir newcomer eventually discovers `Process.put/2` and `Process.get/1` and
thinks: "great, global variables per process". Senior Elixir developers have spent
hours debugging code that used them.

This exercise is deliberately two-part:

1. Build a "request context" using `Process.put/get`. See it work in happy path.
2. Break it in three realistic ways. Refactor to `Agent`. See the problems disappear.

Project structure:

```
request_context/
├── lib/
│   ├── bad_context.ex     # the anti-pattern
│   └── good_context.ex    # the refactor
├── test/
│   └── request_context_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The process dictionary is per-process mutable state

`Process.put(:user_id, 42)` stores `42` under `:user_id` in the *current process*'s
dictionary. `Process.get(:user_id)` reads it. It is **mutable**, **implicit**, and
**invisible to the type system and the compiler**.

None of these are things you want in Elixir. The whole language is organized
around explicit state threading and pure functions. `Process.put` is the escape hatch.

### 2. Why it's an anti-pattern (three real problems)

**(a) Invisible dependencies.** A function that calls `Process.get(:user_id)`
depends on a caller having called `Process.put(:user_id, _)` earlier. That
dependency doesn't appear in the signature. Refactor the caller, break the
callee, no compiler warning.

**(b) Task boundaries don't carry it.** `Task.async/1` spawns a NEW process with
an empty dictionary. Code that reads the dictionary silently returns `nil`
inside a Task. This is the #1 production bug with this pattern.

**(c) Tests become order-dependent.** Tests share the test runner's process
dictionary unless carefully isolated. One test pollutes the next.

### 3. The alternatives

| Need | Use |
|------|-----|
| Stateful GenServer handling requests | GenServer state |
| Cross-process shared mutable state | `Agent` |
| Read-heavy, concurrent access | `ETS` (see advanced level) |
| Per-request data flowing through functions | **Pass it as an argument** |
| Process-local context (Logger metadata) | `Logger.metadata/1` (the one legit use case) |

The last row matters: `Logger.metadata/1` uses the process dictionary internally,
and that's fine — it's explicitly scoped to logging and documented as such.
You're not writing Logger.

---

## Design decisions

The implementation below was chosen for clarity and idiomatic Elixir style. Pattern matching, immutability, and small focused functions guide every clause. Trade-offs are discussed inline within each module's `@moduledoc`.

---

## Implementation

### `mix.exs`
```elixir
defmodule RequestContext.MixProject do
  use Mix.Project

  def project do
    [
      app: :request_context,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```
### Step 1: Create the project

**Objective**: Create side-by-side modules for the bad and good approach so the refactor is diffable and the anti-pattern remains visible for comparison.

```bash
mix new request_context
cd request_context
```

### `lib/bad_context.ex`

**Objective**: Intentionally implement per-process globals with `Process.put/get` so the invisible coupling it creates is witnessed first-hand.

```elixir
defmodule BadContext do
  @moduledoc """
  Request context implemented with the process dictionary.

  This "works" for a single synchronous flow. It breaks in three realistic
  ways that the tests reproduce. Read, then read GoodContext.
  """

  @doc "Stores the current request id in the process dictionary."
  @spec set_request_id(String.t()) :: :ok
  def set_request_id(id) do
    # Process.put/2 mutates the current process's dictionary.
    # Returns the previous value (or nil) — we ignore it.
    Process.put(:request_id, id)
    :ok
  end

  @doc """
  Returns the current request id or raises if absent.

  Note the signature: this function looks pure from the outside. In reality
  it depends on an earlier set_request_id/1 call in the SAME process. That
  dependency is invisible to every tool — compiler, dialyzer, code review.
  """
  @spec current_request_id!() :: String.t()
  def current_request_id! do
    case Process.get(:request_id) do
      nil -> raise "no request id in context"
      id -> id
    end
  end

  @doc """
  Pretends to do some work and log it. The logging call reads the context
  implicitly — classic hidden coupling.
  """
  @spec do_work(term()) :: {:ok, term(), String.t()}
  def do_work(payload) do
    request_id = current_request_id!()
    {:ok, payload, request_id}
  end
end
```
### `lib/good_context.ex`

**Objective**: Refactor the same context into explicit state passed through function arguments, restoring referential transparency and testability.

```elixir
defmodule GoodContext do
  @moduledoc """
  Same feature, implemented with an Agent.

  Key differences from BadContext:
    * State lives in ONE named process, reachable from anywhere on the node.
    * Reads/writes cross process boundaries — Task.async works.
    * State is explicit: you can log the agent's pid, inspect it, test it.

  For per-request data that flows through a call chain, the BEST answer is
  to pass it as an argument. We show the Agent version here because the goal
  is to demonstrate the correct alternative when the dictionary is tempting.
  """

  use Agent

  @doc "Starts the context agent. Registered globally by module name."
  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(_opts \\ []) do
    Agent.start_link(fn -> %{} end, name: __MODULE__)
  end

  @spec set_request_id(String.t()) :: :ok
  def set_request_id(id) do
    Agent.update(__MODULE__, &Map.put(&1, :request_id, id))
  end

  @spec current_request_id!() :: String.t()
  def current_request_id! do
    case Agent.get(__MODULE__, &Map.get(&1, :request_id)) do
      nil -> raise "no request id in context"
      id -> id
    end
  end

  @spec do_work(term()) :: {:ok, term(), String.t()}
  def do_work(payload) do
    {:ok, payload, current_request_id!()}
  end

  @doc """
  Best-of-all-worlds: pass the context as an argument. No global, no Agent,
  no process dictionary. Trivially testable, Task-safe, obvious dependencies.
  """
  @spec do_work_explicit(term(), String.t()) :: {:ok, term(), String.t()}
  def do_work_explicit(payload, request_id) do
    {:ok, payload, request_id}
  end
end
```
### Step 4: `test/request_context_test.exs`

**Objective**: Contrast the two tests: the anti-pattern version needs `setup`/`teardown` of process state, the good one does not — concrete proof of the trade-off.

```elixir
defmodule RequestContextTest do
  use ExUnit.Case, async: false
  doctest GoodContext
  # async: false — BadContext leaks into the test runner's process dictionary.

  setup do
    # Clean the test process dict between runs — otherwise BadContext state
    # leaks across tests. This cleanup is itself evidence of the anti-pattern.
    Process.delete(:request_id)

    # Start a fresh Agent for GoodContext. start_supervised cleans up after each test.
    start_supervised!(GoodContext)
    :ok
  end

  describe "BadContext — happy path" do
    test "set/get works inside a single process" do
      BadContext.set_request_id("req-1")
      assert {:ok, :payload, "req-1"} = BadContext.do_work(:payload)
    end
  end

  describe "BadContext — failure modes" do
    test "Task.async does NOT see the context" do
      BadContext.set_request_id("req-outer")

      # A Task runs in a fresh process with an empty dictionary.
      task =
        Task.async(fn ->
          # This raises because the inner process has no :request_id set.
          try do
            BadContext.current_request_id!()
          rescue
            e in RuntimeError -> {:raised, e.message}
          end
        end)

      assert {:raised, "no request id in context"} = Task.await(task)
    end

    test "leaks across code that forgets to clear it" do
      # If a previous request in this process set the id and forgot to clear,
      # the next call reads a stale value with no indication anything is wrong.
      BadContext.set_request_id("stale-from-previous-request")

      # Imagine we're now handling an unrelated call that "knows" there's no context.
      # No error — we silently get the stale id:
      assert {:ok, _, "stale-from-previous-request"} = BadContext.do_work(:new_payload)
    end
  end

  describe "GoodContext — Agent" do
    test "works across Task boundaries" do
      GoodContext.set_request_id("req-outer")

      # The Agent is a named process — any process on the node can reach it.
      task = Task.async(fn -> GoodContext.do_work(:payload) end)

      assert {:ok, :payload, "req-outer"} = Task.await(task)
    end
  end

  describe "GoodContext — explicit argument (best)" do
    test "no global state, no process, just a function" do
      assert {:ok, :payload, "req-1"} = GoodContext.do_work_explicit(:payload, "req-1")
    end

    test "Task-safe because the id is passed in" do
      task = Task.async(fn -> GoodContext.do_work_explicit(:payload, "req-1") end)
      assert {:ok, :payload, "req-1"} = Task.await(task)
    end
  end
end
```
### Step 5: Run

**Objective**: Run `async: true` to expose how process-dict state survives test isolation — the failure mode is why it is an anti-pattern.

```bash
mix test
```

### Why this works

`Process.put/2` mutates a per-process key-value store that is entirely invisible to the type system — the function signature of `current_request_id!/0` looks pure but is not. `Agent.get/2` crosses a process boundary via message passing, which is why it survives `Task.async/1`: the Task is a new process with an empty dictionary, but the named Agent is reachable from any process on the node. The "pass as argument" version cuts both concerns: the dependency is visible at the call site and there is no shared state to leak or lose.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== RequestContext: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```
Run with: `elixir script/main.exs`

---

Create `lib/request_context.ex` and test in `iex`:

```elixir
defmodule RequestContext do
  def set_request_id(id) do
    Process.put(:request_id, id)
  end

  def get_request_id do
    Process.get(:request_id, "no-id")
  end

  def with_context(id, func) do
    old_id = Process.get(:request_id)
    Process.put(:request_id, id)
    try do
      func.()
    after
      if old_id, do: Process.put(:request_id, old_id), else: Process.delete(:request_id)
    end
  end

  def log_with_context(msg) do
    id = get_request_id()
    IO.puts("[#{id}] #{msg}")
  end
end

# Test it
RequestContext.set_request_id("req-123")
IO.puts(RequestContext.get_request_id())  # "req-123"

RequestContext.log_with_context("Processing")  # [req-123] Processing

RequestContext.with_context("req-456", fn ->
  RequestContext.log_with_context("In context")  # [req-456] In context
end)

RequestContext.log_with_context("Back")  # [req-123] Back
```
## Benchmark

<!-- benchmark N/A: tema conceptual — the lesson is correctness and design, not throughput. `Process.put/get` is slightly faster than `Agent.get` (in-process vs. message), but that difference is irrelevant compared to the bugs it enables. -->

---

## Trade-offs and production gotchas

**1. The process dictionary survives longer than you think**
Inside a long-lived process (a GenServer handling many requests), whatever you
`Process.put` stays until explicitly deleted or the process dies. Request N+1
reads stale state from request N. This is the most expensive version of this bug —
it's silent and deterministic and survives all your tests.

**2. Task/Flow/async work all lose the dictionary**
Any new process starts fresh. This includes `Task.async_stream`, `Flow`, GenServer
children, spawned workers. If you use the process dictionary as "request context",
anything concurrent will silently misbehave.

**3. Dialyzer and the compiler cannot help you**
`Process.get/1` returns `any()`. There is no way to declare "this function requires
`:request_id` to be set". You're programming in dynamic-globals mode.

**4. Legitimate uses exist but they are rare**
- `Logger.metadata/1` — explicitly scoped, documented.
- Performance counters within a tight loop where allocating an `Agent` call would
  dominate cost (measure first).
- Framework internals that must carry context across a specific call stack without
  threading it through user code (e.g. the `Phoenix.LiveView` internals).

If you're not writing a framework, you don't need the process dictionary.

**5. When NOT to use an Agent either**
An Agent is a GenServer in a trench coat. It serializes every read and write
through one process. For per-request data, it's overkill *and* a bottleneck.
**The correct default is: pass context as an argument.** Agent shows up here
because the exercise is about illustrating the refactor.

---

## Reflection

- Your team inherits a codebase that uses `Process.put/get` for a request-id context across 200 functions. A full refactor to explicit arguments is a 6-week project. What's the ordered plan — Agent first, then gradual signature migration? Or big-bang? Defend your sequencing by what risk each step actually removes.
- `Logger.metadata/1` is the one blessed use of the process dictionary. Why is it safe there but dangerous in your code? Identify the precise property of logging that makes the anti-pattern acceptable — and whether that property ever holds in business logic.

---

## Resources

- ["Process dictionary" — Elixir Anti-Patterns guide](https://hexdocs.pm/elixir/process-anti-patterns.html) — official anti-patterns doc
- [`Agent`](https://hexdocs.pm/elixir/Agent.html) — the minimal state-holding process
- [`Logger.metadata/1`](https://hexdocs.pm/logger/Logger.html#metadata/1) — the one well-known legitimate use of the process dictionary

---

## Why The process dictionary — why it's an anti-pattern and what to use instead matters

Mastering **The process dictionary — why it's an anti-pattern and what to use instead** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/request_context_test.exs`

```elixir
defmodule RequestContextTest do
  use ExUnit.Case, async: true

  doctest RequestContext

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert RequestContext.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Process Dictionary: Process-Local Storage
Every process has a dictionary for process-local data. It's faster than Agent because no message passing is involved.

### 2. When NOT to Use It
The process dictionary is tempting but often a mistake: makes code hard to test, makes refactoring dangerous, doesn't survive process restarts.

### 3. Proper Alternatives
Use `Agent` for shared state, `GenServer` for complex state, or pass state as function arguments. These are more explicit and testable.

---
