# Drain on deploy: SIGTERM, K8s preStop, rolling restart

**Project**: `drain_on_deploy` — full rolling-deploy drain: stop accepting, drain in-flight, SIGTERM handler, K8s preStop hook.

---

## Project context

Every deploy of your Phoenix app drops ~0.3% of requests with `502 Bad Gateway`. The cluster has 12 pods; Kubernetes rolls them one by one with `terminationGracePeriodSeconds: 30`. Investigation with tcpdump on a terminating pod shows: Kubernetes sends `SIGTERM`, the BEAM receives it, `Application.stop/1` runs, Cowboy stops accepting on the listener — but there's a 2–4 second window where the kube-proxy still routes traffic to the terminating pod because endpoint propagation through kube-api-server + kube-proxy is not instant. During that window, Cowboy returns `ECONNREFUSED` and NGINX retries as 502.

The fix has four pieces:

1. **K8s `preStop` hook** that sleeps ~5 s BEFORE SIGTERM is sent, so endpoint propagation completes while the app is still serving.
2. **SIGTERM handler in the BEAM** (`:init.stop/0` or a signal handler) that flips the readiness probe to failing AND initiates app-level drain.
3. **Readiness probe drops to failing** as soon as drain starts — this accelerates endpoint removal.
4. **Drain the work**: stop accepting new requests at the HTTP layer, wait for in-flight to complete, shutdown connections cleanly.

This exercise builds the OTP pieces (parts 2–4). Part 1 is YAML and is included for reference.

```
drain_on_deploy/
├── lib/
│   └── drain_on_deploy/
│       ├── application.ex
│       ├── signal_handler.ex
│       ├── gate.ex               # readiness + acceptance flags
│       ├── request_server.ex     # handles work, drains on terminate
│       └── readiness.ex          # HTTP endpoint for K8s probe
├── k8s/
│   └── deployment.yaml           # reference manifest
└── test/
    └── drain_on_deploy/
        ├── signal_handler_test.exs
        └── drain_flow_test.exs
```

---

## Core concepts

### 1. The Kubernetes pod termination sequence

```
t=0.0   kubectl rollout / replicaset scales down
t=0.0   Pod marked Terminating; removed from Service endpoints ← ASYNC propagation
t=0.0   preStop hook runs in container
t=?     preStop returns (or terminationGracePeriodSeconds * 0.1 deadline)
t=?     SIGTERM sent to PID 1
        | ... terminationGracePeriodSeconds total window ...
t=30    SIGKILL sent if still alive
```

The endpoint removal in step 2 takes 1–5 s to propagate through kube-proxy across nodes. If `SIGTERM` arrives before endpoint removal is effective, kube-proxy routes traffic to a dying pod.

**`preStop`** exists to absorb this race: `sleep 5` gives endpoints time to propagate before signal.

### 2. BEAM SIGTERM handling

By default, the BEAM on SIGTERM calls `erl_exit` → `halt(0)`. No clean shutdown. Apps crash mid-request.

Fix: register an OS signal handler via `:os.set_signal/2` and `:gen_event`, OR use the built-in `Node.stop/0` pathway by running via a release with `release_cookie` and relying on `:init.stop/0`.

The cleanest approach in practice: **use Elixir releases** (`mix release`) with `:init.stop/0` triggered from a signal handler. Releases by default install a handler that calls `:init.stop` on SIGTERM, which runs `Application.stop/1` for every app in reverse boot order, invoking `terminate/2` on every supervisor's children.

For dev (`iex -S mix`), there's no release boot script — you must install the handler manually.

### 3. Dual-flag gate

Two separate flags:

- **Readiness** — read by the K8s HTTP probe. Returns 503 during drain → endpoint removed.
- **Acceptance** — read by the request handler. Returns `:draining` if new work arrives after readiness flipped.

Both start `true`; both flip to `false` on drain start. Readers use `:persistent_term.get/1` (sub-µs) or ETS.

```
drain-triggered
       │
       ├──► readiness = false ──► /healthz returns 503 ──► K8s removes endpoint
       │
       └──► accepting  = false ──► new calls return :draining
                                   in-flight calls continue
```

### 4. Drain order: drop new work, finish in-flight, close connections

```
Stage 1: flip both gates (t=0)
Stage 2: wait for in_flight → 0 OR deadline (t=0..10s)
Stage 3: close listener (Cowboy.stop_listener or similar)
Stage 4: return :ok from Application.stop
```

Order matters. If you close the listener before in-flight drains, Cowboy kills in-flight requests.

### 5. Timing budget alignment

```
K8s terminationGracePeriodSeconds = 30s
├── preStop sleep                 = 5s
└── remaining for drain           = 25s
    ├── app drain deadline        = 20s   (leaves 5s safety)
    └── :init.stop shutdown       = ≤ 20s total cascade
```

If drain takes > 25s, K8s SIGKILLs you.

---

## Design decisions

**Option A — rely on supervisor shutdown timeouts alone**
- Pros: zero new code; trust the tree.
- Cons: the supervisor cannot tell K8s to stop routing new traffic; in-flight 502s during the TCP close race.

**Option B — dual-flag readiness gate + SIGTERM handler + preStop hook** (chosen)
- Pros: readiness flips before supervisors stop; K8s stops routing before the pod is killed; in-flight work finishes under a bounded window.
- Cons: four moving parts (readiness flag, accepting flag, signal handler, preStop); test harness must simulate SIGTERM.

→ Chose **B** because the 502-rate-during-deploy metric is the real acceptance criterion, and only explicit coordination between the pod and the cluster eliminates it.

---

## Implementation

### Step 1: Application wiring

**Objective**: Seed `persistent_term` gates up-front and register the signal handler so readiness flips atomically the instant SIGTERM arrives.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
# lib/drain_on_deploy/application.ex
defmodule DrainOnDeploy.Application do
  use Application

  @impl true
  def start(_type, _args) do
    # persistent_term gate is O(1) reads; writes are rare (only on drain).
    :persistent_term.put({__MODULE__, :ready}, true)
    :persistent_term.put({__MODULE__, :accepting}, true)

    # Install signal handler (no-op in release mode — init already handles).
    {:ok, _} = DrainOnDeploy.SignalHandler.install()

    children = [
      DrainOnDeploy.RequestServer,
      DrainOnDeploy.Readiness
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: DrainOnDeploy.Supervisor,
      max_restarts: 3,
      max_seconds: 5
    )
  end

  @impl true
  def stop(_state) do
    # Called when Application.stop(:drain_on_deploy) runs.
    DrainOnDeploy.Gate.start_drain()
    :ok
  end
end
```

### Step 2: The gate

**Objective**: Back the `ready?/0` and `accepting?/0` flags with `persistent_term` so hot-path reads are O(1) and branch-free.

```elixir
# lib/drain_on_deploy/gate.ex
defmodule DrainOnDeploy.Gate do
  @moduledoc "Fast O(1) flags read from everywhere, written once on drain."

  @app DrainOnDeploy.Application

  @spec ready?() :: boolean()
  def ready?, do: :persistent_term.get({@app, :ready}, false)

  @spec accepting?() :: boolean()
  def accepting?, do: :persistent_term.get({@app, :accepting}, false)

  @spec start_drain() :: :ok
  def start_drain do
    :persistent_term.put({@app, :ready}, false)
    :persistent_term.put({@app, :accepting}, false)
    :ok
  end
end
```

### Step 3: Signal handler

**Objective**: Hook SIGTERM via `:erl_signal_server` and spawn `:init.stop/0` so the handler does not deadlock itself.

```elixir
# lib/drain_on_deploy/signal_handler.ex
defmodule DrainOnDeploy.SignalHandler do
  @moduledoc """
  Installs a SIGTERM handler that triggers graceful app shutdown.

  In a release, :init already handles SIGTERM correctly. In dev (iex -S mix),
  we install our own. Idempotent — safe to call multiple times.
  """
  require Logger

  def install do
    # :os.set_signal requires registering a handler via :gen_event.
    case :gen_event.start({:local, :erl_signal_server}) do
      {:ok, _} -> :ok
      {:error, {:already_started, _}} -> :ok
    end

    :ok = :os.set_signal(:sigterm, :handle)

    # Add our handler (idempotent — guard duplicate).
    handlers = :gen_event.which_handlers(:erl_signal_server)

    unless __MODULE__ in handlers do
      :gen_event.add_handler(:erl_signal_server, __MODULE__, [])
    end

    {:ok, self()}
  end

  # :gen_event callbacks
  @behaviour :gen_event

  @impl true
  def init(_), do: {:ok, %{}}

  @impl true
  def handle_event(:sigterm, state) do
    Logger.warning("SIGTERM received — initiating graceful drain")
    # Do NOT call :init.stop here synchronously (it deadlocks the signal
    # handler). Spawn it.
    spawn(fn -> :init.stop() end)
    {:ok, state}
  end

  def handle_event(_, state), do: {:ok, state}

  @impl true
  def handle_call(_msg, state), do: {:ok, :ok, state}

  @impl true
  def handle_info(_msg, state), do: {:ok, state}

  @impl true
  def terminate(_reason, _state), do: :ok

  @impl true
  def code_change(_old, state, _extra), do: {:ok, state}
end
```

### Step 4: The request server

**Objective**: Trap exits and drain in-flight replies against a deadline so SIGTERM never truncates responses mid-flight.

```elixir
# lib/drain_on_deploy/request_server.ex
defmodule DrainOnDeploy.RequestServer do
  @moduledoc """
  Handles requests. Drains in-flight work on shutdown.
  """
  use GenServer
  require Logger

  @drain_deadline_ms 20_000

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec handle_request(term()) :: {:ok, term()} | {:error, :draining}
  def handle_request(req) do
    if DrainOnDeploy.Gate.accepting?() do
      GenServer.call(__MODULE__, {:handle, req}, 30_000)
    else
      {:error, :draining}
    end
  end

  @spec in_flight() :: non_neg_integer()
  def in_flight, do: GenServer.call(__MODULE__, :in_flight)

  @impl true
  def init(:ok) do
    Process.flag(:trap_exit, true)
    {:ok, %{in_flight: 0}}
  end

  @impl true
  def handle_call({:handle, req}, from, state) do
    parent = self()

    Task.start(fn ->
      Process.sleep(100)
      send(parent, {:done, from, {:ok, req}})
    end)

    {:noreply, %{state | in_flight: state.in_flight + 1}}
  end

  def handle_call(:in_flight, _from, state), do: {:reply, state.in_flight, state}

  @impl true
  def handle_info({:done, from, reply}, state) do
    GenServer.reply(from, reply)
    {:noreply, %{state | in_flight: state.in_flight - 1}}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info("[drain] request_server terminating: #{inspect(reason)}, in_flight=#{state.in_flight}")
    DrainOnDeploy.Gate.start_drain()
    drain_loop(state, System.monotonic_time(:millisecond) + @drain_deadline_ms)
    :ok
  end

  defp drain_loop(%{in_flight: 0}, _deadline), do: :ok

  defp drain_loop(state, deadline) do
    now = System.monotonic_time(:millisecond)

    if now >= deadline do
      Logger.warning("[drain] deadline exceeded, #{state.in_flight} in-flight dropped")
      :ok
    else
      receive do
        {:done, from, reply} ->
          GenServer.reply(from, reply)
          drain_loop(%{state | in_flight: state.in_flight - 1}, deadline)
      after
        deadline - now -> :ok
      end
    end
  end
end
```

### Step 5: Readiness probe endpoint

**Objective**: Expose a 200/503 probe bound to the gate so k8s removes the endpoint before SIGTERM reaches the BEAM.

```elixir
# lib/drain_on_deploy/readiness.ex
defmodule DrainOnDeploy.Readiness do
  @moduledoc """
  Minimal HTTP probe endpoint. Returns 200 when ready, 503 when draining.
  Stand-in for the full Cowboy/Plug setup — shows the gate check.
  """
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec probe() :: {200, String.t()} | {503, String.t()}
  def probe do
    if DrainOnDeploy.Gate.ready?() do
      {200, "ok"}
    else
      {503, "draining"}
    end
  end

  @impl true
  def init(:ok), do: {:ok, %{}}
end
```

### Step 6: K8s manifest (reference)

**Objective**: Pair `preStop sleep` with `terminationGracePeriodSeconds` so endpoint removal races ahead of the drain deadline.

```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: drain-on-deploy
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
  template:
    spec:
      terminationGracePeriodSeconds: 30
      containers:
        - name: app
          image: drain_on_deploy:latest
          lifecycle:
            preStop:
              exec:
                # Sleep so endpoint removal propagates before SIGTERM.
                command: ["/bin/sh", "-c", "sleep 5"]
          readinessProbe:
            httpGet:
              path: /healthz
              port: 4000
            periodSeconds: 2
            failureThreshold: 1
          livenessProbe:
            httpGet:
              path: /livez
              port: 4000
            periodSeconds: 10
            failureThreshold: 3
```

### Step 7: Tests

**Objective**: Assert the gate flips under SIGTERM, new traffic is rejected with `:draining`, and in-flight work completes before the deadline.

```elixir
# test/drain_on_deploy/signal_handler_test.exs
defmodule DrainOnDeploy.SignalHandlerTest do
  use ExUnit.Case, async: false

  describe "DrainOnDeploy.SignalHandler" do
    test "installing the handler is idempotent" do
      {:ok, _} = DrainOnDeploy.SignalHandler.install()
      {:ok, _} = DrainOnDeploy.SignalHandler.install()
      handlers = :gen_event.which_handlers(:erl_signal_server)
      assert DrainOnDeploy.SignalHandler in handlers
    end
  end
end

# test/drain_on_deploy/drain_flow_test.exs
defmodule DrainOnDeploy.DrainFlowTest do
  use ExUnit.Case, async: false

  alias DrainOnDeploy.{Gate, RequestServer, Readiness}

  setup do
    :persistent_term.put({DrainOnDeploy.Application, :ready}, true)
    :persistent_term.put({DrainOnDeploy.Application, :accepting}, true)
    :ok
  end

  describe "DrainOnDeploy.DrainFlow" do
    test "ready and accepting by default" do
      assert Gate.ready?()
      assert Gate.accepting?()
      assert {200, "ok"} = Readiness.probe()
    end

    test "start_drain flips both gates" do
      Gate.start_drain()
      refute Gate.ready?()
      refute Gate.accepting?()
      assert {503, "draining"} = Readiness.probe()
    end

    test "requests rejected when not accepting" do
      Gate.start_drain()
      assert {:error, :draining} = RequestServer.handle_request(:ping)
    end

    test "in-flight requests complete during drain when terminate runs" do
      # Kick off 3 concurrent requests.
      tasks =
        for i <- 1..3 do
          Task.async(fn -> RequestServer.handle_request({:req, i}) end)
        end

      Process.sleep(20)

      pid = Process.whereis(RequestServer)
      ref = Process.monitor(pid)
      Process.exit(pid, :shutdown)
      assert_receive {:DOWN, ^ref, :process, ^pid, :shutdown}, 25_000

      results = Task.await_many(tasks, 25_000)
      assert Enum.all?(results, &match?({:ok, _}, &1))
    end
  end
end
```

---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---


## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. `preStop sleep 5` wastes 5 s × N pods on every rollout.** For a 12-pod rolling deploy that's 1 minute of rollout overhead. It is NOT optional — it is the fix for the endpoint-propagation race. If you have aggressive CD, budget for this.

**2. `:init.stop/0` from a signal handler can deadlock.** If you call it synchronously from the `:gen_event` handler, `:init` waits for the signal dispatcher to finish — but the signal dispatcher IS the process trying to call `:init.stop`. Always `spawn(fn -> :init.stop() end)`.

**3. `:persistent_term.put/2` triggers a global GC.** Fine for once-per-deploy drain flip. Catastrophic if you put it on a hot path. Keep this invariant: writes are `O(deploys)`, reads are `O(requests)`.

**4. Readiness probe period matters.** A `periodSeconds: 10, failureThreshold: 3` probe takes 30 s to notice your 503. By then you've already been SIGKILLed. Use `periodSeconds: 2, failureThreshold: 1` for fast drain endpoints.

**5. `Application.stop/1` callback runs AFTER supervisors stop.** The order in a release is: `:init.stop` → app by app in reverse boot order → for each app, the app's root supervisor terminates (which cascades `:shutdown` to children) → THEN the `Application.stop/1` callback runs. If you move drain logic to `Application.stop/1`, your supervisors have already terminated your workers.

**6. `Cowboy.stop_listener` vs `Cowboy.suspend_listener`.** `stop` closes the listener immediately; in-flight connections may die. `suspend` stops accepting new connections but lets existing ones finish. For drains, `suspend` is correct — check your Plug/Phoenix adapter for the equivalent.

**7. Dev vs release behaviour differs.** `iex -S mix` on SIGTERM kills the beam without running your handler unless you install it. Releases have the handler built-in. Write tests that exercise `Process.exit(pid, :shutdown)` directly — don't rely on OS signals in the test suite.

**8. When NOT to use this.** For stateless workers with no in-flight obligations (pure cron-like jobs, cache warmers), drain is overkill. The complexity is warranted only when user-visible requests are in flight at shutdown time.

### Why this works

The dual-flag gate separates "stop passing readiness" from "stop accepting work", which lets K8s remove the pod from the load balancer before the pod refuses connections. The SIGTERM handler delegates to the gate rather than short-circuiting supervisor shutdown, so the supervisor tree is still the authority on child termination. The preStop sleep gives kube-proxy time to propagate endpoint updates cluster-wide, which is the actual cause of the residual 502 rate people blame on "the pod dying too fast".

---

## Benchmark

The drain itself has no steady-state cost. Readiness probe check is `:persistent_term.get/1` — ~20 ns.  
Signal handler install is one-time.

The real measurement to make is the **502 rate during deploy**. Before fix: run a deploy while `ab -n 100000 -c 50` hammers the service. Count 502s. After fix: same load, expect 0 (or < 0.01 %).

Target: 502 rate during rolling deploy < 0.01 % under 5k rps; drain completes within the supervisor shutdown timeout.

---

## Reflection

1. Your service has long-poll connections that last up to 60 seconds. How do you reconcile the K8s `terminationGracePeriodSeconds` with the tail latency of those connections — do you cap the long-poll, extend the grace period, or drop them mid-poll? Argue from the client's perspective.
2. A new team argues drain is unnecessary because their service is idempotent. Construct a concrete failure scenario where idempotency alone does not prevent user-visible regressions during a rolling deploy.

---

## Resources

- [K8s pod lifecycle — termination](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination) — preStop semantics, SIGTERM timing.
- [`:os.set_signal/2` — erlang docs](https://www.erlang.org/doc/man/os.html#set_signal-2) — OS signal handling in BEAM.
- [`:init.stop/0` — erlang docs](https://www.erlang.org/doc/man/init.html#stop-0) — graceful VM halt.
- [Phoenix.Endpoint drain_connections](https://hexdocs.pm/phoenix/Phoenix.Endpoint.html) — production drain in a real web server.
- [Fred Hébert — Handling Overload](https://ferd.ca/handling-overload.html) — drain + load shedding combined.
- [SRE book — zero-downtime deploys](https://sre.google/workbook/non-abstract-design/) — the end-to-end view.
- [Bandit web server drain handling](https://github.com/mtrudel/bandit/blob/main/lib/bandit/application.ex) — modern Elixir HTTP server drain implementation.
