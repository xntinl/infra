# Erlang Interop — Advanced Data Structures

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're extending `api_gateway` with two subsystems that cannot be built efficiently using
Elixir's standard library alone: a job dispatch queue and a request-priority scheduler.
Both require data structures with semantics that `List` and `Map` do not provide.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       └── middleware/
│           ├── job_queue.ex            # ← you implement this
│           ├── priority_scheduler.ex   # ← and this
│           └── erlang_adapter.ex       # ← and this
├── test/
│   └── api_gateway/
│       └── middleware/
│           ├── job_queue_test.exs
│           ├── priority_scheduler_test.exs
│           └── erlang_adapter_test.exs
└── mix.exs
```

---

## The business problem

Two problems exposed in production:

1. **Job dispatch**: the router dispatches async jobs via a GenServer. The current
   implementation appends to a list (`state ++ [job]`), which is O(n). Under burst
   traffic of 2,000 jobs/s, the GenServer's state modification alone costs ~4ms per
   enqueue. You need O(1) amortized enqueue and dequeue.

2. **Priority scheduling**: the rate limiter has three tiers of clients (`:premium`,
   `:standard`, `:free`). When the system is under pressure, premium requests must be
   served first. You need an ordered collection where `pop_min` is efficient.

3. **Legacy Erlang library**: the auth service returns user records as Erlang tuples and
   proplists. The gateway must normalize these into idiomatic Elixir maps before passing
   them downstream.

---

## Why Erlang modules and not Elixir equivalents

Elixir's standard library covers most daily work. These cases are the exceptions:

| Need | Use | Why not Elixir |
|------|-----|----------------|
| O(1) FIFO queue | `:queue` | `List` enqueue is O(n); no queue in Elixir stdlib |
| Ordered key-value | `:gb_trees` | `Map` has no ordering guarantee |
| Sorted set with membership | `:gb_sets` | `MapSet` has no ordering |
| Erlang legacy record interop | `:proplists` | No Elixir equivalent for Erlang proplists |

All Erlang standard modules are available as atoms prefixed with `:`. There is no import
step — they are loaded as part of OTP.

---

## `:queue` — how the two-stack trick works

`:queue` stores a FIFO queue as two lists: `in` (reversed) and `out`. Enqueue appends to
`in` in O(1). Dequeue takes from `out` in O(1). When `out` is empty, reverse `in` into
`out` — O(n) but amortized O(1) over N operations.

```
enqueue :a, :b, :c  →  in = [:c, :b, :a],  out = []
dequeue              →  reverse in → out = [:a, :b, :c], return :a
dequeue              →  out = [:b, :c],     return :b   (O(1))
```

## `:gb_trees` — ordered key-value

A balanced binary tree that maintains sorted order by key at all times. Unlike `Map`
(hash-based, no order), `:gb_trees` keeps keys sorted so `smallest/1`, `largest/1`, and
`to_list/1` are always O(log n) or O(n) and always in order.

Cost: O(log n) for insert and delete vs O(1) amortized for `Map`.

---

## Implementation

### Step 1: `lib/api_gateway/middleware/job_queue.ex`

```elixir
defmodule ApiGateway.Middleware.JobQueue do
  @moduledoc """
  FIFO job queue backed by :queue.

  Replaces the previous list-based implementation that had O(n) enqueue.
  All operations are O(1) amortized.
  """
  use GenServer

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, opts)
  end

  @spec enqueue(GenServer.server(), term()) :: :ok
  def enqueue(pid, job), do: GenServer.call(pid, {:enqueue, job})

  @spec dequeue(GenServer.server()) :: {:ok, term()} | {:error, :empty}
  def dequeue(pid), do: GenServer.call(pid, :dequeue)

  @spec peek(GenServer.server()) :: {:ok, term()} | {:error, :empty}
  def peek(pid), do: GenServer.call(pid, :peek)

  @spec size(GenServer.server()) :: non_neg_integer()
  def size(pid), do: GenServer.call(pid, :size)

  @spec drain(GenServer.server()) :: [term()]
  def drain(pid), do: GenServer.call(pid, :drain)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(:ok) do
    # TODO: initialize state as an empty :queue
    # HINT: :queue.new/0
  end

  @impl true
  def handle_call({:enqueue, job}, _from, queue) do
    # TODO: enqueue job at the back
    # HINT: :queue.in/2 appends to the back
  end

  @impl true
  def handle_call(:dequeue, _from, queue) do
    # TODO: dequeue from the front
    # HINT: :queue.out/1 returns {{:value, item}, new_queue} or {:empty, queue}
  end

  @impl true
  def handle_call(:peek, _from, queue) do
    # TODO: inspect without removing
    # HINT: :queue.peek/1 returns {:value, item} or :empty
  end

  @impl true
  def handle_call(:size, _from, queue) do
    # TODO: return the number of elements
    # HINT: :queue.len/1
  end

  @impl true
  def handle_call(:drain, _from, queue) do
    # TODO: return all elements in FIFO order and reset the queue
    # HINT: :queue.to_list/1 preserves FIFO order
  end
end
```

### Step 2: `lib/api_gateway/middleware/priority_scheduler.ex`

```elixir
defmodule ApiGateway.Middleware.PriorityScheduler do
  @moduledoc """
  Priority queue for request scheduling.

  Uses :gb_trees as the backing store. Keys are {priority, sequence_number}
  tuples to allow multiple items with the same numeric priority while
  preserving insertion order within the same priority level.

  Lower priority number = higher urgency (1 is served before 3).
  """

  @type priority :: pos_integer()
  @type t :: {:gb_trees.tree(), non_neg_integer()}

  @spec new() :: t()
  def new do
    # TODO: return {:gb_trees.empty(), 0}
    # The second element is the sequence counter for tie-breaking
  end

  @spec insert(t(), priority(), term()) :: t()
  def insert({tree, seq}, priority, value) do
    # TODO: insert with key {priority, seq} into the tree
    # HINT: :gb_trees.insert/3 — key must be unique; {priority, seq} guarantees that
    # HINT: return {new_tree, seq + 1}
  end

  @spec peek_min(t()) :: {:ok, {priority(), term()}} | {:error, :empty}
  def peek_min({tree, _seq}) do
    # TODO: inspect the minimum element without removing it
    # HINT: :gb_trees.is_empty/1, :gb_trees.smallest/1
    # HINT: smallest returns {{priority, _seq}, value} — strip the seq from the key
  end

  @spec pop_min(t()) :: {{:ok, {priority(), term()}}, t()} | {{:error, :empty}, t()}
  def pop_min({tree, seq}) do
    # TODO: remove and return the minimum element
    # HINT: :gb_trees.take_smallest/1 returns {key, value, new_tree}
  end

  @spec size(t()) :: non_neg_integer()
  def size({tree, _seq}) do
    # TODO: :gb_trees.size/1
  end

  @spec to_sorted_list(t()) :: [{priority(), term()}]
  def to_sorted_list({tree, _seq}) do
    # TODO: :gb_trees.to_list/1 returns [{key, value}] in ascending key order
    # HINT: map each {{priority, _seq}, value} to {priority, value}
  end
end
```

### Step 3: `lib/api_gateway/middleware/erlang_adapter.ex`

```elixir
defmodule ApiGateway.Middleware.ErlangAdapter do
  @moduledoc """
  Normalizes data returned by the legacy Erlang auth service.

  The auth service returns:
  - User records as Erlang tuples: {:user, charlist_name, integer_age, charlist_role}
  - Session data as Erlang proplists: [{:key, value}, ...]
  - Nested charlists throughout

  This module converts all of the above to idiomatic Elixir.
  """

  @type record :: tuple()
  @type proplist :: [{atom(), term()}]

  @doc """
  Converts an Erlang record tuple to a map, given the field names (excluding
  the record name at position 0).

  Example:
    record_to_map({:user, 'Alice', 30, 'admin'}, [:name, :age, :role])
    #=> %{name: "Alice", age: 30, role: "admin"}
  """
  @spec record_to_map(record(), [atom()]) :: map()
  def record_to_map(record, fields) when is_tuple(record) do
    # TODO: skip the first element (record name) with tl/1
    # HINT: Tuple.to_list/1, then tl/1 to drop the record name
    # HINT: zip fields with values, then Map.new, converting charlists recursively
  end

  @doc """
  Converts an Erlang proplist to an Elixir map.

  Example:
    proplist_to_map([{:name, 'Bob'}, {:age, 25}])
    #=> %{name: "Bob", age: 25}
  """
  @spec proplist_to_map(proplist()) :: map()
  def proplist_to_map(proplist) do
    # TODO: use :proplists.get_keys/1 to get unique keys
    # HINT: Map.new(keys, fn k -> {k, deep_charlist_to_string(:proplists.get_value(k, proplist))} end)
  end

  @doc """
  Recursively converts charlists to strings anywhere in a nested structure.

  Handles: lists (charlist or regular), tuples, maps, and scalar values.
  """
  @spec deep_charlist_to_string(term()) :: term()
  def deep_charlist_to_string(value) when is_list(value) do
    # TODO: use :io_lib.deep_char_list/1 to detect charlists
    # If it is a charlist → List.to_string/1
    # If it is a regular list → Enum.map each element recursively
  end

  def deep_charlist_to_string(value) when is_tuple(value) do
    # TODO: convert each element recursively, then rebuild the tuple
    # HINT: Tuple.to_list, Enum.map, List.to_tuple
  end

  def deep_charlist_to_string(value), do: value
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/job_queue_test.exs
defmodule ApiGateway.Middleware.JobQueueTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Middleware.JobQueue

  setup do
    {:ok, pid} = JobQueue.start_link()
    {:ok, queue: pid}
  end

  test "enqueue and dequeue preserve FIFO order", %{queue: q} do
    JobQueue.enqueue(q, :a)
    JobQueue.enqueue(q, :b)
    JobQueue.enqueue(q, :c)

    assert {:ok, :a} = JobQueue.dequeue(q)
    assert {:ok, :b} = JobQueue.dequeue(q)
    assert {:ok, :c} = JobQueue.dequeue(q)
    assert {:error, :empty} = JobQueue.dequeue(q)
  end

  test "peek does not remove the element", %{queue: q} do
    JobQueue.enqueue(q, :first)
    assert {:ok, :first} = JobQueue.peek(q)
    assert {:ok, :first} = JobQueue.peek(q)
    assert JobQueue.size(q) == 1
  end

  test "drain returns all elements and empties the queue", %{queue: q} do
    for i <- 1..5, do: JobQueue.enqueue(q, i)
    assert [1, 2, 3, 4, 5] = JobQueue.drain(q)
    assert JobQueue.size(q) == 0
  end

  test "dequeue from empty queue returns error", %{queue: q} do
    assert {:error, :empty} = JobQueue.dequeue(q)
    assert {:error, :empty} = JobQueue.peek(q)
  end
end

# test/api_gateway/middleware/priority_scheduler_test.exs
defmodule ApiGateway.Middleware.PrioritySchedulerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Middleware.PriorityScheduler

  test "pop_min always returns the lowest priority number" do
    pq =
      PriorityScheduler.new()
      |> PriorityScheduler.insert(3, :low)
      |> PriorityScheduler.insert(1, :urgent)
      |> PriorityScheduler.insert(2, :normal)

    {{:ok, {1, :urgent}}, pq} = PriorityScheduler.pop_min(pq)
    {{:ok, {2, :normal}}, pq} = PriorityScheduler.pop_min(pq)
    {{:ok, {3, :low}}, _pq} = PriorityScheduler.pop_min(pq)
  end

  test "insertion order preserved for equal priorities" do
    pq =
      PriorityScheduler.new()
      |> PriorityScheduler.insert(1, :first)
      |> PriorityScheduler.insert(1, :second)
      |> PriorityScheduler.insert(1, :third)

    {{:ok, {1, :first}}, pq} = PriorityScheduler.pop_min(pq)
    {{:ok, {1, :second}}, pq} = PriorityScheduler.pop_min(pq)
    {{:ok, {1, :third}}, _pq} = PriorityScheduler.pop_min(pq)
  end

  test "to_sorted_list returns elements in ascending priority" do
    pq =
      PriorityScheduler.new()
      |> PriorityScheduler.insert(2, :b)
      |> PriorityScheduler.insert(1, :a)
      |> PriorityScheduler.insert(3, :c)

    assert [{1, :a}, {2, :b}, {3, :c}] = PriorityScheduler.to_sorted_list(pq)
  end
end

# test/api_gateway/middleware/erlang_adapter_test.exs
defmodule ApiGateway.Middleware.ErlangAdapterTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Middleware.ErlangAdapter

  test "record_to_map converts tuple record to map" do
    record = {:user, 'Alice', 30, 'admin'}
    result = ErlangAdapter.record_to_map(record, [:name, :age, :role])
    assert %{name: "Alice", age: 30, role: "admin"} = result
  end

  test "proplist_to_map converts Erlang proplist to Elixir map" do
    proplist = [{:name, 'Bob'}, {:age, 25}, {:active, true}]
    result = ErlangAdapter.proplist_to_map(proplist)
    assert %{name: "Bob", age: 25, active: true} = result
  end

  test "deep_charlist_to_string converts nested charlists" do
    assert ErlangAdapter.deep_charlist_to_string('hello') == "hello"
    assert ErlangAdapter.deep_charlist_to_string(['a', 'b']) == ["a", "b"]
    assert ErlangAdapter.deep_charlist_to_string({:user, 'Alice'}) == {:user, "Alice"}
    assert ErlangAdapter.deep_charlist_to_string(42) == 42
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/middleware/ --trace
```

---

## Trade-off analysis

| Aspect | `:queue` | List as queue | `:gb_trees` | `Map` |
|--------|----------|---------------|-------------|-------|
| Enqueue (back) | O(1) amortized | O(n) | O(log n) | O(1) amortized |
| Dequeue (front) | O(1) amortized | O(1) | O(log n) | N/A |
| Ordered iteration | O(n) FIFO | O(n) | O(n) sorted | Not guaranteed |
| Memory overhead | 2 lists | 1 list | Tree nodes | Hash buckets |
| Best for | FIFO dispatch queues | LIFO (stack) | Priority queues, ordered scans | Unordered lookup |

Reflection: the router accumulates deferred jobs during a rate-limit burst. Should you use
`:queue` or a list here? Consider the ratio of enqueue to dequeue operations in that scenario.

---

## Common production mistakes

**1. List append as queue (`state ++ [job]`)**
This is the most common performance regression when prototyping GenServers. It copies the
entire list on each append. Replace with `:queue` the moment queuing semantics appear.

**2. `:gb_trees.insert/3` on duplicate keys**
`:gb_trees.insert/3` raises `{:key_exists, key}` if the key already exists. Use
`:gb_trees.enter/3` to upsert, or model keys as `{priority, sequence_number}` for uniqueness.

**3. Trusting charlists from Erlang libraries without converting**
Passing a charlist to `String.length/1` raises `ArgumentError`. Always convert at the
boundary — in the adapter module, before the data enters application code.

**4. `:io_lib.deep_char_list/1` returns `:true`/`:false`, not `true`/`false`**
In older OTP versions, this function returns Erlang atoms. Pattern match on both or use
`== true` in a guard to be safe.

**5. Using `:proplists.get_value/3` without a default**
If the key is missing, `:proplists.get_value/2` returns `undefined` (the atom). Provide
an explicit default or guard against it at the boundary.

---

## Resources

- [`:queue` — Erlang docs](https://www.erlang.org/doc/man/queue.html)
- [`:gb_trees` — Erlang docs](https://www.erlang.org/doc/man/gb_trees.html)
- [`:proplists` — Erlang docs](https://www.erlang.org/doc/man/proplists.html)
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — chapter on data structures (free PDF)
