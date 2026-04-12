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

## Why OT and not CRDT for this exercise

Both Operational Transformation and CRDTs solve the convergence problem. CRDTs (specifically YATA/Yjs) are simpler to implement correctly because they embed ordering into the operation identifier — no `transform` function required. However, OT is educationally more valuable: it forces you to understand the core convergence problem at the algorithmic level. You will implement both (OT for the server, a CRDT simulation for comparison) and observe where the implementations diverge in complexity.

## Why Lamport clocks rather than wall-clock timestamps for operation ordering

Wall-clock timestamps are unreliable across distributed systems (NTP drift, clock skew up to 250ms). If two users insert at position 5 "simultaneously" and we order by wall clock, the result depends on which computer's clock is faster. Lamport clocks are logical: each operation carries a counter that is incremented on send and updated to `max(local, received) + 1` on receive. Two operations from the same user are always ordered; concurrent operations from different users are ordered by a deterministic tiebreaker (e.g., user ID).

## Why per-user undo is hard in collaborative context

In a single-user editor, undo just reverses the last operation. In a collaborative editor, after user A applies `insert(5, "hello")`, user B may apply `insert(0, "world")`. Now user A's undo must reverse `insert(5, "hello")` — but the document has changed. The inverse operation `delete(5, 5)` would delete from the wrong position because user B's insertion shifted everything. The undo operation must be transformed through all operations applied after the original, including operations from other users.

## Project Structure

```
collab/
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

```elixir
defmodule Collab.OT.Operation do
  @enforce_keys [:type, :pos, :clock, :user_id]
  defstruct [:type, :pos, :text, :len, :clock, :user_id, :deleted_text]

  @type t ::
    %__MODULE__{type: :insert, pos: non_neg_integer(), text: String.t(), clock: non_neg_integer(), user_id: String.t()} |
    %__MODULE__{type: :delete, pos: non_neg_integer(), len: pos_integer(), clock: non_neg_integer(), user_id: String.t(), deleted_text: String.t() | nil}
end

defmodule Collab.OT.Apply do
  alias Collab.OT.Operation

  @doc "Apply an operation to a document string. Returns new string."
  def apply(doc, %Operation{type: :insert, pos: pos, text: text}) do
    {before, after_} = String.split_at(doc, pos)
    before <> text <> after_
  end

  def apply(doc, %Operation{type: :delete, pos: pos, len: len}) do
    {before, rest} = String.split_at(doc, pos)
    {_deleted, after_} = String.split_at(rest, len)
    before <> after_
  end
end
```

### Step 2: OT transform function

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
# test/ot/transform_test.exs
defmodule Collab.OT.TransformTest do
  use ExUnit.Case, async: true
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

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
```

Target: <1ms to apply a remote op on a 10k-character document.

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
