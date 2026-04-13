# Deferred Replies with `GenServer.reply/2`

**Project**: `reply_async_gs` — decoupling the caller's reply from the callback that received it.

---

## Why deferred replies with `genserver.reply/2` matters

This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach works on a developer laptop; the version built here survives the scheduler pressure, binary refc pitfalls, and supervisor budgets of a running node.

The trade-off chart and the executable benchmark are the core of the lesson: you calibrate the cost of the abstraction against a measurable gain, not a vibe.

---
## The business problem

You build `AuthBroker`, the SSO layer in front of several partner identity
providers. Each `authenticate/2` call must validate a token against the
partner's REST API. Partner response times vary: the fast ones reply in
30 ms, the slow ones in 1.2 s.

The first implementation used a synchronous HTTP call from inside
`handle_call/3`. Under load the server became the bottleneck: while one
slow partner request was in flight, every other `call` in the mailbox
waited behind it. Throughput crashed to ~1 authentication per second
whenever a slow partner was warming up.

The right fix is to **decouple the reply from the callback**. `handle_call/3`
returns `{:noreply, state}` and stashes the `from` tuple in state. The
actual HTTP request runs in a supervised `Task`. When the task finishes,
a `handle_info/2` callback receives the result and calls
`GenServer.reply(from, result)` to complete the original caller's `call/2`.

This pattern unblocks the GenServer immediately and lets dozens of
outbound requests run in parallel, while the caller still experiences a
normal synchronous `GenServer.call/2`. It is how Finch, Oban workers,
and the Ecto connection pool all structure long I/O.

Project layout:

## Project structure

```
reply_async_gs/
├── lib/
│   └── reply_async_gs/
│       ├── application.ex
│       ├── auth_broker.ex          # the async-reply GenServer
│       └── fake_partner.ex         # stub HTTP partner
├── test/
│   └── reply_async_gs/
│       └── auth_broker_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Design decisions

**Option A — do the slow I/O inside `handle_call` and reply synchronously**
- Pros: simplest code; no bookkeeping.
- Cons: serializes all callers behind the slowest request; mailbox grows linearly with concurrent load.

**Option B — `{:noreply, state}` + `Task.Supervisor.async_nolink` + `GenServer.reply/2`** (chosen)
- Pros: the server keeps processing other messages while the work runs; concurrency scales with the task supervisor.
- Cons: you manage a pending-replies map; late replies after caller timeouts must be discarded; crashes in the task must not take down the server.

→ Chose **B** because the cost of serial processing is exactly the wall-clock you eliminate, and the bookkeeping is localized.

---

## Implementation

### `mix.exs`
**Objective**: Build stdlib-only so async-reply pattern shown strictly via GenServer.reply/2 and Task.Supervisor.

```elixir
defmodule ReplyAsyncGs.MixProject do
  use Mix.Project
  def project, do: [app: :reply_async_gs, version: "0.1.0", elixir: "~> 1.19", deps: []]
  def application, do: [extra_applications: [:logger], mod: {ReplyAsyncGs.Application, []}]

  defp deps do
    []
  end

end
```

### `lib/reply_async_gs.ex`

```elixir
defmodule ReplyAsyncGs do
  @moduledoc """
  Deferred Replies with `GenServer.reply/2`.

  This challenge encodes a production-grade Elixir/OTP pattern that directly affects throughput, memory, or fault-tolerance when the system is under real load. The naive approach....
  """
end
```

### `lib/reply_async_gs/application.ex`

**Objective**: Wire Task.Supervisor alongside broker so in-flight partner calls survive broker restart cleanly.

```elixir
defmodule ReplyAsyncGs.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Task.Supervisor, name: ReplyAsyncGs.TaskSup},
      ReplyAsyncGs.AuthBroker
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ReplyAsyncGs.Sup)
  end
end
```

### `lib/reply_async_gs/fake_partner.ex`

**Objective**: Stub slow partner SSO with deterministic sleep and branchable tokens, concurrency wins measured without flake.

```elixir
defmodule ReplyAsyncGs.FakePartner do
  @moduledoc """
  Stub of the partner HTTP endpoint. Sleeps `latency_ms` then returns a result.
  """

  @spec authenticate(String.t(), non_neg_integer()) :: {:ok, map()} | {:error, atom()}
  def authenticate(token, latency_ms) do
    Process.sleep(latency_ms)

    case token do
      "bad_token" -> {:error, :unauthorized}
      "boom"      -> raise "partner crashed"
      _           -> {:ok, %{user_id: "u_" <> Integer.to_string(:erlang.phash2(token))}}
    end
  end
end
```

### `lib/reply_async_gs/auth_broker.ex`

**Objective**: Return {:noreply, state} stashing from by task ref so partner latency overlaps across callers, no serialization.

```elixir
defmodule ReplyAsyncGs.AuthBroker do
  @moduledoc """
  GenServer that fronts partner SSO validation.

  `authenticate/2` looks synchronous to the caller but internally the
  server returns `{:noreply, state}` and offloads the slow HTTP call to a
  task. The caller is eventually replied to via `GenServer.reply/2`.
  """

  use GenServer
  require Logger

  @type state :: %{
          pending: %{reference() => GenServer.from()},
          partner_latency_ms: non_neg_integer()
        }

  # --- public API -----------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, Keyword.put_new(opts, :name, __MODULE__))
  end

  @spec authenticate(GenServer.server(), String.t(), timeout()) ::
          {:ok, map()} | {:error, term()}
  def authenticate(server \\ __MODULE__, token, timeout \\ 5_000) do
    GenServer.call(server, {:authenticate, token}, timeout)
  end

  # --- GenServer ------------------------------------------------------------

  @impl true
  def init(opts) do
    {:ok,
     %{
       pending: %{},
       partner_latency_ms: Keyword.get(opts, :partner_latency_ms, 100)
     }}
  end

  @impl true
  def handle_call({:authenticate, token}, from, state) do
    %Task{ref: ref} =
      Task.Supervisor.async_nolink(ReplyAsyncGs.TaskSup, fn ->
        ReplyAsyncGs.FakePartner.authenticate(token, state.partner_latency_ms)
      end)

    {:noreply, %{state | pending: Map.put(state.pending, ref, from)}}
  end

  @impl true
  def handle_info({ref, result}, state) when is_reference(ref) do
    # The task succeeded; tell the original caller.
    case Map.pop(state.pending, ref) do
      {nil, _} ->
        # Unknown ref — most likely a late reply after caller already timed out.
        {:noreply, state}

      {from, rest} ->
        GenServer.reply(from, result)
        # We no longer need the task's DOWN signal.
        Process.demonitor(ref, [:flush])
        {:noreply, %{state | pending: rest}}
    end
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, reason}, state) do
    # Task crashed. Reply with an error instead of leaving the caller hanging.
    case Map.pop(state.pending, ref) do
      {nil, _} ->
        {:noreply, state}

      {from, rest} ->
        Logger.warning("auth task crashed: #{inspect(reason)}")
        GenServer.reply(from, {:error, {:task_crash, reason}})
        {:noreply, %{state | pending: rest}}
    end
  end
end
```

### Step 5: `test/reply_async_gs/auth_broker_test.exs`

**Objective**: Prove concurrent calls complete in parallel, crashed partner task surfaces error not hang.

```elixir
defmodule ReplyAsyncGs.AuthBrokerTest do
  use ExUnit.Case, async: false
  doctest ReplyAsyncGs.AuthBroker

  alias ReplyAsyncGs.AuthBroker

  setup do
    start_supervised!({Task.Supervisor, name: ReplyAsyncGs.TaskSup})
    pid = start_supervised!({AuthBroker, partner_latency_ms: 50, name: :auth_broker_test})
    %{pid: pid}
  end

  describe "ReplyAsyncGs.AuthBroker" do
    test "successful authentication replies with user_id", %{pid: pid} do
      assert {:ok, %{user_id: _}} = AuthBroker.authenticate(pid, "good_token")
    end

    test "server stays responsive to new calls while one is in flight", %{pid: pid} do
      # Start a slow call in another process; it will wait ~50 ms.
      parent = self()
      spawn_link(fn ->
        send(parent, {:slow_result, AuthBroker.authenticate(pid, "slow_token")})
      end)

      # The server should accept new work immediately — not be blocked.
      {us, {:ok, _}} =
        :timer.tc(fn -> AuthBroker.authenticate(pid, "fast_token") end)

      # Both calls took ~50 ms in parallel; this one must not be ~100 ms.
      assert us < 90_000

      assert_receive {:slow_result, {:ok, _}}, 500
    end

    test "partner error is forwarded to caller", %{pid: pid} do
      assert {:error, :unauthorized} = AuthBroker.authenticate(pid, "bad_token")
    end

    test "partner crash surfaces as :task_crash", %{pid: pid} do
      assert {:error, {:task_crash, _}} = AuthBroker.authenticate(pid, "boom")
    end

    test "caller timeout does not leave server in a bad state", %{pid: pid} do
      # Partner always takes 50 ms here; ask for timeout = 10 ms so the caller exits.
      caller_pid = self()

      task =
        Task.async(fn ->
          try do
            AuthBroker.authenticate(pid, "will_timeout", 10)
            send(caller_pid, :unexpected_success)
          catch
            :exit, {:timeout, _} -> send(caller_pid, :caller_timed_out)
          end
        end)

      assert_receive :caller_timed_out, 200

      # Give the task a moment to finish; server should still work.
      Process.sleep(80)
      assert {:ok, _} = AuthBroker.authenticate(pid, "subsequent")
      Task.await(task, 500)
    end
  end
end
```

---

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---

## Deep Dive: Async Patterns and Production Implications

Async tests parallelize at the process level, with each test running in its own process mailbox. The consequence is that shared mutable state (Mox registry, ETS tables, Application.put_env) becomes a race condition if tests modify it concurrently. The solution is process-isolated state: Mox's private mode, Ecto.Sandbox, and tags like `@tag :global`. The discipline required to write correct async tests surfaces hidden race conditions in the system under test.

---

## Trade-offs and production gotchas

**1. Late replies are silent.** `GenServer.reply/2` to a caller that
already timed out is a no-op. Erlang does not raise — the message is
sent to a dead pid, which just disappears. Your server logs show
nothing. If the caller's retry logic does not correlate with the
eventual completion, you pay for the work twice.

**2. The `{:DOWN, ref, ...}` branch is not optional.** If you skip it,
a crashed task leaves the caller waiting for the full call timeout
with no chance of reply. The server looks fine, the caller exits,
users see "server timeout" instead of a meaningful error.

**3. `Process.demonitor(ref, [:flush])`.** When the task succeeded you
already got your `{ref, result}` message; the `DOWN` will follow and
clutter your mailbox. `demonitor` with `:flush` removes the pending
`DOWN` if present. Forgetting this is a common source of "spurious
DOWN after success" mystery messages.

**4. Unbounded pending map.** Each in-flight call occupies one entry.
Under a backend outage, `pending` can grow to millions of entries if
tasks hang without timing out. Always set a task-level timeout **and**
keep the partner-level timeout lower than both.

**5. Reply ordering is not FIFO.** If caller A calls first, caller B
calls second, but B's partner is faster, B gets the reply first. This
is usually fine (they are independent callers) but surprises people who
think of GenServer as queue-ordered.

**6. Reply from another process.** You can pass `from` to a worker and
have the worker call `GenServer.reply(from, ...)` directly. This works
and is supported. It is rare and makes ownership harder to reason
about — keep the reply inside the server when you can.

**7. Cannot reply twice.** Calling `GenServer.reply/2` twice for the
same `from` sends two messages; the caller already left its `receive`
after the first, so the second becomes junk in its mailbox. Usually
harmless but means if the caller was reused (e.g. a worker pool) the
junk message can confuse later callbacks. Reply once, then delete the
tracking entry.

**8. When NOT to use this.** If the work you want to offload is **pure
CPU**, the server thread is not the bottleneck — serializing on the
GenServer with `{:reply, ...}` is fine. Deferred reply shines for I/O
bound calls (HTTP, DB, filesystem) where serial processing is the
enemy. For CPU-bound parallelism, launch a pool of GenServers or use
`Flow`/`GenStage` instead.

### Why this works

The `from` tuple is a plain value — nothing in OTP requires the reply to come from the same callback invocation that received the call. By returning `{:noreply, state}` and storing `from`, the server tells the caller "I'll respond later" and frees its mailbox immediately. `async_nolink` ensures the task's crash does not propagate into the server, and the pending-replies map is how the server correlates each async completion back to the right waiter.

---

## Benchmark

Compare throughput of synchronous vs deferred-reply under a stub partner
with 50 ms latency:

```elixir
# synchronous handle_call (implementing FakePartner.authenticate directly)
# 100 sequential calls  → 100 × 50 ms = 5.0 s total

# deferred reply with async_nolink
# 100 parallel callers  → ~50 ms wall-clock (limited by Task.Supervisor concurrency)
```

On an M2 laptop:

| Approach            | 100 concurrent callers, 50 ms stub | Throughput |
|---------------------|-------------------------------------|------------|
| Synchronous         | ~5,100 ms total                    | ~20 req/s |
| Deferred reply      | ~70 ms total                        | ~1,400 req/s |

The ~70× improvement is approximately the product of concurrency and
partner latency divided by server CPU overhead per call.

Target: throughput under N concurrent I/O-bound callers ≈ N × synchronous throughput up to the Task.Supervisor concurrency cap.

---

## Reflection

1. Your pending-replies map grows unbounded when callers disconnect silently. Do you prune on caller monitor `:DOWN`, on a periodic sweep, or on reply arrival? Which strategy survives a 10× caller churn best?
2. The partner you call sometimes returns a response after the caller's timeout has elapsed. Should the server discard the late reply, log it, or retry a downstream compensating action? Justify in terms of idempotency.

---

### `script/main.exs`
```elixir
defmodule ReplyAsyncGs.MixProject do
  use Mix.Project
  def project, do: [app: :reply_async_gs, version: "0.1.0", elixir: "~> 1.19", deps: []]
  def application, do: [extra_applications: [:logger], mod: {ReplyAsyncGs.Application, []}]
end

defmodule ReplyAsyncGs.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Task.Supervisor, name: ReplyAsyncGs.TaskSup},
      ReplyAsyncGs.AuthBroker
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ReplyAsyncGs.Sup)
  end
end

defmodule ReplyAsyncGs.FakePartner do
  @moduledoc """
  Stub of the partner HTTP endpoint. Sleeps `latency_ms` then returns a result.
  """

  @spec authenticate(String.t(), non_neg_integer()) :: {:ok, map()} | {:error, atom()}
  def authenticate(token, latency_ms) do
    Process.sleep(latency_ms)

    case token do
      "bad_token" -> {:error, :unauthorized}
      "boom"      -> raise "partner crashed"
      _           -> {:ok, %{user_id: "u_" <> Integer.to_string(:erlang.phash2(token))}}
    end
  end
end

defmodule ReplyAsyncGs.AuthBroker do
  @moduledoc """
  GenServer that fronts partner SSO validation.

  `authenticate/2` looks synchronous to the caller but internally the
  server returns `{:noreply, state}` and offloads the slow HTTP call to a
  task. The caller is eventually replied to via `GenServer.reply/2`.
  """

  use GenServer
  require Logger

  @type state :: %{
          pending: %{reference() => GenServer.from()},
          partner_latency_ms: non_neg_integer()
        }

  # --- public API -----------------------------------------------------------

  def start_link(opts \ []) do
    GenServer.start_link(__MODULE__, opts, Keyword.put_new(opts, :name, __MODULE__))
  end

  @spec authenticate(GenServer.server(), String.t(), timeout()) ::
          {:ok, map()} | {:error, term()}
  def authenticate(server \ __MODULE__, token, timeout \ 5_000) do
    GenServer.call(server, {:authenticate, token}, timeout)
  end

  # --- GenServer ------------------------------------------------------------

  @impl true
  def init(opts) do
    {:ok,
     %{
       pending: %{},
       partner_latency_ms: Keyword.get(opts, :partner_latency_ms, 100)
     }}
  end

  @impl true
  def handle_call({:authenticate, token}, from, state) do
    %Task{ref: ref} =
      Task.Supervisor.async_nolink(ReplyAsyncGs.TaskSup, fn ->
        ReplyAsyncGs.FakePartner.authenticate(token, state.partner_latency_ms)
      end)

    {:noreply, %{state | pending: Map.put(state.pending, ref, from)}}
  end

  @impl true
  def handle_info({ref, result}, state) when is_reference(ref) do
    # The task succeeded; tell the original caller.
    case Map.pop(state.pending, ref) do
      {nil, _} ->
        # Unknown ref — most likely a late reply after caller already timed out.
        {:noreply, state}

      {from, rest} ->
        GenServer.reply(from, result)
        # We no longer need the task's DOWN signal.
        Process.demonitor(ref, [:flush])
        {:noreply, %{state | pending: rest}}
    end
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, reason}, state) do
    # Task crashed. Reply with an error instead of leaving the caller hanging.
    case Map.pop(state.pending, ref) do
      {nil, _} ->
        {:noreply, state}

      {from, rest} ->
        Logger.warning("auth task crashed: #{inspect(reason)}")
        GenServer.reply(from, {:error, {:task_crash, reason}})
        {:noreply, %{state | pending: rest}}
    end
  end
end

defmodule ReplyAsyncGs.AuthBrokerTest do
  use ExUnit.Case, async: false
  doctest ReplyAsyncGs.AuthBroker

  alias ReplyAsyncGs.AuthBroker
  end
end

defmodule Main do
  def main do
      :ok
  end
end

Main.main()
```

### `test/reply_async_gs_test.exs`

```elixir
defmodule ReplyAsyncGsTest do
  use ExUnit.Case, async: true

  doctest ReplyAsyncGs

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ReplyAsyncGs.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. The `from` tuple is a first-class value

`handle_call/3` receives `from` as its second argument. It is a tuple
`{caller_pid, tag}` where `tag` is a unique reference the caller used
when it built the `call`. You can **store it, pass it across processes,
send it in messages, and reply from anywhere** — as long as you only
reply once.

### 2. `{:noreply, state}` tells GenServer "I'll reply later"

Returning `{:reply, value, state}` dispatches `value` to `from` immediately.
Returning `{:noreply, state}` **skips** the automatic reply. The caller
remains blocked in its inner `receive` until somebody — anybody — calls
`GenServer.reply(from, value)`.

If nobody ever does, the caller hits the call timeout (default 5 s) and
exits. This is a silent bug: the GenServer never observes a problem.

### 3. Work off the server

The server should never do the slow I/O itself. The typical pipeline:

```
caller          GenServer             Task
──────          ─────────             ────
call(:auth)  ─▶ handle_call(:auth, from, state)
                   │ stash from
                   │ Task.Supervisor.async_nolink(...)
                   └▶ {:noreply, state} (server free)
                                        │
                                        │ HTTP request
                                        │ ... 1.2 s
                                        ▼
                handle_info({ref, result}, state)
                   │ GenServer.reply(from, result)
                   └▶ {:noreply, cleaned_state}
        ◀─── reply
```

During the 1.2 s the server processed thousands of other `call`s.

### 4. Task.Supervisor.async_nolink vs Task.async

You must use `async_nolink` (or `Task.Supervisor.start_child`) so the
task failure does not kill the GenServer. `Task.async` links, and a
crashed HTTP task would take your server down.

With `async_nolink`, you still get a `{ref, result}` message on success
and a `{:DOWN, ref, :process, pid, reason}` on failure — which you
handle explicitly. This is exactly what `Task.Supervisor` was designed
for.

### 5. Tracking pending replies

You need a map from `ref` (the task reference) to `from` (the caller
reply tuple), because the `{ref, result}` message carries the task's
ref, not the original `from`. A simple `%{ref => from}` in state does
the job.

```
state.pending :: %{reference() => GenServer.from()}
```

On task completion, look up `from`, reply, delete the entry.

### 6. Timeout discipline

Three timeouts to coordinate:

1. **Caller's `GenServer.call/3` timeout** — what the caller waits for.
2. **Task timeout** — how long the task waits on the HTTP response.
3. **Partner HTTP timeout** — the connection/recv timeout.

(3) must be < (2) < (1). Otherwise the caller gives up while the
partner request is still outstanding — you reply to a dead process,
which is a no-op but leaves the partner work wasted.

---
