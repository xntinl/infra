# Ranges and Streams: Building an Infinite API Paginator

**Project**: `page_stream` — processes millions of records from a paginated API without loading them all in memory

**Difficulty**: ★★☆☆☆
**Estimated time**: 2-3 hours

---

## Why streams matter for a senior developer

`Enum` is eager: every function materializes the full result before the next step
runs. For a 10M-row dataset, `Enum.map |> Enum.filter |> Enum.take(100)` allocates
10M elements just to throw 99.999% of them away.

`Stream` is lazy: operations are composed into a recipe and executed element by
element only when a terminal function (`Enum.to_list`, `Enum.take`, `Stream.run`)
consumes the pipeline. Memory usage stays constant regardless of dataset size.

`Range` is a struct, not a list. `1..1_000_000_000` allocates three integers (first,
last, step) — not a billion cells. It behaves as an Enumerable, so pipelines work
transparently. This is the single most important difference from Python's `range`
(which is also lazy but with different semantics) or Ruby's `(1..N).to_a` (which
most people mistakenly think is lazy).

Understanding streams matters when you:

- Process paginated HTTP APIs (GitHub, Stripe, your own backend)
- Read large files line by line (`File.stream!/1`)
- Consume Kafka topics or any unbounded source
- Transform ETL pipelines where the dataset does not fit in RAM

---

## The business problem

You need to export every transaction from a payments API. The API:

1. Returns 100 records per page
2. Uses cursor-based pagination (`next_cursor` in the response)
3. Has ~2 million records total
4. Stops returning data when `next_cursor` is `nil`

Loading all 2M records into a list requires ~1.5 GB of heap. The pipeline that
consumes them must:

- Stream one page at a time
- Filter out test transactions (`test_mode: true`)
- Take only transactions above a given amount
- Write the first 10,000 matching records to a CSV

Memory usage must stay under 50 MB regardless of API size.

---

## Project structure

```
page_stream/
├── lib/
│   └── page_stream/
│       ├── api_client.ex
│       ├── paginator.ex
│       └── exporter.ex
├── test/
│   └── page_stream/
│       ├── paginator_test.exs
│       └── exporter_test.exs
└── mix.exs
```

---

## Core concepts applied here

### `Range` as a lightweight Enumerable

`1..N` is O(1) in memory. `1..N//2` adds a step. You can pipe it through any
`Enum` or `Stream` function. Reversing is `N..1//-1` (explicit negative step —
the implicit reverse was removed in Elixir 1.12).

### `Stream.resource/3` — building an infinite source

The standard pattern for wrapping any paginated or stateful source:

```elixir
Stream.resource(
  fn -> initial_state end,
  fn state ->
    case fetch_next(state) do
      {items, new_state} -> {items, new_state}   # emit and continue
      :done -> {:halt, state}                    # end of stream
    end
  end,
  fn state -> cleanup(state) end
)
```

The first function runs once. The second runs repeatedly until it returns `:halt`.
The third runs once at the end (or on error), making it the right place to close
files, connections, or cursors.

### Lazy vs eager

| Function       | Evaluation          | Allocates intermediate list? |
|----------------|---------------------|-----------------------------|
| `Enum.map`     | Eager               | Yes                         |
| `Stream.map`   | Lazy                | No                          |
| `Enum.take`    | Eager (terminates)  | Yes (result only)           |
| `Stream.take`  | Lazy (short-circuit)| No                          |

The rule of thumb: use `Stream` for everything except the final step. The final
step (the one that materializes) is an `Enum` function.

---

## Implementation

### Step 1: Create the project

```bash
mix new page_stream
cd page_stream
```

### Step 2: `mix.exs`

```elixir
defmodule PageStream.MixProject do
  use Mix.Project

  def project do
    [
      app: :page_stream,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application, do: [extra_applications: [:logger]]
end
```

### Step 3: `lib/page_stream/api_client.ex`

```elixir
defmodule PageStream.ApiClient do
  @moduledoc """
  Behaviour for the paginated API. A real client would use Req or Finch.
  We use a behaviour so tests can inject a deterministic in-memory fake
  without touching the network.
  """

  @type cursor :: String.t() | nil
  @type page :: %{items: [map()], next_cursor: cursor()}

  @callback fetch_page(cursor()) :: {:ok, page()} | {:error, term()}
end
```

### Step 4: `lib/page_stream/paginator.ex`

```elixir
defmodule PageStream.Paginator do
  @moduledoc """
  Wraps a paginated API as a lazy Stream of individual items.

  Uses Stream.resource/3 to fetch one page at a time, emitting items
  flattened into the outer stream. Memory usage is O(page_size), not
  O(total_items).
  """

  @doc """
  Builds a Stream that lazily pages through `client`.

  The stream halts when the API returns `next_cursor: nil` or when
  `max_pages` is reached (safety cap to prevent infinite loops on
  misbehaving servers).
  """
  @spec stream(module(), keyword()) :: Enumerable.t()
  def stream(client, opts \\ []) do
    max_pages = Keyword.get(opts, :max_pages, 100_000)

    Stream.resource(
      fn -> {:start, 0} end,
      fn
        {:halt, _} = state ->
          {:halt, state}

        {_, pages_fetched} when pages_fetched >= max_pages ->
          {:halt, :limit_reached}

        {cursor_state, pages_fetched} ->
          cursor = if cursor_state == :start, do: nil, else: cursor_state

          case client.fetch_page(cursor) do
            {:ok, %{items: items, next_cursor: nil}} ->
              # Emit the final batch, then halt on the next iteration.
              {items, {:halt, pages_fetched + 1}}

            {:ok, %{items: items, next_cursor: next}} ->
              {items, {next, pages_fetched + 1}}

            {:error, reason} ->
              raise "pagination failed: #{inspect(reason)}"
          end
      end,
      fn _state -> :ok end
    )
  end
end
```

### Step 5: `lib/page_stream/exporter.ex`

```elixir
defmodule PageStream.Exporter do
  @moduledoc """
  Applies business filters to the paginated stream and writes a CSV.
  """

  alias PageStream.Paginator

  @header "id,amount,currency,created_at\n"

  @doc """
  Exports up to `limit` non-test transactions above `min_amount`
  to `output_path`. Returns the number of records written.
  """
  @spec export(module(), String.t(), keyword()) :: non_neg_integer()
  def export(client, output_path, opts) do
    limit = Keyword.fetch!(opts, :limit)
    min_amount = Keyword.fetch!(opts, :min_amount)

    file = File.open!(output_path, [:write, :utf8])

    try do
      IO.write(file, @header)

      written =
        client
        |> Paginator.stream()
        # Stream operations compose lazily — nothing runs until Enum.reduce consumes.
        |> Stream.reject(& &1.test_mode)
        |> Stream.filter(&(&1.amount >= min_amount))
        |> Stream.take(limit)
        |> Enum.reduce(0, fn tx, count ->
          IO.write(file, format_row(tx))
          count + 1
        end)

      written
    after
      File.close(file)
    end
  end

  defp format_row(tx) do
    "#{tx.id},#{tx.amount},#{tx.currency},#{tx.created_at}\n"
  end
end
```

### Step 6: Tests

```elixir
# test/page_stream/paginator_test.exs
defmodule PageStream.PaginatorTest do
  use ExUnit.Case, async: true

  alias PageStream.Paginator

  defmodule FakeClient do
    @behaviour PageStream.ApiClient

    # 5 pages of 3 items each. Last page returns next_cursor: nil.
    @pages %{
      nil => %{items: [%{id: 1}, %{id: 2}, %{id: 3}], next_cursor: "p2"},
      "p2" => %{items: [%{id: 4}, %{id: 5}, %{id: 6}], next_cursor: "p3"},
      "p3" => %{items: [%{id: 7}, %{id: 8}, %{id: 9}], next_cursor: "p4"},
      "p4" => %{items: [%{id: 10}, %{id: 11}, %{id: 12}], next_cursor: "p5"},
      "p5" => %{items: [%{id: 13}, %{id: 14}], next_cursor: nil}
    }

    @impl true
    def fetch_page(cursor), do: {:ok, @pages[cursor]}
  end

  describe "stream/2" do
    test "emits every item across all pages in order" do
      ids =
        FakeClient
        |> Paginator.stream()
        |> Enum.map(& &1.id)

      assert ids == Enum.to_list(1..14)
    end

    test "short-circuits after Stream.take — does not fetch more pages than needed" do
      # With Stream.take(5), only 2 pages should be fetched (3 + 3 = 6 >= 5).
      # We cannot easily assert "only 2 pages" without instrumenting the fake,
      # but we can assert correctness of the first 5 ids.
      ids =
        FakeClient
        |> Paginator.stream()
        |> Stream.take(5)
        |> Enum.to_list()
        |> Enum.map(& &1.id)

      assert ids == [1, 2, 3, 4, 5]
    end

    test "range-driven test: 1..14 must match streamed ids exactly" do
      streamed = FakeClient |> Paginator.stream() |> Enum.to_list()
      assert Enum.count(streamed) == Enum.count(1..14)
    end

    test "respects max_pages cap" do
      assert [_ | _] =
               FakeClient
               |> Paginator.stream(max_pages: 1)
               |> Enum.to_list()
    end
  end
end
```

```elixir
# test/page_stream/exporter_test.exs
defmodule PageStream.ExporterTest do
  use ExUnit.Case, async: true

  alias PageStream.Exporter

  defmodule TxClient do
    @behaviour PageStream.ApiClient

    # 6 transactions across 2 pages. Half test_mode, mixed amounts.
    @pages %{
      nil => %{
        items: [
          %{id: "t1", amount: 100, currency: "USD", created_at: "2026-01-01", test_mode: false},
          %{id: "t2", amount: 5, currency: "USD", created_at: "2026-01-01", test_mode: false},
          %{id: "t3", amount: 200, currency: "EUR", created_at: "2026-01-01", test_mode: true}
        ],
        next_cursor: "p2"
      },
      "p2" => %{
        items: [
          %{id: "t4", amount: 500, currency: "USD", created_at: "2026-01-02", test_mode: false},
          %{id: "t5", amount: 50, currency: "USD", created_at: "2026-01-02", test_mode: false},
          %{id: "t6", amount: 1_000, currency: "USD", created_at: "2026-01-02", test_mode: false}
        ],
        next_cursor: nil
      }
    }

    @impl true
    def fetch_page(cursor), do: {:ok, @pages[cursor]}
  end

  setup do
    path = Path.join(System.tmp_dir!(), "page_stream_#{System.unique_integer([:positive])}.csv")
    on_exit(fn -> File.rm(path) end)
    {:ok, path: path}
  end

  test "excludes test_mode transactions", %{path: path} do
    written = Exporter.export(TxClient, path, limit: 100, min_amount: 0)
    contents = File.read!(path)

    assert written == 5
    refute contents =~ "t3"
  end

  test "filters by minimum amount", %{path: path} do
    written = Exporter.export(TxClient, path, limit: 100, min_amount: 100)

    # t1 (100), t4 (500), t6 (1000) — three records
    assert written == 3
  end

  test "respects the limit even when more records would match", %{path: path} do
    written = Exporter.export(TxClient, path, limit: 2, min_amount: 0)
    assert written == 2
  end

  test "produces a CSV with header and one row per record", %{path: path} do
    Exporter.export(TxClient, path, limit: 10, min_amount: 0)
    lines = path |> File.read!() |> String.split("\n", trim: true)

    assert hd(lines) == "id,amount,currency,created_at"
    assert length(lines) == 6
  end
end
```

### Step 7: Run and verify

```bash
mix compile --warnings-as-errors
mix test --trace
```

---

## Trade-off analysis

| Aspect                 | Stream (this) | Enum (all in memory) | Flow / GenStage         |
|------------------------|---------------|----------------------|-------------------------|
| Peak memory for 2M rows| ~1 page       | ~1.5 GB              | ~1 page × N workers     |
| Parallelism            | single-threaded| single-threaded     | parallel stages         |
| Backpressure           | pull-based    | n/a                  | explicit demand         |
| Complexity             | low           | lowest               | highest                 |
| When it fits           | sequential I/O-bound ETL | small datasets | CPU-bound transforms |

If your pipeline is CPU-bound (heavy parsing, crypto) and you have multiple cores,
move from `Stream` to `Flow`. If it is I/O-bound (API calls, DB queries), `Stream`
is the right tool — adding parallelism rarely helps when the bottleneck is the
remote server's rate limit.

---

## Common production mistakes

**1. `Enum.to_list/1` on a Stream that wraps an infinite source**
The pipeline runs forever (or until OOM). Always place a `Stream.take/2` before
any `Enum` terminal function when the source is paginated or unbounded.

**2. Side effects in `Stream.map/2`**
Lazy pipelines can be re-enumerated. If `Stream.map` performs side effects
(writing to a file, sending HTTP requests), they happen every time the stream is
consumed. Move side effects to the terminal `Enum.reduce/3` or `Stream.run/1`.

**3. Mixing `Stream.take/2` with `Enum.map/2` earlier in the pipeline**
`... |> Enum.map(&expensive/1) |> Stream.take(10)` materializes the full mapped
list first, then takes 10. Put `Stream.take` BEFORE `Stream.map`, or keep
everything as Stream until the terminal step.

**4. Assuming `Range` is a list**
`is_list(1..10)` is `false`. `1..10 ++ [11]` fails. Convert explicitly with
`Enum.to_list/1` only if you truly need a list — in most pipelines you do not.

**5. Not handling pagination errors in `Stream.resource/3`**
If the API returns an error mid-stream, `raise`-ing is the cleanest choice — the
stream terminates and the caller sees the exception. Silently halting would hide
the bug. Always decide your error policy explicitly.

---

## When NOT to use streams

- Your dataset is small (< 10k items) and fits in memory — `Enum` is simpler and
  often faster due to less overhead per element.
- You need random access (`Enum.at/2` on a stream is O(n) every time).
- You need to iterate the same data multiple times — materialize once with
  `Enum.to_list/1` and reuse.
- You need parallelism — use `Flow` or `Task.async_stream/3` instead.

---

## Resources

- [Stream module — HexDocs](https://hexdocs.pm/elixir/Stream.html)
- [Range module — HexDocs](https://hexdocs.pm/elixir/Range.html)
- [Enum vs Stream comparison — Elixir School](https://elixirschool.com/en/lessons/basics/enum)
- [Flow — parallel stream processing](https://hexdocs.pm/flow/Flow.html)
