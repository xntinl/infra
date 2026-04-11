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
│       ├── parser.ex                # grok-like pattern matching: nginx, postgres, JSON, custom
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

## The problem

A single server producing 50k log lines per minute must be ingested, parsed, indexed, and made searchable within a few seconds — without dropping events under burst load. The ingestion pipeline and the query engine must run concurrently without blocking each other. Percentile metrics like P99 latency cannot be computed exactly over a stream without storing every value; a streaming approximation (T-Digest) must bound memory use while maintaining <1% quantile error at the tails.

---

## Why this design

**ETS per tenant for data isolation**: each tenant (identified by `source`) gets its own ETS table for raw entries and a separate ETS table for the inverted index. A query for source A cannot accidentally scan source B's table because the table references are never shared. This physical separation also allows the reaper to drop an entire table atomically when retention expires.

**Time-bucketed raw store**: entries are stored under keys `{bucket_ts, entry_id}` where `bucket_ts = floor(inserted_at / bucket_ms) * bucket_ms`. Range queries over a time window only scan the relevant buckets rather than all entries. Bucket size (e.g., 1 minute) trades range scan granularity for memory overhead.

**T-Digest for streaming percentiles**: the T-Digest algorithm maintains a sorted list of weighted centroids. When the centroid count exceeds a configured compression factor, nearby centroids are merged. Quantile estimation interpolates between centroids. Memory is bounded at O(compression_factor) regardless of input size. P99 accuracy is within 1% for typical distributions.

**Alerting with fire/resolve lifecycle**: an alert rule has two states: `normal` and `firing`. It transitions `normal → firing` when the condition is first true and calls the webhook. It transitions `firing → normal` when the condition becomes false and calls the webhook again (resolve notification). Without the resolve transition, a webhook endpoint cannot clear its alert state.

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

### Step 3: Grok-like parser

```elixir
# lib/logplex/parser.ex
defmodule Logplex.Parser do
  @moduledoc """
  Pattern-based log parser.

  Built-in patterns:
    :nginx_access   — extracts: ip, method, path, status, bytes, duration_ms
    :postgres       — extracts: level, pid, message, duration_ms
    :json           — parses JSON body, extracts all top-level keys as fields

  Pattern format: a map of field_name → regex capture.
  Custom patterns can be registered at runtime via register/2.

  Returns {:ok, %{source: s, level: l, message: m, timestamp: t, fields: %{}}}
         or {:error, :no_pattern_matched}.
  """

  @builtin_patterns %{
    nginx_access: ~r/^(?P<ip>\S+) .* "(?P<method>\w+) (?P<path>\S+) HTTP\/\S+" (?P<status>\d+) (?P<bytes>\d+) ".*" ".*" (?P<duration_ms>\d+\.\d+)/,
    postgres:     ~r/^(?P<level>\w+):  .*: (?P<message>.+)$/
  }

  def parse(raw, source, pattern \\ :auto) do
    # TODO: if :auto, try built-in patterns in order; if :json, Jason.decode
    # TODO: if custom pattern name, look up from registered table
    # TODO: extract named captures, normalize timestamp to DateTime
    # TODO: return {:ok, entry} or {:error, :no_pattern_matched}
  end

  def register(name, regex_with_named_captures) do
    # TODO: store in :persistent_term or an ETS table for runtime lookup
  end
end
```

### Step 4: Inverted index

```elixir
# lib/logplex/index.ex
defmodule Logplex.Index do
  @moduledoc """
  Per-tenant inverted index for full-text search.

  Structure: ETS table named :"logplex_index_#{tenant_id}"
    key:   token (lowercase string, stop-word filtered)
    value: sorted list of entry_ids (descending by time)

  On index:
    1. Tokenize message: split on non-word chars, lowercase, remove stop words
    2. For each token, append entry_id to its posting list
    3. Prune posting list if it exceeds max_entries_per_token (prevents hot-term memory blowup)

  On query:
    1. Tokenize query string the same way
    2. Retrieve posting list for each token
    3. Intersect posting lists (entries must match ALL query terms)
    4. Return entry_ids sorted by timestamp descending
  """

  @stop_words ~w(the a an and or in of to is are was were be been being)

  def ensure_table(tenant_id) do
    table_name = :"logplex_index_#{tenant_id}"
    # TODO: :ets.new if not exists, else return existing; use :named_table, :set, :public
    table_name
  end

  def index(tenant_id, entry_id, message) do
    # TODO: tokenize, for each token: :ets.update_element or insert posting list
  end

  def search(tenant_id, query_string, opts \\ []) do
    # TODO: tokenize query, retrieve posting lists, intersect, apply limit
    # HINT: intersection can be done with MapSet.intersection or sorted list merge
  end

  defp tokenize(text) do
    text
    |> String.downcase()
    |> String.split(~r/\W+/, trim: true)
    |> Enum.reject(&(&1 in @stop_words))
  end
end
```

### Step 5: T-Digest streaming quantile estimator

```elixir
# lib/logplex/tdigest.ex
defmodule Logplex.TDigest do
  @moduledoc """
  T-Digest streaming quantile estimator.

  State: %{centroids: [{mean, weight}], compression: k, count: n}

  Algorithm:
    - On add(value): create a new centroid {value, 1}
    - Append to buffer; when buffer exceeds compression, compress
    - compress: sort centroids by mean, merge adjacent centroids within weight limit
    - Weight limit at quantile q: 4 * n * q * (1 - q) / compression

  Quantile estimation:
    - Find the centroid at cumulative weight floor(q * n)
    - Interpolate linearly between adjacent centroids
  """

  def new(compression \\ 100) do
    %{centroids: [], compression: compression, count: 0, buffer: []}
  end

  def add(%{buffer: buf, count: n} = digest, value) do
    new_buf = [value | buf]
    new_digest = %{digest | buffer: new_buf, count: n + 1}
    if length(new_buf) >= digest.compression * 2 do
      compress(new_digest)
    else
      new_digest
    end
  end

  def quantile(%{centroids: centroids, count: n}, q) when q >= 0.0 and q <= 1.0 do
    # TODO: walk centroids accumulating weight, find position floor(q * n)
    # TODO: interpolate between the two centroids straddling the target quantile
  end

  defp compress(%{buffer: buf, centroids: existing, compression: k, count: n} = digest) do
    # TODO: merge buffer + existing centroids, sort by mean
    # TODO: greedy merge: accumulate weight, merge into current centroid while within limit
    # TODO: weight limit for centroid at quantile q: 4 * n * q * (1 - q) / k
    %{digest | centroids: merged, buffer: []}
  end
end
```

### Step 6: Alerter

```elixir
# lib/logplex/alerter.ex
defmodule Logplex.Alerter do
  use GenServer
  @moduledoc """
  Continuous alert rule evaluation.

  A rule: %{id, tenant, condition, webhook_url, state: :normal | :firing}
  Condition example: {count, :error, 5, :minutes, :>, 50}
  Evaluation: every 30 seconds, re-evaluate all rules.
    :normal → :firing  when condition becomes true  → POST webhook {alert: id, status: "firing"}
    :firing → :normal  when condition becomes false → POST webhook {alert: id, status: "resolved"}
  """

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def add_rule(rule), do: GenServer.call(__MODULE__, {:add_rule, rule})

  def init(_opts) do
    schedule_eval()
    {:ok, %{rules: []}}
  end

  def handle_info(:eval, state) do
    # TODO: for each rule, compute current metric value via Logplex.Aggregator
    # TODO: compare to threshold, transition state if needed, fire webhook if transitioned
    schedule_eval()
    {:noreply, updated_state}
  end

  defp schedule_eval, do: Process.send_after(self(), :eval, 30_000)
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/logplex/tdigest_test.exs
defmodule Logplex.TDigestTest do
  use ExUnit.Case, async: true

  alias Logplex.TDigest

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
```

```elixir
# test/logplex/tenancy_test.exs
defmodule Logplex.TenancyTest do
  use ExUnit.Case, async: false

  setup do
    :ok = Logplex.Store.reset_all()
    :ok
  end

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
```

### Step 8: Run the tests

```bash
mix test test/logplex/ --trace
```

### Step 9: Benchmark

```elixir
# bench/logplex_bench.exs
alias Logplex

# Seed some existing data
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
    "P99 aggregation over 1-minute window" => fn ->
      Logplex.aggregate("bench_source", :p99, :duration_ms,
                        from: DateTime.add(DateTime.utc_now(), -60, :second),
                        to: DateTime.utc_now())
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

## Trade-off analysis

| Aspect | ETS inverted index (this impl) | Elasticsearch | PostgreSQL full-text |
|--------|-------------------------------|---------------|----------------------|
| Search latency | sub-ms (in-process ETS) | 5–50ms (network) | 10–100ms (disk) |
| Durability | none (restart = empty index) | full | full ACID |
| Horizontal scale | single node | distributed shards | read replicas |
| Query expressiveness | AND intersection, term match | full DSL, aggregations | SQL with `tsvector` |
| Percentile computation | T-Digest streaming | percentile aggregation (exact) | not built-in |
| Multi-tenancy | ETS table per tenant | index-per-tenant or `_source` filter | schema-per-tenant |

Reflection: the ETS inverted index loses all data on restart. What is the minimum change needed to make the index durable? Compare DETS, writing posting lists to disk on insert, and periodic snapshot approaches for an ingestion rate of 10k entries/second.

---

## Common production mistakes

**1. Ingestion pipeline blocking on index write**
If `Logplex.ingest/2` synchronously writes to both the raw store and the inverted index before returning, a slow full-text indexing operation blocks the TCP acceptor. The ingestor must write to a bounded buffer (e.g., `:queue` or a `GenStage` producer) and return immediately; a separate indexer process drains the buffer.

**2. Posting list growing unboundedly for common tokens**
A token like "error" may appear in every log entry. Without a cap, its posting list grows to millions of entries and every query that includes "error" scans the full list. Cap posting lists at `max_results * 10` entries (drop oldest) or store only the most recent N entries per token.

**3. T-Digest not reset between time windows**
A P99 latency aggregation for "the last 5 minutes" must use only values from that window. If the T-Digest accumulates all historical values, the percentile is computed over all time rather than the requested window. Use a separate T-Digest per time bucket and query only the relevant buckets.

**4. Alert not firing on first evaluation**
An alert rule that transitions `normal → firing` must fire the webhook on the first evaluation where the condition is true, not only on the transition from a previously seen `false` evaluation. Store the previous state explicitly; treat `nil` (never evaluated) as `normal`.

---

## Resources

- RFC 5424: The Syslog Protocol — [tools.ietf.org/html/rfc5424](https://tools.ietf.org/html/rfc5424) — normative syslog format reference
- Dunning, T. — *T-Digest: Computing Accurate Quantiles Using Clusters* — [arxiv.org/abs/1902.04023](https://arxiv.org/abs/1902.04023) — the algorithm paper with error bounds
- Elastic — [Inverted Index documentation](https://www.elastic.co/guide/en/elasticsearch/reference/current/inverted-index.html) — how Elasticsearch builds and queries posting lists
- Logstash Grok Filter — [elastic.co/guide/en/logstash/current/plugins-filters-grok.html](https://www.elastic.co/guide/en/logstash/current/plugins-filters-grok.html) — reference for grok pattern design
- Bourgon, P. — *Logs vs. Metrics vs. Traces* — [peter.bourgon.org/blog/2017/02/21/metrics-tracing-and-logging.html](https://peter.bourgon.org/blog/2017/02/21/metrics-tracing-and-logging.html) — system observability conceptual framing
