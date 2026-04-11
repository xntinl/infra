# Memory Profiling and Leak Detection with Recon

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. After two weeks in production, the gateway node's
memory climbs from 400 MB at startup to 1.8 GB over 48 hours before triggering
an OOM restart. The team suspects a leak but has no visibility: the application
logs show nothing abnormal, and adding more logging would require a deploy. The
fix must be diagnosable and applied to a live node without restarting it.

This exercise covers BEAM's memory model, binary leak patterns, the `:recon`
library for live diagnosis, and GenServer design changes that reduce GC pressure.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── middleware/
│       │   └── body_reader.ex      # ← you implement this (the leaky version + fix)
│       └── ...
├── test/
│   └── api_gateway/
│       └── middleware/
│           └── body_reader_test.exs
└── mix.exs
```

Add `:recon` to `mix.exs`:
```elixir
defp deps do
  [
    # ...
    {:recon, "~> 2.5"}
  ]
end
```

---

## The business problem

Two requirements:

1. **Binary leak diagnosis**: the request body reader middleware accumulates
   large binaries in process heap. Even after requests complete, the binaries
   are not released because the processes that held them keep a sub-binary
   reference. The fix requires understanding ProcBin vs heap binary and knowing
   when to force GC.

2. **Memory snapshot tooling**: a `ApiGateway.Dev.MemorySnapshot` module that
   wraps `:recon` to produce a structured memory report without a running
   `:observer` GUI — usable from `iex -S mix` on the production node.

---

## BEAM's memory model — why binaries leak

BEAM has two binary storage locations:

### Heap binaries (≤ 64 bytes)
Stored directly in the process heap. Garbage collected with the process heap.
When the process dies, the binary is freed immediately.

### Reference-counted binaries — ProcBin (> 64 bytes)
Stored in a shared binary heap outside process memory. The process heap holds
a `ProcBin` pointer (16 bytes). Multiple processes can reference the same binary
without copying it.

**The leak pattern**:
```
Request process reads body (10 KB binary) → stored in shared binary heap
Process creates a sub-binary: String.slice(body, 0, 100)
Sub-binary holds a ProcBin reference to the original 10 KB binary
Request process completes, its heap is GC'd
BUT: the ProcBin reference count is still > 0 — the 10 KB binary survives
```

Sub-binaries and pattern-match results from large binaries keep the original
ProcBin alive. This is the most common BEAM memory leak pattern.

**The fix**: copy the sub-binary with `:binary.copy/1` to create an independent
heap binary (if ≤ 64 bytes) or a new ProcBin (if > 64 bytes) that does not
reference the original:

```elixir
# LEAKS — sub_binary holds a ProcBin reference to the 10 KB body
sub = binary_part(body, 0, 50)

# SAFE — independent copy, original ProcBin can be collected
sub = :binary.copy(binary_part(body, 0, 50))
```

---

## `:recon` — safe production diagnosis

`:recon` is designed for use on live production nodes. Its functions are
rate-limited and safe; they do not crash the node or cause significant overhead.

Key functions:

```elixir
# Top processes by memory
:recon.proc_count(:memory, 10)
# => [{#PID<0.123.0>, 2_097_152, [{registered_name, []}, {current_function, ...}]}, ...]

# Top processes by binary memory (detects binary leaks)
:recon.proc_count(:binary_memory, 10)

# Process info for a specific pid
:recon.info(pid, [:memory, :binary, :message_queue_len, :current_function])

# Binary memory usage across all processes
:recon_alloc.memory(:usage)

# Force GC on a process (use sparingly)
:recon.proc_count(:binary_memory, 5)
|> Enum.each(fn {pid, _, _} -> :erlang.garbage_collect(pid) end)
```

---

## Implementation

### Step 1: `lib/api_gateway/middleware/body_reader.ex`

```elixir
defmodule ApiGateway.Middleware.BodyReader do
  @moduledoc """
  Reads and parses the request body, storing the result in conn.assigns.

  This middleware has two implementations:
    - read_body_leaky/2: exhibits the ProcBin sub-binary leak pattern
    - read_body_safe/2: uses :binary.copy/1 to break sub-binary references

  The call/2 function delegates to read_body_safe/2.
  """

  alias ApiGateway.Conn

  use ApiGateway.Middleware.Behaviour

  @impl true
  def call(conn, opts) do
    read_body_safe(conn, opts)
  end

  @doc """
  LEAKY: reads the request body and extracts sub-fields using binary_part/3.
  The sub-binary references keep the original body ProcBin alive after the
  request completes.
  """
  @spec read_body_leaky(Conn.t(), keyword()) :: Conn.t()
  def read_body_leaky(conn, _opts) do
    # Simulate reading a large body (in production this comes from the socket)
    body = Map.get(conn, :raw_body, "")

    # These sub-binaries hold ProcBin references to `body`
    # If body > 64 bytes, these references prevent GC of the full body
    content_type = extract_content_type(body)
    first_line = extract_first_line(body)

    %{conn | assigns: Map.merge(conn.assigns || %{}, %{
      content_type: content_type,
      first_line: first_line,
      body_size: byte_size(body)
    })}
  end

  @doc """
  SAFE: uses :binary.copy/1 to break ProcBin references.
  After this function returns, the original body binary can be GC'd.
  """
  @spec read_body_safe(Conn.t(), keyword()) :: Conn.t()
  def read_body_safe(conn, _opts) do
    body = Map.get(conn, :raw_body, "")

    # HINT: wrap each extracted sub-binary with :binary.copy/1
    # HINT: :binary.copy creates an independent binary that does not reference body
    # HINT: after the function returns, `body` has no surviving references → GC eligible
    # TODO: implement — same logic as read_body_leaky but with :binary.copy/1 on each result
    content_type = extract_content_type(body)
    first_line = extract_first_line(body)

    %{conn | assigns: Map.merge(conn.assigns || %{}, %{
      content_type: content_type,
      first_line: first_line,
      body_size: byte_size(body)
    })}
  end

  # Private helpers that produce sub-binaries
  defp extract_content_type(body) when byte_size(body) > 12 do
    # Returns a sub-binary reference — caller must :binary.copy if needed
    binary_part(body, 0, min(50, byte_size(body)))
  end
  defp extract_content_type(_), do: ""

  defp extract_first_line(body) do
    case :binary.split(body, "\n") do
      [line | _] -> line   # sub-binary reference
      [] -> ""
    end
  end
end
```

### Step 2: `lib/api_gateway/dev/memory_snapshot.ex`

```elixir
defmodule ApiGateway.Dev.MemorySnapshot do
  @moduledoc """
  Wraps :recon to produce structured memory reports for live diagnosis.

  All functions are safe to call on a production node.
  They do not cause significant overhead and do not require :observer.

  Usage from IEx on the production node:
    iex> ApiGateway.Dev.MemorySnapshot.report()
    iex> ApiGateway.Dev.MemorySnapshot.top_binary_consumers(5)
    iex> ApiGateway.Dev.MemorySnapshot.force_gc_top_consumers(3)
  """

  @doc """
  Returns a structured map with system-wide memory stats.
  """
  @spec report() :: map()
  def report do
    mem = :erlang.memory()

    %{
      total_mb: div(mem[:total], 1_048_576),
      processes_mb: div(mem[:processes], 1_048_576),
      binary_mb: div(mem[:binary], 1_048_576),
      code_mb: div(mem[:code], 1_048_576),
      ets_mb: div(mem[:ets], 1_048_576),
      process_count: :erlang.system_info(:process_count),
      scheduler_count: System.schedulers_online()
    }
  end

  @doc """
  Returns the top `n` processes by binary memory usage.
  Each entry: {pid, binary_bytes, process_info_keyword_list}
  """
  @spec top_binary_consumers(pos_integer()) :: list()
  def top_binary_consumers(n \\ 10) do
    # HINT: use :recon.proc_count(:binary_memory, n)
    # TODO: implement
    []
  end

  @doc """
  Returns the top `n` processes by total heap memory.
  """
  @spec top_memory_consumers(pos_integer()) :: list()
  def top_memory_consumers(n \\ 10) do
    # HINT: use :recon.proc_count(:memory, n)
    # TODO: implement
    []
  end

  @doc """
  Forces GC on the top `n` processes by binary memory.
  Returns the total binary memory freed (before - after).

  Use this as a diagnostic tool — not a production fix.
  Frequent forced GC indicates a binary leak that must be fixed at the source.
  """
  @spec force_gc_top_consumers(pos_integer()) :: non_neg_integer()
  def force_gc_top_consumers(n \\ 5) do
    before_total = :erlang.memory(:binary)

    top_binary_consumers(n)
    |> Enum.each(fn {pid, _bytes, _info} ->
      # HINT: use :erlang.garbage_collect(pid) — safe to call on any pid
      # HINT: it returns false if the pid no longer exists — handle gracefully
      # TODO: implement
      _ = pid
    end)

    after_total = :erlang.memory(:binary)
    max(0, before_total - after_total)
  end

  @doc """
  Returns process info for a specific pid as a map.
  """
  @spec process_info(pid()) :: map()
  def process_info(pid) do
    keys = [:memory, :binary, :message_queue_len, :current_function, :registered_name, :status]

    case :recon.info(pid, keys) do
      info when is_list(info) -> Map.new(info)
      _ -> %{}
    end
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/body_reader_test.exs
defmodule ApiGateway.Middleware.BodyReaderTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Middleware.BodyReader
  alias ApiGateway.Dev.MemorySnapshot

  # A body large enough to be stored as ProcBin (> 64 bytes)
  @large_body String.duplicate("application/json; charset=utf-8\nContent-Length: 42\n", 10)

  describe "read_body_safe/2" do
    test "assigns are populated correctly" do
      conn = %ApiGateway.Conn{method: "POST", path: "/", assigns: %{}, raw_body: @large_body}
      result = BodyReader.read_body_safe(conn, [])

      assert result.assigns.body_size == byte_size(@large_body)
      assert is_binary(result.assigns.content_type)
      assert is_binary(result.assigns.first_line)
    end

    test "safe version produces same assigns as leaky version" do
      conn = %ApiGateway.Conn{method: "POST", path: "/", assigns: %{}, raw_body: @large_body}
      leaky = BodyReader.read_body_leaky(conn, [])
      safe = BodyReader.read_body_safe(conn, [])

      assert leaky.assigns == safe.assigns
    end

    test "extracted sub-binaries are independent copies (not sub-binary references)" do
      conn = %ApiGateway.Conn{method: "POST", path: "/", assigns: %{}, raw_body: @large_body}
      result = BodyReader.read_body_safe(conn, [])

      # :binary.referenced_byte_size/1 returns the size of the original binary
      # that a binary references. For a truly independent copy:
      #   :binary.referenced_byte_size(copy) == byte_size(copy)
      # For a sub-binary reference:
      #   :binary.referenced_byte_size(sub) == byte_size(original)  (much larger)
      ct = result.assigns.content_type
      if byte_size(ct) > 0 do
        assert :binary.referenced_byte_size(ct) == byte_size(ct),
               "content_type is a sub-binary reference — use :binary.copy/1"
      end
    end
  end

  describe "MemorySnapshot" do
    test "report/0 returns a map with expected keys" do
      report = MemorySnapshot.report()

      assert is_map(report)
      assert is_integer(report.total_mb) and report.total_mb > 0
      assert is_integer(report.binary_mb)
      assert is_integer(report.process_count) and report.process_count > 0
    end

    test "top_binary_consumers/1 returns a list" do
      result = MemorySnapshot.top_binary_consumers(3)
      assert is_list(result)
    end

    test "top_memory_consumers/1 returns a list of length <= n" do
      result = MemorySnapshot.top_memory_consumers(5)
      assert is_list(result)
      assert length(result) <= 5
    end

    test "force_gc_top_consumers/1 returns a non-negative integer" do
      freed = MemorySnapshot.force_gc_top_consumers(3)
      assert is_integer(freed) and freed >= 0
    end

    test "process_info/1 returns a map for the current process" do
      info = MemorySnapshot.process_info(self())
      assert is_map(info)
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/middleware/body_reader_test.exs --trace
```

---

## Trade-off analysis

| Approach | Binary leak risk | Memory overhead | GC pressure | Throughput |
|----------|-----------------|-----------------|-------------|------------|
| Return sub-binary directly | High — original ProcBin survives | Low (16-byte ProcBin ptr) | Low (no copy) | Highest |
| `:binary.copy/1` always | None | Medium (extra allocation) | Medium (triggers GC sooner) | Lower |
| Avoid large binary creation | None | Lowest | Lowest | Highest |
| Stream body (don't accumulate) | None | Constant | None | Best for large bodies |

**Rule of thumb**: use `:binary.copy/1` for any sub-binary that will outlive
the function that created the original large binary. For sub-binaries that are
local (used and discarded in the same function), copying is unnecessary.

---

## Common production mistakes

**1. Calling `:erlang.garbage_collect/1` in production loops**
Forced GC is a diagnostic tool, not a fix. If you need to call `garbage_collect`
to prevent OOM, the real problem is a sub-binary reference or a process accumulating
state. Find and fix the source; do not mask it with forced GC.

**2. Confusing `:erlang.memory(:binary)` growing with a leak**
`:erlang.memory(:binary)` grows when large binaries are *being used*, not only when
they leak. A spike followed by a return to baseline is normal. A monotonically
increasing baseline over hours is a leak signal. Use `:recon.proc_count(:binary_memory, 10)`
to find which process owns the growing binaries.

**3. Using `String.split/2` on large binaries and keeping the results in GenServer state**
`String.split` returns a list of sub-binary references. If these are stored in a
GenServer's state, the original binary is pinned in memory until the GenServer
restarts. Always apply `:binary.copy/1` to list elements before storing.

**4. Not calling `:recon` — using `:observer` instead in production**
`:observer.start/0` over a remote shell is fine for development. In production,
the Observer GUI connects over distribution and streams all process state to your
laptop. This creates significant network and CPU overhead. Use `:recon` functions
instead — they compute summaries on the node and return compact results.

**5. Adding `:recon` only to `:dev` deps**
`:recon` is a diagnostic library that must be present in the production release.
Add it to the main `deps/0` list (not scoped to `:dev` or `:test`), and include
it in the release `applications` list. A library you can't use in production is
useless for production diagnosis.

---

## Resources

- [`:recon` documentation](https://ferd.github.io/recon/) — Fred Hébert's production diagnostic library
- [Erlang in Anger — Fred Hébert](https://www.erlang-in-anger.com/) — free book, chapter 7 covers memory analysis
- [BEAM book: binaries and memory](https://happi.github.io/theBeamBook/#_binaries) — ProcBin vs heap binary internals
- [`:binary.referenced_byte_size/1` — Erlang docs](https://www.erlang.org/doc/man/binary.html#referenced_byte_size-1) — diagnosing sub-binary references
- [`:recon_alloc` — memory allocator stats](https://ferd.github.io/recon/recon_alloc.html) — allocator-level memory analysis
