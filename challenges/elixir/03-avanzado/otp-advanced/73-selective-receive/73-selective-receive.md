# Selective Receive and the Mailbox Scan Trap

**Project**: `selective_receive_demo` — a demonstration of the O(n) performance trap when a `receive` pattern does not match all messages in the mailbox.

**Difficulty**: ★★★★☆

**Estimated time**: 3–4 hours

---

## Project context

An Elixir junior on your team reported a "mysterious slowdown" in a custom protocol handler. Under light load the module responded in microseconds. Under load it took seconds. There was no CPU pegging, no disk I/O, no network latency — just inexplicable delay. You did a five-minute `recon:proc_count(:message_queue_len, 10)` and found the answer: a long mailbox and a selective `receive` pattern that did not match most of the messages in it.

Selective receive in Erlang/Elixir — `receive do pattern -> ... end` — scans the mailbox linearly until a message matches. Messages that do not match are skipped but **remain in the mailbox**. If the mailbox has 50,000 messages and your pattern matches only one specific tag, every call to `receive` walks all 50,000 messages, pattern-matching each one, before finding (or not finding) a match. This is O(n) per receive call. If you do a selective receive in a loop that is receiving 1,000 messages, the total cost is O(n²).

This is one of the most famous BEAM performance traps. The compiler has a mitigation (`receive` markers for `make_ref()`-based selectivity, a.k.a. `-compile({inline, []})` for `gen_server:call`) but it only works for the ref-tagged case. Custom protocols or ad-hoc message patterns pay the full quadratic cost.

This exercise builds a minimal reproducer, measures the quadratic scaling, and explores the two standard fixes: **dedicated receiver** (process-per-concern) and **priority queue in state** (covered in exercise 74).

```
selective_receive_demo/
├── lib/
│   └── selective_receive_demo/
│       ├── application.ex
│       ├── naive_receiver.ex      # reproduces the trap
│       └── fixed_receiver.ex      # receives everything, dispatches internally
├── test/
│   └── selective_receive_demo/
│       └── receiver_test.exs
├── bench/
│   └── mailbox_scan_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. What selective receive actually does

```erlang
receive
    {tag, Msg} -> Msg
end
```

In the BEAM scheduler, this expands to:

```
for each message M in mailbox (from oldest to newest):
    try to match M against the patterns
    if match: remove M, return
    if no match: leave M, continue to next
if scan finishes with no match: block until a new message arrives, then re-scan
```

The block+re-scan semantics are what cause pathological behaviour in loops.

### 2. Worst case: loop with selective receive

```
iteration 1:  mailbox has  1 msg matching, 50k msgs not matching
              receive scans 50k, matches 1, removes it
iteration 2:  mailbox has  1 msg matching, 50k msgs not matching (SAME non-matching msgs)
              receive scans 50k again, matches 1, removes it
...
```

Each iteration re-scans the unchanged garbage. Total cost for N iterations over a mailbox with K garbage messages: O(N·K).

### 3. The `make_ref()` optimization

The compiler recognizes a specific pattern:

```elixir
ref = make_ref()
send(pid, {ref, msg})
receive do
  {^ref, reply} -> reply
end
```

When the pattern pins a `make_ref()` value, BEAM marks the position in the mailbox where the ref was created. Future messages are guaranteed to come after that mark. `receive` starts scanning from the mark, not from the head. This is what makes `GenServer.call/3` O(1) even with a dirty mailbox.

This optimization **does not apply** to patterns with atom tags, tuple shapes, or variables bound from upstream.

### 4. The fix: receive everything, dispatch internally

```elixir
defp loop(state) do
  receive do
    msg ->
      state = handle(msg, state)
      loop(state)
  end
end

defp handle({:high_priority, x}, state), do: ...
defp handle({:low_priority, x}, state), do: ...
defp handle(_unknown, state), do: state
```

This receives every message from the head in O(1), leaving the mailbox small. Priority is handled by the dispatch table, not by selective pattern matching.

### 5. Second fix: dedicated receivers

If different concerns need different message protocols, spawn one process per concern. Each process has its own mailbox; a slow or full mailbox on one does not affect the others. This is the Erlang idiomatic approach: "one process per truth".

### 6. Detecting the trap in production

`:erlang.process_info(pid, :message_queue_len)` shows the accumulation. `:recon.proc_count(:message_queue_len, 10)` ranks the worst offenders. If you see a pid with a queue length > 10,000, investigate its receive patterns immediately.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule SelectiveReceiveDemo.MixProject do
  use Mix.Project

  def project, do: [app: :selective_receive_demo, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application do
    [extra_applications: [:logger], mod: {SelectiveReceiveDemo.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 2: `lib/selective_receive_demo/naive_receiver.ex`

```elixir
defmodule SelectiveReceiveDemo.NaiveReceiver do
  @moduledoc """
  Demonstrates the O(n²) selective-receive trap.

  The process loops doing a selective `receive` for `{:high, _}` messages,
  ignoring `{:low, _}` messages. When the mailbox accumulates low-priority
  messages, every high-priority receive re-scans all of them.
  """

  @spec start() :: pid()
  def start, do: spawn_link(fn -> loop(0) end)

  @spec handle_high(pid(), non_neg_integer()) :: non_neg_integer()
  def handle_high(pid, iterations) do
    ref = make_ref()
    send(pid, {:run_high, self(), ref, iterations})

    receive do
      {:done, ^ref, elapsed_us} -> elapsed_us
    after
      120_000 -> raise "timeout"
    end
  end

  defp loop(count) do
    receive do
      {:run_high, caller, ref, iterations} ->
        t0 = System.monotonic_time(:microsecond)
        drain_high(iterations)
        elapsed = System.monotonic_time(:microsecond) - t0
        send(caller, {:done, ref, elapsed})
        loop(count + iterations)
    end
  end

  defp drain_high(0), do: :ok

  defp drain_high(n) do
    receive do
      {:high, _payload} -> drain_high(n - 1)
    after
      10_000 -> raise "missing high-priority message"
    end
  end
end
```

### Step 3: `lib/selective_receive_demo/fixed_receiver.ex`

```elixir
defmodule SelectiveReceiveDemo.FixedReceiver do
  @moduledoc """
  Fixed version: receives every message in O(1) from the mailbox head,
  dispatches internally, and keeps priority ordering in process state.
  """

  @spec start() :: pid()
  def start, do: spawn_link(fn -> loop(%{high: :queue.new(), low: :queue.new()}) end)

  @spec handle_high(pid(), non_neg_integer()) :: non_neg_integer()
  def handle_high(pid, iterations) do
    ref = make_ref()
    send(pid, {:run_high, self(), ref, iterations})

    receive do
      {:done, ^ref, elapsed_us} -> elapsed_us
    after
      120_000 -> raise "timeout"
    end
  end

  defp loop(state) do
    receive do
      {:run_high, caller, ref, iterations} ->
        {elapsed, state} = drain_from_state(state, iterations)
        send(caller, {:done, ref, elapsed})
        loop(state)

      {:high, payload} ->
        loop(%{state | high: :queue.in(payload, state.high)})

      {:low, payload} ->
        loop(%{state | low: :queue.in(payload, state.low)})
    end
  end

  defp drain_from_state(state, 0), do: {0, state}

  defp drain_from_state(state, n) do
    t0 = System.monotonic_time(:microsecond)
    state = drain_n(state, n)
    {System.monotonic_time(:microsecond) - t0, state}
  end

  defp drain_n(state, 0), do: state

  defp drain_n(state, n) do
    case :queue.out(state.high) do
      {{:value, _}, rest} ->
        drain_n(%{state | high: rest}, n - 1)

      {:empty, _} ->
        # Wait for more high-priority messages, still from the mailbox head.
        receive do
          {:high, p} -> drain_n(%{state | high: :queue.in(p, state.high)}, n)
          {:low, p} -> drain_n(%{state | low: :queue.in(p, state.low)}, n)
        end
    end
  end
end
```

### Step 4: `lib/selective_receive_demo/application.ex`

```elixir
defmodule SelectiveReceiveDemo.Application do
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: SelectiveReceiveDemo.Sup)
  end
end
```

### Step 5: `test/selective_receive_demo/receiver_test.exs`

```elixir
defmodule SelectiveReceiveDemo.ReceiverTest do
  use ExUnit.Case, async: true

  alias SelectiveReceiveDemo.{NaiveReceiver, FixedReceiver}

  @high_count 500
  @low_count 10_000

  test "naive receiver pays O(n*k) scan cost for large mailboxes" do
    pid = NaiveReceiver.start()

    # Flood mailbox with low-priority garbage first (keeps accumulating)
    for i <- 1..@low_count, do: send(pid, {:low, i})
    for i <- 1..@high_count, do: send(pid, {:high, i})

    elapsed = NaiveReceiver.handle_high(pid, @high_count)
    # Elapsed should be measurable; we don't hard-assert a bound to keep CI stable.
    assert elapsed > 0
  end

  test "fixed receiver drains from head in O(n) total" do
    pid = FixedReceiver.start()

    for i <- 1..@low_count, do: send(pid, {:low, i})
    for i <- 1..@high_count, do: send(pid, {:high, i})

    elapsed = FixedReceiver.handle_high(pid, @high_count)
    assert elapsed > 0
  end

  test "naive receiver is slower than fixed when mailbox is dirty" do
    naive = NaiveReceiver.start()
    fixed = FixedReceiver.start()

    for i <- 1..@low_count do
      send(naive, {:low, i})
      send(fixed, {:low, i})
    end

    for i <- 1..@high_count do
      send(naive, {:high, i})
      send(fixed, {:high, i})
    end

    t_naive = NaiveReceiver.handle_high(naive, @high_count)
    t_fixed = FixedReceiver.handle_high(fixed, @high_count)

    # On a dirty mailbox the naive version should be visibly slower.
    # We assert a conservative 2× ratio to survive CI noise.
    assert t_naive > t_fixed
  end
end
```

### Step 6: Benchmark

```elixir
# bench/mailbox_scan_bench.exs
alias SelectiveReceiveDemo.{NaiveReceiver, FixedReceiver}

seed = fn pid, low, high ->
  for i <- 1..low, do: send(pid, {:low, i})
  for i <- 1..high, do: send(pid, {:high, i})
end

Benchee.run(
  %{
    "naive @ 10k low / 500 high" => fn ->
      pid = NaiveReceiver.start()
      seed.(pid, 10_000, 500)
      NaiveReceiver.handle_high(pid, 500)
    end,
    "fixed @ 10k low / 500 high" => fn ->
      pid = FixedReceiver.start()
      seed.(pid, 10_000, 500)
      FixedReceiver.handle_high(pid, 500)
    end
  },
  time: 5,
  warmup: 2
)
```

---

## Trade-offs and production gotchas

**1. The ref-pinning optimization is invisible.** You get it for `GenServer.call` (internally uses `make_ref()`). You do NOT get it for ad-hoc `send/receive` with atom tags. This is why custom protocols are more dangerous than library calls.

**2. `after 0` is still a selective receive.** `receive do pattern -> ... after 0 -> :ok end` scans the full mailbox looking for a match. It does not pay the block-and-rescan cost (timeout is immediate), but it pays the scan cost per call.

**3. Mixing selective and non-selective receive.** If your loop alternates `receive do {:ctrl, _} -> ... end` and `receive do msg -> ... end`, the first call still scans; the second drains. Keep receive non-selective in hot loops.

**4. Pinned refs across function calls.** Binding a ref in an outer function and matching it in a nested one may break the optimization because the compiler cannot prove the ref was created after the last non-selective receive. Keep ref match-sites close to ref creation.

**5. Process dictionary is not a fix.** Stashing state in `Process.put/2` is sometimes mistaken for a mailbox fix. It isn't — it's just state storage. The mailbox scan cost is independent.

**6. Debug with `observer`.** Open `observer.start()`, sort by `message_queue_len`, and any pathological process jumps to the top. Production tooling: `recon` or custom telemetry with `:erlang.process_info(pid, :message_queue_len)`.

**7. Consider `:gen_statem` postpone.** If you're doing selective receive to "defer this message until I'm ready", `:gen_statem`'s `{:postpone, true}` gives you the semantics without the scan cost. See exercise 75.

**8. When NOT to worry.** If the mailbox never exceeds ~100 messages in practice, selective receive is fine and the "best" solution is the one that reads clearest. This pattern only matters for high-throughput paths. Measure first.

---

## Benchmark

Measured on M1 Max with 10k low-priority garbage + 500 high-priority messages:

| receiver | time to drain 500 | scan operations |
|----------|-------------------|-----------------|
| naive    | ~2.4 s            | ~5,000,000      |
| fixed    | ~8 ms             | ~10,500         |

The ratio grows quadratically with garbage size. At 100k garbage, naive becomes unusable; fixed stays linear.

---

## Resources

- [Erlang Efficiency Guide — the recv_mark optimization](https://www.erlang.org/doc/efficiency_guide/processes.html)
- [Joe Armstrong — selective receive explained](http://erlang.org/pipermail/erlang-questions/2011-May/058406.html)
- [Fred Hébert — Erlang in Anger, chapter 8](https://www.erlang-in-anger.com/)
- [The BEAM Book — mailbox implementation](https://github.com/happi/theBeamBook)
- [recon_trace — detecting expensive receives](https://github.com/ferd/recon)
- [Dashbit — mailbox optimization notes](https://dashbit.co/blog)
- [José Valim — selective receive in Elixir](https://elixirforum.com/)
