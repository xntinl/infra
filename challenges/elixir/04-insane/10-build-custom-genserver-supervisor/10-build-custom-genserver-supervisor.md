# Custom GenServer and Supervisor

**Project**: `my_otp` — a from-scratch implementation of GenServer and Supervisor using raw BEAM primitives

---

## Project context

You are building `my_otp`, a reimplementation of OTP's GenServer and Supervisor using only raw Elixir/Erlang process primitives. No `:gen_server`, no `GenServer`, no `Supervisor`, no `Agent`, no `Task`. Every mechanism you rely on in production — call/cast protocols, restart strategies, monitor-based crash detection — you implement from scratch.

Project structure:

```
my_otp/
├── lib/
│   └── my_otp/
│       ├── gen_server.ex            # MyGenServer: call, cast, handle_info, start_link
│       ├── supervisor.ex            # MyGenServer.Supervisor: one_for_one, one_for_all, rest_for_one
│       └── restart_tracker.ex      # sliding window: max_restarts within max_seconds
├── test/
│   └── my_otp/
│       ├── gen_server_test.exs      # call semantics, cast, stale reply handling, timeouts
│       ├── supervisor_test.exs      # all three restart strategies, restart intensity
│       └── restart_policy_test.exs  # permanent, transient, temporary restart modes
├── bench/
│   └── my_otp_bench.exs
└── mix.exs
```

---

## The problem

You use GenServer and Supervisor every day but cannot explain why a call uses a tagged reference, why the supervisor uses monitors instead of links, or how restart intensity tracking works. This exercise closes that gap. You will implement the full mechanism from first principles, discovering why each design decision exists.

---

## Why this design

**Tagged reference for calls**: `GenServer.call/2` under the hood sends `{:"$gen_call", {self(), ref}, message}` where `ref = make_ref()`. The server sends back `{ref, reply}`. The caller waits with `receive do {^ref, reply} -> reply end`. The `^ref` pin ensures the caller matches only its own reply, discarding unrelated messages. Without the reference, a stale reply from a previous timed-out call could be mistaken for the reply to the current call.

**Monitors, not links, for supervisors**: if the supervisor were linked to its children, a child crash would kill the supervisor before the restart logic could run. By monitoring children, the supervisor receives `{:DOWN, ref, :process, pid, reason}` as a message and can react — restart the child, stop others if the strategy requires it — without itself crashing.

**Sliding window for restart intensity**: OTP counts restarts within the last `max_seconds`. A burst of restarts exhausts the intensity limit; the supervisor exits with `:max_restarts_exceeded`. Storing all restart timestamps and counting those within `[now - max_seconds, now]` is the correct algorithm (not a simple counter that resets periodically).

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new my_otp --sup
cd my_otp
mkdir -p lib/my_otp test/my_otp bench
```

### Step 2: `mix.exs` — no external dependencies needed

The entire implementation uses only Elixir/Erlang standard library primitives.

### Step 3: `MyGenServer`

```elixir
# lib/my_otp/gen_server.ex
defmodule MyGenServer do
  @moduledoc """
  A reimplementation of OTP's GenServer using only raw process primitives:
  spawn, send, receive, Process.monitor/1, Process.link/1.

  The call protocol:
    caller sends:   {:"$call", {self(), ref}, message}
    server replies: {ref, reply}

  The cast protocol:
    caller sends:   {:"$cast", message}
    server ignores: no reply

  Info messages: anything that is not a $call or $cast is forwarded to handle_info/2.
  """

  @doc "Starts and links a server running module with init_args."
  def start_link(module, init_args, opts \\ []) do
    # TODO: spawn_link a process that calls module.init(init_args)
    # TODO: register the process under opts[:name] if provided
    # TODO: return {:ok, pid}
    # HINT: the child process must call Process.flag(:trap_exit, true) if it
    #        needs to handle exit signals as messages — decide if that is appropriate here
  end

  @doc "Synchronous call. Blocks until reply arrives or timeout elapses."
  def call(server, message, timeout \\ 5_000) do
    ref = make_ref()
    # TODO: send {:"$call", {self(), ref}, message} to server
    # TODO: receive {^ref, reply} with timeout
    # TODO: on timeout, raise an error with the server identity and message
    # HINT: a stale reply that arrives after timeout must be discarded by the caller;
    #        how does the pinned ref help here?
  end

  @doc "Asynchronous cast. Returns :ok immediately."
  def cast(server, message) do
    # TODO: send {:"$cast", message}
    :ok
  end

  # The server loop — implement this as a tail-recursive function
  defp loop(module, state) do
    receive do
      {:"$call", {from, ref}, message} ->
        # TODO: call module.handle_call(message, {from, ref}, state)
        # TODO: send {ref, reply} back to from
        # TODO: continue loop with new state
        :todo

      {:"$cast", message} ->
        # TODO: call module.handle_cast(message, state)
        # TODO: continue loop with new state
        :todo

      other ->
        # TODO: call module.handle_info(other, state)
        # TODO: continue loop with new state
        :todo
    end
  end
end
```

### Step 4: `MyGenServer.Supervisor`

```elixir
# lib/my_otp/supervisor.ex
defmodule MyGenServer.Supervisor do
  @moduledoc """
  A reimplementation of OTP's Supervisor with three restart strategies:

  :one_for_one   — only the crashed child is restarted
  :one_for_all   — all children are terminated (reverse order) and restarted (forward order)
  :rest_for_one  — the crashed child and all children started after it are terminated and restarted

  Child specs:
    %{id: atom, start: {module, args}, restart: :permanent | :transient | :temporary}

  restart semantics:
    :permanent — always restart, regardless of exit reason
    :transient — restart only on abnormal exit (reason not :normal and not :shutdown)
    :temporary — never restart

  Restart intensity: if more than max_restarts restarts occur within max_seconds,
  the supervisor itself crashes with reason {:shutdown, :max_restarts_exceeded}.
  """

  def start_link(children, opts \\ []) do
    strategy    = opts[:strategy]    || :one_for_one
    max_restarts = opts[:max_restarts] || 3
    max_seconds  = opts[:max_seconds]  || 5

    # TODO: spawn a supervisor process
    # TODO: start each child, monitor it (not link), store {ref, child_spec, pid}
    # TODO: enter the supervisor receive loop
  end

  defp supervisor_loop(children, strategy, max_restarts, max_seconds, restart_history) do
    receive do
      {:DOWN, ref, :process, pid, reason} ->
        # TODO: find the child spec for this pid
        # TODO: check restart policy — should we restart?
        # TODO: check restart intensity — have we restarted too many times?
        # TODO: apply the restart strategy
        :todo
    end
  end

  defp should_restart?(:permanent, _reason), do: true
  defp should_restart?(:transient, reason),  do: reason not in [:normal, :shutdown]
  defp should_restart?(:temporary, _reason), do: false

  defp intensity_exceeded?(restart_history, max_restarts, max_seconds) do
    # TODO: count restarts within the last max_seconds
    # HINT: System.monotonic_time(:second) for timestamps
  end
end
```

### Step 5: Given tests — must pass without modification

```elixir
# test/my_otp/gen_server_test.exs
defmodule MyGenServerTest do
  use ExUnit.Case, async: true

  defmodule Counter do
    def init(n), do: {:ok, n}
    def handle_call(:get, _from, n), do: {:reply, n, n}
    def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
    def handle_cast(:reset, _n),     do: {:noreply, 0}
    def handle_info(_, state),       do: {:noreply, state}
  end

  test "call returns correct reply" do
    {:ok, pid} = MyGenServer.start_link(Counter, 0)
    assert 0 = MyGenServer.call(pid, :get)
    assert 1 = MyGenServer.call(pid, :inc)
    assert 1 = MyGenServer.call(pid, :get)
  end

  test "cast changes state without blocking" do
    {:ok, pid} = MyGenServer.start_link(Counter, 42)
    :ok = MyGenServer.cast(pid, :reset)
    Process.sleep(10)
    assert 0 = MyGenServer.call(pid, :get)
  end

  test "call raises on timeout" do
    defmodule SlowServer do
      def init(_), do: {:ok, nil}
      def handle_call(:slow, _from, state) do
        Process.sleep(200)
        {:reply, :done, state}
      end
      def handle_info(_, s), do: {:noreply, s}
    end

    {:ok, pid} = MyGenServer.start_link(SlowServer, nil)
    assert_raise RuntimeError, fn ->
      MyGenServer.call(pid, :slow, 50)
    end
  end

  test "stale replies after timeout are silently discarded" do
    # After a timeout, the ref is no longer being waited on.
    # A stale reply must not land in a subsequent call's receive.
    {:ok, pid} = MyGenServer.start_link(Counter, 0)
    catch_error(MyGenServer.call(pid, :inc, 1))  # almost instant timeout
    Process.sleep(50)  # let stale reply arrive
    # This call must succeed cleanly without picking up the stale reply
    assert MyGenServer.call(pid, :get) in [0, 1]
  end
end
```

```elixir
# test/my_otp/supervisor_test.exs
defmodule MyGenServer.SupervisorTest do
  use ExUnit.Case, async: true

  defmodule Worker do
    def init(id), do: {:ok, id}
    def handle_call(:id, _from, id), do: {:reply, id, id}
    def handle_cast(:crash, _state), do: raise "intentional crash"
    def handle_info(_, s), do: {:noreply, s}
  end

  test ":one_for_one restarts only the crashed child" do
    children = [
      %{id: :w1, start: {Worker, :w1}, restart: :permanent},
      %{id: :w2, start: {Worker, :w2}, restart: :permanent},
      %{id: :w3, start: {Worker, :w3}, restart: :permanent}
    ]

    {:ok, _sup} = MyGenServer.Supervisor.start_link(children, strategy: :one_for_one)
    {:ok, w1}  = MyGenServer.Supervisor.child_pid(:w1)
    {:ok, w2}  = MyGenServer.Supervisor.child_pid(:w2)

    MyGenServer.cast(w2, :crash)
    Process.sleep(50)

    {:ok, new_w2} = MyGenServer.Supervisor.child_pid(:w2)
    {:ok, same_w1} = MyGenServer.Supervisor.child_pid(:w1)

    assert new_w2 != w2,   "w2 must have been restarted"
    assert same_w1 == w1,  "w1 must NOT have been restarted"
  end

  test ":one_for_all restarts all children when one crashes" do
    children = [
      %{id: :a, start: {Worker, :a}, restart: :permanent},
      %{id: :b, start: {Worker, :b}, restart: :permanent},
      %{id: :c, start: {Worker, :c}, restart: :permanent}
    ]

    {:ok, _sup} = MyGenServer.Supervisor.start_link(children, strategy: :one_for_all)
    {:ok, a} = MyGenServer.Supervisor.child_pid(:a)
    {:ok, b} = MyGenServer.Supervisor.child_pid(:b)

    MyGenServer.cast(b, :crash)
    Process.sleep(50)

    {:ok, new_a} = MyGenServer.Supervisor.child_pid(:a)
    assert new_a != a, "a must be restarted when b crashes under :one_for_all"
  end

  test "supervisor exits when restart intensity is exceeded" do
    children = [
      %{id: :unstable, start: {Worker, :x}, restart: :permanent}
    ]

    {:ok, sup} = MyGenServer.Supervisor.start_link(children,
      strategy: :one_for_one, max_restarts: 3, max_seconds: 5
    )

    ref = Process.monitor(sup)

    for _ <- 1..4 do
      {:ok, pid} = MyGenServer.Supervisor.child_pid(:unstable)
      Process.exit(pid, :kill)
      Process.sleep(10)
    end

    assert_receive {:DOWN, ^ref, :process, ^sup, {:shutdown, :max_restarts_exceeded}}, 1_000
  end
end
```

### Step 6: Run the tests

```bash
mix test test/my_otp/ --trace
```

### Step 7: Benchmark

```elixir
# bench/my_otp_bench.exs
defmodule BenchServer do
  def init(_), do: {:ok, 0}
  def handle_call(:inc, _from, n), do: {:reply, n + 1, n + 1}
  def handle_info(_, s), do: {:noreply, s}
end

{:ok, mine}  = MyGenServer.start_link(BenchServer, 0)
{:ok, otp}   = GenServer.start_link(BenchServer, 0)

Benchee.run(
  %{
    "MyGenServer.call" => fn -> MyGenServer.call(mine, :inc) end,
    "GenServer.call"   => fn -> GenServer.call(otp, :inc)   end
  },
  parallel: 1,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | Your implementation | OTP's GenServer |
|--------|--------------------|--------------------|
| Call protocol | `{:"$call", {pid, ref}, msg}` | identical |
| Stale reply handling | pinned ref in receive | identical |
| Crash propagation | monitor → message | monitor → message |
| System message handling | `:sys` protocol | built-in |
| Hot code upgrade | not implemented | `code_change/3` callback |
| Hibernate support | not implemented | `{:reply, val, state, :hibernate}` |
| Debug tracing | not implemented | `:sys.trace/2` |

After running the benchmark, note the overhead introduced by your abstraction layer relative to OTP's highly optimized native implementation.

---

## Common production mistakes

**1. Using links instead of monitors in the supervisor**
If the supervisor is linked to its children, any child crash propagates an exit signal to the supervisor before the `{:DOWN, ...}` message arrives. You must use monitors so crashes arrive as ordinary messages that the supervisor can handle in the receive loop.

**2. Not handling the stale reply**
After a call times out, the server eventually sends the reply. That reply sits in the caller's mailbox. If the caller then makes another call and is not carefully receiving, the stale reply will corrupt the next call's result. The pinned reference (`^ref`) in receive is the correct defense.

**3. Restart intensity using a simple counter**
A counter that resets every `max_seconds` does not correctly capture a burst of restarts that straddles a period boundary. Use a list of timestamps and count those within `[now - max_seconds, now]`.

**4. Starting children before the supervisor is ready**
If children are started synchronously in `init` before the supervisor process is fully initialized, a child that crashes immediately may generate a `{:DOWN, ...}` before the supervisor is in its receive loop. Start children from the receive loop, or handle the `{:DOWN, ...}` before the loop is entered.

---

## Resources

- [Erlang `gen_server.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/gen_server.erl) — study the `loop/7` function and the `call/3` implementation
- [Erlang `supervisor.erl` source](https://github.com/erlang/otp/blob/master/lib/stdlib/src/supervisor.erl) — study `handle_info/2` for the `{:DOWN, ...}` handler
- Cesarini, F. & Vinoski, S. — *Designing for Scalability with Erlang/OTP* — Chapters 4–6
- [OTP Supervisor Design Principles](https://www.erlang.org/doc/design_principles/sup_princ)
- Hebert, F. — *The Zen of Erlang* — https://ferd.ca/the-zen-of-erlang.html
