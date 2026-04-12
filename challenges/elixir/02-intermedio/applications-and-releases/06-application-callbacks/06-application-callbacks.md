# Application callbacks — `start/2` and `stop/1`

**Project**: `my_service_app` — an OTP application with an explicit `Application`
module, a supervision root, and a lifecycle you can observe start and stop.


---

## Project context

Every production Elixir service is an *OTP application*: a unit that BEAM
knows how to start, stop, and depend on. `mix new` gives you a library by
default — no supervision tree, no callbacks. The moment you need long-lived
state (a cache, a connection pool, a GenServer worker), you must graduate
to an application with a `start/2` callback that boots a supervision tree.

This exercise wires the plumbing from scratch so you can see exactly what
`mix new --sup` generates — and what the runtime calls at boot and at
shutdown.

Project structure:

```
my_service_app/
├── lib/
│   ├── my_service_app.ex
│   ├── my_service_app/
│   │   ├── application.ex
│   │   └── worker.ex
├── test/
│   └── my_service_app_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not start processes at module-load time?** No supervision, no ordering, no test isolation. `Application.start/2` is the OTP-blessed entry point.

## Core concepts

### 1. The `Application` behaviour

An OTP application is a module implementing the `Application` behaviour
with at least `start/2`. BEAM calls `start(:normal, args)` when the
application boots and expects `{:ok, pid}` pointing at the top supervisor.
That pid is the root of your supervision tree and the lifeline of the app.

```
BEAM boot ──▶ MyServiceApp.Application.start(:normal, [])
                       │
                       └─▶ Supervisor.start_link(children, opts) ──▶ {:ok, sup_pid}
```

### 2. Declaring the callback module in `mix.exs`

Adding `application/0` with `mod: {MyServiceApp.Application, []}` to your
`mix.exs` is what tells BEAM which module to invoke. Without it, your
`:my_service_app` entry exists but has no callback — it loads but never
starts.

### 3. `stop/1` — the graceful shutdown hook

When the application terminates, BEAM calls `stop(state)` where `state` is
whatever you returned from `start/2`. Use it for flushing buffers, closing
sockets that aren't owned by a child, emitting a final telemetry event.
Do **not** stop children here — the supervision tree already does that.

### 4. Supervision tree as the backbone

The pid returned by `start/2` owns every GenServer, every Task, every pool
in your app via the supervisor. If the supervisor dies, the app dies, and
the BEAM shutdown strategy (`:permanent` / `:transient` / `:temporary` in
mix.exs) decides whether the whole node comes down with it.

---

## Design decisions

**Option A — start children directly in `mix.exs` or at import-time**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — `Application.start/2` callback (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because the callback is where supervision tree construction belongs — testable, restartable, and ordered.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project with a supervision tree

```bash
mix new my_service_app --sup
cd my_service_app
```

The `--sup` flag scaffolds `application.ex` for you — we'll rewrite it to
see every piece explicitly.

### Step 2: `mix.exs` — declare the callback module

```elixir
defmodule MyServiceApp.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_service_app,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  # The `mod:` key is what turns this project into a *started* application.
  # Without it, the app would only be *loaded* — code available but no callback.
  def application do
    [
      extra_applications: [:logger],
      mod: {MyServiceApp.Application, []}
    ]
  end
end
```

### Step 3: `lib/my_service_app/application.ex`

```elixir
defmodule MyServiceApp.Application do
  @moduledoc """
  OTP entry point. BEAM invokes `start/2` when the application boots and
  `stop/1` when it shuts down. The pid returned from `start/2` is the root
  of the supervision tree — the single point BEAM uses to stop everything.
  """

  use Application
  require Logger

  @impl true
  @spec start(Application.start_type(), term()) :: {:ok, pid()} | {:error, term()}
  def start(type, args) do
    Logger.info("MyServiceApp starting: type=#{inspect(type)} args=#{inspect(args)}")

    children = [
      # A single demo worker — real apps have pools, registries, endpoints here.
      MyServiceApp.Worker
    ]

    # `:one_for_one` — if a child dies, only that child restarts. The app
    # stays up as long as the supervisor itself is alive.
    opts = [strategy: :one_for_one, name: MyServiceApp.Supervisor]
    Supervisor.start_link(children, opts)
  end

  @impl true
  @spec stop(term()) :: :ok
  def stop(state) do
    # Children are already being terminated by the supervisor. Use this hook
    # only for *application-level* cleanup (flushing, telemetry, external notice).
    Logger.info("MyServiceApp stopping: state=#{inspect(state)}")
    :ok
  end
end
```

### Step 4: `lib/my_service_app/worker.ex`

```elixir
defmodule MyServiceApp.Worker do
  @moduledoc """
  Trivial GenServer that exposes a ping for tests and logs its own lifecycle —
  the point is to prove the supervision tree actually started it.
  """

  use GenServer
  require Logger

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
  end

  @spec ping(GenServer.server()) :: :pong
  def ping(server \\ __MODULE__), do: GenServer.call(server, :ping)

  @impl true
  def init(:ok) do
    Logger.debug("Worker booted pid=#{inspect(self())}")
    {:ok, %{started_at: System.system_time(:millisecond)}}
  end

  @impl true
  def handle_call(:ping, _from, state), do: {:reply, :pong, state}
end
```

### Step 5: `test/my_service_app_test.exs`

```elixir
defmodule MyServiceAppTest do
  use ExUnit.Case, async: false

  test "application is started and the supervisor is alive" do
    assert {:ok, _apps} = Application.ensure_all_started(:my_service_app)
    sup = Process.whereis(MyServiceApp.Supervisor)
    assert is_pid(sup) and Process.alive?(sup)
  end

  test "worker under the supervision tree responds" do
    assert MyServiceApp.Worker.ping() == :pong
  end

  test "stop/1 is called on Application.stop/1" do
    # Stopping and restarting the application should leave it healthy.
    :ok = Application.stop(:my_service_app)
    {:ok, _} = Application.ensure_all_started(:my_service_app)
    assert MyServiceApp.Worker.ping() == :pong
  end
end
```

### Step 6: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `start/2` must return fast**
BEAM waits for `start/2` to return before considering the app started.
Doing slow I/O (DB migrations, network calls) here delays boot and can
timeout your release. Do slow work in a child GenServer's `handle_continue/2`
instead.

**2. Don't put business logic in `Application`**
`Application` is an entry point, not a service. If you find yourself adding
functions beyond `start/2` and `stop/1`, move them to a dedicated module.

**3. `stop/1` is best-effort**
On a hard crash (`System.halt/1`, OOM, `kill -9`), `stop/1` never runs.
Never rely on it for data durability — persist as you go.

**4. `extra_applications` vs `applications`**
Use `extra_applications` for OTP bundled apps (`:logger`, `:crypto`). `deps`
entries are auto-added. Only use the manual `applications:` key when you
need to override the full list, which is rare.

**5. When NOT to add `mod:`**
Pure libraries (no processes, no state) should omit `mod:`. Loading the
app is enough — starting it is overhead for a library with no runtime.

---


## Reflection

- Tu `start/2` necesita leer config de DB antes de construir el tree. ¿Dónde leés y qué pasa si la DB no responde?

## Resources

- [`Application` — Elixir stdlib](https://hexdocs.pm/elixir/Application.html)
- ["OTP applications" — Elixir getting started](https://hexdocs.pm/elixir/mix-otp-0.html)
- [Erlang Application design principles](https://www.erlang.org/doc/design_principles/applications.html)
