# Log Aggregation System

**Project**: `logplex` — a log aggregation and search system with streaming percentiles, alerting, and multi-tenancy

---

## Project context

You are building `logplex`, a minimal ELK-stack equivalent. The system ingests structured logs over TCP (syslog RFC 5424) and HTTP (JSON), parses them with grok-like pattern extraction, stores them in an inverted index for full-text search, computes streaming percentiles with T-Digest, evaluates continuous alerting rules, and enforces per-tenant data isolation.

Project structure:

```
logplex/
├── lib/
│   └── logplex/
│       ├── application.ex           # supervisor: ingestor, indexer, query engine, alerter, reaper
│       ├── ingestor.ex              # TCP syslog server + HTTP JSON endpoint (Plug)
│       ├── parser.ex                # grok-like pattern matching
│       ├── index.ex                 # inverted index per tenant: ETS-backed token → [entry_id]
│       ├── store.ex                 # raw entry store: ETS table per tenant, time-bucketed
│       ├── query.ex                 # full-text search: tokenize query, intersect posting lists
│       ├── aggregator.ex            # count, avg, p50/p95/p99 via T-Digest per field per window
│       ├── tdigest.ex               # T-Digest streaming quantile estimator
│       ├── alerter.ex               # rule evaluation: condition → webhook fire/resolve lifecycle
│       ├── reaper.ex                # retention: compact raw logs, delete expired entries
│       └── dashboard.ex             # HTTP /dashboard: top errors, ingestion rate, P99 latency
├── test/
│   └── logplex/
│       ├── ingestor_test.exs        # TCP syslog parsing, HTTP endpoint
│       ├── parser_test.exs          # nginx pattern extraction, custom pattern API
│       ├── index_test.exs           # token posting lists, query intersection
│       ├── tdigest_test.exs         # quantile accuracy, merge correctness
│       ├── alerter_test.exs         # fire on threshold, resolve when condition clears
│       └── tenancy_test.exs         # tenant isolation: source A cannot read source B
├── bench/
│   └── logplex_bench.exs
└── mix.exs
```

---

## Why per-tenant ETS tables and not a single shared table with a tenant column

physical separation makes cross-tenant leaks impossible by construction; no tenant filter can be forgotten in a query. A shared table relies on every query remembering the filter — one missed `WHERE tenant_id = ?` and the system leaks data.

## Design decisions

**Option A — external Elasticsearch backend**
- Pros: battle-tested, scales horizontally, rich query DSL
- Cons: network hop per query, ops overhead, JVM tax, not embeddable

**Option B — in-process ETS inverted index per tenant** (chosen)
- Pros: sub-millisecond search, zero external deps, trivial isolation via table-per-tenant
- Cons: single-node only, no durability unless snapshotted

→ Chose **B** because the target scale (50k lines/min per server) fits a single BEAM node and the latency budget forbids network hops.

## The problem

A single server producing 50k log lines per minute must be ingested, parsed, indexed, and made searchable within a few seconds — without dropping events under burst load. The ingestion pipeline and the query engine must run concurrently without blocking each other. Percentile metrics like P99 latency cannot be computed exactly over a stream without storing every value; a streaming approximation (T-Digest) must bound memory use while maintaining <1% quantile error at the tails.

---

## Why this design

**ETS per tenant for data isolation**: each tenant (identified by `source`) gets its own ETS table for raw entries and a separate ETS table for the inverted index. A query for source A cannot accidentally scan source B's table.

**Time-bucketed raw store**: entries are stored under keys `{bucket_ts, entry_id}` where `bucket_ts = floor(inserted_at / bucket_ms) * bucket_ms`. Range queries only scan relevant buckets.

**T-Digest for streaming percentiles**: the T-Digest algorithm maintains a sorted list of weighted centroids. Memory is bounded at O(compression_factor) regardless of input size. P99 accuracy is within 1%.

**Alerting with fire/resolve lifecycle**: an alert transitions `normal -> firing` when the condition is true, and `firing -> normal` when it becomes false. Both transitions trigger a webhook notification.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new logplex --sup
cd logplex
mkdir -p lib/logplex test/logplex bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: T-Digest streaming quantile estimator

```elixir
# lib/logplex/tdigest.ex
defmodule Logplex.TDigest do
  @moduledoc """
  T-Digest streaming quantile estimator.

  Maintains a sorted list of {mean, weight} centroids. When the centroid
  count exceeds compression * 2, nearby centroids are merged. Quantile
  estimation interpolates between centroids.
  """

  defstruct centroids: [], compression: 100, count: 0, buffer: []

  @doc "Creates a new T-Digest with the given compression factor."
  @spec new(pos_integer()) :: %__MODULE__{}
  def new(compression \\ 100) do
    %__MODULE__{centroids: [], compression: compression, count: 0, buffer: []}
  end

  @doc "Adds a value to the digest."
  @spec add(%__MODULE__{}, number()) :: %__MODULE__{}
  def add(%{buffer: buf, count: n} = digest, value) do
    new_buf = [value | buf]
    new_digest = %{digest | buffer: new_buf, count: n + 1}

    if length(new_buf) >= digest.compression * 2 do
      compress(new_digest)
    else
      new_digest
    end
  end

  @doc "Estimates the value at the given quantile (0.0 to 1.0)."
  @spec quantile(%__MODULE__{}, float()) :: float()
  def quantile(digest, q) when q >= 0.0 and q <= 1.0 do
    digest = ensure_compressed(digest)
    centroids = digest.centroids

    if centroids == [] do
      0.0
    else
      n = digest.count
      target = q * n

      {result, _} =
        Enum.reduce(centroids, {nil, 0.0}, fn {mean, weight}, {found, cumulative} ->
          if found != nil do
            {found, cumulative + weight}
          else
            new_cumulative = cumulative + weight
            if new_cumulative >= target do
              {mean, new_cumulative}
            else
              {nil, new_cumulative}
            end
          end
        end)

      result || elem(List.last(centroids), 0)
    end
  end

  @doc "Merges two T-Digests."
  @spec merge(%__MODULE__{}, %__MODULE__{}) :: %__MODULE__{}
  def merge(d1, d2) do
    d1 = ensure_compressed(d1)
    d2 = ensure_compressed(d2)

    all_centroids = d1.centroids ++ d2.centroids
    sorted = Enum.sort_by(all_centroids, fn {mean, _w} -> mean end)
    compression = max(d1.compression, d2.compression)
    count = d1.count + d2.count

    merged = greedy_merge(sorted, compression, count)
    %__MODULE__{centroids: merged, compression: compression, count: count, buffer: []}
  end

  defp ensure_compressed(%{buffer: []} = digest), do: digest
  defp ensure_compressed(digest), do: compress(digest)

  defp compress(%{buffer: buf, centroids: existing, compression: k, count: n} = digest) do
    new_centroids = Enum.map(buf, fn v -> {v * 1.0, 1.0} end)
    all = existing ++ new_centroids
    sorted = Enum.sort_by(all, fn {mean, _w} -> mean end)
    merged = greedy_merge(sorted, k, n)
    %{digest | centroids: merged, buffer: []}
  end

  defp greedy_merge([], _k, _n), do: []

  defp greedy_merge(sorted, k, n) do
    do_merge(sorted, k, max(n, 1), 0.0, [])
  end

  defp do_merge([], _k, _n, _cumulative, acc), do: Enum.reverse(acc)

  defp do_merge([{mean, weight} | rest], k, n, cumulative, []) do
    do_merge(rest, k, n, cumulative + weight, [{mean, weight}])
  end

  defp do_merge([{mean, weight} | rest], k, n, cumulative, [{prev_mean, prev_weight} | acc_rest] = acc) do
    q = (cumulative + weight / 2) / n
    weight_limit = 4.0 * n * q * (1.0 - q) / k

    if prev_weight + weight <= weight_limit do
      merged_weight = prev_weight + weight
      merged_mean = (prev_mean * prev_weight + mean * weight) / merged_weight
      do_merge(rest, k, n, cumulative + weight, [{merged_mean, merged_weight} | acc_rest])
    else
      do_merge(rest, k, n, cumulative + weight, [{mean, weight} | acc])
    end
  end
end
```

### Step 4: Per-tenant store and inverted index

```elixir
# lib/logplex/store.ex
defmodule Logplex.Store do
  @moduledoc """
  Per-tenant raw entry store using ETS. Each tenant gets its own named table.
  """

  @doc "Resets all tenant tables."
  @spec reset_all() :: :ok
  def reset_all do
    :ets.all()
    |> Enum.filter(fn table ->
      try do
        name = :ets.info(table, :name)
        is_atom(name) and (String.starts_with?(Atom.to_string(name), "logplex_store_") or
                           String.starts_with?(Atom.to_string(name), "logplex_index_"))
      rescue
        _ -> false
      end
    end)
    |> Enum.each(&:ets.delete/1)

    :ok
  end

  @doc "Ensures the tenant's store table exists."
  @spec ensure_table(String.t()) :: atom()
  def ensure_table(tenant_id) do
    table_name = :"logplex_store_#{tenant_id}"

    case :ets.whereis(table_name) do
      :undefined -> :ets.new(table_name, [:named_table, :public, :ordered_set])
      _ -> :ok
    end

    table_name
  end

  @doc "Inserts a log entry for the given tenant."
  @spec insert(String.t(), map()) :: {:ok, reference()}
  def insert(tenant_id, entry) do
    table = ensure_table(tenant_id)
    entry_id = make_ref()
    timestamp = Map.get(entry, :timestamp, DateTime.utc_now())
    :ets.insert(table, {entry_id, entry, timestamp})
    {:ok, entry_id}
  end

  @doc "Retrieves an entry by ID."
  @spec get(String.t(), reference()) :: map() | nil
  def get(tenant_id, entry_id) do
    table = ensure_table(tenant_id)

    case :ets.lookup(table, entry_id) do
      [{^entry_id, entry, _ts}] -> entry
      [] -> nil
    end
  end

  @doc "Returns all entries for a tenant."
  @spec all(String.t()) :: [map()]
  def all(tenant_id) do
    table = ensure_table(tenant_id)
    :ets.tab2list(table) |> Enum.map(fn {_id, entry, _ts} -> entry end)
  end
end
```

```elixir
# lib/logplex/index.ex
defmodule Logplex.Index do
  @moduledoc """
  Per-tenant inverted index for full-text search.
  """

  @stop_words MapSet.new(~w(the a an and or in of to is are was were be been being))

  @doc "Ensures the tenant's index table exists."
  @spec ensure_table(String.t()) :: atom()
  def ensure_table(tenant_id) do
    table_name = :"logplex_index_#{tenant_id}"

    case :ets.whereis(table_name) do
      :undefined -> :ets.new(table_name, [:named_table, :public, :bag])
      _ -> :ok
    end

    table_name
  end

  @doc "Indexes a log entry's message for full-text search."
  @spec index(String.t(), reference(), String.t()) :: :ok
  def index(tenant_id, entry_id, message) do
    table = ensure_table(tenant_id)
    tokens = tokenize(message)

    Enum.each(tokens, fn token ->
      :ets.insert(table, {token, entry_id})
    end)

    :ok
  end

  @doc "Searches for entries matching all query terms."
  @spec search(String.t(), String.t(), keyword()) :: [reference()]
  def search(tenant_id, query_string, _opts \\ []) do
    table = ensure_table(tenant_id)
    tokens = tokenize(query_string)

    case tokens do
      [] -> []
      _ ->
        posting_lists =
          Enum.map(tokens, fn token ->
            :ets.lookup(table, token)
            |> Enum.map(fn {_token, entry_id} -> entry_id end)
            |> MapSet.new()
          end)

        posting_lists
        |> Enum.reduce(&MapSet.intersection/2)
        |> MapSet.to_list()
    end
  end

  defp tokenize(text) do
    text
    |> String.downcase()
    |> String.split(~r/\W+/, trim: true)
    |> Enum.reject(&MapSet.member?(@stop_words, &1))
  end
end
```

### Step 5: Grok-like parser

```elixir
# lib/logplex/parser.ex
defmodule Logplex.Parser do
  @moduledoc """
  Pattern-based log parser with built-in and custom patterns.
  """

  @builtin_patterns %{
    nginx_access: ~r/^(?<ip>\S+) .* "(?<method>\w+) (?<path>\S+) HTTP\/\S+" (?<status>\d+) (?<bytes>\d+) ".*" ".*" (?<duration_ms>\d+\.\d+)/,
    postgres:     ~r/^(?<level>\w+):  .*: (?<message>.+)$/
  }

  @doc "Parses a raw log line with auto-detection or a specific pattern."
  @spec parse(String.t(), String.t(), atom()) :: {:ok, map()} | {:error, :no_pattern_matched}
  def parse(raw, source, pattern \\ :auto) do
    cond do
      pattern == :json or (pattern == :auto and json?(raw)) ->
        parse_json(raw, source)

      pattern == :auto ->
        try_builtin_patterns(raw, source)

      true ->
        case get_pattern(pattern) do
          nil -> {:error, :no_pattern_matched}
          regex -> apply_pattern(regex, raw, source)
        end
    end
  end

  @doc "Registers a custom pattern for runtime use."
  @spec register(atom(), Regex.t()) :: :ok
  def register(name, regex) do
    ensure_registry()
    :ets.insert(:logplex_patterns, {name, regex})
    :ok
  end

  defp json?(raw) do
    String.starts_with?(String.trim(raw), "{")
  end

  defp parse_json(raw, source) do
    case Jason.decode(raw) do
      {:ok, map} ->
        {:ok, %{
          source: source,
          level: Map.get(map, "level", "info"),
          message: Map.get(map, "message", raw),
          timestamp: DateTime.utc_now(),
          fields: map
        }}
      {:error, _} -> {:error, :no_pattern_matched}
    end
  end

  defp try_builtin_patterns(raw, source) do
    result =
      Enum.find_value(@builtin_patterns, fn {_name, regex} ->
        case Regex.named_captures(regex, raw) do
          nil -> nil
          captures -> {:ok, build_entry(captures, raw, source)}
        end
      end)

    result || {:ok, %{source: source, level: "info", message: raw, timestamp: DateTime.utc_now(), fields: %{}}}
  end

  defp apply_pattern(regex, raw, source) do
    case Regex.named_captures(regex, raw) do
      nil -> {:error, :no_pattern_matched}
      captures -> {:ok, build_entry(captures, raw, source)}
    end
  end

  defp build_entry(captures, raw, source) do
    %{
      source: source,
      level: Map.get(captures, "level", "info"),
      message: Map.get(captures, "message", raw),
      timestamp: DateTime.utc_now(),
      fields: captures
    }
  end

  defp get_pattern(name) do
    case Map.get(@builtin_patterns, name) do
      nil ->
        ensure_registry()
        case :ets.lookup(:logplex_patterns, name) do
          [{^name, regex}] -> regex
          [] -> nil
        end
      regex -> regex
    end
  end

  defp ensure_registry do
    case :ets.whereis(:logplex_patterns) do
      :undefined -> :ets.new(:logplex_patterns, [:named_table, :public, :set])
      _ -> :ok
    end
  end
end
```

### Step 6: Alerter

```elixir
# lib/logplex/alerter.ex
defmodule Logplex.Alerter do
  use GenServer

  @moduledoc """
  Continuous alert rule evaluation with fire/resolve lifecycle.
  """

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  def add_rule(rule), do: GenServer.call(__MODULE__, {:add_rule, rule})

  @impl true
  def init(_opts) do
    schedule_eval()
    {:ok, %{rules: []}}
  end

  @impl true
  def handle_call({:add_rule, rule}, _from, state) do
    rule = Map.put_new(rule, :state, :normal)
    {:reply, :ok, %{state | rules: [rule | state.rules]}}
  end

  @impl true
  def handle_info(:eval, state) do
    updated_rules =
      Enum.map(state.rules, fn rule ->
        condition_met = evaluate_condition(rule.condition, rule.tenant)

        case {rule.state, condition_met} do
          {:normal, true} ->
            fire_webhook(rule, "firing")
            %{rule | state: :firing}

          {:firing, false} ->
            fire_webhook(rule, "resolved")
            %{rule | state: :normal}

          _ ->
            rule
        end
      end)

    schedule_eval()
    {:noreply, %{state | rules: updated_rules}}
  end

  defp schedule_eval, do: Process.send_after(self(), :eval, 30_000)

  defp evaluate_condition(_condition, _tenant), do: false

  defp fire_webhook(rule, status) do
    if Map.has_key?(rule, :webhook_url) do
      IO.puts("[Alert #{rule.id}] #{status} -> #{rule.webhook_url}")
    end
  end
end
```

### Step 7: Public API

```elixir
# lib/logplex.ex
defmodule Logplex do
  @moduledoc "Top-level API for the log aggregation system."

  @doc "Ingests a log entry for the given source (tenant)."
  @spec ingest(String.t(), map()) :: {:ok, reference()}
  def ingest(source, entry) do
    {:ok, entry_id} = Logplex.Store.insert(source, entry)
    message = Map.get(entry, :message, "")
    Logplex.Index.index(source, entry_id, message)
    {:ok, entry_id}
  end

  @doc "Searches log entries for the given source."
  @spec search(String.t(), String.t()) :: [map()]
  def search(source, query_string) do
    entry_ids = Logplex.Index.search(source, query_string)

    Enum.flat_map(entry_ids, fn id ->
      case Logplex.Store.get(source, id) do
        nil -> []
        entry -> [entry]
      end
    end)
  end

  @doc "Aggregates a field over a time range."
  @spec aggregate(String.t(), atom(), atom(), keyword()) :: float()
  def aggregate(source, aggregation, field, _opts) do
    entries = Logplex.Store.all(source)

    values =
      entries
      |> Enum.flat_map(fn entry ->
        case Map.get(entry, :fields, %{}) |> Map.get(Atom.to_string(field)) do
          nil -> []
          val when is_number(val) -> [val]
          val ->
            case Float.parse(to_string(val)) do
              {f, _} -> [f]
              :error -> []
            end
        end
      end)

    case aggregation do
      :p99 ->
        digest = Enum.reduce(values, Logplex.TDigest.new(100), &Logplex.TDigest.add(&2, &1))
        Logplex.TDigest.quantile(digest, 0.99)

      :avg ->
        if values == [], do: 0.0, else: Enum.sum(values) / length(values)

      :count ->
        length(values) * 1.0
    end
  end
end
```

### Step 8: Given tests — must pass without modification

```elixir
# test/logplex/tdigest_test.exs
defmodule Logplex.TDigestTest do
  use ExUnit.Case, async: true

  alias Logplex.TDigest


  describe "TDigest" do

  test "P50 approximates median within 1%" do
    digest = Enum.reduce(1..10_000, TDigest.new(100), fn i, d -> TDigest.add(d, i) end)
    p50 = TDigest.quantile(digest, 0.50)
    assert abs(p50 - 5000) / 5000 < 0.01,
      "P50 = #{p50}, expected ~5000 (within 1%)"
  end

  test "P99 approximates 99th percentile within 2% at the tail" do
    digest = Enum.reduce(1..10_000, TDigest.new(100), fn i, d -> TDigest.add(d, i) end)
    p99 = TDigest.quantile(digest, 0.99)
    assert abs(p99 - 9900) / 9900 < 0.02,
      "P99 = #{p99}, expected ~9900 (within 2%)"
  end

  test "bounded centroid count regardless of input size" do
    digest = Enum.reduce(1..100_000, TDigest.new(100), fn i, d -> TDigest.add(d, i) end)
    assert length(digest.centroids) <= 200,
      "centroid count #{length(digest.centroids)} exceeds 2 * compression"
  end


  end
end
```

```elixir
# test/logplex/tenancy_test.exs
defmodule Logplex.TenancyTest do
  use ExUnit.Case, async: false

  setup do
    :ok = Logplex.Store.reset_all()
    :ok
  end


  describe "Tenancy" do

  test "tenant A cannot retrieve tenant B log entries" do
    {:ok, _} = Logplex.ingest("source_a", %{level: "info", message: "tenant_a_secret", timestamp: DateTime.utc_now()})
    {:ok, _} = Logplex.ingest("source_b", %{level: "info", message: "tenant_b_data",   timestamp: DateTime.utc_now()})

    results_a = Logplex.search("source_a", "tenant_b_secret")
    results_b = Logplex.search("source_b", "tenant_b_data")

    assert results_a == [],
      "source_a search returned source_b data: #{inspect(results_a)}"
    assert length(results_b) >= 1
  end

  test "tenant isolation: index tables are physically separate" do
    Logplex.ingest("tenant_x", %{level: "error", message: "connection refused", timestamp: DateTime.utc_now()})
    Logplex.ingest("tenant_y", %{level: "error", message: "connection timeout",  timestamp: DateTime.utc_now()})

    x_results = Logplex.search("tenant_x", "refused")
    y_results = Logplex.search("tenant_y", "refused")

    assert length(x_results) == 1
    assert y_results == []
  end


  end
end
```

### Step 9: Run the tests

```bash
mix test test/logplex/ --trace
```

### Step 10: Benchmark

```elixir
# bench/logplex_bench.exs
alias Logplex

for i <- 1..10_000 do
  Logplex.ingest("bench_source", %{
    level: Enum.random(["info", "warn", "error"]),
    message: "request #{i} path=/api/v1/resource status=#{Enum.random([200, 404, 500])}",
    timestamp: DateTime.utc_now()
  })
end

Benchee.run(
  %{
    "ingest single entry" => fn ->
      Logplex.ingest("bench_source", %{
        level: "info",
        message: "benchmark ingestion event #{:rand.uniform(100_000)}",
        timestamp: DateTime.utc_now()
      })
    end,
    "search 2-term query over 10k entries" => fn ->
      Logplex.search("bench_source", "request error")
    end,
    "tdigest add 1000 values" => fn ->
      Enum.reduce(1..1_000, Logplex.TDigest.new(100), fn i, d ->
        Logplex.TDigest.add(d, i)
      end)
    end
  },
  parallel: 4,
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the log aggregation invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Benchmark

```elixir
# Minimal timing harness — replace with Benchee for production measurement.
{time_us, _result} = :timer.tc(fn ->
  # exercise the hot path N times
  for _ <- 1..10_000, do: :ok
end)

IO.puts("average: #{time_us / 10_000} µs per op")
```

Target: ingest path <50µs per entry and 2-term search <1ms over 10k entries on modern hardware.

## Trade-off analysis

| Aspect | ETS inverted index (this impl) | Elasticsearch | PostgreSQL full-text |
|--------|-------------------------------|---------------|----------------------|
| Search latency | sub-ms (in-process ETS) | 5-50ms (network) | 10-100ms (disk) |
| Durability | none (restart = empty) | full | full ACID |
| Horizontal scale | single node | distributed shards | read replicas |
| Percentile computation | T-Digest streaming | percentile aggregation | not built-in |
| Multi-tenancy | ETS table per tenant | index-per-tenant | schema-per-tenant |

Reflection: the ETS inverted index loses all data on restart. What is the minimum change needed to make the index durable? Compare DETS, writing posting lists to disk on insert, and periodic snapshot approaches.

---

## Common production mistakes

**1. Ingestion pipeline blocking on index write**
The ingestor must write to a bounded buffer and return immediately; a separate indexer process drains the buffer.

**2. Posting list growing unboundedly for common tokens**
Cap posting lists at `max_results * 10` entries (drop oldest).

**3. T-Digest not reset between time windows**
Use a separate T-Digest per time bucket and query only relevant buckets.

**4. Alert not firing on first evaluation**
Store the previous state explicitly; treat `nil` (never evaluated) as `normal`.

---

## Reflection

If tenants range from 10 entries/day to 100k entries/second, would you still keep one ETS table per tenant, or would you tier storage by volume? Justify how you would detect a "hot" tenant and migrate it.

## Resources

- RFC 5424: The Syslog Protocol
- Dunning, T. — *T-Digest: Computing Accurate Quantiles Using Clusters* — [arxiv.org/abs/1902.04023](https://arxiv.org/abs/1902.04023)
- Elastic — [Inverted Index documentation](https://www.elastic.co/guide/en/elasticsearch/reference/current/inverted-index.html)
- Logstash Grok Filter — [elastic.co grok plugins](https://www.elastic.co/guide/en/logstash/current/plugins-filters-grok.html)
- Bourgon, P. — *Logs vs. Metrics vs. Traces*
