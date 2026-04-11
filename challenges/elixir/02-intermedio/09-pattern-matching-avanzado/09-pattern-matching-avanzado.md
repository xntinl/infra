# Advanced Pattern Matching

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system receives diverse job payloads and must route them to the correct
handler, validate their shape, and decode nested structures from external sources (webhook
events, cron definitions, pipeline steps). All of this routing and decoding logic is cleaner,
safer, and faster with advanced pattern matching than with conditional logic.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── job_router.ex        # ← you implement this
│       └── payload_decoder.ex   # ← you implement this
├── test/
│   └── task_queue/
│       └── pattern_matching_test.exs   # given tests — must pass without modification
└── mix.exs
```

---

## Why advanced pattern matching matters in production code

Pattern matching in Elixir is not just destructuring. It is:

- **Guards** (`when`) that express constraints impossible to encode in structural patterns
  alone — numeric ranges, type checks, string lengths.
- **The pin operator** (`^`) that distinguishes "bind this variable" from "assert this
  variable has the value it already has" — critical in message receive loops and ETS lookups.
- **Multi-clause functions** ordered from most to least specific — the compiler calls the
  first matching clause, making exhaustiveness and precedence explicit.
- **Nested matching** that decodes deeply nested structures in a single `case` arm instead
  of multiple nested conditionals.

The cost of getting this wrong: a function that matches a `:done` job and processes it as
`:pending` because the clause order was wrong. Pattern matching bugs are logic bugs — no
exception is raised, the wrong branch just runs silently.

---

## The business problem

`TaskQueue.JobRouter` takes an incoming job map (from an external webhook or internal
source) and routes it to one of several handlers based on its type, priority, and payload
shape.

`TaskQueue.PayloadDecoder` takes raw maps from external sources (JSON decoded, no struct
guarantees) and decodes them into typed job structs, validating shape and constraints.

---

## Implementation

### Step 1: `lib/task_queue/job_router.ex`

```elixir
defmodule TaskQueue.JobRouter do
  @moduledoc """
  Routes incoming jobs to the correct handler based on type, priority, and payload shape.
  Uses multi-clause functions and guards — no if/cond chains.
  """

  @type job_map :: %{
    type: atom(),
    priority: :low | :normal | :high | :critical,
    payload: any(),
    retry_count: non_neg_integer()
  }

  @doc """
  Returns the handler module for the given job.

  Routing rules (in order of priority):
  1. Critical-priority jobs always go to CriticalHandler, regardless of type.
  2. Jobs that have been retried 3+ times go to DeadLetterHandler.
  3. :webhook jobs with a URL payload go to WebhookHandler.
  4. :cron jobs go to CronHandler.
  5. :pipeline jobs with a list of steps go to PipelineHandler.
  6. All others go to DefaultHandler.
  """
  @spec route(job_map()) :: module()

  # Rule 1: critical priority — always wins
  def route(%{priority: :critical}) do
    TaskQueue.Handlers.CriticalHandler
  end

  # Rule 2: dead letter — too many retries
  # HINT: use a guard: when retry_count >= 3
  def route(%{retry_count: retry_count}) when retry_count >= 3 do
    # TODO: return TaskQueue.Handlers.DeadLetterHandler
  end

  # Rule 3: webhook jobs with a URL payload
  # HINT: nested pattern match on payload: %{url: url} where is_binary(url)
  def route(%{type: :webhook, payload: %{url: url}}) when is_binary(url) do
    # TODO: return TaskQueue.Handlers.WebhookHandler
  end

  # Rule 4: cron jobs
  def route(%{type: :cron}) do
    TaskQueue.Handlers.CronHandler
  end

  # Rule 5: pipeline jobs with a list of steps
  # HINT: pattern match payload: steps when is_list(steps)
  def route(%{type: :pipeline, payload: steps}) when is_list(steps) do
    # TODO: return TaskQueue.Handlers.PipelineHandler
  end

  # Rule 6: default fallback
  def route(_job) do
    TaskQueue.Handlers.DefaultHandler
  end

  @doc """
  Validates a job map and returns {:ok, job} or {:error, reason}.

  Validation rules:
  - :type must be one of :webhook, :cron, :pipeline, :batch, or :adhoc
  - :priority must be one of :low, :normal, :high, :critical
  - :payload must not be nil
  - :retry_count must be a non-negative integer
  """
  @spec validate(map()) :: {:ok, job_map()} | {:error, atom()}
  def validate(%{type: type, priority: priority, payload: payload, retry_count: rc})
      when type in [:webhook, :cron, :pipeline, :batch, :adhoc]
      and priority in [:low, :normal, :high, :critical]
      and payload != nil
      and is_integer(rc)
      and rc >= 0 do
    {:ok, %{type: type, priority: priority, payload: payload, retry_count: rc}}
  end

  def validate(%{type: type})
      when type not in [:webhook, :cron, :pipeline, :batch, :adhoc] do
    {:error, :invalid_type}
  end

  def validate(%{priority: priority})
      when priority not in [:low, :normal, :high, :critical] do
    {:error, :invalid_priority}
  end

  def validate(%{payload: nil}), do: {:error, :nil_payload}
  def validate(%{retry_count: rc}) when not is_integer(rc) or rc < 0, do: {:error, :invalid_retry_count}
  def validate(_), do: {:error, :missing_required_fields}
end
```

### Step 2: `lib/task_queue/payload_decoder.ex`

```elixir
defmodule TaskQueue.PayloadDecoder do
  @moduledoc """
  Decodes raw maps (from JSON, external APIs) into typed job payload structs.
  Demonstrates nested pattern matching, pin operator, and guard clauses.
  """

  defmodule WebhookPayload do
    defstruct [:url, :method, :headers, :body]
  end

  defmodule CronPayload do
    defstruct [:schedule, :command, :timezone]
  end

  defmodule PipelinePayload do
    defstruct [:steps, :on_failure]
  end

  @doc """
  Decodes a raw map into the appropriate payload struct.
  Returns {:ok, struct} or {:error, {field, reason}}.
  """
  @spec decode(map()) :: {:ok, struct()} | {:error, {atom(), atom()}}

  # Webhook — must have url (binary) and method (one of the known HTTP verbs)
  def decode(%{"url" => url, "method" => method} = raw)
      when is_binary(url)
      and method in ["GET", "POST", "PUT", "PATCH", "DELETE"] do
    headers = Map.get(raw, "headers", %{})
    body = Map.get(raw, "body")
    {:ok, %WebhookPayload{url: url, method: method, headers: headers, body: body}}
  end

  # Cron — must have schedule (cron string) and command
  def decode(%{"schedule" => schedule, "command" => command} = raw)
      when is_binary(schedule)
      and is_binary(command) do
    # HINT: build a CronPayload with timezone defaulting to "UTC"
    timezone = Map.get(raw, "timezone", "UTC")
    # TODO: return {:ok, %CronPayload{schedule: schedule, command: command, timezone: timezone}}
  end

  # Pipeline — must have "steps" as a non-empty list
  def decode(%{"steps" => [_ | _] = steps} = raw) do
    # HINT: build a PipelinePayload, on_failure defaults to :abort
    on_failure = Map.get(raw, "on_failure", "abort") |> String.to_existing_atom()
    # TODO: return {:ok, %PipelinePayload{steps: steps, on_failure: on_failure}}
  end

  # Empty steps list is invalid
  def decode(%{"steps" => []}), do: {:error, {:steps, :empty}}

  # Missing required fields
  def decode(%{"url" => _}), do: {:error, {:method, :missing}}
  def decode(%{"method" => _}), do: {:error, {:url, :missing}}
  def decode(%{"schedule" => _}), do: {:error, {:command, :missing}}
  def decode(_), do: {:error, {:payload, :unrecognized_format}}

  @doc """
  Uses the pin operator to assert that the decoded type matches an expected type.
  Returns {:ok, payload} if the type matches, {:error, :type_mismatch} otherwise.
  """
  @spec decode_as(map(), atom()) :: {:ok, struct()} | {:error, atom()}
  def decode_as(raw, expected_type) do
    case decode(raw) do
      {:ok, %^expected_type{} = payload} -> {:ok, payload}
      # HINT: the ^ operator pins expected_type — matches only if the struct module
      #   equals expected_type exactly
      {:ok, _wrong_type} -> {:error, :type_mismatch}
      {:error, _} = err -> err
    end
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/pattern_matching_test.exs
defmodule TaskQueue.PatternMatchingTest do
  use ExUnit.Case, async: true

  alias TaskQueue.JobRouter
  alias TaskQueue.PayloadDecoder
  alias TaskQueue.PayloadDecoder.{WebhookPayload, CronPayload, PipelinePayload}

  describe "JobRouter.route/1" do
    test "critical priority wins over all other rules" do
      job = %{type: :webhook, priority: :critical, payload: %{url: "https://x"}, retry_count: 5}
      assert TaskQueue.Handlers.CriticalHandler = JobRouter.route(job)
    end

    test "dead letter for retry_count >= 3 (non-critical)" do
      job = %{type: :batch, priority: :normal, payload: :anything, retry_count: 3}
      assert TaskQueue.Handlers.DeadLetterHandler = JobRouter.route(job)
    end

    test "webhook handler for :webhook type with url" do
      job = %{type: :webhook, priority: :normal, payload: %{url: "https://example.com"}, retry_count: 0}
      assert TaskQueue.Handlers.WebhookHandler = JobRouter.route(job)
    end

    test "cron handler for :cron type" do
      job = %{type: :cron, priority: :normal, payload: %{schedule: "*/5 * * * *"}, retry_count: 0}
      assert TaskQueue.Handlers.CronHandler = JobRouter.route(job)
    end

    test "pipeline handler for :pipeline type with list payload" do
      job = %{type: :pipeline, priority: :normal, payload: [:step_a, :step_b], retry_count: 0}
      assert TaskQueue.Handlers.PipelineHandler = JobRouter.route(job)
    end

    test "default handler for unmatched jobs" do
      job = %{type: :adhoc, priority: :low, payload: :custom, retry_count: 0}
      assert TaskQueue.Handlers.DefaultHandler = JobRouter.route(job)
    end
  end

  describe "JobRouter.validate/1" do
    test "valid job passes validation" do
      job = %{type: :batch, priority: :high, payload: "work", retry_count: 0}
      assert {:ok, _} = JobRouter.validate(job)
    end

    test "invalid type returns error" do
      job = %{type: :unknown, priority: :normal, payload: "x", retry_count: 0}
      assert {:error, :invalid_type} = JobRouter.validate(job)
    end

    test "nil payload returns error" do
      job = %{type: :batch, priority: :normal, payload: nil, retry_count: 0}
      assert {:error, :nil_payload} = JobRouter.validate(job)
    end
  end

  describe "PayloadDecoder.decode/1" do
    test "decodes webhook payload" do
      raw = %{"url" => "https://api.example.com/hook", "method" => "POST", "body" => %{"event" => "push"}}
      assert {:ok, %WebhookPayload{url: "https://api.example.com/hook", method: "POST"}} = PayloadDecoder.decode(raw)
    end

    test "rejects webhook with missing method" do
      raw = %{"url" => "https://example.com"}
      assert {:error, {:method, :missing}} = PayloadDecoder.decode(raw)
    end

    test "decodes cron payload with default timezone" do
      raw = %{"schedule" => "0 * * * *", "command" => "mix task_queue.run"}
      assert {:ok, %CronPayload{timezone: "UTC"}} = PayloadDecoder.decode(raw)
    end

    test "decodes pipeline payload" do
      raw = %{"steps" => ["compile", "test", "deploy"]}
      assert {:ok, %PipelinePayload{steps: ["compile", "test", "deploy"]}} = PayloadDecoder.decode(raw)
    end

    test "rejects empty steps list" do
      assert {:error, {:steps, :empty}} = PayloadDecoder.decode(%{"steps" => []})
    end
  end

  describe "PayloadDecoder.decode_as/2 — pin operator" do
    test "matches when decoded type equals expected" do
      raw = %{"url" => "https://x.com", "method" => "GET"}
      assert {:ok, %WebhookPayload{}} = PayloadDecoder.decode_as(raw, WebhookPayload)
    end

    test "returns type_mismatch when types differ" do
      raw = %{"url" => "https://x.com", "method" => "GET"}
      assert {:error, :type_mismatch} = PayloadDecoder.decode_as(raw, CronPayload)
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/task_queue/pattern_matching_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Multi-clause + guards | `cond` chain | `case` with nested `if` |
|--------|----------------------|-------------|------------------------|
| Exhaustiveness check | Compiler warns on unreachable clauses | No check | No check |
| Clause ordering | Explicit — first match wins | Explicit | Explicit |
| Readability | High — each clause is self-contained | Medium | Low |
| Performance | Equal — all compile to pattern match bytecode | Equal | Equal |
| Error on no match | `FunctionClauseError` | Falls through `cond` to nil | Depends on else clause |

Reflection question: the `validate/1` function in `JobRouter` has multiple clauses that
each check one field. What happens when a job is missing both `type` and `priority`?
Which clause matches? How would you reorder or restructure to give a more precise error?

---

## Common production mistakes

**1. Wrong clause order — less specific before more specific**
```elixir
# WRONG — the catch-all fires first, specific clauses are unreachable
def route(_job), do: DefaultHandler
def route(%{priority: :critical}), do: CriticalHandler  # never reached

# CORRECT — most specific first
def route(%{priority: :critical}), do: CriticalHandler
def route(_job), do: DefaultHandler
```

**2. Forgetting the pin operator in receive loops**
```elixir
# WRONG — rebinds `ref` to whatever reference arrives
receive do
  {:reply, ref, result} -> result
end

# CORRECT — only matches the reply for the specific ref you sent
ref = make_ref()
send(pid, {:call, ref, :get})
receive do
  {:reply, ^ref, result} -> result
end
```

**3. Guards with functions that can raise**
Guard expressions must be pure and cannot raise. `is_integer/1`, `is_binary/1`, and
arithmetic are safe. Calling arbitrary functions in a guard is a compile error.

**4. Pattern matching on string content with `=~`**
`=~` is not valid in a guard. Use `String.starts_with?/2` inside the function body, or
use binary pattern matching for prefix checks:
```elixir
def route(%{payload: %{url: "https://" <> _rest}}), do: SecureHandler
```

---

## Resources

- [Pattern Matching — Elixir Getting Started](https://elixir-lang.org/getting-started/pattern-matching.html)
- [Guards — HexDocs](https://hexdocs.pm/elixir/patterns-and-guards.html)
- [Kernel — Guard expressions](https://hexdocs.pm/elixir/Kernel.html#module-guards)
- [Elixir in Action — Saša Jurić](https://www.manning.com/books/elixir-in-action-third-edition) — Chapter 4: Data abstractions
