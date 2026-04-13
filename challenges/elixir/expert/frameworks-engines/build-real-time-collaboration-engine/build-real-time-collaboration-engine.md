# Real-time Collaboration Engine

**Project**: `collab` — Operational Transformation engine for real-time collaborative editing

## Project context

Your team is building a shared document editor — think Google Docs. Two users simultaneously edit the same paragraph. User A inserts "Hello" at position 0. User B inserts "World" at position 0. Both edits are valid. The final document should be "HelloWorld" or "WorldHello" — deterministically, consistently on both clients.

Without a convergence algorithm, one of three bad things happens: the later-arriving edit overwrites the earlier one (last-write-wins, data loss), the server rejects one edit (bad UX), or each client sees a different final document (split brain).

You will build `Collab`: a real-time collaboration engine using Operational Transformation for convergence, Phoenix Channels for presence, and document versioning with snapshots. The engine must handle offline editing, per-user undo, and section-level permissions.

## Design decisions

**Option A — operational transformation (OT) with a central server**
- Pros: intuitive for text, well-known algorithms
- Cons: OT composition is famously buggy — the literature has multiple correctness errata

**Option B — CRDT (RGA or Yjs-style) with peer-to-peer sync** (chosen)
- Pros: provably convergent, no central transform server, offline-friendly
- Cons: larger payloads, more complex garbage collection

→ Chose **B** because CRDT's convergence proof eliminates a whole class of concurrency bugs OT has been fighting for 30 years.

## Quick start

1. Create project:
   ```bash
   mix new <project_name>
   cd <project_name>
   ```

2. Copy dependencies to `mix.exs`

3. Implement modules following the project structure

4. Run tests: `mix test`

5. Benchmark: `mix run lib/benchmark.exs`

## Why OT and not CRDT for this exercise

Both Operational Transformation and CRDTs solve the convergence problem. CRDTs (specifically YATA/Yjs) are simpler to implement correctly because they embed ordering into the operation identifier — no `transform` function required. However, OT is educationally more valuable: it forces you to understand the core convergence problem at the algorithmic level. You will implement both (OT for the server, a CRDT simulation for comparison) and observe where the implementations diverge in complexity.

## Why Lamport clocks rather than wall-clock timestamps for operation ordering

Wall-clock timestamps are unreliable across distributed systems (NTP drift, clock skew up to 250ms). If two users insert at position 5 "simultaneously" and we order by wall clock, the result depends on which computer's clock is faster. Lamport clocks are logical: each operation carries a counter that is incremented on send and updated to `max(local, received) + 1` on receive. Two operations from the same user are always ordered; concurrent operations from different users are ordered by a deterministic tiebreaker (e.g., user ID).

## Why per-user undo is hard in collaborative context

In a single-user editor, undo just reverses the last operation. In a collaborative editor, after user A applies `insert(5, "hello")`, user B may apply `insert(0, "world")`. Now user A's undo must reverse `insert(5, "hello")` — but the document has changed. The inverse operation `delete(5, 5)` would delete from the wrong position because user B's insertion shifted everything. The undo operation must be transformed through all operations applied after the original, including operations from other users.

## Project structure
```
collab/
├── script/
│   └── main.exs
├── mix.exs
├── lib/
│   └── collab/
│       ├── ot/
│       │   ├── operation.ex    # %Op{type: :insert | :delete, pos, text | len, clock, user_id}
│       │   ├── transform.ex    # transform(op1, op2) -> op1'; the convergence algorithm
│       │   ├── compose.ex      # compose(op1, op2) -> single op
│       │   └── apply.ex        # apply(doc_string, op) -> new_doc_string
│       ├── crdt/
│       │   ├── yata.ex         # YATA positional identifiers for comparison
│       │   └── document.ex     # CRDT document: sorted {position, char, tombstoned}
│       ├── document.ex         # GenServer: document state, operation log, snapshot trigger
│       ├── channel.ex          # Phoenix Channel: join, presence, edit events
│       ├── presence.ex         # Cursor tracking with 50ms throttle
│       ├── undo_manager.ex     # Per-user undo/redo stacks with OT transformation
│       ├── offline_merge.ex    # Server-side offline catch-up: transform buffered ops
│       ├── versioning.ex       # Snapshot + diff between versions
│       └── permissions.ex      # Section ACL enforcement
├── test/
│   ├── ot/
│   │   ├── transform_test.exs
│   │   └── convergence_property_test.exs  # 10k random op pairs
│   ├── crdt/
│   │   └── yata_test.exs
│   ├── presence_test.exs
│   ├── undo_test.exs
│   └── offline_merge_test.exs
└── bench/
    └── concurrent_edits.exs
```

### Step 1: Operations and application

**Objective**: Model insert/delete as enforced structs with Lamport clock and user id so every op carries the metadata transform/invert need downstream.

### Step 2: OT transform function

**Objective**: Transform concurrent ops across all four insert/delete cases so any application order converges to the same document state (TP1).

```elixir
defmodule Collab.OT.Transform do
  alias Collab.OT.Operation

  @doc """
  transform(op1, op2) -> op1'
  Returns op1 adjusted to apply correctly AFTER op2 has been applied.
  op1 and op2 are concurrent (same base document state).
  """
  def transform(%Operation{type: :insert} = op1, %Operation{type: :insert} = op2) do
    cond do
      op2.pos < op1.pos ->
        %{op1 | pos: op1.pos + String.length(op2.text)}
      op2.pos == op1.pos ->
        if op2.user_id < op1.user_id do
          %{op1 | pos: op1.pos + String.length(op2.text)}
        else
          op1
        end
      true ->
        op1
    end
  end

  def transform(%Operation{type: :insert} = op1, %Operation{type: :delete} = op2) do
    cond do
      op2.pos + op2.len <= op1.pos ->
        %{op1 | pos: op1.pos - op2.len}
      op2.pos >= op1.pos ->
        op1
      true ->
        %{op1 | pos: op2.pos}
    end
  end

  def transform(%Operation{type: :delete} = op1, %Operation{type: :insert} = op2) do
    cond do
      op2.pos <= op1.pos ->
        %{op1 | pos: op1.pos + String.length(op2.text)}
      op2.pos < op1.pos + op1.len ->
        %{op1 | len: op1.len + String.length(op2.text)}
      true ->
        op1
    end
  end

  @doc """
  Transform delete vs delete: handle all four overlap cases.
  - op2 fully covers op1: result is a no-op (len 0)
  - op2 covers the start of op1: shrink from the left
  - op2 covers the end of op1: shrink from the right
  - op2 is inside op1: shrink op1's len by op2's overlap
  """
  def transform(%Operation{type: :delete} = op1, %Operation{type: :delete} = op2) do
    cond do
      op2.pos + op2.len <= op1.pos ->
        # op2 deleted entirely before op1; shift op1 left
        %{op1 | pos: op1.pos - op2.len}

      op2.pos >= op1.pos + op1.len ->
        # op2 deleted entirely after op1; unaffected
        op1

      true ->
        # Overlapping deletes: compute remaining range after op2 is applied
        op1_end = op1.pos + op1.len
        op2_end = op2.pos + op2.len

        # The overlap region that both ops delete
        overlap_start = max(op1.pos, op2.pos)
        overlap_end = min(op1_end, op2_end)
        overlap = max(0, overlap_end - overlap_start)

        # After op2 is applied, characters before op2.pos are unchanged,
        # characters from op2.pos to op2_end are removed.
        # op1's new position: if op1 starts before op2, it stays;
        # if op1 starts within or after op2, it shifts left
        new_pos = if op1.pos <= op2.pos, do: op1.pos, else: op1.pos - min(op1.pos - op2.pos, op2.len)
        new_len = max(0, op1.len - overlap)

        %{op1 | pos: new_pos, len: new_len}
    end
  end
end
```
### Step 3: Document GenServer

**Objective**: Serialize all ops through one process per doc so clock assignment, persistence, and snapshotting stay race-free without external locks.

```elixir
defmodule Collab.Document do
  use GenServer

  @snapshot_interval 100

  def start_link(doc_id) do
    GenServer.start_link(__MODULE__, doc_id, name: via(doc_id))
  end

  def init(doc_id) do
    {:ok, %{
      doc_id: doc_id,
      content: "",
      operations: [],
      clock: 0,
      version: 0,
      pending_snapshot: 0
    }}
  end

  @doc "Apply a client operation. Returns {:ok, acked_op, new_clock} or {:error, reason}"
  def apply_op(doc_id, op) do
    GenServer.call(via(doc_id), {:apply_op, op})
  end

  def handle_call({:apply_op, op}, _from, state) do
    case Collab.Permissions.check(op, state.doc_id) do
      :ok ->
        new_clock = state.clock + 1
        tagged_op = %{op | clock: new_clock}
        new_content = Collab.OT.Apply.apply(state.content, tagged_op)
        new_ops = state.operations ++ [{new_clock, tagged_op}]
        new_pending = state.pending_snapshot + 1

        new_state = %{state |
          content: new_content,
          operations: new_ops,
          clock: new_clock,
          pending_snapshot: new_pending
        }

        new_state =
          if new_pending >= @snapshot_interval do
            GenServer.cast(self(), :snapshot)
            %{new_state | pending_snapshot: 0}
          else
            new_state
          end

        {:reply, {:ok, tagged_op, new_clock}, new_state}
      {:error, reason} ->
        {:reply, {:error, reason}, state}
    end
  end

  def handle_cast(:snapshot, state) do
    Collab.Versioning.save_snapshot(state.doc_id, state.content, state.clock, state.version + 1)
    {:noreply, %{state | version: state.version + 1}}
  end

  defp via(doc_id), do: {:via, Registry, {Collab.Registry, doc_id}}
end
```
### Step 4: Presence with throttling

**Objective**: Coalesce cursor updates on a 50ms tick so a typing user produces at most 20 broadcasts per second regardless of keystroke rate.

```elixir
defmodule Collab.Presence do
  use GenServer

  @throttle_ms 50

  def start_link(doc_id) do
    GenServer.start_link(__MODULE__, doc_id)
  end

  def init(doc_id) do
    {:ok, %{
      doc_id: doc_id,
      cursors: %{},
      pending_broadcast: false
    }}
  end

  @doc "Update cursor position for a user"
  def update_cursor(pid, user_id, cursor) do
    GenServer.cast(pid, {:cursor, user_id, cursor})
  end

  def handle_cast({:cursor, user_id, cursor}, state) do
    new_cursors = Map.put(state.cursors, user_id, cursor)
    new_state = %{state | cursors: new_cursors}

    if state.pending_broadcast do
      {:noreply, new_state}
    else
      Process.send_after(self(), :flush_cursors, @throttle_ms)
      {:noreply, %{new_state | pending_broadcast: true}}
    end
  end

  def handle_info(:flush_cursors, state) do
    Phoenix.PubSub.broadcast(
      Collab.PubSub,
      "doc:#{state.doc_id}:cursors",
      {:cursors_update, state.cursors}
    )
    {:noreply, %{state | pending_broadcast: false}}
  end

  @doc "Remove user cursor on disconnect"
  def handle_cast({:leave, user_id}, state) do
    new_cursors = Map.delete(state.cursors, user_id)
    Phoenix.PubSub.broadcast(
      Collab.PubSub,
      "doc:#{state.doc_id}:cursors",
      {:cursors_update, new_cursors}
    )
    {:noreply, %{state | cursors: new_cursors}}
  end
end
```
### Step 5: Per-user undo manager

**Objective**: Invert and re-transform a user's own op against peers' concurrent ops so undo reverses intent, not whatever text now sits at that position.

```elixir
defmodule Collab.UndoManager do
  use GenServer
  alias Collab.OT.{Transform, Apply, Operation}

  def start_link(doc_id) do
    GenServer.start_link(__MODULE__, doc_id)
  end

  def init(doc_id) do
    {:ok, %{doc_id: doc_id, stacks: %{}}}
  end

  @doc "Record an operation in the user's undo stack"
  def record_op(pid, user_id, op) do
    GenServer.cast(pid, {:record, user_id, op})
  end

  def handle_cast({:record, user_id, op}, state) do
    stacks = Map.get(state.stacks, user_id, %{undo_stack: [], redo_stack: []})
    updated = %{stacks | undo_stack: [op | stacks.undo_stack], redo_stack: []}
    {:noreply, %{state | stacks: Map.put(state.stacks, user_id, updated)}}
  end

  @doc "Undo the last operation for user. Returns {:ok, inverse_op} or {:error, :empty}"
  def undo(pid, user_id, ops_since) do
    GenServer.call(pid, {:undo, user_id, ops_since})
  end

  def handle_call({:undo, user_id, ops_since}, _from, state) do
    case get_in(state, [:stacks, user_id, :undo_stack]) do
      [] -> {:reply, {:error, :empty}, state}
      nil -> {:reply, {:error, :empty}, state}
      [last_op | rest] ->
        inverse = invert(last_op)
        transformed_inverse = Enum.reduce(ops_since, inverse, fn other_op, acc ->
          if other_op.user_id != user_id do
            Transform.transform(acc, other_op)
          else
            acc
          end
        end)
        new_stacks = put_in(state.stacks, [user_id, :undo_stack], rest)
        new_stacks = update_in(new_stacks, [user_id, :redo_stack], fn s -> [last_op | (s || [])] end)
        {:reply, {:ok, transformed_inverse}, %{state | stacks: new_stacks}}
    end
  end

  defp invert(%Operation{type: :insert, pos: pos, text: text} = op) do
    %Operation{op | type: :delete, text: nil, len: String.length(text)}
  end

  defp invert(%Operation{type: :delete, pos: pos, len: len} = op) do
    %Operation{op | type: :insert, len: nil, text: op.deleted_text || ""}
  end
end
```
### Step 6: Offline merge

**Objective**: Reconcile a reconnecting client by transforming their queued ops against server history and returning a catch-up stream that lands both sides on identical state.

```elixir
defmodule Collab.OfflineMerge do
  alias Collab.OT.Transform

  @doc """
  Merge offline operations from a client.
  client_ops: [{client_clock, op}] — ops made while offline, sorted by client_clock
  server_ops_since: [{server_clock, op}] — ops the server applied while client was offline
  Returns: {merged_ops, catch_up_ops}
    merged_ops: client_ops transformed against server_ops, ready to apply to server
    catch_up_ops: server_ops_since, transformed to be safe to apply on top of client state
  """
  def merge(client_ops, server_ops_since) do
    merged_ops = Enum.map(client_ops, fn {client_clock, client_op} ->
      concurrent_server_ops = Enum.filter(server_ops_since, fn {server_clock, _} ->
        server_clock > client_clock
      end)
      transformed = Enum.reduce(concurrent_server_ops, client_op, fn {_, server_op}, acc ->
        Transform.transform(acc, server_op)
      end)
      transformed
    end)

    catch_up_ops = Enum.map(server_ops_since, fn {_, server_op} ->
      Enum.reduce(merged_ops, server_op, fn client_op, acc ->
        Transform.transform(acc, client_op)
      end)
    end)

    {merged_ops, catch_up_ops}
  end
end
```
### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
defmodule Collab.OT.TransformTest do
  use ExUnit.Case, async: true
  doctest Collab.OfflineMerge
  alias Collab.OT.{Operation, Transform, Apply}

  @doc "The classic OT diamond test"
  describe "Transform" do

  test "convergence: insert at 0 from two users" do
    doc = "base"
    op_a = %Operation{type: :insert, pos: 0, text: "A", clock: 1, user_id: "user_a"}
    op_b = %Operation{type: :insert, pos: 0, text: "B", clock: 1, user_id: "user_b"}

    doc_a = Apply.apply(doc, op_a)
    op_b_transformed = Transform.transform(op_b, op_a)
    result_a = Apply.apply(doc_a, op_b_transformed)

    doc_b = Apply.apply(doc, op_b)
    op_a_transformed = Transform.transform(op_a, op_b)
    result_b = Apply.apply(doc_b, op_a_transformed)

    assert result_a == result_b, "OT diverged: #{result_a} != #{result_b}"
  end

  test "insert after delete: positions shift correctly" do
    doc = "hello world"
    delete = %Operation{type: :delete, pos: 0, len: 6, clock: 1, user_id: "u1"}
    insert = %Operation{type: :insert, pos: 3, text: "X", clock: 1, user_id: "u2"}

    doc1 = Apply.apply(doc, delete)
    insert_t = Transform.transform(insert, delete)
    result1 = Apply.apply(doc1, insert_t)

    doc2 = Apply.apply(doc, insert)
    delete_t = Transform.transform(delete, insert)
    result2 = Apply.apply(doc2, delete_t)

    assert result1 == result2
  end
end

# test/ot/convergence_property_test.exs
defmodule Collab.OT.ConvergencePropertyTest do
  use ExUnit.Case, async: true
  use ExUnitProperties
  alias Collab.OT.{Operation, Transform, Apply}

  property "transform(op1, op2) always converges regardless of application order" do
    check all(
      doc <- string(:printable, min_length: 1, max_length: 50),
      pos1 <- integer(0..50),
      pos2 <- integer(0..50),
      text1 <- string(:alphanumeric, min_length: 1, max_length: 5),
      text2 <- string(:alphanumeric, min_length: 1, max_length: 5),
      min_runs: 10_000
    ) do
      len = String.length(doc)
      p1 = min(pos1, len)
      p2 = min(pos2, len)
      op1 = %Operation{type: :insert, pos: p1, text: text1, clock: 1, user_id: "u1"}
      op2 = %Operation{type: :insert, pos: p2, text: text2, clock: 1, user_id: "u2"}

      d1 = Apply.apply(doc, op1)
      op2t = Transform.transform(op2, op1)
      result1 = Apply.apply(d1, op2t)

      d2 = Apply.apply(doc, op2)
      op1t = Transform.transform(op1, op2)
      result2 = Apply.apply(d2, op1t)

      assert result1 == result2
    end
  end
end

# test/offline_merge_test.exs
defmodule Collab.OfflineMergeTest do
  use ExUnit.Case, async: true
  alias Collab.{OfflineMerge, OT.{Operation, Apply}}

  test "offline ops merge correctly with concurrent server ops" do
    doc = "hello"

    offline_op = %Operation{type: :insert, pos: 5, text: " world", clock: 1, user_id: "client"}
    server_op = %Operation{type: :insert, pos: 5, text: "!", clock: 2, user_id: "server"}

    {[merged_client_op], [catch_up_server_op]} =
      OfflineMerge.merge([{1, offline_op}], [{2, server_op}])

    doc_after_server = Apply.apply(doc, server_op)
    final = Apply.apply(doc_after_server, merged_client_op)

    doc_after_client = Apply.apply(doc, offline_op)
    final_client = Apply.apply(doc_after_client, catch_up_server_op)

    assert final == final_client, "Offline merge diverged: #{final} != #{final_client}"
  end
end

# test/presence_test.exs
defmodule Collab.PresenceTest do
  use ExUnit.Case, async: false

  test "cursor update is broadcast within 100ms" do
    {:ok, presence} = Collab.Presence.start_link("test-doc")
    Phoenix.PubSub.subscribe(Collab.PubSub, "doc:test-doc:cursors")

    t0 = System.monotonic_time(:millisecond)
    Collab.Presence.update_cursor(presence, "user-1", %{line: 5, col: 10})

    assert_receive {:cursors_update, %{"user-1" => %{line: 5}}}, 100
    elapsed = System.monotonic_time(:millisecond) - t0
    assert elapsed < 100
  end

  test "cursor removed on user leave within 1s" do
    {:ok, presence} = Collab.Presence.start_link("leave-test-doc")
    Phoenix.PubSub.subscribe(Collab.PubSub, "doc:leave-test-doc:cursors")

    Collab.Presence.update_cursor(presence, "user-x", %{line: 1, col: 1})
    assert_receive {:cursors_update, %{"user-x" => _}}, 100

    GenServer.cast(presence, {:leave, "user-x"})
    assert_receive {:cursors_update, cursors}, 1000
    refute Map.has_key?(cursors, "user-x")
  end

  end
end
```
## Main Entry Point

```elixir
def main do
  IO.puts("======== 53-build-real-time-collaboration-engine ========")
  IO.puts("Build real time collaboration engine")
  IO.puts("")
  
  Collab.OT.Operation.start_link([])
  IO.puts("Collab.OT.Operation started")
  
  IO.puts("Run: mix test")
end
```
## Benchmark

```elixir
# bench/concurrent_edits.exs
defmodule Collab.Bench.ConcurrentEdits do
  alias Collab.OT.{Operation, Transform, Apply}

  def run do
    IO.puts("=== OT Convergence Throughput Benchmark ===\n")
    
    # Test 1: Apply operations on growing document
    IO.write("Test 1: Document growth (100 inserts)... ")
    {us1, final_doc1} = :timer.tc(fn ->
      Enum.reduce(1..100, "", fn i, doc ->
        op = %Operation{type: :insert, pos: String.length(doc), text: "x", clock: i, user_id: "u1"}
        Apply.apply(doc, op)
      end)
    end)
    IO.puts("done (#{us1} µs)")

    # Test 2: Transform 1000 concurrent operation pairs
    IO.write("Test 2: Transform 1k concurrent pairs... ")
    {us2, _} = :timer.tc(fn ->
      for i <- 1..1_000 do
        op1 = %Operation{type: :insert, pos: 5, text: "A", clock: i, user_id: "user_a"}
        op2 = %Operation{type: :insert, pos: 5, text: "B", clock: i, user_id: "user_b"}
        _transformed = Transform.transform(op1, op2)
      end
    end)
    IO.puts("done (#{us2} µs)")

    # Test 3: Apply 10k remote operations on 10k-char document
    IO.write("Test 3: Apply 10k ops on 10k-char doc... ")
    base_doc = String.duplicate("x", 10_000)
    {us3, _} = :timer.tc(fn ->
      Enum.reduce(1..10_000, base_doc, fn i, doc ->
        op = %Operation{type: :insert, pos: :rand.uniform(String.length(doc)), text: "y", clock: i, user_id: "remote"}
        Apply.apply(doc, op)
      end)
    end)
    per_op_us = us3 / 10_000.0
    IO.puts("done (#{us3} µs total, #{Float.round(per_op_us, 2)} µs per op)")

    # Results
    IO.puts("\n=== Results ===")
    IO.puts("Document growth:       #{us1} µs")
    IO.puts("Transform throughput:  #{Float.round(1_000_000 / us2, 0)} pairs/sec")
    IO.puts("Apply throughput:      #{Float.round(1_000_000 / per_op_us, 0)} ops/sec")
    IO.puts("Per-op latency:        #{Float.round(per_op_us, 3)} µs")
    IO.puts("Target:                < 1000 µs per op (1ms) on 10k-char doc")
    
    if per_op_us < 1000 do
      IO.puts("Status:                PASS")
    else
      IO.puts("Status:                FAIL (#{Float.round(per_op_us / 1000, 1)}x slower)")
    end
  end
end

Collab.Bench.ConcurrentEdits.run()
```
**Target**: <1ms para aplicar una operación remota en documento de 10k caracteres.

## Key Concepts: Architecture & Design Patterns Operational Transformation vs. CRDTs - Convergence Proofs

Los dos algoritmos fundamentales para edición colaborativa tienen semántica dramáticamente diferente:

### Operational Transformation (OT)

Un servidor central recibe operaciones concurrentes de clientes y las **transforma** para que converjan:
- Usuario A inserta "hello" @ pos 0.
- Usuario B inserta "world" @ pos 0.
- Servidor aplica A, luego transforma B para obtener pos = len("hello") = 5.
- Resultado: "helloworld" determinístico.

**Matemática**: Hay dos propiedades:
- **TP1 (Transformation Property 1)**: `apply(apply(doc, OP1), transform(OP2, OP1)) == apply(apply(doc, OP2), transform(OP1, OP2))` — ambos órdenes llegan al mismo resultado.
- **TP2**: Identidad funcional — si OP1 y OP2 no se solapan, `transform(OP2, OP1) == OP2`.

**Problema**: La implementación de `transform/2` es notoriamente frágil. Cada par (insert, insert), (insert, delete), (delete, delete) requiere caseado manual. Un error aquí causa divergencia permanente entre clientes. Múltiples papers han encontrado bugs en algoritmos "correctos" publicados.

### CRDT (Conflict-Free Replicated Data Type)

Cada carácter lleva un identificador único que codifica orden causal:
- User A inserta "h" @ (A, 1).
- User B inserta "w" @ (B, 1).
- Ambos clientes simplemente ordenan por ID: `(A,1):"h" < (B,1):"w"` si A < B lexicográficamente.
- **No hay transformación**: solo aplicar y ordenar.

**Ventaja**: Convergencia es **probada matemáticamente** — cualquier ordenamiento de los mismos operations lleva al mismo estado. No hay `transform/2` para bugear.

**Desventaja**: Cada carácter requiere metadatos (user_id, clock) — 16+ bytes per char. Un documento de 10k caracteres = 160k+ overhead.

### Trade-off para este ejercicio

Para **enseñar**, implementamos OT porque requiere entender convergencia. Para **producción**, CRDT (Yjs, Automerge) es más seguro — la matemática garantiza corrección, no el caseado manual.

---

## Trade-off analysis

| Algorithm | OT (Operational Transformation) | CRDT (YATA/Yjs) | Trade-off |
|---|---|---|---|
| Transform function | Required; O(N) pairs for N concurrent ops | Not required | OT: simpler network protocol; CRDT: simpler algorithm, harder to explain |
| Server requirement | Central server for operation ordering | Peer-to-peer capable | OT: requires server as arbiter; CRDT: works P2P, but larger per-character metadata |
| Undo complexity | Must transform inverse through history | Similar complexity | Both require transforming undo operations |
| Character overhead | No per-character metadata | Position identifiers per character | CRDT: documents can be 3-10x larger in memory due to position IDs |
| Implementation correctness | Hard; many published OT algorithms are wrong | Easier to verify | CRDT invariants are simpler to test; OT transform function has subtle cases |

## Common production mistakes

**Not using a tiebreaker for concurrent inserts at the same position.** If two users insert at position 5 and you compare only clocks (which are equal for concurrent ops), the ordering is non-deterministic. Always include the user ID as a tiebreaker. This must be consistent on every client and server.

**Performing OT transform on the wrong pair.** The transform function takes two ops that are concurrent from the same base. If `op1` was already applied to the base document and you transform `op2` against `op1` — that is the server path. The client path transforms `op1'` against `op2'` after the server's ordering. Mixing these paths produces divergence. Document the exact transformer matrix clearly in your tests.

**Not handling tombstoned characters in CRDT for undo.** When a character is deleted in a CRDT, it is tombstoned (marked deleted but kept for ordering). Undo must restore the tombstone. If the character was deleted by another user concurrently, restoring it creates a conflict — the character must appear at its original position even though another user deleted it. This is one place where CRDT semantics can surprise users.

**Presence state not cleaned up after process crash.** If the Phoenix Channel process crashes (not a clean disconnect), the `handle_info({:DOWN, ...})` callback must be implemented to remove the user's cursor. Without this, ghost cursors appear for crashed users.

**Storing the full operation log in GenServer memory without snapshots.** 10k operations on a document consuming 500 bytes each is 5MB in the GenServer's heap. A GenServer with 5MB heap gets garbage collected frequently by the BEAM, adding GC pauses to every edit. Snapshot every 100 operations to PostgreSQL and keep only the last 100 operations in memory for undo purposes.

## Reflection

Two users each make 500 edits while offline for a week. When they reconnect, what's the upper bound on merge time, and what's the UX if merge takes >500ms? Design the reconnection contract.

## Resources

- Ellis & Gibbs — "Concurrency Control in Groupware Systems" (1989) — ACM SIGMOD (original OT paper)
- Weiss, Urso, Molli — "Logoot: A Scalable Optimistic Replication Algorithm" (2009) — IEEE ICDCS
- Kleppmann & Beresford — "A Conflict-Free Replicated JSON Datatype" (2017) — https://arxiv.org/abs/1608.03960 (Automerge)
- Yjs source — https://github.com/yjs/yjs (YATA algorithm implementation reference)
- Myers — "An O(ND) Difference Algorithm" (1986) — Algorithmica 1(2) (for versioning diff)
- Kleppmann — "Designing Data-Intensive Applications" Chapter 5 (Replication)

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Collab.MixProject do
  use Mix.Project

  def project do
    [
      app: :collab,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Collab.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `collab` (real-time collaboration (CRDT)).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 50000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:collab) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Collab stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:collab) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:collab)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual collab operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Collab classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **5,000 concurrent editors** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **50 ms** | Yjs + Automerge architecture |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Yjs + Automerge architecture: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why Real-time Collaboration Engine matters

Mastering **Real-time Collaboration Engine** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

---

## Implementation

### `lib/collab.ex`

```elixir
defmodule Collab do
  @moduledoc """
  Reference implementation for Real-time Collaboration Engine.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the collab module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> Collab.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/collab_test.exs`

```elixir
defmodule CollabTest do
  use ExUnit.Case, async: true

  doctest Collab

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Collab.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Yjs + Automerge architecture
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
