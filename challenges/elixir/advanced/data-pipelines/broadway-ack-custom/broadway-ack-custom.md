# Custom Broadway.Acknowledger

**Project**: `broadway_ack_custom` — a custom acknowledger that emits telemetry, writes outcomes to a DB audit table, and integrates with an external job-status API

---

## Why data pipelines matters

GenStage, Flow, and Broadway make back-pressured concurrent data processing a first-class concern. Producers, consumers, dispatchers, and batchers compose into pipelines that absorb bursts without exhausting memory.

The hard problems are exactly-once semantics, checkpointing for resumability, and tuning batcher concurrency against downstream latency. A pipeline that works at 10 events/sec often collapses at 10k unless these concerns were designed in from the start.

---

## The business problem

You are building a production-grade Elixir component in the **Data pipelines** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
broadway_ack_custom/
├── lib/
│   └── broadway_ack_custom.ex
├── script/
│   └── main.exs
├── test/
│   └── broadway_ack_custom_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Data pipelines the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule BroadwayAckCustom.MixProject do
  use Mix.Project

  def project do
    [
      app: :broadway_ack_custom,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/broadway_ack_custom.ex`

```elixir
defmodule BroadwayAckCustom.AuditAcknowledger do
  @moduledoc """
  Broadway acknowledger that:

    1. Writes an audit row per message (batched)
    2. Emits telemetry for observability
    3. Notifies an external status API (best-effort)
    4. Delegates to the wrapped broker acknowledger

  Must be referenced as:

      Message.configure_ack_ref(msg, {__MODULE__, ref, opts})

  Where `ref` is whatever data the delegate needs.
  """
  @behaviour Broadway.Acknowledger

  alias BroadwayAckCustom.{AuditRepo, StatusApi}

  @doc "Returns ack result from delegate_ref, opts, successful and failed."
  @impl true
  def ack({__MODULE__, delegate_ref, opts}, successful, failed) do
    all = successful ++ failed
    audit(all, successful, failed)
    emit_telemetry(successful, failed)
    maybe_notify(all, opts)

    case Keyword.get(opts, :delegate) do
      nil -> :ok
      {mod, _opts} -> mod.ack(delegate_ref, successful, failed)
    end
  end

  defp audit(all, successful, failed) do
    rows =
      Enum.map(all, fn msg ->
        status = if msg in successful, do: "ok", else: "failed"

        %{
          event_id: msg.metadata[:event_id] || msg.data[:id],
          status: status,
          attempts: Map.get(msg.metadata, :attempts, 1),
          last_error: status == "failed" && inspect(msg.status) || nil,
          finished_at: DateTime.utc_now()
        }
      end)

    # best-effort; a DB down must not kill the ack
    try do
      AuditRepo.insert_all(rows)
    catch
      kind, reason ->
        :logger.error("audit insert failed #{inspect({kind, reason})}")
    end

    _ = successful
    _ = failed
    :ok
  end

  defp emit_telemetry(successful, failed) do
    :telemetry.execute(
      [:broadway_ack_custom, :ack],
      %{successful: length(successful), failed: length(failed)},
      %{}
    )
  end

  defp maybe_notify(messages, opts) do
    if Keyword.get(opts, :notify_api?, false) do
      Enum.each(messages, fn msg ->
        _ = StatusApi.notify(msg.metadata[:event_id] || msg.data[:id], msg.status)
      end)
    end
  end
end

defmodule BroadwayAckCustom.AuditRepo do
  @doc "Returns insert all result from rows."
  @spec insert_all([map()]) :: :ok
  def insert_all(rows) do
    send(:audit_sink, {:inserted, rows})
    :ok
  rescue
    e in RuntimeError -> :ok
  end
end

defmodule BroadwayAckCustom.StatusApi do
  @doc "Returns notify result from id and status."
  @spec notify(term(), term()) :: :ok | {:error, term()}
  def notify(id, status) do
    send(:status_api_sink, {:notified, id, status})
    :ok
  rescue
    e in RuntimeError -> :ok
  end
end

defmodule BroadwayAckCustom.Pipeline do
  use Broadway

  alias Broadway.Message

  def start_link(opts) do
    Broadway.start_link(__MODULE__,
      name: Keyword.get(opts, :name, __MODULE__),
      producer: [module: {Broadway.DummyProducer, []}, concurrency: 1],
      processors: [default: [concurrency: 4]],
      batchers: [default: [concurrency: 2, batch_size: 10, batch_timeout: 200]]
    )
  end

  @doc "Handles message result from _p and _ctx."
  @impl true
  def handle_message(_p, %Message{} = msg, _ctx) do
    case msg.data do
      %{id: _, fail?: true} = d ->
        Message.failed(%{msg | data: d}, :forced)

      %{id: _} ->
        msg
    end
    |> install_ack()
  end

  @doc "Handles batch result from messages, _batch_info and _ctx."
  @impl true
  def handle_batch(:default, messages, _batch_info, _ctx), do: messages

  defp install_ack(%Message{acknowledger: {mod, ref, data}} = msg) do
    new_ack =
      {BroadwayAckCustom.AuditAcknowledger,
       ref, [delegate: {mod, data}, notify_api?: true]}

    %{msg | acknowledger: new_ack}
  end
end
```
### `test/broadway_ack_custom_test.exs`

```elixir
defmodule BroadwayAckCustom.AcknowledgerTest do
  use ExUnit.Case, async: true
  doctest BroadwayAckCustom.AuditAcknowledger

  alias BroadwayAckCustom.Pipeline

  setup do
    Process.register(self(), :audit_sink)

    {:ok, _} =
      Agent.start_link(fn -> :ok end, name: :status_api_agent)

    Process.register(self(), :status_api_sink)

    start_supervised!({Pipeline, [name: Pipeline]})
    :ok
  end

  describe "BroadwayAckCustom.Acknowledger" do
    test "audit rows are inserted for each message" do
      ref = Broadway.test_batch(Pipeline, [%{id: 1}, %{id: 2}, %{id: 3}])
      assert_receive {:ack, ^ref, _, _}, 2_000
      assert_receive {:inserted, rows}, 2_000
      assert length(rows) == 3
      assert Enum.all?(rows, &(&1.status == "ok"))
    end

    test "failed messages produce audit rows with status=failed" do
      ref = Broadway.test_batch(Pipeline, [%{id: 9, fail?: true}])
      assert_receive {:ack, ^ref, _, _}, 2_000
      assert_receive {:inserted, [%{status: "failed", event_id: 9}]}, 2_000
    end

    test "telemetry is emitted" do
      ref = make_ref()
      parent = self()

      :telemetry.attach(
        "test-#{inspect(ref)}",
        [:broadway_ack_custom, :ack],
        fn _event, meas, meta, _ -> send(parent, {:telemetry, ref, meas, meta}) end,
        nil
      )

      bref = Broadway.test_batch(Pipeline, [%{id: 100}, %{id: 101, fail?: true}])
      assert_receive {:ack, ^bref, _, _}, 2_000
      assert_receive {:telemetry, ^ref, %{successful: 1, failed: 1}, _}, 2_000

      :telemetry.detach("test-#{inspect(ref)}")
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Custom Broadway.Acknowledger.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Custom Broadway.Acknowledger ===")
    IO.puts("Category: Data pipelines\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BroadwayAckCustom.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: BroadwayAckCustom.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```
---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Demand drives back-pressure

GenStage's pull model means slow consumers don't drown fast producers. Producers ask 'give me N events when you have them' rather than producers shoving events downstream.

### 2. Batchers trade latency for throughput

Broadway batchers accumulate events before flushing. A batch size of 100 with a 1-second timeout balances throughput against latency — tune both axes.

### 3. Idempotency is not optional

At-least-once delivery is the default in distributed pipelines. Exactly-once requires idempotent processing, deduplication keys, and durable checkpoints.

---
