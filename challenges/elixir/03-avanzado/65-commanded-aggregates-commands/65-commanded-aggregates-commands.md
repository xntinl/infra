# Ejercicio 65: Commanded — Aggregates y Commands

## Tema
Event Sourcing y CQRS con el framework **Commanded** en Elixir. El aggregate root es la pieza central: recibe comandos, valida reglas de negocio, emite eventos, y reconstruye su estado aplicando esos eventos.

## Conceptos clave

| Concepto | Rol |
|---|---|
| `Commanded.Aggregate` | Módulo que define estado + `execute/2` + `apply/2` |
| `execute/2` | Valida el comando → devuelve `{:ok, event}` o `{:error, reason}` |
| `apply/2` | Aplica un evento al estado del aggregate (pura, sin efectos) |
| `Commanded.Commands.Router` | Enruta comandos al aggregate y al handler correcto |
| `Commanded.Application` | Punto de entrada: configura event store y registra routers |
| Event store | `EventStore` (Postgres) o `Commanded.EventStore.Adapters.InMemory` para tests |

---

## Ejercicio 1 — BankAccount aggregate

### Contexto

Un `BankAccount` puede abrirse, recibir depósitos y procesar retiros. Las reglas de negocio son:

- No se puede retirar más saldo del disponible.
- No se puede operar sobre una cuenta cerrada.
- Una cuenta ya abierta no puede volver a abrirse.

### Estructura de archivos

```
bank/
├── mix.exs
├── config/
│   └── config.exs
├── lib/
│   bank/
│   ├── application.ex          # Commanded.Application
│   ├── router.ex               # Commands.Router
│   ├── commands/
│   │   ├── open_account.ex
│   │   ├── deposit_money.ex
│   │   └── withdraw_money.ex
│   ├── events/
│   │   ├── account_opened.ex
│   │   ├── money_deposited.ex
│   │   └── money_withdrawn.ex
│   └── aggregates/
│       └── bank_account.ex
└── test/
    └── aggregates/
        └── bank_account_test.exs
```

### Comandos

```elixir
# lib/bank/commands/open_account.ex
defmodule Bank.Commands.OpenAccount do
  defstruct [:account_id, :initial_balance, :owner]
end

# lib/bank/commands/deposit_money.ex
defmodule Bank.Commands.DepositMoney do
  defstruct [:account_id, :amount]
end

# lib/bank/commands/withdraw_money.ex
defmodule Bank.Commands.WithdrawMoney do
  defstruct [:account_id, :amount]
end
```

### Eventos

```elixir
# lib/bank/events/account_opened.ex
defmodule Bank.Events.AccountOpened do
  defstruct [:account_id, :initial_balance, :owner, :opened_at]
end

# lib/bank/events/money_deposited.ex
defmodule Bank.Events.MoneyDeposited do
  defstruct [:account_id, :amount, :balance_after]
end

# lib/bank/events/money_withdrawn.ex
defmodule Bank.Events.MoneyWithdrawn do
  defstruct [:account_id, :amount, :balance_after]
end
```

### Aggregate

```elixir
# lib/bank/aggregates/bank_account.ex
defmodule Bank.Aggregates.BankAccount do
  @moduledoc """
  Aggregate root para cuentas bancarias.

  El estado se reconstruye replaying todos los eventos desde el event store.
  `execute/2` contiene las reglas de negocio (puede fallar).
  `apply/2` es pura: actualiza el struct con los datos del evento.
  """

  defstruct account_id: nil,
            balance: 0,
            owner: nil,
            status: :new  # :new | :open | :closed

  alias Bank.Commands.{OpenAccount, DepositMoney, WithdrawMoney}
  alias Bank.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn}

  # ── Command handlers ──────────────────────────────────────────────────────

  def execute(%__MODULE__{status: :new}, %OpenAccount{} = cmd) do
    # Emite el evento de apertura. Commanded lo persistirá en el event store.
    %AccountOpened{
      account_id: cmd.account_id,
      initial_balance: cmd.initial_balance,
      owner: cmd.owner,
      opened_at: DateTime.utc_now()
    }
  end

  def execute(%__MODULE__{status: :open}, %OpenAccount{}) do
    # Fail fast: la cuenta ya existe
    {:error, :account_already_open}
  end

  def execute(%__MODULE__{status: :open, balance: balance}, %DepositMoney{amount: amount})
      when amount > 0 do
    %MoneyDeposited{
      account_id: nil,  # Commanded lo inyecta desde el aggregate_id
      amount: amount,
      balance_after: balance + amount
    }
  end

  def execute(%__MODULE__{}, %DepositMoney{}) do
    {:error, :invalid_deposit}
  end

  def execute(%__MODULE__{status: :open, balance: balance}, %WithdrawMoney{amount: amount})
      when balance >= amount and amount > 0 do
    %MoneyWithdrawn{
      account_id: nil,
      amount: amount,
      balance_after: balance - amount
    }
  end

  def execute(%__MODULE__{status: :open}, %WithdrawMoney{}) do
    {:error, :insufficient_funds}
  end

  def execute(%__MODULE__{status: :closed}, _command) do
    {:error, :account_closed}
  end

  # ── State mutators (apply es SIEMPRE exitoso) ─────────────────────────────

  def apply(%__MODULE__{} = account, %AccountOpened{} = event) do
    %__MODULE__{account |
      account_id: event.account_id,
      balance: event.initial_balance,
      owner: event.owner,
      status: :open
    }
  end

  def apply(%__MODULE__{} = account, %MoneyDeposited{} = event) do
    %__MODULE__{account | balance: event.balance_after}
  end

  def apply(%__MODULE__{} = account, %MoneyWithdrawn{} = event) do
    %__MODULE__{account | balance: event.balance_after}
  end
end
```

### Router

```elixir
# lib/bank/router.ex
defmodule Bank.Router do
  use Commanded.Commands.Router

  alias Bank.Aggregates.BankAccount
  alias Bank.Commands.{OpenAccount, DepositMoney, WithdrawMoney}

  # identify: campo del comando que contiene el aggregate_id
  identify BankAccount, by: :account_id

  dispatch [OpenAccount, DepositMoney, WithdrawMoney],
    to: BankAccount
end
```

### Application

```elixir
# lib/bank/application.ex
defmodule Bank.Application do
  use Commanded.Application, otp_app: :bank

  router Bank.Router
end
```

### Configuración (InMemory para dev/test)

```elixir
# config/config.exs
import Config

config :bank, Bank.Application,
  event_store: [
    adapter: Commanded.EventStore.Adapters.InMemory
  ]
```

### Tests del aggregate (sin infraestructura)

Commanded provee `Commanded.AggregateCase` para testear aggregates en aislamiento puro: aplicas eventos previos, disparas un comando, y aseguras qué eventos nuevos se emiten.

```elixir
# test/aggregates/bank_account_test.exs
defmodule Bank.Aggregates.BankAccountTest do
  use ExUnit.Case, async: true

  alias Bank.Aggregates.BankAccount
  alias Bank.Commands.{OpenAccount, DepositMoney, WithdrawMoney}
  alias Bank.Events.{AccountOpened, MoneyDeposited, MoneyWithdrawn}

  # Helper: reconstruye el estado aplicando una lista de eventos
  defp build_state(events) do
    Enum.reduce(events, %BankAccount{}, &BankAccount.apply(&2, &1))
  end

  describe "OpenAccount" do
    test "abre una cuenta nueva" do
      cmd = %OpenAccount{account_id: "acc-1", initial_balance: 1000, owner: "Alice"}
      assert %AccountOpened{initial_balance: 1000} = BankAccount.execute(%BankAccount{}, cmd)
    end

    test "falla si la cuenta ya está abierta" do
      state = build_state([
        %AccountOpened{account_id: "acc-1", initial_balance: 500, owner: "Bob", opened_at: ~U[2024-01-01 00:00:00Z]}
      ])
      cmd = %OpenAccount{account_id: "acc-1", initial_balance: 500, owner: "Bob"}

      assert {:error, :account_already_open} = BankAccount.execute(state, cmd)
    end
  end

  describe "WithdrawMoney" do
    setup do
      state = build_state([
        %AccountOpened{account_id: "acc-1", initial_balance: 1000, owner: "Alice", opened_at: ~U[2024-01-01 00:00:00Z]}
      ])
      {:ok, state: state}
    end

    test "retira fondos suficientes", %{state: state} do
      cmd = %WithdrawMoney{account_id: "acc-1", amount: 300}
      assert %MoneyWithdrawn{amount: 300, balance_after: 700} = BankAccount.execute(state, cmd)
    end

    test "rechaza retiro por fondos insuficientes", %{state: state} do
      cmd = %WithdrawMoney{account_id: "acc-1", amount: 5000}
      assert {:error, :insufficient_funds} = BankAccount.execute(state, cmd)
    end
  end

  describe "apply/2" do
    test "acumula balance correctamente con múltiples eventos" do
      events = [
        %AccountOpened{account_id: "acc-1", initial_balance: 1000, owner: "Alice", opened_at: ~U[2024-01-01 00:00:00Z]},
        %MoneyDeposited{account_id: "acc-1", amount: 500, balance_after: 1500},
        %MoneyWithdrawn{account_id: "acc-1", amount: 200, balance_after: 1300}
      ]

      state = build_state(events)
      assert state.balance == 1300
      assert state.status == :open
    end
  end
end
```

---

## Ejercicio 2 — Process Manager (Saga): TransferFunds

### Contexto

Una transferencia entre cuentas requiere dos pasos coordinados. Si el débito en la cuenta origen tiene éxito pero el crédito en la cuenta destino falla, hay que compensar (reembolsar el débito). Esto es una **saga** orquestada por un `ProcessManager`.

### Flujo

```
TransferRequested
      │
      ▼
  Debit source account
      │
  ┌───┴───┐
  │ OK    │ FAIL
  ▼       ▼
Credit   TransferFailed
target
  │
  ▼
TransferCompleted
```

### Comandos y eventos adicionales

```elixir
# Comandos nuevos
defmodule Bank.Commands.DebitAccount do
  defstruct [:account_id, :transfer_id, :amount]
end

defmodule Bank.Commands.CreditAccount do
  defstruct [:account_id, :transfer_id, :amount]
end

defmodule Bank.Commands.RefundAccount do
  defstruct [:account_id, :transfer_id, :amount]
end

# Eventos nuevos
defmodule Bank.Events.TransferRequested do
  defstruct [:transfer_id, :source_account_id, :target_account_id, :amount]
end

defmodule Bank.Events.AccountDebited do
  defstruct [:account_id, :transfer_id, :amount, :balance_after]
end

defmodule Bank.Events.AccountCredited do
  defstruct [:account_id, :transfer_id, :amount, :balance_after]
end

defmodule Bank.Events.TransferCompleted do
  defstruct [:transfer_id]
end

defmodule Bank.Events.TransferFailed do
  defstruct [:transfer_id, :reason]
end
```

### Process Manager

```elixir
defmodule Bank.ProcessManagers.TransferMoney do
  @moduledoc """
  Saga que coordina la transferencia entre dos cuentas.

  Commanded correlaciona eventos con el proceso via `interested?/1`.
  El estado del saga también se persiste en el event store.
  """

  use Commanded.ProcessManagers.ProcessManager,
    application: Bank.Application,
    name: "TransferMoneyProcessManager"

  alias Bank.Commands.{CreditAccount, RefundAccount}
  alias Bank.Events.{
    TransferRequested,
    AccountDebited,
    AccountCredited,
    TransferCompleted,
    TransferFailed
  }

  defstruct [
    :transfer_id,
    :source_account_id,
    :target_account_id,
    :amount,
    status: :pending  # :pending | :debited | :completed | :failed
  ]

  # Qué eventos le interesan a este process manager
  def interested?(%TransferRequested{transfer_id: id}), do: {:start, id}
  def interested?(%AccountDebited{transfer_id: id}), do: {:continue, id}
  def interested?(%AccountCredited{transfer_id: id}), do: {:continue, id}
  def interested?(%TransferFailed{transfer_id: id}), do: {:stop, id}
  def interested?(_event), do: false

  # Maneja TransferRequested: debit ya fue emitido por el aggregate, esperamos AccountDebited
  def handle(%__MODULE__{}, %TransferRequested{} = event) do
    # El debit se despacha como comando al aggregate source
    # (asumimos que TransferRequested ya incluye el debit via el aggregate)
    # Aquí solo actualizamos estado local del PM
    []
  end

  def handle(%__MODULE__{} = pm, %AccountDebited{} = event) do
    # Débito OK → intentamos acreditar al destino
    %CreditAccount{
      account_id: pm.target_account_id,
      transfer_id: pm.transfer_id,
      amount: pm.amount
    }
  end

  def handle(%__MODULE__{}, %AccountCredited{}) do
    # Crédito OK → transferencia completada, no hay más comandos
    []
  end

  # Aplica eventos al estado del proceso manager
  def apply(%__MODULE__{} = pm, %TransferRequested{} = event) do
    %__MODULE__{pm |
      transfer_id: event.transfer_id,
      source_account_id: event.source_account_id,
      target_account_id: event.target_account_id,
      amount: event.amount,
      status: :pending
    }
  end

  def apply(%__MODULE__{} = pm, %AccountDebited{}) do
    %__MODULE__{pm | status: :debited}
  end

  def apply(%__MODULE__{} = pm, %AccountCredited{}) do
    %__MODULE__{pm | status: :completed}
  end

  def apply(%__MODULE__{} = pm, %TransferFailed{}) do
    %__MODULE__{pm | status: :failed}
  end

  # Compensación: si el crédito falla, Commanded llamará a error/3
  def error({:error, :insufficient_funds}, %CreditAccount{} = failed_cmd, _failure_context) do
    refund = %RefundAccount{
      account_id: failed_cmd.account_id,
      transfer_id: failed_cmd.transfer_id,
      amount: failed_cmd.amount
    }
    # Despachamos el reembolso y detenemos el saga
    {:stop, refund}
  end
end
```

---

## Ejercicio 3 — Optimistic Concurrency con `expected_version`

### Contexto

Cuando dos procesos intentan modificar el mismo aggregate simultáneamente, el segundo debe fallar con un error de concurrencia, no sobrescribir silenciosamente.

### Mecanismo

Commanded soporta `expected_version` en el dispatch. Si la versión actual del aggregate en el event store no coincide, el dispatch retorna `{:error, :wrong_expected_version}`.

```elixir
defmodule Bank.ConcurrencyExample do
  @moduledoc """
  Demuestra optimistic concurrency con expected_version.

  Caso de uso real: dos cajeros intentan retirar de la misma cuenta
  simultáneamente. Solo el primero debe tener éxito.
  """

  alias Bank.Commands.WithdrawMoney

  def concurrent_withdraw_demo(account_id) do
    cmd = %WithdrawMoney{account_id: account_id, amount: 500}

    # Leemos la versión actual del aggregate
    {:ok, version} = Bank.Application.aggregate_version(account_id)

    # Proceso 1: usa la versión leída
    task_1 = Task.async(fn ->
      Bank.Application.dispatch(cmd,
        consistency: :strong,
        expected_version: version
      )
    end)

    # Proceso 2: usa la misma versión (carrera)
    task_2 = Task.async(fn ->
      Bank.Application.dispatch(cmd,
        consistency: :strong,
        expected_version: version
      )
    end)

    results = Task.await_many([task_1, task_2])

    # Exactamente uno de los dos debe fallar
    successes = Enum.count(results, &(&1 == :ok))
    failures  = Enum.count(results, &match?({:error, :wrong_expected_version}, &1))

    IO.puts("Éxitos: #{successes}, Conflictos detectados: #{failures}")
  end
end
```

### Por qué funciona

El event store (Postgres) aplica un `UNIQUE` constraint sobre `(stream_id, stream_version)`. Si dos transacciones intentan insertar el mismo `stream_version` para el mismo aggregate, la segunda falla con una violación de unicidad que Commanded convierte en `{:error, :wrong_expected_version}`.

---

## Preguntas de reflexión

1. ¿Por qué `apply/2` debe ser una función pura sin efectos secundarios?
2. ¿Qué pasa si el event store tiene 10.000 eventos de un aggregate? ¿Cómo lo resuelves? (pista: snapshots, ver ejercicio 66)
3. ¿Cuál es la diferencia entre `consistency: :strong` y `consistency: :eventual`?
4. ¿Por qué el `ProcessManager` también persiste su estado en el event store?
5. En la saga, ¿qué ocurre si el proceso se cae entre el débito y el crédito?

## Dependencias (`mix.exs`)

```elixir
defp deps do
  [
    {:commanded, "~> 1.4"},
    {:commanded_eventstore_adapter, "~> 1.4"},
    {:eventstore, "~> 1.4"},
    # Para tests en memoria:
    # {:commanded, "~> 1.4"} ya incluye el adaptador InMemory
  ]
end
```

## Referencias

- [Commanded Docs](https://hexdocs.pm/commanded)
- [CQRS/ES Pattern](https://martinfowler.com/bliki/CQRS.html)
- [Saga Pattern](https://microservices.io/patterns/data/saga.html)
