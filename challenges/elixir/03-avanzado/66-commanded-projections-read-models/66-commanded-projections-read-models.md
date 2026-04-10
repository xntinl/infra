# Ejercicio 66: Commanded — Projections y Read Models

## Tema
En CQRS el lado de escritura (aggregates/events) y el de lectura (read models) son independientes. Las **proyecciones** consumen el stream de eventos y materializan vistas optimizadas para queries. Commanded provee `Commanded.Projections.Ecto` para sincronizar eventos con una base de datos Ecto.

## Conceptos clave

| Concepto | Rol |
|---|---|
| `Commanded.Projections.Ecto` | Proyecta eventos a tablas Ecto de forma transaccional |
| `project/3` | Callback que recibe `(event, metadata, multi)` y devuelve un `Ecto.Multi` |
| `after_update/3` | Hook post-proyección para notificaciones o side effects |
| `Commanded.Event.Handler` | Handler genérico para side effects (sin persistencia Ecto) |
| Snapshots | Persiste el estado del aggregate cada N eventos para acelerar replay |
| Replay | Reproyecta todos los eventos desde cero para reconstruir/corregir read models |

---

## Ejercicio 1 — Dashboard Read Model: AccountSummary

### Contexto

El frontend necesita mostrar un dashboard con el balance actual y el número de transacciones de cada cuenta. En lugar de consultar el event store (lento), proyectamos los eventos a una tabla `account_summaries` en Postgres que siempre tiene el estado más reciente.

### Schema Ecto

```elixir
# lib/bank/projections/account_summary.ex
defmodule Bank.Projections.AccountSummary do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:account_id, :string, autogenerate: false}
  schema "account_summaries" do
    field :owner,             :string
    field :balance,           :integer, default: 0
    field :transaction_count, :integer, default: 0
    field :status,            :string,  default: "open"
    field :last_transaction_at, :utc_datetime

    timestamps()
  end

  def changeset(summary, attrs) do
    summary
    |> cast(attrs, [:owner, :balance, :transaction_count, :status, :last_transaction_at])
  end
end
```

### Migration

```elixir
# priv/repo/migrations/20240101000000_create_account_summaries.exs
defmodule Bank.Repo.Migrations.CreateAccountSummaries do
  use Ecto.Migration

  def change do
    create table(:account_summaries, primary_key: false) do
      add :account_id,        :string,  primary_key: true
      add :owner,             :string,  null: false
      add :balance,           :integer, default: 0
      add :transaction_count, :integer, default: 0
      add :status,            :string,  default: "open"
      add :last_transaction_at, :utc_datetime

      timestamps()
    end
  end
end
```

### Projector

```elixir
# lib/bank/projectors/account_projector.ex
defmodule Bank.Projectors.AccountProjector do
  @moduledoc """
  Proyecta eventos de cuenta bancaria a la tabla account_summaries.

  Commanded.Projections.Ecto garantiza exactamente-una-vez semántica
  usando una tabla interna de "projection versions" para rastrear
  qué eventos ya fueron procesados, incluso tras reinicios.
  """

  use Commanded.Projections.Ecto,
    application: Bank.Application,
    repo: Bank.Repo,
    name: "AccountProjector",
    consistency: :strong  # el dispatch espera a que la proyección se complete

  alias Bank.Projections.AccountSummary
  alias Bank.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn}

  # project/3 recibe el evento, metadata de Commanded, y un Ecto.Multi vacío
  # Devuelve el Multi con las operaciones a ejecutar de forma atómica

  project(%AccountOpened{} = event, _metadata, multi) do
    summary = %AccountSummary{
      account_id: event.account_id,
      owner:      event.owner,
      balance:    event.initial_balance,
      status:     "open"
    }

    Ecto.Multi.insert(multi, :account_summary, summary)
  end

  project(%MoneyDeposited{} = event, metadata, multi) do
    Ecto.Multi.update_all(
      multi,
      :account_summary,
      from(s in AccountSummary, where: s.account_id == ^event.account_id),
      set: [
        balance:             event.balance_after,
        transaction_count:   fragment("transaction_count + 1"),
        last_transaction_at: metadata.created_at
      ]
    )
  end

  project(%MoneyWithdrawn{} = event, metadata, multi) do
    Ecto.Multi.update_all(
      multi,
      :account_summary,
      from(s in AccountSummary, where: s.account_id == ^event.account_id),
      set: [
        balance:             event.balance_after,
        transaction_count:   fragment("transaction_count + 1"),
        last_transaction_at: metadata.created_at
      ]
    )
  end

  # after_update/3 se llama DESPUÉS de que el Multi fue commitado exitosamente
  def after_update(event, _metadata, changes) do
    # Notifica via PubSub para que LiveView actualice la UI en tiempo real
    Phoenix.PubSub.broadcast(
      Bank.PubSub,
      "account:#{account_id_from(event)}",
      {:account_updated, changes[:account_summary]}
    )

    :ok
  end

  defp account_id_from(%{account_id: id}), do: id
end
```

### Query del read model

```elixir
# lib/bank/queries/account_queries.ex
defmodule Bank.Queries.AccountQueries do
  import Ecto.Query

  alias Bank.Projections.AccountSummary
  alias Bank.Repo

  def get_summary(account_id) do
    Repo.get(AccountSummary, account_id)
  end

  def list_top_balances(limit \\ 10) do
    AccountSummary
    |> where([s], s.status == "open")
    |> order_by([s], desc: s.balance)
    |> limit(^limit)
    |> Repo.all()
  end

  def total_assets do
    AccountSummary
    |> where([s], s.status == "open")
    |> select([s], sum(s.balance))
    |> Repo.one()
  end
end
```

### Test del projector

```elixir
defmodule Bank.Projectors.AccountProjectorTest do
  use Bank.DataCase, async: false  # async: false porque escribe en DB

  alias Bank.Projectors.AccountProjector
  alias Bank.Projections.AccountSummary
  alias Bank.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn}

  # Commanded.Projections.Ecto expone project/2 en tests
  # pero la forma canónica es usar el CommandedCase que dispatcha comandos reales

  describe "AccountOpened" do
    test "crea un registro en account_summaries" do
      event = %AccountOpened{
        account_id: "acc-test-1",
        owner: "Alice",
        initial_balance: 1000,
        opened_at: ~U[2024-01-01 12:00:00Z]
      }

      # En tests se puede aplicar la proyección directamente
      multi = Ecto.Multi.new()
      result_multi = AccountProjector.__project__(event, %{created_at: DateTime.utc_now()}, multi)
      {:ok, changes} = Bank.Repo.transaction(result_multi)

      summary = Bank.Repo.get(AccountSummary, "acc-test-1")
      assert summary.balance == 1000
      assert summary.owner == "Alice"
      assert summary.transaction_count == 0
    end
  end

  describe "MoneyWithdrawn" do
    setup do
      Bank.Repo.insert!(%AccountSummary{
        account_id: "acc-test-2",
        owner: "Bob",
        balance: 1000,
        transaction_count: 2
      })
      :ok
    end

    test "decrementa el balance y aumenta transaction_count" do
      event = %MoneyWithdrawn{
        account_id: "acc-test-2",
        amount: 300,
        balance_after: 700
      }

      multi = Ecto.Multi.new()
      result_multi = AccountProjector.__project__(event, %{created_at: DateTime.utc_now()}, multi)
      {:ok, _} = Bank.Repo.transaction(result_multi)

      summary = Bank.Repo.get(AccountSummary, "acc-test-2")
      assert summary.balance == 700
      assert summary.transaction_count == 3
    end
  end
end
```

---

## Ejercicio 2 — Email Notification Event Handler

### Contexto

Cuando el balance de una cuenta cae por debajo de 100€ tras un retiro, hay que enviar un email de alerta al propietario. Este es un **side effect** puro que no necesita proyectar a una tabla; se implementa como un `Commanded.Event.Handler`.

### Handler

```elixir
# lib/bank/handlers/low_balance_notifier.ex
defmodule Bank.Handlers.LowBalanceNotifier do
  @moduledoc """
  Envía email cuando el balance cae bajo el umbral.

  Usa Commanded.Event.Handler (no Projections.Ecto) porque no persiste
  datos en DB — solo dispara un side effect externo.

  Idempotencia: si el handler falla y se reintenta, el email puede
  enviarse dos veces. Para evitarlo se puede idempotency-key con Redis
  o una tabla DB de "notifications_sent".
  """

  use Commanded.Event.Handler,
    application: Bank.Application,
    name: "LowBalanceNotifier"

  alias Bank.Events.MoneyWithdrawn
  alias Bank.Emails.LowBalanceEmail
  alias Bank.Mailer

  @threshold 100

  def handle(%MoneyWithdrawn{balance_after: balance} = event, _metadata)
      when balance < @threshold do
    # Buscamos el owner en el read model (no en el aggregate)
    case Bank.Queries.AccountQueries.get_summary(event.account_id) do
      nil ->
        # El projector aún no procesó AccountOpened — eventual consistency
        # Podemos reintentar o ignorar
        :ok

      %{owner: owner} ->
        owner
        |> LowBalanceEmail.build(balance)
        |> Mailer.deliver()

        :ok
    end
  end

  # Si el balance es suficiente, no hacemos nada
  def handle(%MoneyWithdrawn{}, _metadata), do: :ok
end
```

### Email con Swoosh

```elixir
# lib/bank/emails/low_balance_email.ex
defmodule Bank.Emails.LowBalanceEmail do
  import Swoosh.Email

  @sender {"Bank Alerts", "alerts@bank.example"}

  def build(owner_email, balance) do
    new()
    |> to(owner_email)
    |> from(@sender)
    |> subject("Alerta: saldo bajo en tu cuenta")
    |> text_body("""
    Hola,

    Tu saldo actual es #{balance}€, por debajo del umbral de alerta.

    Considera hacer un depósito para evitar cargos por saldo insuficiente.

    — Equipo Bank
    """)
  end
end
```

### Test del handler con mock de mailer

```elixir
defmodule Bank.Handlers.LowBalanceNotifierTest do
  use ExUnit.Case, async: true
  import Swoosh.TestAssertions  # verifica emails en test

  alias Bank.Handlers.LowBalanceNotifier
  alias Bank.Events.MoneyWithdrawn

  setup do
    # El test adapter de Swoosh captura los emails sin enviarlos
    Application.put_env(:bank, Bank.Mailer, adapter: Swoosh.Adapters.Test)
    :ok
  end

  test "envía email cuando balance < 100" do
    # Precondición: summary existe en DB
    Bank.Repo.insert!(%Bank.Projections.AccountSummary{
      account_id: "acc-alert",
      owner: "alice@example.com",
      balance: 50,
      transaction_count: 1
    })

    event = %MoneyWithdrawn{account_id: "acc-alert", amount: 950, balance_after: 50}

    :ok = LowBalanceNotifier.handle(event, %{})

    assert_email_sent(to: "alice@example.com", subject: "Alerta: saldo bajo en tu cuenta")
  end

  test "no envía email cuando balance >= 100" do
    event = %MoneyWithdrawn{account_id: "acc-safe", amount: 50, balance_after: 200}

    :ok = LowBalanceNotifier.handle(event, %{})

    assert_no_email_sent()
  end
end
```

---

## Ejercicio 3 — Snapshot Strategy

### Contexto

Un aggregate con 10.000 eventos tarda demasiado en reconstruirse en memoria. La solución es tomar un **snapshot** del estado del aggregate cada N eventos. En lugar de replay desde el evento 1, Commanded carga el último snapshot y solo reproduce los eventos posteriores.

### Habilitar snapshots en el aggregate

```elixir
# lib/bank/aggregates/bank_account.ex (fragmento adicional)
defmodule Bank.Aggregates.BankAccount do
  # ... código anterior ...

  # Indica a Commanded que tome snapshot cada 100 eventos
  @snapshot_every 100

  def snapshot_after(_version, events_since_last_snapshot)
      when events_since_last_snapshot >= @snapshot_every,
      do: true

  def snapshot_after(_version, _events), do: false
end
```

### Registro del snapshot threshold en la configuración

```elixir
# config/config.exs
config :bank, Bank.Application,
  event_store: [
    adapter: Commanded.EventStore.Adapters.InMemory
  ],
  snapshotting: %{
    Bank.Aggregates.BankAccount => %{
      snapshot_every: 100,
      snapshot_version: 1  # versión del snapshot para migraciones
    }
  }
```

### Snapshot serialization

Commanded serializa el estado del aggregate vía Jason (JSON) por defecto. Si el struct tiene campos complejos, implementa `Commanded.Serialization.JsonDecoder`:

```elixir
defimpl Commanded.Serialization.JsonDecoder, for: Bank.Aggregates.BankAccount do
  def decode(%Bank.Aggregates.BankAccount{} = state) do
    # Convierte strings a atoms donde sea necesario
    %{state | status: String.to_existing_atom(state.status)}
  end
end
```

### Test de rendimiento: replay vs snapshot

```elixir
defmodule Bank.Aggregates.SnapshotBenchmark do
  @moduledoc """
  Benchmark informal para comparar tiempos de reconstrucción.
  Ejecutar con: mix run lib/bank/aggregates/snapshot_benchmark.ex
  """

  alias Bank.Commands.{OpenAccount, DepositMoney}

  def run do
    account_id = "bench-account-#{System.unique_integer([:positive])}"

    # Abre la cuenta
    :ok = Bank.Application.dispatch(%OpenAccount{
      account_id: account_id,
      initial_balance: 0,
      owner: "Benchmark"
    })

    # Genera 500 depósitos (5 snapshots si threshold = 100)
    Enum.each(1..500, fn _i ->
      Bank.Application.dispatch(%DepositMoney{account_id: account_id, amount: 10})
    end)

    # Mide el tiempo de reconstrucción (Commanded hace esto internamente
    # al recibir el primer comando tras un reinicio)
    {time, _} = :timer.tc(fn ->
      Bank.Application.dispatch(%DepositMoney{account_id: account_id, amount: 1})
    end)

    IO.puts("Reconstrucción con snapshots: #{time / 1_000}ms")
  end
end
```

---

## Ejercicio 4 — Replay desde el inicio

### Cuándo se necesita replay

- Corregiste un bug en un projector y necesitas recalcular el read model.
- Añadiste una nueva proyección y necesitas poblarla desde el histórico.
- Los datos del read model quedaron corruptos.

### Cómo hacer replay en Commanded

```elixir
defmodule Bank.Maintenance.ReplayProjections do
  @moduledoc """
  Resetea y reproyecta el AccountProjector desde el evento 0.

  PRECAUCIÓN: en producción esto puede tardar minutos/horas.
  Ejecutar en mantenimiento o con el projector en modo "offline".
  """

  alias Bank.Projectors.AccountProjector

  def reset_and_replay do
    # 1. Borra el estado del projector (reinicia desde posición 0)
    :ok = Commanded.Projections.Ecto.reset(AccountProjector)

    IO.puts("Projector reseteado. El replay comenzará automáticamente al reiniciar el handler.")
    IO.puts("El projector procesará todos los eventos desde el inicio del stream.")
  end
end
```

Commanded reinicia automáticamente la subscripción desde el evento 1 cuando detecta que la versión del projector es 0 (tras el reset).

---

## Preguntas de reflexión

1. ¿Por qué `project/3` recibe un `Ecto.Multi` en lugar de ejecutar operaciones Ecto directamente?
2. ¿Qué garantía de consistencia ofrece `consistency: :strong` y cuál es su coste?
3. Si el handler de email falla en el medio, ¿se puede garantizar exactamente-una-vez? ¿Cómo?
4. ¿Cuál es la diferencia entre un `Event.Handler` y un `Projections.Ecto` projector?
5. ¿Qué pasa si cambias la estructura de un snapshot (e.g., añades un campo al aggregate)? ¿Cómo manejas la migración de snapshots?
6. ¿Por qué no se deben hacer queries a la DB dentro del `apply/2` del aggregate?

## Dependencias (`mix.exs`)

```elixir
defp deps do
  [
    {:commanded, "~> 1.4"},
    {:commanded_ecto_projections, "~> 1.4"},
    {:commanded_eventstore_adapter, "~> 1.4"},
    {:eventstore, "~> 1.4"},
    {:ecto_sql, "~> 3.10"},
    {:postgrex, "~> 0.17"},
    {:swoosh, "~> 1.14"},        # emails
    {:phoenix_pubsub, "~> 2.1"}  # notificaciones real-time
  ]
end
```

## Referencias

- [Commanded.Projections.Ecto](https://hexdocs.pm/commanded_ecto_projections)
- [Commanded Event Handlers](https://hexdocs.pm/commanded/event-handlers.html)
- [Snapshots](https://hexdocs.pm/commanded/snapshotting.html)
- [Replay Events](https://hexdocs.pm/commanded/read-model-projections.html#resetting-a-projection)
