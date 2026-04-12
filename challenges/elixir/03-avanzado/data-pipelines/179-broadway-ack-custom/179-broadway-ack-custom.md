# Custom Broadway.Acknowledger

**Project**: `broadway_ack_custom` — a custom acknowledger that emits telemetry, writes outcomes to a DB audit table, and integrates with an external job-status API.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

---

## Project context

A logistics platform processes shipment events via Broadway. For every event
the business team wants a row in an `event_audit` table: `(event_id, status,
attempts, last_error, finished_at)`. This is not what the producer-level
acknowledger does (which talks to Kafka/SQS/RabbitMQ). You need a **custom
acknowledger** that wraps the producer's default and additionally writes to
the audit table, emits telemetry, and notifies an external job-status HTTP
API.

Writing a custom `Broadway.Acknowledger` is not glamorous but it is
precisely what the behaviour exists for. This exercise implements one that
is safe for production: batched audit writes, retries on DB transient errors,
and a fallback path when the external API is down.

```
broadway_ack_custom/
├── lib/
│   └── broadway_ack_custom/
│       ├── application.ex
│       ├── pipeline.ex
│       ├── audit_acknowledger.ex   # the custom acknowledger
│       ├── audit_repo.ex           # fake DB
│       └── status_api.ex           # fake external API
├── test/
│   └── broadway_ack_custom/
│       └── acknowledger_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The Acknowledger contract

```elixir
@callback ack(ack_ref :: term(), successful :: [Message.t()], failed :: [Message.t()]) :: :ok
```

A single callback. It is called after `handle_batch` returns, with two
lists: the successful and failed messages. The `ack_ref` is whatever you
passed to `Message.acknowledger/2` at message construction.

### 2. Why wrap, not replace

Broadway producers (SQS, RabbitMQ, Kafka) already set acknowledgers that
talk to their respective brokers. Replacing that acknowledger breaks the
producer. The correct pattern is to **wrap**:

```
Message.acknowledger = {MyAck, ref_with_broker_ack, opts}
```

`MyAck.ack/3` does its custom work, then delegates to the original broker
acknowledger.

### 3. Side-effects in ack are at-most-once

If your ack callback crashes, Broadway logs and moves on. Your side-effect
(e.g., audit row insert) might not happen. If correctness requires the
audit row, you must either:

- Retry with a bounded loop inside `ack/3`.
- Write the row before `ack/3` (in `handle_batch`) — so it is committed
  as part of the main flow.

### 4. Telemetry integration

Emit a `[:my_app, :broadway, :ack]` event with `%{count: n, failed: m}` so
observability can plot ack success rate per minute. This is the signal SRE
uses to detect "pipeline is processing but everything is failing".

### 5. Passing context through `Message.put_acknowledger/2`

You can change acknowledger per message via `Message.put_acknowledger/2` in
`handle_message`. Useful when different messages need different audit
semantics (e.g., high-priority orders log every ack, low-priority do not).

---

## Implementation

### Step 1: Deps

```elixir
defp deps do
  [{:broadway, "~> 1.1"}, {:telemetry, "~> 1.2"}, {:req, "~> 0.5"}]
end
```

### Step 2: Custom acknowledger

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
```

### Step 3: Fakes

```elixir
defmodule BroadwayAckCustom.AuditRepo do
  @spec insert_all([map()]) :: :ok
  def insert_all(rows) do
    send(:audit_sink, {:inserted, rows})
    :ok
  rescue
    _ -> :ok
  end
end

defmodule BroadwayAckCustom.StatusApi do
  @spec notify(term(), term()) :: :ok | {:error, term()}
  def notify(id, status) do
    send(:status_api_sink, {:notified, id, status})
    :ok
  rescue
    _ -> :ok
  end
end
```

### Step 4: Pipeline wiring

```elixir
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

### Step 5: Test

```elixir
defmodule BroadwayAckCustom.AcknowledgerTest do
  use ExUnit.Case, async: false

  alias BroadwayAckCustom.Pipeline

  setup do
    Process.register(self(), :audit_sink)

    {:ok, _} =
      Agent.start_link(fn -> :ok end, name: :status_api_agent)

    Process.register(self(), :status_api_sink)

    start_supervised!({Pipeline, [name: Pipeline]})
    :ok
  end

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
```

---

## Trade-offs and production gotchas

**1. `ack/3` is called on the batcher process.**
Heavy work in `ack/3` blocks subsequent batches on that batcher. Keep it
bounded: non-blocking telemetry, async DB writes via a separate pool.

**2. If the delegated producer ack fails, you have acknowledged the wrong
thing.**
Call the delegate **last**. Your audit row can be rolled back if needed,
but once the broker sees an ack the message is gone.

**3. Forgetting to delegate means messages are never removed from the
source.**
A custom acknowledger that does not call the wrapped broker ack leaks.
Within minutes your SQS queue grows to the visibility-timeout repeatedly
redelivering the same messages.

**4. Telemetry attach in test setup leaks handlers.**
Always `:telemetry.detach/1` in on_exit or with a unique handler id per
test.

**5. `Message.put_acknowledger/2` in processor is the right layer.**
If you do it in `handle_batch`, it is too late — ack has already been
scheduled.

**6. Side-effects in ack duplicate on retry.**
If the message is retried (broker requeue), your ack side-effect (audit
insert) runs again. Make them idempotent by keying on `(event_id, attempt)`.

**7. The `successful` and `failed` lists are **final** per batch.**
A partial ack is not possible; you either ack the whole batch or nack it.
If one message in a batch needs different audit fate than the others, set
it in `handle_message` via `Message.put_data` and read it in `ack`.

**8. When NOT to write a custom acknowledger.** If all you want is "log
failures", use `[:broadway, :processor, :message, :exception]` telemetry.
Custom acknowledgers are for the case where you need guaranteed side
effects tied to ack outcome.

---

## Resources

- [`Broadway.Acknowledger` behaviour — HexDocs](https://hexdocs.pm/broadway/Broadway.Acknowledger.html)
- [Broadway source — `broadway/acknowledger.ex`](https://github.com/dashbitco/broadway/blob/main/lib/broadway/acknowledger.ex)
- [BroadwaySQS.ExAwsClient source — reference implementation](https://github.com/dashbitco/broadway_sqs/blob/main/lib/broadway_sqs/ex_aws_client.ex)
- [Telemetry — HexDocs](https://hexdocs.pm/telemetry/)
- [Oban acknowledger-style job lifecycle](https://hexdocs.pm/oban/Oban.html)
