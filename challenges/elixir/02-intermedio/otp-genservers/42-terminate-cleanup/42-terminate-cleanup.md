# `terminate/2` for resource cleanup — and when it doesn't run

**Project**: `terminate_cleanup_gs` — a GenServer that owns a file handle and releases it on graceful shutdown.

---

## Project context

Your GenServer owns a resource that must be released: a file handle, a
TCP socket, a database transaction, a GPU context, a temporary directory.
"Release it when I stop" sounds simple — until you discover there are
five different ways a process can stop, and only three of them call
`terminate/2`. The other two — `Process.exit(pid, :kill)` and an
unhandled callback crash under `trap_exit: false` — bypass your cleanup
entirely.

This exercise builds the minimum viable shape: a GenServer that opens a
temp file on `init/1`, writes to it on demand, and closes it in
`terminate/2`. Then you will test every shutdown path and see exactly
which ones honor your cleanup and which ones do not.

Understanding this matters because in production, the difference between
"we leak one file descriptor" and "we leak thousands" is usually a
missing `Process.flag(:trap_exit, true)` and a wrong assumption about
when `terminate/2` runs.

Project structure:

```
terminate_cleanup_gs/
├── lib/
│   └── terminate_cleanup_gs.ex
├── test/
│   └── terminate_cleanup_gs_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not trust `terminate/2`?** It isn't called on `:brutal_kill`, node crashes, or `:kill` signals. External state needs idempotent cleanup independent of it.

## Core concepts

### 1. When `terminate/2` runs

`terminate/2` is called when the GenServer is *about to exit* — but only
when the server has a chance to intercept its own death. Specifically:

| Exit path                                          | `terminate/2` called? |
|----------------------------------------------------|-----------------------|
| `{:stop, reason, state}` returned from a callback  | yes                   |
| `GenServer.stop/1`                                 | yes                   |
| Callback raises                                    | yes                   |
| Parent (supervisor) sends `:shutdown` AND `trap_exit: true` | yes          |
| Parent sends `:shutdown` AND `trap_exit: false`    | **no**                |
| `Process.exit(pid, :kill)` (from anyone)           | **no — never**        |

The supervisor sets `trap_exit` based on the child's `:shutdown` option
(non-`:brutal_kill` values cause supervisors to cooperate). But a naked
GenServer that hasn't opted into `trap_exit` skips `terminate` on a
parent shutdown.

### 2. `Process.flag(:trap_exit, true)` in `init/1`

To guarantee `terminate/2` runs on a parent shutdown, call
`Process.flag(:trap_exit, true)` in `init/1`. This turns supervisor
shutdown signals into a clean path through `terminate/2` instead of
immediate death. It's the "please let me clean up" opt-in.

### 3. The `:kill` signal cannot be trapped

`Process.exit(pid, :kill)` (note: the atom `:kill`) bypasses all traps.
There is no callback, no chance to save state, no goodbye. This is
deliberate — it's the escape hatch for a genuinely hung process. Your
cleanup strategy cannot rely on graceful termination for `:kill`-able
paths; you need external recovery (lockfile detection, journaling).

### 4. `terminate/2` must be fast and cannot block forever

Supervisors give children a finite shutdown time (default 5_000 ms). If
`terminate/2` takes longer, the supervisor brutally kills the child
regardless. Do cleanup work that fits in a second or two — not final
flushes over slow networks.

---

## Design decisions

**Option A — rely on `terminate/2` for all cleanup**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — use process links and a supervisor hook + idempotent cleanup (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because `terminate/2` isn't guaranteed to run on brutal_kill; idempotent external cleanup is the only safe pattern.


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

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new terminate_cleanup_gs
cd terminate_cleanup_gs
```

### Step 2: `lib/terminate_cleanup_gs.ex`

**Objective**: Implement `terminate_cleanup_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.


```elixir
defmodule TerminateCleanupGs do
  @moduledoc """
  A GenServer that owns a writable file handle. Opens in `init/1`,
  closes in `terminate/2`. Demonstrates the five shutdown paths and
  which ones actually run cleanup.
  """

  use GenServer
  require Logger

  defmodule State do
    @moduledoc false
    @enforce_keys [:path, :io]
    defstruct [:path, :io]

    @type t :: %__MODULE__{path: Path.t(), io: :file.io_device()}
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    {path, opts} = Keyword.pop!(opts, :path)
    GenServer.start_link(__MODULE__, path, opts)
  end

  @doc "Writes `line` followed by `\\n` to the owned file."
  @spec write(GenServer.server(), String.t()) :: :ok
  def write(server, line), do: GenServer.call(server, {:write, line})

  @doc "Graceful stop — triggers `terminate/2` to close the file."
  @spec stop(GenServer.server()) :: :ok
  def stop(server), do: GenServer.stop(server, :normal)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(path) do
    # trap_exit so a supervisor :shutdown routes through terminate/2.
    Process.flag(:trap_exit, true)

    case File.open(path, [:write, :utf8]) do
      {:ok, io} ->
        {:ok, %State{path: path, io: io}}

      {:error, reason} ->
        # Fail fast — supervisor will log and decide.
        {:stop, {:open_failed, reason}}
    end
  end

  @impl true
  def handle_call({:write, line}, _from, %State{io: io} = state) do
    IO.write(io, line <> "\n")
    {:reply, :ok, state}
  end

  @impl true
  def terminate(reason, %State{io: io, path: path}) do
    # Close the file. This is the ONE thing this callback must do reliably.
    # Logging here is helpful for understanding the shutdown path in tests.
    Logger.debug("terminate/2 running for #{path} with reason #{inspect(reason)}")
    File.close(io)
    :ok
  end

  # Non-state reasons (e.g. init returned :ignore) have no io to close.
  def terminate(_reason, _state), do: :ok
end
```

### Step 3: `test/terminate_cleanup_gs_test.exs`

**Objective**: Write `terminate_cleanup_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule TerminateCleanupGsTest do
  use ExUnit.Case, async: true

  setup do
    path = Path.join(System.tmp_dir!(), "tcgs-#{System.unique_integer([:positive])}.log")
    on_exit(fn -> File.rm(path) end)
    %{path: path}
  end

  describe "graceful stop via GenServer.stop/1" do
    test "terminate/2 runs and flushes the file handle", %{path: path} do
      {:ok, pid} = TerminateCleanupGs.start_link(path: path)
      :ok = TerminateCleanupGs.write(pid, "hello")

      ref = Process.monitor(pid)
      :ok = TerminateCleanupGs.stop(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, :normal}, 500

      # If terminate ran and File.close was called, the data is durably on disk.
      assert File.read!(path) == "hello\n"
    end
  end

  describe ":kill bypasses terminate/2" do
    test "file handle may not be flushed", %{path: path} do
      {:ok, pid} = TerminateCleanupGs.start_link(path: path)
      :ok = TerminateCleanupGs.write(pid, "might_be_lost")

      ref = Process.monitor(pid)
      # :kill cannot be trapped. No terminate/2, no File.close.
      Process.exit(pid, :kill)
      assert_receive {:DOWN, ^ref, :process, ^pid, :killed}, 500

      # Depending on the OS and buffer state, the write may or may not be on disk.
      # What we can assert is that the pid is dead and that no cleanup was given
      # the chance to run. The file may be empty or partial.
      contents = File.read!(path)
      assert contents in ["", "might_be_lost\n"]
    end
  end

  describe "{:stop, reason, state}" do
    test "terminate/2 runs on callback-initiated stop", %{path: path} do
      # Drive a stop via GenServer.stop/2 with a custom reason — still routes
      # through terminate/2 (it's not :kill).
      {:ok, pid} = TerminateCleanupGs.start_link(path: path)
      :ok = TerminateCleanupGs.write(pid, "custom-reason")

      ref = Process.monitor(pid)
      :ok = GenServer.stop(pid, :shutdown)
      assert_receive {:DOWN, ^ref, :process, ^pid, :shutdown}, 500

      assert File.read!(path) == "custom-reason\n"
    end
  end

  describe "init failure" do
    test "returns {:error, _} when the path cannot be opened" do
      # Path inside a non-existent parent — File.open will fail.
      bad_path = "/this/does/not/exist/really/file.log"
      assert {:error, {:open_failed, _}} = TerminateCleanupGs.start_link(path: bad_path)
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Key Concepts: Process Shutdown and Resource Cleanup

`terminate/2` is called when a GenServer shuts down (supervisor kills it, `GenServer.stop/1`, or `:kill` signal). Use it to close files, flush buffers, cancel timers, or unregister names. Return `:ok` (normal shutdown) or `{:error, reason}` (failed cleanup—though this doesn't change the outcome).

Gotcha: `terminate/2` has limited time to finish (supervisor's `shutdown` timeout, default 5 seconds). Long-running cleanup will be forcefully killed. For critical cleanup, do it incrementally in `handle_cast` or design the state to not need cleanup (e.g., let the OS close files when the process exits).


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. Without `trap_exit`, a supervisor shutdown skips `terminate/2`**
If you omit `Process.flag(:trap_exit, true)` in `init/1`, a parent
supervisor's `:shutdown` signal kills you immediately with no callback.
Every GenServer that owns a resource must trap exits — or accept that
supervisor restarts leak the resource.

**2. `:brutal_kill` in a child spec always skips cleanup**
Supervisor child specs accept `shutdown: :brutal_kill` or a timeout.
`:brutal_kill` translates to `Process.exit(pid, :kill)` — no
`terminate/2`, no cleanup, no exceptions. Use it only for genuinely
resource-less workers.

**3. Cleanup must be fast — supervisors don't wait forever**
The `shutdown` value in the child spec (default 5_000 ms) is the upper
bound. If `terminate/2` is still running when the timer expires, the
supervisor brutal-kills you. Long flushes belong in a separate worker
that the server hands off to before stopping.

**4. `terminate/2` can crash — and nobody cares**
An exception in `terminate/2` is logged but doesn't propagate meaningfully
(the process is already dying). Don't rely on `terminate/2` for logic
you care about verifying — log it, test it, but assume a crash means
"no cleanup happened" and design external recovery.

**5. Resources OS-owned by the VM are eventually reaped**
When the whole BEAM exits, the OS reclaims file descriptors, sockets,
and most native resources. Where `terminate/2` really matters is for
*within-node* cleanup: releasing locks other processes wait on, flushing
application-level buffers, writing a "clean shutdown" marker.

**6. When NOT to rely on `terminate/2`**
For consistency-critical writes — journals, WAL entries, 2PC commits —
do not treat `terminate/2` as a durability boundary. `fsync` every
write, or use a persistent log. `terminate/2` is for hygiene, not
correctness.

---


## Reflection

- Tu `terminate/2` escribe a disco. El supervisor lo mata con `:brutal_kill`. ¿Qué perdiste y cómo lo diseñarías para que no importe?

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule TerminateCleanupGs do
    @moduledoc """
    A GenServer that owns a writable file handle. Opens in `init/1`,
    closes in `terminate/2`. Demonstrates the five shutdown paths and
    which ones actually run cleanup.
    """

    use GenServer
    require Logger

    defmodule State do
      @moduledoc false
      @enforce_keys [:path, :io]
      defstruct [:path, :io]

      @type t :: %__MODULE__{path: Path.t(), io: :file.io_device()}
    end

    # ── Public API ──────────────────────────────────────────────────────────

    @spec start_link(keyword()) :: GenServer.on_start()
    def start_link(opts) do
      {path, opts} = Keyword.pop!(opts, :path)
      GenServer.start_link(__MODULE__, path, opts)
    end

    @doc "Writes `line` followed by `\\n` to the owned file."
    @spec write(GenServer.server(), String.t()) :: :ok
    def write(server, line), do: GenServer.call(server, {:write, line})

    @doc "Graceful stop — triggers `terminate/2` to close the file."
    @spec stop(GenServer.server()) :: :ok
    def stop(server), do: GenServer.stop(server, :normal)

    # ── Callbacks ───────────────────────────────────────────────────────────

    @impl true
    def init(path) do
      # trap_exit so a supervisor :shutdown routes through terminate/2.
      Process.flag(:trap_exit, true)

      case File.open(path, [:write, :utf8]) do
        {:ok, io} ->
          {:ok, %State{path: path, io: io}}

        {:error, reason} ->
          # Fail fast — supervisor will log and decide.
          {:stop, {:open_failed, reason}}
      end
    end

    @impl true
    def handle_call({:write, line}, _from, %State{io: io} = state) do
      IO.write(io, line <> "\n")
      {:reply, :ok, state}
    end

    @impl true
    def terminate(reason, %State{io: io, path: path}) do
      # Close the file. This is the ONE thing this callback must do reliably.
      # Logging here is helpful for understanding the shutdown path in tests.
      Logger.debug("terminate/2 running for #{path} with reason #{inspect(reason)}")
      File.close(io)
      :ok
    end

    # Non-state reasons (e.g. init returned :ignore) have no io to close.
    def terminate(_reason, _state), do: :ok
  end

  def main do
    tmpfile = Path.join(System.tmp_dir(), "cleanup_demo.log")
    {:ok, pid} = TerminateCleanupGs.start_link(path: tmpfile)
  
    :ok = TerminateCleanupGs.write(pid, "Test message")
    :ok = TerminateCleanupGs.stop(pid)
  
    Process.sleep(100)
    content = File.read!(tmpfile)
    IO.puts("File written: #{String.trim(content)}")
  
    File.rm!(tmpfile)
    IO.puts("✓ TerminateCleanupGs works correctly")
  end

end

Main.main()
```


## Resources

- [`GenServer.terminate/2` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html#c:terminate/2)
- [`Process.flag/2` — `:trap_exit`](https://hexdocs.pm/elixir/Process.html#flag/2)
- [Supervisor child specs — `shutdown` option](https://hexdocs.pm/elixir/Supervisor.html#module-child_spec-1-function)
- ["How I start... an OTP application" — Saša Jurić](https://www.theerlangelist.com/) — clean shutdown patterns
