# Livebook — Operational Notebooks and Kino Widgets

**Project**: `livebook_demo` — interactive operational runbooks, live dashboards, and documentation-as-code with Livebook + Kino.
**Difficulty**: ★★★☆☆
**Estimated time**: 3–6 hours

---

## Project context

You're the tech lead of a team running a fleet of Elixir services. Three problems
keep recurring:

1. **Runbooks rot.** Markdown runbooks in Confluence are out of date within weeks.
   On-call engineers end up IEx-ing into production anyway, running ad-hoc
   snippets from Slack.
2. **Exploration is siloed.** When a senior debugs a weird Redis pattern or an
   Ecto query plan, the knowledge dies in their terminal scrollback.
3. **Non-devs can't see the data.** Product managers ask for a CSV every time
   they need a cohort breakdown. You burn hours on one-off queries.

[Livebook](https://livebook.dev) — José Valim's "collaborative, interactive
notebook" — solves all three at once. A Livebook `.livemd` file is:

- Markdown that also runs Elixir code cells.
- Versionable in git (plain text, no binary blobs).
- Attachable to a **running node** (`runtime = attached`) to debug production
  with real state.
- Embeddable of **Kino** widgets: live tables, time-series charts, forms, maps.

This exercise builds three concrete deliverables you can drop into any project:

- **`livebooks/operational/rate_limiter_inspector.livemd`** — attach to a running
  `api_gateway` node and inspect the ETS rate-limiter state in real time.
- **`livebooks/ml/embedding_playground.livemd`** — explore a Bumblebee
  text-embedding model without touching app code.
- **`livebooks/docs/README.livemd`** — living README that boots the app and
  walks a newcomer through its architecture.

Project structure:

```
livebook_demo/
├── lib/
│   └── livebook_demo/
│       ├── application.ex
│       ├── gateway.ex          # minimal rate-limiter to inspect
│       └── metrics.ex          # in-memory metric store
├── livebooks/
│   ├── operational/
│   │   └── rate_limiter_inspector.livemd
│   ├── ml/
│   │   └── embedding_playground.livemd
│   └── docs/
│       └── README.livemd
└── mix.exs
```

---

## Core concepts

### 1. Livebook runtimes — the three flavors

| Runtime | When to use | Security |
|---------|-------------|----------|
| **Standalone** | Quick experiments, tutorials | Fresh BEAM, no app code |
| **Mix standalone** | Work against the project's deps | Boots `Mix.install` or the repo's `mix.exs` |
| **Attached** | Connect to a running node (dev or prod) | Full node access — gate behind auth |

Attached runtime is the killer feature for operations. You cookie-auth into a
production node and get the same `iex` power plus Kino widgets and persistent
markdown.

### 2. Kino widgets

Kino is the UI toolkit. The primitives you need:

- `Kino.DataTable.new/2` — paginated, sortable tables
- `Kino.VegaLite` — charts via VegaLite grammar
- `Kino.Input.*` — forms: text, number, select, file upload
- `Kino.Frame.new/0` + `Kino.Frame.render/2` — live-update a region
- `Kino.Control.*` — buttons, interval timers, form submissions
- `Kino.Process.app_tree/0` — visual supervision tree

### 3. Smart cells

Smart cells are code cells with a GUI. A Postgres smart cell shows a query
builder; running it generates the underlying Ecto snippet in the notebook. Users
can start visual and transition to code. Popular ones: Chart, Database query,
Neural network (Bumblebee).

### 4. Kino + Nx for dashboards

Combine a polling `Kino.Control.interval/2` with a `Kino.VegaLite` and you get a
live-updating dashboard with ~20 lines of code. No Grafana deployment required
for ad-hoc monitoring.

### 5. Security caveats

Attached runtime with `Node.connect/1` requires:

- Distributed Erlang between notebook and target (`EPMD` reachable or
  `dist_listen: true`).
- The **same cookie** (`--cookie` or `$RELEASE_COOKIE`).
- Firewalling — never expose EPMD (4369) or the BEAM dist port to the internet.

In production we use an SSH tunnel and a Livebook running on the operator's
laptop, never a public Livebook instance talking to prod.

---

## Implementation

### Step 1: Create the supporting project

```bash
mix new livebook_demo --sup
cd livebook_demo
```

`mix.exs`:

```elixir
defp deps do
  [
    {:kino, "~> 0.14"},
    {:kino_vega_lite, "~> 0.1"},
    {:vega_lite, "~> 0.1"}
  ]
end
```

### Step 2: `lib/livebook_demo/gateway.ex`

```elixir
defmodule LivebookDemo.Gateway do
  @moduledoc """
  Minimal rate limiter stored in an ETS table, used as the target of the
  operational livebook. Records request timestamps per client.
  """

  use GenServer

  @table :gateway_requests

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @spec record(String.t()) :: :ok
  def record(client_id) do
    :ets.insert(@table, {client_id, System.monotonic_time(:millisecond)})
    :ok
  end

  @spec snapshot() :: [{String.t(), non_neg_integer()}]
  def snapshot do
    :ets.tab2list(@table)
    |> Enum.group_by(fn {cid, _} -> cid end)
    |> Enum.map(fn {cid, entries} -> {cid, length(entries)} end)
    |> Enum.sort_by(fn {_, n} -> -n end)
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :bag, read_concurrency: true])
    {:ok, %{}}
  end
end
```

### Step 3: `lib/livebook_demo/metrics.ex`

```elixir
defmodule LivebookDemo.Metrics do
  @moduledoc "Rolling in-memory ring buffer of recent metric points."

  use GenServer

  @capacity 1_000

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @spec record(atom(), number()) :: :ok
  def record(name, value), do: GenServer.cast(__MODULE__, {:record, name, value})

  @spec window(atom(), pos_integer()) :: [map()]
  def window(name, last_n), do: GenServer.call(__MODULE__, {:window, name, last_n})

  @impl true
  def init(_), do: {:ok, %{points: []}}

  @impl true
  def handle_cast({:record, name, value}, state) do
    point = %{name: name, value: value, ts: System.system_time(:millisecond)}
    new_points = [point | state.points] |> Enum.take(@capacity)
    {:noreply, %{state | points: new_points}}
  end

  @impl true
  def handle_call({:window, name, last_n}, _from, state) do
    result =
      state.points
      |> Enum.filter(&(&1.name == name))
      |> Enum.take(last_n)
      |> Enum.reverse()

    {:reply, result, state}
  end
end
```

### Step 4: `lib/livebook_demo/application.ex`

```elixir
defmodule LivebookDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      LivebookDemo.Gateway,
      LivebookDemo.Metrics
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: LivebookDemo.Supervisor)
  end
end
```

### Step 5: `livebooks/operational/rate_limiter_inspector.livemd`

Create the notebook file with this content:

````markdown
# Rate Limiter Inspector

## Runtime

Connect this notebook to the target node:

- Open the **Runtime** panel (right sidebar) → `Attached node`
- Name: `livebook_demo@127.0.0.1`
- Cookie: read from `~/.erlang.cookie` or your `.env`

Then run:

```elixir
Node.self()
```

The cell must return a node name that is NOT `nonode@nohost`.

## Generate traffic (for demo only — skip on prod)

```elixir
for _ <- 1..200 do
  client = Enum.random(["alice", "bob", "charlie", "daisy"])
  LivebookDemo.Gateway.record(client)
end

:ok
```

## Live table of request counts

```elixir
data = LivebookDemo.Gateway.snapshot()

Kino.DataTable.new(
  Enum.map(data, fn {client, count} -> %{client: client, requests: count} end),
  sorting_enabled: true
)
```

## Auto-refresh chart

```elixir
chart =
  VegaLite.new(width: 500, height: 300, title: "Requests per client (live)")
  |> VegaLite.mark(:bar)
  |> VegaLite.encode_field(:x, "client", type: :nominal)
  |> VegaLite.encode_field(:y, "requests", type: :quantitative)
  |> Kino.VegaLite.new()
```

```elixir
Kino.listen(Kino.Control.interval(2_000), fn _event ->
  points =
    LivebookDemo.Gateway.snapshot()
    |> Enum.map(fn {c, n} -> %{"client" => c, "requests" => n} end)

  Kino.VegaLite.clear(chart)
  Kino.VegaLite.push_many(chart, points)
end)
```

## Supervision tree

```elixir
Kino.Process.app_tree(:livebook_demo)
```
````

### Step 6: `livebooks/ml/embedding_playground.livemd`

````markdown
# Embedding playground

## Setup

```elixir
Mix.install([
  {:bumblebee, "~> 0.5"},
  {:exla, "~> 0.7"},
  {:kino, "~> 0.14"}
])

Nx.global_default_backend(EXLA.Backend)
```

## Load a tiny model

```elixir
{:ok, model_info} = Bumblebee.load_model({:hf, "sentence-transformers/all-MiniLM-L6-v2"})
{:ok, tokenizer} = Bumblebee.load_tokenizer({:hf, "sentence-transformers/all-MiniLM-L6-v2"})

serving = Bumblebee.Text.text_embedding(model_info, tokenizer)
```

## Interactive form

```elixir
form =
  Kino.Control.form(
    [text: Kino.Input.text("Sentence")],
    submit: "Embed"
  )

out = Kino.Frame.new()
Kino.Layout.grid([form, out], boxed: true)
```

```elixir
Kino.listen(form, fn %{data: %{text: t}} ->
  %{embedding: tensor} = Nx.Serving.run(serving, t)

  Kino.Frame.render(
    out,
    Kino.Markdown.new("**Dims**: #{elem(tensor.shape, 0)}. First 5: `#{inspect(Nx.to_flat_list(tensor) |> Enum.take(5))}`")
  )
end)
```

## Cosine-similarity matrix between 3 sentences

```elixir
sentences = [
  "How do I refund a payment?",
  "I want my money back",
  "What is the airspeed velocity of an unladen swallow?"
]

tensors =
  sentences
  |> Enum.map(&Nx.Serving.run(serving, &1))
  |> Enum.map(& &1.embedding)

# Cosine similarity matrix
cosine = fn a, b ->
  dot = Nx.dot(a, b) |> Nx.to_number()
  norm_a = Nx.LinAlg.norm(a) |> Nx.to_number()
  norm_b = Nx.LinAlg.norm(b) |> Nx.to_number()
  dot / (norm_a * norm_b)
end

for a <- tensors, b <- tensors, do: cosine.(a, b)
|> Enum.chunk_every(3)
```
````

### Step 7: `livebooks/docs/README.livemd`

````markdown
# LivebookDemo — walking tour

This notebook boots the app and walks you through its pieces. Run every cell.

## Boot

```elixir
Application.ensure_all_started(:livebook_demo)
```

## Record some fake traffic

```elixir
for _ <- 1..50, do: LivebookDemo.Gateway.record("acme")
for _ <- 1..20, do: LivebookDemo.Gateway.record("globex")
:ok
```

## Inspect the ETS table directly

```elixir
:ets.info(:gateway_requests)
```

## Metrics ring buffer

```elixir
for _ <- 1..5, do: LivebookDemo.Metrics.record(:request_latency_ms, :rand.uniform(200))
LivebookDemo.Metrics.window(:request_latency_ms, 5)
```

## Architecture diagram

```elixir
Kino.Mermaid.new("""
graph TD
  A[HTTP Request] --> B[Gateway.record/1]
  B --> C[(ETS gateway_requests)]
  D[Livebook] -->|snapshot| C
  D -->|VegaLite chart| E((You))
""")
```
````

### Step 8: Run it

1. Start the target node:

   ```bash
   iex --sname livebook_demo --cookie secret -S mix
   ```

2. Start Livebook (locally):

   ```bash
   mix escript.install hex livebook
   livebook server
   ```

3. Open one of the `.livemd` files. In the **Runtime** panel choose
   *Attached node* and provide `livebook_demo@<your-hostname>` plus cookie
   `secret`.

---

## Trade-offs and production gotchas

**1. Attached runtime shares everything.** A livebook with attached runtime can
read and modify any state in the target node: `:ets.delete_all_objects/1`,
`System.stop/1`, anything. Treat it as `iex` access — SSH in, don't expose.

**2. Smart cells encode versions.** The generated code depends on the installed
version of `kino_*` packages. Upgrading Kino can break old notebooks. Pin deps
or regenerate cells.

**3. `Mix.install/1` is slow on first run.** Compiling dependencies inside the
notebook can take minutes. Pre-warm the Livebook runtime cache or use a shared
`LIVEBOOK_RUNTIME_NODE` with deps pre-installed.

**4. Kino rendering cost.** `Kino.DataTable.new/2` with 100k rows will hog the
notebook. Paginate or sample. For huge tables use `Kino.DataTable.new/2` with
`page_size: 50` and stream data.

**5. Notebooks are markdown.** Diffable in git, but any runtime state created
during execution is transient. Livebook now supports file attachments (uploaded
binaries) under `files/`, but don't commit large ones — the repo grows fast.

**6. Secrets management.** `livebook.dev` doesn't encrypt secrets at rest. Use
**notebook secrets** (prefixed `LB_`) and Livebook's secret manager rather than
committing `.env` files or hard-coding credentials.

**7. Attached notebooks contend with the node's scheduler.** Running a heavy
Nx computation in a notebook attached to a prod node will slow your prod node.
Run Bumblebee/Nx notebooks in standalone or Mix standalone runtimes.

**8. When NOT to use Livebook.** Permanent dashboards (use Phoenix LiveDashboard
or Grafana), customer-facing reports (use a proper BI tool), CI-integrated
checks (use tests). Livebook shines in the "exploratory and collaborative"
middle ground.

---

## Resources

- [Livebook — hexdocs.pm](https://hexdocs.pm/livebook/readme.html)
- [Livebook home — livebook.dev](https://livebook.dev)
- [Kino — hexdocs.pm](https://hexdocs.pm/kino/Kino.html)
- [José Valim — Livebook announcement (Dashbit)](https://dashbit.co/blog/announcing-livebook)
- [Bumblebee notebooks in Livebook](https://hexdocs.pm/bumblebee/examples.html)
- [VegaLite grammar reference](https://vega.github.io/vega-lite/docs/)
- [Livebook security model](https://github.com/livebook-dev/livebook/blob/main/SECURITY.md)
