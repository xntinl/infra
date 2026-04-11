# Tracing in Production: :sys, :dbg, and :recon_trace

## Project context

You are building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
A request type is intermittently slow -- p99 latency at 800ms while median is 5ms. The slow
requests appear in production only; the behavior disappears when the node is restarted.
You cannot add logging and deploy because the problem is intermittent and you need to
observe it *live*.

Elixir/Erlang has three production-safe tracing tools built into the runtime,
each with different scope, overhead, and safety guarantees. This exercise covers
all three applied to diagnosing slow requests in a live gateway node.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── dev/
│           └── tracer.ex
├── test/
│   └── api_gateway/
│       └── dev/
│           └── tracer_test.exs
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

1. **GenServer-level tracing**: observe the state transitions and message queue
   of a GenServer in real time to determine if slow requests are caused by a
   backed-up message queue or a slow `handle_call` handler.

2. **Function call tracing**: trace all calls to specific functions on a live node,
   with a call count limit to avoid flooding the system.

---

## Tracing tool comparison

```
Tool            Scope               Overhead          Production safe
----------------------------------------------------------------------
:sys.trace      One GenServer        Low               Yes
:sys.log        One GenServer        Minimal           Yes (ring buffer)
:sys.get_state  One GenServer        Zero              Yes (snapshot)
:dbg            Any module/function  HIGH without limit NO without limits
:recon_trace    Any module/function  Controlled        Yes (with msg limit)
```

**Never use `:dbg.tracer/0` on a production node without setting a message limit.**
It traces every matching call system-wide and can generate enough traffic to OOM
the tracing process.

---

## `:sys` -- safe OTP process inspection

Any process implementing an OTP behaviour (GenServer, GenStateMachine, Supervisor)
automatically implements the `:sys` protocol.

```elixir
# Get current state without a code change
:sys.get_state(pid_or_name)

# Log the last N system messages (calls, casts, info) into a ring buffer
:sys.log(pid_or_name, {true, 20})   # enable, keep last 20
:sys.log(pid_or_name, :get)         # retrieve the log
:sys.log(pid_or_name, false)        # disable

# Print each incoming message to the console
:sys.trace(pid_or_name, true)       # enable
:sys.trace(pid_or_name, false)      # disable

# Suspend a process (freezes it -- use only in emergencies)
:sys.suspend(pid_or_name)
:sys.resume(pid_or_name)
```

`:sys.log` is the right tool for diagnosing intermittent issues: enable the ring
buffer, wait for the slow request to reproduce, then read the log. The buffer
captures only OTP messages, not raw `send/receive`.

---

## `:recon_trace` -- safe function tracing

`:recon_trace.calls/3` traces function calls across the live system with a
mandatory message limit. When the limit is reached, tracing stops automatically.

```elixir
# Trace up to 100 calls to Router.dispatch/1
:recon_trace.calls({ApiGateway.Router, :dispatch, 1}, 100)

# Trace with a match spec -- only trace calls where first arg matches a pattern
ms = [{{:_, :_, :"GET"}, [], [{:return_trace}]}]
:recon_trace.calls({ApiGateway.Router, :dispatch, 1}, 50, [{:scope, :local}])

# Stop all tracing
:recon_trace.clear()
```

`:recon_trace` uses `:dbg` internally but adds automatic cleanup and safety
wrappers. Always use `:recon_trace` instead of raw `:dbg` in production.

---

## `:dbg` match specs -- filtering what gets traced

Match specs are the Erlang equivalent of a pattern-match filter for the tracing
system. They specify which calls to trace based on argument patterns:

```elixir
# Trace only when first argument is a map with method "POST"
ms = :dbg.fun2ms(fn [%{method: "POST"} | _] -> true end)

# Return trace -- also trace the return value
ms = :dbg.fun2ms(fn [_conn, _opts] -> {:return_trace} end)
```

`:dbg.fun2ms/1` compiles an Elixir anonymous function into a match spec at
compile time. The argument pattern must match the actual function arguments.

---

## Implementation

### Step 1: `lib/api_gateway/dev/tracer.ex`

```elixir
defmodule ApiGateway.Dev.Tracer do
  @moduledoc """
  Production-safe tracing utilities wrapping :sys and :recon_trace.

  All functions are safe to call on a live node.
  :recon_trace.calls always has a message limit -- tracing stops automatically.

  Usage:
    # Inspect a GenServer state without restarting
    ApiGateway.Dev.Tracer.get_state(MyServer)

    # Log the last 20 messages received by a GenServer
    ApiGateway.Dev.Tracer.start_log(MyServer, 20)
    # ... wait for slow request ...
    ApiGateway.Dev.Tracer.read_log(MyServer)

    # Trace up to 50 calls to Pipeline.call/2
    ApiGateway.Dev.Tracer.trace_calls(MyModule, :call, 2, 50)
  """

  @doc """
  Returns the current state of a GenServer identified by name or pid.
  Safe: read-only, no side effects.
  """
  @spec get_state(GenServer.server()) :: term()
  def get_state(server) do
    :sys.get_state(server)
  end

  @doc """
  Enables the :sys message log ring buffer on `server`, keeping the last `n` messages.
  Call read_log/1 to retrieve the captured messages.
  Call stop_log/1 to disable.

  The ring buffer captures OTP system messages (calls, casts, info) as they arrive.
  It overwrites oldest entries when the buffer is full, so only the last N messages
  are retained at any point.
  """
  @spec start_log(GenServer.server(), pos_integer()) :: :ok
  def start_log(server, n \\ 20) do
    :sys.log(server, {true, n})
    :ok
  end

  @doc """
  Returns the list of OTP messages captured by the ring buffer.
  Format: [{:in, msg} | {:out, reply}] per OTP message.

  :sys.log/2 with :get returns {:ok, messages}. We extract just the messages list.
  """
  @spec read_log(GenServer.server()) :: list()
  def read_log(server) do
    {:ok, messages} = :sys.log(server, :get)
    messages
  end

  @doc """
  Disables the :sys log buffer. Always call this after diagnosis is complete
  to free the ring buffer memory.
  """
  @spec stop_log(GenServer.server()) :: :ok
  def stop_log(server) do
    :sys.log(server, false)
    :ok
  end

  @doc """
  Returns statistics about a GenServer: start time, message counts, reductions.

  Enables statistics collection, immediately retrieves the stats, then disables
  collection. Returns a map with all available statistics data.
  """
  @spec stats(GenServer.server()) :: map()
  def stats(server) do
    :sys.statistics(server, true)

    case :sys.statistics(server, :get) do
      {:ok, stats_list} ->
        :sys.statistics(server, false)
        Map.new(stats_list)

      _ ->
        :sys.statistics(server, false)
        %{}
    end
  end

  @doc """
  Traces up to `max_messages` calls to `module.function/arity` using :recon_trace.
  Prints each call to the calling process's group leader.

  Tracing stops automatically after max_messages calls or when clear/0 is called.
  Uses :recon_trace which wraps :dbg with safety limits -- never use raw :dbg
  in production without these limits.
  """
  @spec trace_calls(module(), atom(), arity(), pos_integer()) :: :ok
  def trace_calls(module, function, arity, max_messages \\ 50) do
    :recon_trace.calls({module, function, arity}, max_messages)
    :ok
  end

  @doc """
  Traces calls with a match spec filter.
  Only calls where the arguments match `match_spec` are traced.

  Example match spec for calls where the first arg has method "GET":
    ms = :dbg.fun2ms(fn [%{method: "GET"} | _] -> true end)

  When a match spec is provided, the arity is embedded in the spec itself.
  """
  @spec trace_calls_with_spec(module(), atom(), arity(), list(), pos_integer()) :: :ok
  def trace_calls_with_spec(module, function, _arity, match_spec, max_messages \\ 50) do
    :recon_trace.calls({module, function, match_spec}, max_messages)
    :ok
  end

  @doc """
  Stops all active tracing. Call this after diagnosis is complete.
  """
  @spec clear() :: :ok
  def clear do
    :recon_trace.clear()
    :ok
  end

  @doc """
  Inspects the message queue length of a pid or registered name.
  A long queue indicates the process is a bottleneck.
  """
  @spec message_queue_len(GenServer.server()) :: non_neg_integer()
  def message_queue_len(server) do
    pid =
      case server do
        pid when is_pid(pid) -> pid
        name when is_atom(name) -> Process.whereis(name)
      end

    case pid do
      nil -> 0
      pid ->
        {:message_queue_len, len} = Process.info(pid, :message_queue_len)
        len
    end
  end
end
```

### Step 2: Tests

```elixir
# test/api_gateway/dev/tracer_test.exs
defmodule ApiGateway.Dev.TracerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Dev.Tracer

  # A test GenServer to use as a target for :sys tracing
  defmodule TestServer do
    use GenServer

    def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts)
    def get(pid), do: GenServer.call(pid, :get)
    def put(pid, value), do: GenServer.cast(pid, {:put, value})

    @impl true
    def init(_), do: {:ok, %{value: :initial}}

    @impl true
    def handle_call(:get, _from, state), do: {:reply, state.value, state}

    @impl true
    def handle_cast({:put, value}, state), do: {:noreply, %{state | value: value}}
  end

  setup do
    {:ok, pid} = start_supervised(TestServer)
    %{pid: pid}
  end

  describe "get_state/1" do
    test "returns current GenServer state", %{pid: pid} do
      state = Tracer.get_state(pid)
      assert state == %{value: :initial}
    end

    test "reflects state after mutation", %{pid: pid} do
      TestServer.put(pid, :updated)
      # Allow the cast to process
      _ = TestServer.get(pid)
      state = Tracer.get_state(pid)
      assert state == %{value: :updated}
    end
  end

  describe "start_log/2, read_log/1, stop_log/1" do
    test "captures messages after start_log", %{pid: pid} do
      Tracer.start_log(pid, 10)

      TestServer.put(pid, :logged_value)
      _ = TestServer.get(pid)

      messages = Tracer.read_log(pid)
      Tracer.stop_log(pid)

      assert is_list(messages)
      assert length(messages) >= 1
    end

    test "read_log returns empty list before any messages", %{pid: pid} do
      Tracer.start_log(pid, 10)
      messages = Tracer.read_log(pid)
      Tracer.stop_log(pid)

      assert is_list(messages)
    end
  end

  describe "message_queue_len/1" do
    test "returns 0 for a process with no queued messages", %{pid: pid} do
      # Ensure the server has processed everything
      _ = TestServer.get(pid)
      assert Tracer.message_queue_len(pid) == 0
    end

    test "returns 0 for a non-existent registered name" do
      assert Tracer.message_queue_len(:nonexistent_server_xyz) == 0
    end
  end

  describe "trace_calls/4 and clear/0" do
    test "trace_calls returns :ok without crashing" do
      result = Tracer.trace_calls(ApiGateway.Dev.Tracer, :get_state, 1, 5)
      assert result == :ok
      Tracer.clear()
    end

    test "clear/0 returns :ok" do
      assert Tracer.clear() == :ok
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/api_gateway/dev/tracer_test.exs --trace
```

---

## Trade-off analysis

| Tool | Scope | Overhead | Stops automatically | Production safe |
|------|-------|----------|---------------------|-----------------|
| `:sys.get_state` | One process | Zero | N/A | Yes |
| `:sys.log` | One process | Minimal (ring buffer) | No -- must call `false` | Yes |
| `:sys.trace` | One process | Low | No | Yes (single process) |
| `:recon_trace.calls` | Any function | Controlled | Yes -- message limit | Yes |
| `:dbg` raw | Any function | HIGH | No | No without limits |

**Decision rule**:
- If the issue is in one known GenServer -> start with `:sys.log` and `:sys.get_state`
- If the issue is spread across function calls -> use `:recon_trace.calls` with a low limit
- Never use raw `:dbg` without `:recon_trace` wrapper on a production node

---

## Common production mistakes

**1. Using `:dbg.tracer/0` without a message limit on a production node**
`:dbg.tracer()` followed by `:dbg.p(:all, :call)` will trace every function call
system-wide. On a busy gateway, this generates millions of trace messages per
second, OOMs the tracer process, and can crash the node. Always use `:recon_trace`
which has mandatory limits.

**2. Forgetting to call `:sys.log(pid, false)` or `:recon_trace.clear()`**
`:sys.log` and `:recon_trace` tracing stays active until explicitly disabled.
In `:sys.log`'s case, the ring buffer accumulates memory. In `:recon_trace`'s case,
it stops after the message limit but leaves trace flags on the tracing target.
Always clean up after diagnosis.

**3. Using `:sys.suspend/1` on a critical GenServer in production**
`:sys.suspend` freezes the process -- all messages queue up. On a GenServer that
handles requests, this causes request timeouts. Use `:sys.get_state` (read-only)
instead of suspend for observation.

**4. Reading `:sys.log` before the slow event happens**
The ring buffer captures the *last N* messages, overwriting older ones. Enable
the log, wait for the slow event to reproduce, *then* read it. Reading it
immediately after enabling returns zero messages.

**5. Assuming `:recon_trace` shows *all* calls up to the limit**
`:recon_trace` uses BEAM's trace mechanism which samples at the VM level. Under
very high call frequency, some calls may not appear in the trace output. Use it
for diagnosis, not as a precise call counter.

---

## Resources

- [`:sys` module -- Erlang docs](https://www.erlang.org/doc/man/sys.html) -- complete API for OTP process inspection
- [`:recon_trace` -- Recon docs](https://ferd.github.io/recon/recon_trace.html) -- safe function tracing with examples
- [Erlang in Anger -- Fred Hebert](https://www.erlang-in-anger.com/) -- chapter 9 covers production tracing strategies
- [`:dbg` module -- Erlang docs](https://www.erlang.org/doc/man/dbg.html) -- raw trace API (understand before using)
- [Match specs -- Erlang docs](https://www.erlang.org/doc/apps/erts/match_spec.html) -- match specification language for tracing filters
