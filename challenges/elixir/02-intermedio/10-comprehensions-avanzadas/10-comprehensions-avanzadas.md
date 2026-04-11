# Advanced Comprehensions

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system accumulates job results, stats, and configuration from multiple
sources. Transforming, filtering, and reshaping these collections is a constant need.
`for` comprehensions — with filters, multiple generators, `:into`, `:uniq`, and `:reduce`
— produce cleaner, more declarative code than chained `Enum` calls for many of these cases.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       └── report_builder.ex
├── test/
│   └── task_queue/
│       └── comprehensions_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## Why comprehensions over Enum chains

A comprehension makes the **source, filter, and transformation** visible in one expression.
Compare:

```elixir
# Enum chain — three passes over the data
results
|> Enum.filter(fn r -> r.status == :ok end)
|> Enum.map(fn r -> {r.job_id, r.duration_ms} end)
|> Enum.into(%{})

# Comprehension — one pass, intent is explicit
for %{status: :ok, job_id: id, duration_ms: ms} <- results, into: %{}, do: {id, ms}
```

The comprehension's filter also **pattern-matches** — it skips non-matching shapes silently
instead of crashing. This is important when processing data from external sources where
shape is not guaranteed.

When Enum wins: when you need `Enum.reduce` with complex accumulation, `Enum.sort`, or
chained operations where each step depends on the previous result. Comprehensions are not
a universal replacement — they are the right tool for map/filter/collect patterns.

---

## The business problem

`TaskQueue.ReportBuilder` transforms raw collections of job results and queue stats into
report-friendly structures. It uses comprehensions throughout to keep transformation logic
readable.

---

## Implementation

### Step 1: `lib/task_queue/report_builder.ex`

```elixir
defmodule TaskQueue.ReportBuilder do
  @moduledoc """
  Builds summary reports from raw job result lists.
  Demonstrates comprehension patterns: multiple generators, filters,
  :into, :uniq, :reduce, and nested matching.
  """

  @doc """
  Builds a map of job_id => duration_ms for all successful jobs.
  Uses :into to produce a map directly.
  """
  @spec success_durations([map()]) :: %{String.t() => non_neg_integer()}
  def success_durations(results) do
    for %{status: :ok, job_id: id, duration_ms: ms} <- results, into: %{}, do: {id, ms}
  end

  @doc """
  Returns the list of unique error reasons across all failed jobs.
  Uses :uniq to deduplicate.
  """
  @spec unique_errors([map()]) :: [any()]
  def unique_errors(results) do
    for %{status: :error, error: reason} <- results, uniq: true, do: reason
  end

  @doc """
  Produces a cross-product report of (worker_id, job_type) pairs for all
  completed jobs. Uses two generators.
  """
  @spec worker_type_matrix([map()], [String.t()]) :: [{String.t(), atom()}]
  def worker_type_matrix(results, worker_ids) do
    for worker_id <- worker_ids, %{type: type, status: :ok} <- results, do: {worker_id, type}
  end

  @doc """
  Groups results by status. Returns %{status => [job_ids]}.
  Uses :reduce to accumulate into a map.
  """
  @spec group_by_status([map()]) :: %{atom() => [String.t()]}
  def group_by_status(results) do
    for %{status: status, job_id: id} <- results,
        reduce: %{ok: [], error: [], timeout: []} do
      acc ->
        Map.update(acc, status, [id], fn existing -> [id | existing] end)
    end
  end

  @doc """
  Returns job IDs for jobs that exceeded a duration threshold, sorted by duration desc.
  Combines comprehension with Enum.sort_by at the end.
  """
  @spec slow_jobs([map()], pos_integer()) :: [String.t()]
  def slow_jobs(results, threshold_ms) do
    for %{duration_ms: ms, job_id: id} <- results, ms > threshold_ms, do: {ms, id}
    |> Enum.sort_by(fn {ms, _id} -> ms end, :desc)
    |> Enum.map(fn {_ms, id} -> id end)
  end

  @doc """
  Builds a nested summary: %{worker_id => %{ok: count, error: count, total_ms: integer}}.
  Uses comprehension with :reduce.
  """
  @spec worker_summary([map()]) :: %{String.t() => map()}
  def worker_summary(results) do
    for %{worker_id: wid, status: status, duration_ms: ms} <- results,
        reduce: %{} do
      acc ->
        default = %{ok: 0, error: 0, total_ms: 0}
        worker_stats = Map.get(acc, wid, default)

        updated =
          worker_stats
          |> Map.update!(:total_ms, &(&1 + ms))
          |> Map.update!(status, &(&1 + 1))

        Map.put(acc, wid, updated)
    end
  end
end
```

Each function demonstrates a different comprehension feature:

- **`success_durations/1`** uses `into: %{}` to collect directly into a map. The generator
  pattern `%{status: :ok, job_id: id, duration_ms: ms}` both filters (only `:ok` status)
  and destructures (extracts `id` and `ms`) in one expression. Entries that do not match
  the pattern (like entries with `:error` status or missing keys) are silently skipped.

- **`unique_errors/1`** uses `uniq: true` to deduplicate results. This is equivalent to
  piping through `Enum.uniq/1` at the end, but expressed declaratively.

- **`worker_type_matrix/2`** uses two generators to produce a cartesian product. For each
  `worker_id` and each successful job, a `{worker_id, type}` tuple is emitted. The pattern
  `%{type: type, status: :ok}` in the second generator acts as both a filter and a
  destructure.

- **`group_by_status/1`** uses `:reduce` to build a map accumulator. Each iteration updates
  the map by prepending the job ID to the appropriate status list. The `Map.update/4`
  function handles both initial insertion and subsequent updates.

- **`slow_jobs/1`** combines a comprehension (for filtering and extracting) with
  `Enum.sort_by/3` (for ordering). Comprehensions do not support sorting, so post-processing
  with `Enum` is the right approach.

- **`worker_summary/1`** uses `:reduce` with a nested map structure. The `default` map
  provides zero-value initialization for each new worker. `Map.update!/3` increments the
  counter for the matching status.

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/comprehensions_test.exs
defmodule TaskQueue.ComprehensionsTest do
  use ExUnit.Case, async: true

  alias TaskQueue.ReportBuilder

  @results [
    %{job_id: "j1", status: :ok, duration_ms: 50, type: :webhook, worker_id: "w1", error: nil},
    %{job_id: "j2", status: :error, duration_ms: 200, type: :cron, worker_id: "w1", error: :timeout},
    %{job_id: "j3", status: :ok, duration_ms: 30, type: :batch, worker_id: "w2", error: nil},
    %{job_id: "j4", status: :error, duration_ms: 100, type: :webhook, worker_id: "w2", error: :network},
    %{job_id: "j5", status: :ok, duration_ms: 500, type: :pipeline, worker_id: "w1", error: nil},
    %{job_id: "j6", status: :error, duration_ms: 80, type: :cron, worker_id: "w3", error: :timeout},
    # Entry with missing fields — should be silently skipped by comprehension filters
    %{job_id: "j7", status: :unknown}
  ]

  test "success_durations builds a map of ok job durations" do
    result = ReportBuilder.success_durations(@results)
    assert result == %{"j1" => 50, "j3" => 30, "j5" => 500}
  end

  test "unique_errors deduplicates error reasons" do
    errors = ReportBuilder.unique_errors(@results)
    assert length(errors) == 2
    assert :timeout in errors
    assert :network in errors
  end

  test "worker_type_matrix produces cross-product of workers and successful job types" do
    pairs = ReportBuilder.worker_type_matrix(@results, ["w1", "w2"])
    # 2 workers x 3 ok jobs = 6 pairs
    assert length(pairs) == 6
    assert {"w1", :webhook} in pairs
    assert {"w2", :pipeline} in pairs
  end

  test "group_by_status groups job IDs by status" do
    groups = ReportBuilder.group_by_status(@results)
    assert length(groups.ok) == 3
    assert length(groups.error) == 3
    assert "j1" in groups.ok
    assert "j2" in groups.error
  end

  test "slow_jobs returns IDs exceeding threshold, sorted desc by duration" do
    slow = ReportBuilder.slow_jobs(@results, 100)
    # j5 (500ms) and j2 (200ms) exceed 100ms; j4 is exactly 100 so not included
    assert ["j5", "j2"] == slow
  end

  test "worker_summary aggregates counts and total duration per worker" do
    summary = ReportBuilder.worker_summary(@results)

    assert summary["w1"].ok == 2
    assert summary["w1"].error == 1
    assert summary["w1"].total_ms == 750  # 50 + 200 + 500

    assert summary["w2"].ok == 1
    assert summary["w2"].error == 1
  end

  test "comprehension silently skips entries with missing fields" do
    # j7 has :unknown status and no duration_ms — should not cause KeyError
    assert is_map(ReportBuilder.group_by_status(@results))
    assert is_map(ReportBuilder.worker_summary(@results))
  end
end
```

### Step 3: Run the tests

```bash
mix test test/task_queue/comprehensions_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Comprehension | Enum.map + Enum.filter | Enum.reduce |
|--------|--------------|----------------------|-------------|
| Multiple generators (cartesian product) | Yes — `for a <- x, b <- y` | No — manual nesting needed | Manual |
| Pattern filter (skips non-matching) | Yes — built into generator | No — must use `Enum.filter` first | Manual guard |
| :into — direct collection target | Yes | `Enum.into` at the end | Manual |
| :reduce — arbitrary accumulator | Yes (Elixir 1.8+) | No | Yes |
| :uniq deduplication | Yes — `:uniq: true` | `Enum.uniq` at the end | Manual |
| Readable for simple map/filter? | Yes | Yes | No |
| Readable for complex multi-step transforms? | Moderate | Better | Best |

Reflection question: `worker_type_matrix/2` uses two generators and produces a cartesian
product of workers x ok jobs. If your system has 100 workers and 10,000 results, the
product has 1,000,000 tuples. What alternative data structure or query approach would you
use instead, and when would the cartesian product actually be the correct choice?

---

## Common production mistakes

**1. Using comprehension filters for side effects**
A `for` filter (`for x <- list, condition(x), do: ...`) must be a pure boolean expression.
Using `IO.puts` or `Agent.update` inside the filter will cause unexpected behaviour because
filters are not for side effects.

**2. Expecting comprehension to behave like `Enum.filter` alone**
The comprehension generator with a pattern **silently skips non-matching entries**. This is
usually what you want (resilient to bad data), but if you need to know which entries were
skipped, you cannot detect them from inside the comprehension. Use `Enum.split_with` first.

**3. Forgetting that `:reduce` replaces `:into`**
You cannot use both `:reduce` and `:into` in the same comprehension. If you need to produce
a map with complex accumulation, use `:reduce`. If you just need `Enum.into`, use `:into`.

**4. Mutable-style thinking with `:reduce`**
The accumulator in `:reduce` is immutable. You must return the updated accumulator from each
iteration:
```elixir
# WRONG — returns nothing, acc is not updated
for x <- list, reduce: %{} do
  acc -> Map.put(acc, x.key, x.value)  # must be the last expression
end

# CORRECT — the last expression in the block is the new accumulator value
```

---

## Resources

- [for — Elixir Getting Started](https://elixir-lang.org/getting-started/comprehensions.html)
- [Kernel.SpecialForms.for/1 — HexDocs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#for/1)
- [Enum — HexDocs](https://hexdocs.pm/elixir/Enum.html) — compare with comprehension capabilities
