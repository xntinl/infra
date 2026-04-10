# 18 — Mnesia Basics

## Prerequisites

- ETS avanzado (ejercicio 16) y DETS (ejercicio 17)
- GenServer supervisado con estado complejo
- Conceptos básicos de transacciones ACID
- Erlang/Elixir node naming y clustering básico

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Crear un schema Mnesia y arrancar el sistema correctamente
2. Definir tablas con `create_table/2` y configurar su tipo de almacenamiento
3. Ejecutar transacciones ACID con `mnesia.transaction/1`
4. Diferenciar operaciones transaccionales de dirty ops y saber cuándo usar cada una
5. Escribir queries con QLC (Query List Comprehensions) sobre tablas Mnesia
6. Configurar replicación automática en un cluster de nodos Erlang

---

## Concepts

### 1. Qué es Mnesia y cuándo usarla

Mnesia es la base de datos distribuida embebida de Erlang, incluida en OTP. A diferencia de ETS/DETS, ofrece:
- Transacciones ACID distribuidas
- Replicación automática entre nodos
- Queries complejas con QLC
- Soporte para tablas en RAM, disco, o ambas simultáneamente

```
¿Necesito Mnesia?
├── Datos en memoria, sin persistencia → ETS
├── Datos simples, un nodo, persisten → DETS
├── Transacciones, queries complejas, o cluster → Mnesia
└── Datos relacionales, SQL nativo → PostgreSQL/Ecto
```

Mnesia brilla en sistemas distribuidos donde necesitas estado replicado con consistencia fuerte y la base de datos no excede decenas de GB.

### 2. Setup: schema, start, create_table

El bootstrapping de Mnesia tiene un orden estricto:

```elixir
# 1. Crear schema (una sola vez, persiste en disco)
# El schema define en qué nodos existe Mnesia
:mnesia.create_schema([node()])
# => :ok si es la primera vez
# => {:error, {node, {:already_exists, node}}} si ya existe

# 2. Arrancar Mnesia
:mnesia.start()
# => :ok

# 3. Definir tablas (idempotente si usas :ignore en result)
defmodule UserSession do
  defstruct [:id, :user_id, :token, :expires_at, :metadata]
end

{:atomic, :ok} = :mnesia.create_table(UserSession, [
  attributes: [:id, :user_id, :token, :expires_at, :metadata],
  # ram_copies: tablas en memoria, fast, no persiste
  ram_copies: [node()],
  # disc_copies: tablas en RAM + copia en disco, persiste
  # disc_copies: [node()],
  # disc_only_copies: solo en disco, lento pero bajo uso de RAM
  # disc_only_copies: [node()],
  type: :set,  # :set | :bag | :ordered_set
  index: [:user_id, :token]  # índices secundarios para búsquedas rápidas
])
```

**Estrategias de almacenamiento:**

| Tipo | RAM | Disco | Velocidad | Durabilidad |
|------|-----|-------|-----------|------------|
| `ram_copies` | Si | No | Maxima | No |
| `disc_copies` | Si | Si | Alta | Si |
| `disc_only_copies` | No | Si | Baja | Si |

### 3. Operaciones transaccionales

Las transacciones en Mnesia son funciones anónimas que se ejecutan atómicamente. Pueden incluir múltiples operaciones en múltiples tablas:

```elixir
# Lectura
:mnesia.transaction(fn ->
  :mnesia.read(UserSession, session_id)
end)
# => {:atomic, [{UserSession, id, user_id, token, expires_at, metadata}]}
# => {:atomic, []}  si no existe
# => {:aborted, reason}  si hay error

# Escritura
:mnesia.transaction(fn ->
  session = %UserSession{
    id: UUID.uuid4(),
    user_id: 42,
    token: generate_token(),
    expires_at: DateTime.utc_now() |> DateTime.add(3600) |> DateTime.to_unix(),
    metadata: %{}
  }
  :mnesia.write(session)
end)

# Delete
:mnesia.transaction(fn ->
  :mnesia.delete({UserSession, session_id})
end)

# Lectura con lock — útil para read-modify-write atómico
:mnesia.transaction(fn ->
  case :mnesia.read({UserSession, id}, :write) do  # lock de escritura
    [session] ->
      updated = %{session | metadata: Map.put(session.metadata, :last_seen, now())}
      :mnesia.write(updated)
    [] ->
      :mnesia.abort(:not_found)  # aborta la transacción
  end
end)
```

### 4. Dirty operations — cuándo y por qué

Las dirty ops saltean el sistema de transacciones y locks de Mnesia. Son 10-100x más rápidas pero sin atomicidad ni aislamiento:

```elixir
# Lecturas dirty — muy comunes para caches de solo lectura
:mnesia.dirty_read(UserSession, session_id)
:mnesia.dirty_index_read(UserSession, user_id, :user_id)

# Escrituras dirty — usar solo cuando la atomicidad no importa
:mnesia.dirty_write(%UserSession{...})
:mnesia.dirty_delete({UserSession, session_id})

# Match dirty — sin transacción, sin locks
:mnesia.dirty_match_object(%UserSession{user_id: 42, _ => :_})
```

**Regla de uso:**
- Lecturas de datos raramente cambiados (sesiones activas, configuración): dirty ok
- Escrituras donde la pérdida de una entrada es aceptable: dirty ok
- Cualquier operación que combine leer + escribir atómicamente: usa transacción
- Contadores, balances, inventarios: siempre transacción

### 5. QLC — Query List Comprehensions

QLC permite queries tipo SQL sobre tablas ETS y Mnesia:

```elixir
# Necesita :qlc (Erlang) — disponible en Elixir via require Record
:mnesia.transaction(fn ->
  :qlc.e(:qlc.q([
    session ||
    session <- :mnesia.table(UserSession),
    session.user_id == 42,
    session.expires_at > :erlang.system_time(:second)
  ]))
end)
```

En Elixir, QLC es verboso. La alternativa más idiomática es `select/2` con match specs generadas por `:ets.fun2ms`:

```elixir
def active_sessions_for_user(user_id) do
  now = :erlang.system_time(:second)

  {:atomic, result} = :mnesia.transaction(fn ->
    :mnesia.index_read(UserSession, user_id, :user_id)
    |> Enum.filter(&(&1.expires_at > now))
  end)

  result
end
```

### 6. Replicación en cluster

La magia de Mnesia: añadir un nodo al cluster replica las tablas automáticamente:

```elixir
# En el nodo nuevo (node2@host):
:mnesia.start()

# Desde cualquier nodo existente, añadir el nuevo al schema:
:mnesia.change_config(:extra_db_nodes, [:"node2@host"])

# Replicar tablas existentes al nuevo nodo:
:mnesia.add_table_copy(UserSession, :"node2@host", :disc_copies)

# Verificar estado de replicación:
:mnesia.table_info(UserSession, :where_to_read)
# => :"node1@host"  (nodo actual que sirve lecturas)

:mnesia.table_info(UserSession, :all)
# Muestra todos los nodos con copia de la tabla
```

Desde ese momento, escrituras en cualquier nodo se replican automáticamente a todos los nodos con copia de la tabla.

---

## Exercises

### Exercise 1 — User session store en Mnesia

**Problem**

Implementa un `SessionStore` completo para gestión de sesiones de usuario usando Mnesia con `disc_copies`. El store debe:
1. Crear el schema e iniciar Mnesia en Application.start
2. Crear/renovar/invalidar sesiones de forma transaccional
3. Buscar sesiones activas por `user_id` usando el índice secundario
4. Limpiar sesiones expiradas automáticamente con un GenServer de cleanup

**Hints**

- Define el record con `defstruct` para que Mnesia sepa el shape del tuple
- Mnesia guarda los registros como tuplas con el módulo como primer elemento: `{UserSession, id, ...}`
- Para limpiar expiradas sin full scan, el índice en `expires_at` permite select eficiente
- El cleanup puede usar `dirty_select` con match spec para evitar overhead transaccional

**One possible solution**

```elixir
defmodule SessionStore do
  @moduledoc """
  Session store backed by Mnesia with disc_copies.
  Schema and table creation happen in start/0 — call once at app startup.
  """

  defstruct [:id, :user_id, :token, :expires_at, :data]

  @table __MODULE__
  @ttl_seconds 3_600  # 1 hora

  def setup do
    nodes = [node()]

    case :mnesia.create_schema(nodes) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
    end

    :ok = :mnesia.start()

    case :mnesia.create_table(@table, [
      attributes: [:id, :user_id, :token, :expires_at, :data],
      disc_copies: nodes,
      type: :set,
      index: [:user_id, :token]
    ]) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, @table}} -> :ok
    end
  end

  def create(user_id, data \\ %{}) do
    session = %__MODULE__{
      id: :crypto.strong_rand_bytes(16) |> Base.url_encode64(padding: false),
      user_id: user_id,
      token: :crypto.strong_rand_bytes(32) |> Base.url_encode64(padding: false),
      expires_at: :erlang.system_time(:second) + @ttl_seconds,
      data: data
    }

    {:atomic, :ok} = :mnesia.transaction(fn ->
      :mnesia.write(session)
    end)

    {:ok, session}
  end

  def get_by_token(token) do
    case :mnesia.dirty_index_read(@table, token, :token) do
      [session] ->
        if session.expires_at > :erlang.system_time(:second) do
          {:ok, session}
        else
          {:error, :expired}
        end
      [] ->
        {:error, :not_found}
    end
  end

  def get_active_for_user(user_id) do
    now = :erlang.system_time(:second)
    sessions = :mnesia.dirty_index_read(@table, user_id, :user_id)
    Enum.filter(sessions, &(&1.expires_at > now))
  end

  def renew(session_id) do
    {:atomic, result} = :mnesia.transaction(fn ->
      case :mnesia.read({@table, session_id}, :write) do
        [session] ->
          updated = %{session | expires_at: :erlang.system_time(:second) + @ttl_seconds}
          :mnesia.write(updated)
          {:ok, updated}
        [] ->
          :mnesia.abort(:not_found)
      end
    end)

    result
  end

  def invalidate(session_id) do
    {:atomic, :ok} = :mnesia.transaction(fn ->
      :mnesia.delete({@table, session_id})
    end)
    :ok
  end

  def invalidate_all_for_user(user_id) do
    {:atomic, count} = :mnesia.transaction(fn ->
      sessions = :mnesia.index_read(@table, user_id, :user_id)
      Enum.each(sessions, fn s -> :mnesia.delete({@table, s.id}) end)
      length(sessions)
    end)

    {:ok, count}
  end

  def cleanup_expired do
    now = :erlang.system_time(:second)
    # match spec: todos los registros donde expires_at < now
    ms = [
      {
        {@table, :"$1", :"$2", :"$3", :"$4", :"$5"},
        [{:<, :"$4", now}],
        [:"$1"]  # retorna solo el id
      }
    ]

    expired_ids = :mnesia.dirty_select(@table, ms)
    Enum.each(expired_ids, fn id ->
      :mnesia.dirty_delete({@table, id})
    end)

    {:cleaned, length(expired_ids)}
  end
end

# Demo
SessionStore.setup()

{:ok, session1} = SessionStore.create(1, %{device: "mobile"})
{:ok, session2} = SessionStore.create(1, %{device: "desktop"})
{:ok, session3} = SessionStore.create(2, %{device: "tablet"})

IO.inspect(SessionStore.get_by_token(session1.token), label: "get by token")
IO.inspect(SessionStore.get_active_for_user(1), label: "active for user 1")

{:ok, renewed} = SessionStore.renew(session1.id)
IO.inspect(renewed.expires_at > session1.expires_at, label: "renewed TTL increased")

{:ok, 2} = SessionStore.invalidate_all_for_user(1)
IO.inspect(SessionStore.get_active_for_user(1), label: "after invalidate all (user 1)")
# Expected: []
```

---

### Exercise 2 — Transacciones multi-tabla

**Problem**

Implementa un sistema de transferencia de créditos entre cuentas. Esto es el caso de uso canónico de transacciones ACID: debitar una cuenta y acreditar otra debe ser atómico — o ambas operaciones ocurren, o ninguna.

Define:
- Tabla `Account`: `{id, user_id, balance, currency}`
- Tabla `Transaction`: `{id, from_account, to_account, amount, timestamp, status}`

Implementa `transfer/3` que:
1. Verifica fondos suficientes con lock de escritura (read-for-update)
2. Actualiza ambos balances atómicamente
3. Registra la transacción
4. Si algún paso falla, aborta todo con `:mnesia.abort/1`

**Hints**

- `mnesia.read({Table, key}, :write)` adquiere un write lock — usa este, no el read lock, en read-modify-write
- `:mnesia.abort(reason)` dentro de una transacción hace rollback completo
- El `id` de Transaction puede ser `:erlang.unique_integer([:monotonic, :positive])`
- Prueba con transferencias concurrentes al mismo par de cuentas

**One possible solution**

```elixir
defmodule AccountSystem do
  defstruct_account = [:id, :user_id, :balance, :currency]
  defstruct_transaction = [:id, :from_account, :to_account, :amount, :timestamp, :status]

  defmodule Account do
    defstruct [:id, :user_id, :balance, :currency]
  end

  defmodule Txn do
    defstruct [:id, :from_account, :to_account, :amount, :timestamp, :status]
  end

  def setup do
    nodes = [node()]
    :mnesia.create_schema(nodes)
    :mnesia.start()

    :mnesia.create_table(Account, [
      attributes: [:id, :user_id, :balance, :currency],
      ram_copies: nodes,
      type: :set
    ])

    :mnesia.create_table(Txn, [
      attributes: [:id, :from_account, :to_account, :amount, :timestamp, :status],
      ram_copies: nodes,
      type: :set,
      index: [:from_account, :to_account]
    ])

    {:atomic, :ok}
  end

  def create_account(user_id, initial_balance, currency \\ "USD") do
    account = %Account{
      id: :erlang.unique_integer([:monotonic, :positive]),
      user_id: user_id,
      balance: initial_balance,
      currency: currency
    }

    {:atomic, :ok} = :mnesia.transaction(fn -> :mnesia.write(account) end)
    {:ok, account}
  end

  def get_balance(account_id) do
    case :mnesia.dirty_read(Account, account_id) do
      [account] -> {:ok, account.balance, account.currency}
      [] -> {:error, :account_not_found}
    end
  end

  def transfer(from_id, to_id, amount) when amount > 0 do
    {:atomic, result} = :mnesia.transaction(fn ->
      # Adquirir write locks en orden canónico para evitar deadlocks
      {from_key, to_key} = if from_id < to_id,
        do: {{Account, from_id}, {Account, to_id}},
        else: {{Account, to_id}, {Account, from_id}}

      # Leer con write lock
      from_account = case :mnesia.read(from_key, :write) do
        [acc] -> acc
        [] -> :mnesia.abort({:not_found, :from_account, from_id})
      end

      to_account = case :mnesia.read(to_key, :write) do
        [acc] -> acc
        [] -> :mnesia.abort({:not_found, :to_account, to_id})
      end

      # Verificar moneda compatible
      if from_account.currency != to_account.currency do
        :mnesia.abort({:currency_mismatch, from_account.currency, to_account.currency})
      end

      # Verificar fondos
      if from_account.balance < amount do
        :mnesia.abort({:insufficient_funds, from_account.balance, amount})
      end

      # Actualizar balances
      :mnesia.write(%{from_account | balance: from_account.balance - amount})
      :mnesia.write(%{to_account | balance: to_account.balance + amount})

      # Registrar transacción
      txn = %Txn{
        id: :erlang.unique_integer([:monotonic, :positive]),
        from_account: from_id,
        to_account: to_id,
        amount: amount,
        timestamp: :erlang.system_time(:second),
        status: :completed
      }
      :mnesia.write(txn)

      {:ok, txn}
    end)

    result
  end

  def transfer_history(account_id) do
    from = :mnesia.dirty_index_read(Txn, account_id, :from_account)
    to = :mnesia.dirty_index_read(Txn, account_id, :to_account)
    (from ++ to) |> Enum.sort_by(& &1.timestamp, :desc)
  end
end

# Demo
AccountSystem.setup()

{:ok, alice} = AccountSystem.create_account(1, 1000, "USD")
{:ok, bob} = AccountSystem.create_account(2, 500, "USD")

IO.inspect(AccountSystem.get_balance(alice.id), label: "Alice initial")
IO.inspect(AccountSystem.get_balance(bob.id), label: "Bob initial")

{:ok, txn} = AccountSystem.transfer(alice.id, bob.id, 200)
IO.puts("Transfer #{txn.id}: $200 Alice -> Bob")

IO.inspect(AccountSystem.get_balance(alice.id), label: "Alice after transfer")
IO.inspect(AccountSystem.get_balance(bob.id), label: "Bob after transfer")
# Alice: 800, Bob: 700

# Intentar transferencia que falla
result = AccountSystem.transfer(bob.id, alice.id, 10_000)
IO.inspect(result, label: "Overdraft attempt")
# Expected: {:error, {:insufficient_funds, 700, 10000}}

IO.inspect(AccountSystem.get_balance(alice.id), label: "Alice unchanged after failed txn")
IO.inspect(AccountSystem.get_balance(bob.id), label: "Bob unchanged after failed txn")
```

---

### Exercise 3 — Mnesia en cluster: replicación automática

**Problem**

Configura dos nodos Erlang en la misma máquina, inicializa Mnesia en el primer nodo, y añade el segundo nodo al cluster para demostrar la replicación automática.

Este ejercicio requiere lanzar dos nodos BEAM desde la línea de comandos. Implementa los scripts de setup y verifica que:
1. Datos escritos en `node1` son visibles en `node2` sin sincronización manual
2. Si `node1` se detiene, `node2` puede seguir sirviendo lecturas desde su copia local
3. Cuando `node1` vuelve, sincroniza automáticamente

**Hints**

- Lanza los nodos con: `iex --sname node1 --cookie secret -S mix`
- `Node.connect(:"node2@hostname")` para conectar los nodos
- `:mnesia.change_config(:extra_db_nodes, [:"node2@hostname"])` para añadir al cluster Mnesia
- `:mnesia.add_table_copy(Table, node, :disc_copies)` para replicar una tabla

**One possible solution**

```elixir
# Módulo de bootstrap — ejecutar en ambos nodos
defmodule ClusterSetup do
  @table :cluster_kv

  def init_primary(peer_nodes \\ []) do
    all_nodes = [node() | peer_nodes]

    # Schema en todos los nodos
    :mnesia.create_schema(all_nodes)
    :mnesia.start()

    # Conectar nodos al cluster Mnesia
    Enum.each(peer_nodes, fn peer ->
      :mnesia.change_config(:extra_db_nodes, [peer])
    end)

    # Crear tabla replicada en todos los nodos
    case :mnesia.create_table(@table, [
      attributes: [:key, :value, :updated_at],
      disc_copies: all_nodes,
      type: :set
    ]) do
      {:atomic, :ok} ->
        IO.puts("Table created and replicated to: #{inspect(all_nodes)}")
      {:aborted, {:already_exists, @table}} ->
        # Si ya existe, añadir copia al nuevo nodo
        Enum.each(peer_nodes, fn peer ->
          :mnesia.add_table_copy(@table, peer, :disc_copies)
        end)
    end

    :ok
  end

  def join_cluster(primary_node) do
    # El nodo secundario se une al cluster existente
    Node.connect(primary_node)
    :mnesia.start()
    :mnesia.change_config(:extra_db_nodes, [primary_node])
    :mnesia.wait_for_tables([@table], 5_000)
    IO.puts("Joined cluster. Tables: #{inspect(:mnesia.system_info(:tables))}")
  end

  def put(key, value) do
    {:atomic, :ok} = :mnesia.transaction(fn ->
      :mnesia.write({@table, key, value, :erlang.system_time(:second)})
    end)
    :ok
  end

  def get(key) do
    case :mnesia.dirty_read(@table, key) do
      [{@table, ^key, value, _ts}] -> {:ok, value}
      [] -> :error
    end
  end

  def cluster_info do
    %{
      local_node: node(),
      connected_nodes: Node.list(),
      mnesia_nodes: :mnesia.system_info(:running_db_nodes),
      table_copies: :mnesia.table_info(@table, :disc_copies),
      local_size: :mnesia.table_info(@table, :size)
    }
  end
end

# === Instrucciones de ejecución ===
# Terminal 1 (nodo primario):
#   iex --sname node1 --cookie mycookie
#   iex> ClusterSetup.init_primary([:"node2@localhost"])
#   iex> ClusterSetup.put("hello", "world")

# Terminal 2 (nodo secundario):
#   iex --sname node2 --cookie mycookie
#   iex> ClusterSetup.join_cluster(:"node1@localhost")
#   iex> ClusterSetup.get("hello")
#   # Expected: {:ok, "world"} — sin haberlo escrito en este nodo

# Verificar en node2:
#   iex> ClusterSetup.cluster_info()
#   # mnesia_nodes debe mostrar ambos nodos

# Simular fallo de node1 (Ctrl+C en Terminal 1), luego en node2:
#   iex> ClusterSetup.get("hello")
#   # Sigue funcionando con la copia local

# Verificar estado de Mnesia cuando un nodo cae:
IO.inspect(:mnesia.system_info(:running_db_nodes), label: "running nodes")
IO.inspect(:mnesia.system_info(:db_nodes), label: "all configured nodes")
```

**Trade-off analysis**: Mnesia en cluster usa consistencia fuerte por defecto para las transacciones — una escritura se confirma cuando todos los nodos con copia han aceptado la transacción. Esto puede crear latencia de red en writes. Para lecturas, Mnesia puede leer de la copia local (dirty_read o `{local_content, true}` en `create_table`), lo que es instantáneo pero puede devolver datos ligeramente desactualizados si hay un write en vuelo.

---

## Common Mistakes

**No llamar `mnesia.wait_for_tables/2` antes de hacer queries**
Mnesia carga tablas de forma asíncrona al arrancar. Si un proceso intenta leer justo después de `mnesia.start()`, puede obtener `{aborted, {no_exists, TableName}}`. Siempre espera con `wait_for_tables` en la inicialización.

```elixir
# En init/1 del GenServer o Application.start:
:mnesia.wait_for_tables([UserSession, Account], 10_000)
```

**Usar dirty ops en read-modify-write**
Un race condition clásico:
```elixir
# INCORRECTO — race condition entre dos procesos concurrentes
[account] = :mnesia.dirty_read(Account, id)
:mnesia.dirty_write(%{account | balance: account.balance - amount})

# CORRECTO — transacción con write lock
:mnesia.transaction(fn ->
  [account] = :mnesia.read({Account, id}, :write)
  :mnesia.write(%{account | balance: account.balance - amount})
end)
```

**Acceder a campos del struct con índice posicional**
Mnesia guarda registros como tuplas. En Elixir, cuando defines `defstruct`, el record en Mnesia es `{ModuleName, field1, field2, ...}`. Si reorganizas los campos del struct, los datos antiguos en disco se corrompen. Los campos deben declararse en el mismo orden que en `attributes:` de `create_table`.

**Crear el schema en múltiples nodos sin coordinar**
Si dos nodos llaman `create_schema` independientemente, crean schemas incompatibles. Solo el nodo primario llama `create_schema` con la lista de todos los nodos. Los otros llaman `start` y `change_config`.

**No definir un orden canónico para adquirir locks y causar deadlocks**
Si el proceso A adquiere lock en tabla X y luego Y, mientras el proceso B adquiere Y y luego X, ambos se bloquean mutuamente. Mnesia detecta esto y aborta una de las transacciones, pero genera retries. La solución: siempre adquirir locks en el mismo orden (por ejemplo, por `account_id` ascendente).

---

## Verification

```elixir
# Test básico de transacción y rollback
AccountSystem.setup()
{:ok, a1} = AccountSystem.create_account(1, 100, "USD")
{:ok, a2} = AccountSystem.create_account(2, 100, "USD")

# Transfer que debe fallar
result = AccountSystem.transfer(a1.id, a2.id, 500)
IO.inspect(result, label: "overdraft")  # debe ser error

# Balances no deben haber cambiado
{:ok, bal1, _} = AccountSystem.get_balance(a1.id)
{:ok, bal2, _} = AccountSystem.get_balance(a2.id)
^bal1 = 100  # si esto lanza, el rollback falló
^bal2 = 100
IO.puts("Rollback verified: balances unchanged after failed transfer")
```

```elixir
# Test de wait_for_tables
:mnesia.start()
result = :mnesia.wait_for_tables([UserSession], 5_000)
IO.inspect(result, label: "tables ready")  # debe ser :ok, no timeout
```

---

## Summary

Mnesia es la base de datos distribuida más integrada con la BEAM. Sus puntos fuertes son transacciones ACID que abarcan múltiples tablas y nodos, replicación automática configurable por tabla, y su capacidad de operar sin infraestructura externa. Sus limitaciones (límite de dataset, curva de aprendizaje de clustering) hacen que sea la opción correcta para estado de aplicación distribuido, no para bases de datos de propósito general.

Los patrones clave: usar `disc_copies` para tablas que deben sobrevivir reinicios, `ram_copies` para caches temporales, `dirty_read` para lecturas frecuentes de datos estables, y transacciones con write locks para cualquier read-modify-write.

---

## What's Next

- **Ejercicio 19**: Cache patterns con ETS — TTL, LRU, cache stampede prevention
- **Horde**: registry distribuido que internamente puede usar Mnesia o CRDT
- **Commanded + EventStore**: event sourcing sobre Postgres cuando Mnesia no es suficiente

---

## Resources

- [Erlang Mnesia user guide](https://www.erlang.org/doc/apps/mnesia/mnesia_chap1.html) — guía oficial completa
- [Mnesia reference manual](https://www.erlang.org/doc/man/mnesia.html) — API reference
- [Learn You Some Erlang — Mnesia chapter](https://learnyousomeerlang.com/mnesia) — tutorial claro con ejemplos
- [Saša Jurić — "Elixir in Action" 2nd ed.](https://www.manning.com/books/elixir-in-action-second-edition) — capítulo sobre persistencia
