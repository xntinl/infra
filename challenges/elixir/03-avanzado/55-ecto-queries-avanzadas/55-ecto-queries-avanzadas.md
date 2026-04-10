# Ejercicio 55 — Ecto Queries Avanzadas

**Nivel**: Avanzado  
**Tema**: Queries complejas, fragments, subqueries, window functions, Ecto.Multi, streaming

---

## Contexto

La mayoría de los tutoriales de Ecto muestran `Repo.all(User)`. En producción, las queries
reales requieren ranking, filtros dinámicos, transacciones multi-paso y procesamiento de
millones de filas sin saturar la memoria. Este ejercicio cubre esos patrones.

---

## Parte 1 — Window Functions con `fragment/1`

### Problema

El equipo de analytics necesita un dashboard que muestre, por categoría de producto,
el ranking de cada vendedor según sus ventas totales del mes. SQL tiene `rank() OVER (PARTITION BY ...)`
para esto, pero Ecto no expone funciones de ventana como macros propias — se usan con `fragment`.

### Schema de referencia

```elixir
# lib/store/sale.ex
defmodule Store.Sale do
  use Ecto.Schema

  schema "sales" do
    field :amount,      :decimal
    belongs_to :user,     Store.User
    belongs_to :category, Store.Category
    timestamps()
  end
end
```

### Solución: query con window function

```elixir
# lib/store/analytics.ex
defmodule Store.Analytics do
  import Ecto.Query
  alias Store.{Repo, Sale}

  def sales_ranking_by_category do
    from(s in Sale,
      join: u in assoc(s, :user),
      join: c in assoc(s, :category),
      group_by: [s.user_id, s.category_id, u.name, c.name],
      select: %{
        user_name:     u.name,
        category_name: c.name,
        total_sales:   sum(s.amount),
        rank: fragment(
          "rank() OVER (PARTITION BY ? ORDER BY sum(?) DESC)",
          s.category_id,
          s.amount
        )
      }
    )
    |> Repo.all()
  end
end
```

**Por qué funciona**: `fragment/1` inyecta SQL literal en la query compilada por Ecto.
El `?` actúa como placeholder seguro (evita inyección). La cláusula `OVER (PARTITION BY ...)`
se evalúa en PostgreSQL después del `GROUP BY`, lo que permite combinar ambas.

### Variante: percentil con `percent_rank()`

```elixir
select: %{
  user_id: s.user_id,
  total:   sum(s.amount),
  percentile: fragment(
    "percent_rank() OVER (PARTITION BY ? ORDER BY sum(?) DESC)",
    s.category_id,
    s.amount
  )
}
```

### Verificación en iex

```elixir
iex> Store.Analytics.sales_ranking_by_category()
[
  %{user_name: "Ana", category_name: "Electronics", total_sales: #Decimal<4500.00>, rank: 1},
  %{user_name: "Luis", category_name: "Electronics", total_sales: #Decimal<3200.00>, rank: 2},
  ...
]
```

---

## Parte 2 — Filtros Dinámicos con `dynamic/2`

### Problema

Una API REST recibe parámetros opcionales de filtrado: `min_price`, `max_price`, `category`,
`in_stock`. El controller no sabe cuáles vendrán. El anti-patrón es construir strings SQL
manualmente — Ecto tiene `dynamic/2` para esto.

### Schema de referencia

```elixir
# lib/store/product.ex
defmodule Store.Product do
  use Ecto.Schema

  schema "products" do
    field :name,     :string
    field :price,    :decimal
    field :category, :string
    field :stock,    :integer
    timestamps()
  end
end
```

### Solución: dynamic query builder

```elixir
# lib/store/product_filters.ex
defmodule Store.ProductFilters do
  import Ecto.Query
  alias Store.{Repo, Product}

  # Punto de entrada público — params es un mapa string-keyed
  def search(params) when is_map(params) do
    Product
    |> where(^build_filters(params))
    |> order_by([p], asc: p.price)
    |> Repo.all()
  end

  # Construye un `dynamic` compuesto a partir de los params presentes
  defp build_filters(params) do
    Enum.reduce(params, dynamic(true), fn
      {"min_price", v}, acc ->
        dynamic([p], ^acc and p.price >= ^to_decimal(v))

      {"max_price", v}, acc ->
        dynamic([p], ^acc and p.price <= ^to_decimal(v))

      {"category", v}, acc ->
        dynamic([p], ^acc and p.category == ^v)

      {"in_stock", "true"}, acc ->
        dynamic([p], ^acc and p.stock > 0)

      _unknown, acc ->
        acc
    end)
  end

  defp to_decimal(v) when is_binary(v), do: Decimal.new(v)
  defp to_decimal(v), do: v
end
```

**Por qué `dynamic/2` sobre `Enum.reduce` con `where`**: ambos funcionan, pero `dynamic/2`
compone la expresión booleana en memoria antes de generar SQL. El resultado es una sola
cláusula `WHERE` compacta — más legible en logs y más fácil de razonar sobre precedencia.

### Uso desde el controller

```elixir
# lib/store_web/controllers/product_controller.ex
def index(conn, params) do
  # params llega como %{"min_price" => "100", "category" => "books"}
  products = Store.ProductFilters.search(params)
  render(conn, :index, products: products)
end
```

### Verificación

```elixir
iex> Store.ProductFilters.search(%{"min_price" => "50", "category" => "books"})
# SELECT p0."id", p0."name", p0."price" ... 
# WHERE (TRUE AND (p0."price" >= $1) AND (p0."category" = $2))
# ORDER BY p0."price"
```

---

## Parte 3 — `Ecto.Multi` para Transacciones Atómicas

### Problema

Un sistema bancario necesita que la transferencia entre cuentas sea atómica:
1. Debitar la cuenta origen (verificando saldo suficiente)
2. Acreditar la cuenta destino
3. Insertar un registro en `audit_log`

Si cualquier paso falla, todo se revierte. `Repo.transaction/1` con lambdas anidadas
funciona pero es frágil. `Ecto.Multi` hace cada paso nombrado, componible y testeable.

### Schemas de referencia

```elixir
schema "accounts" do
  field :balance, :decimal
  field :owner,   :string
  timestamps()
end

schema "audit_logs" do
  field :action,     :string
  field :amount,     :decimal
  field :from_id,    :integer
  field :to_id,      :integer
  timestamps()
end
```

### Solución: transferencia atómica

```elixir
# lib/bank/transfers.ex
defmodule Bank.Transfers do
  import Ecto.Query
  alias Bank.{Repo, Account, AuditLog}
  alias Ecto.Multi

  def transfer(from_id, to_id, amount) do
    Multi.new()
    |> Multi.run(:from, fn repo, _ ->
      case repo.get(Account, from_id) do
        nil     -> {:error, :account_not_found}
        account -> {:ok, account}
      end
    end)
    |> Multi.run(:debit, fn repo, %{from: from} ->
      if Decimal.compare(from.balance, amount) == :lt do
        {:error, :insufficient_funds}
      else
        from
        |> Account.changeset(%{balance: Decimal.sub(from.balance, amount)})
        |> repo.update()
      end
    end)
    |> Multi.run(:credit, fn repo, _ ->
      repo.get!(Account, to_id)
      |> Account.changeset(%{balance: Decimal.add(Repo.get!(Account, to_id).balance, amount)})
      |> repo.update()
    end)
    |> Multi.insert(:audit, fn %{debit: from} ->
      %AuditLog{action: "transfer", amount: amount, from_id: from.id, to_id: to_id}
    end)
    |> Repo.transaction()
  end
end
```

### Manejo de resultados

```elixir
case Bank.Transfers.transfer(1, 2, Decimal.new("500.00")) do
  {:ok, %{debit: from, credit: to, audit: log}} ->
    Logger.info("Transfer OK: audit_log=#{log.id}")

  {:error, :debit, :insufficient_funds, _changes_so_far} ->
    {:error, "Saldo insuficiente"}

  {:error, :from, :account_not_found, _} ->
    {:error, "Cuenta origen no existe"}

  {:error, failed_step, changeset, _} ->
    Logger.error("Transfer failed at #{failed_step}: #{inspect(changeset)}")
    {:error, "Error interno"}
end
```

**Por qué Multi**: cada paso está nombrado, el error devuelve el nombre del paso fallido
y el state acumulado hasta ese punto. En tests puedes ejecutar pasos individuales con
`Multi.to_list/1` y verificar cada operación de forma aislada.

---

## Parte 4 — `Repo.stream/2` para Millones de Registros

### Problema

Un proceso de fin de mes necesita recalcular comisiones de 5 millones de ventas.
`Repo.all/1` cargará todo en memoria y reventará la RAM. `Repo.stream/2` emite
registros en chunks usando cursores de PostgreSQL.

### Solución: stream con procesamiento en pipeline

```elixir
# lib/store/commission_job.ex
defmodule Store.CommissionJob do
  import Ecto.Query
  alias Store.{Repo, Sale, Commission}

  def recalculate_all do
    query = from(s in Sale,
      where: s.processed == false,
      select: s
    )

    # stream DEBE ejecutarse dentro de una transacción
    Repo.transaction(fn ->
      query
      |> Repo.stream(max_rows: 500)       # PostgreSQL cursor: 500 filas por fetch
      |> Stream.map(&calculate_commission/1)
      |> Stream.chunk_every(200)           # Batches de 200 para bulk insert
      |> Enum.each(&Repo.insert_all(Commission, &1))
    end, timeout: :infinity)
  end

  defp calculate_commission(%Sale{amount: amount, user_id: uid}) do
    rate = commission_rate(amount)
    %{
      user_id:    uid,
      amount:     Decimal.mult(amount, rate),
      calculated_at: DateTime.utc_now()
    }
  end

  defp commission_rate(amount) do
    cond do
      Decimal.compare(amount, 1000) == :gt -> Decimal.new("0.15")
      Decimal.compare(amount, 500)  == :gt -> Decimal.new("0.10")
      true                                 -> Decimal.new("0.05")
    end
  end
end
```

**Parámetros clave de `Repo.stream/2`**:

| Opción | Default | Descripción |
|--------|---------|-------------|
| `max_rows` | 500 | Filas por fetch del cursor PostgreSQL |
| `timeout` | 15_000 | Timeout por cada fetch (ms) |

**Importante**: `Repo.stream` requiere una transacción activa — PostgreSQL necesita el
contexto de transacción para mantener el cursor abierto entre fetches.

---

## Parte 5 — Subqueries con `subquery/1`

### Problema

Obtener todos los usuarios cuyas ventas totales superen el promedio global — sin
calcular el promedio en Elixir primero.

```elixir
# lib/store/analytics.ex  (continuación)
def above_average_sellers do
  avg_query =
    from(s in Sale,
      select: avg(s.amount)
    )

  from(s in Sale,
    join: u in assoc(s, :user),
    group_by: [s.user_id, u.name],
    having: sum(s.amount) > subquery(avg_query),
    select: %{user_name: u.name, total: sum(s.amount)}
  )
  |> Repo.all()
end
```

**`subquery/1`** convierte una query Ecto en un subquery SQL válido. PostgreSQL ejecuta
todo en un solo plan de query — no hay round-trip de datos a Elixir.

---

## Ejercicios Propuestos

### Ejercicio A — Window function acumulativa

Modificar `sales_ranking_by_category/0` para incluir también la venta acumulada
hasta cada registro (running total) usando `sum() OVER (PARTITION BY ... ORDER BY ...)`.

**Pista**: 
```elixir
fragment("sum(?) OVER (PARTITION BY ? ORDER BY ?)", s.amount, s.category_id, s.inserted_at)
```

### Ejercicio B — Filtros con OR dinámico

Extender `ProductFilters.search/1` para soportar un parámetro `"tags"` que sea
una lista de strings, filtrando productos que tengan **cualquiera** de esos tags
(requiere `dynamic([p], ^acc or fragment("? = ANY(?)", tag, p.tags))`).

### Ejercicio C — Multi con rollback explícito

Modificar `Bank.Transfers.transfer/3` para que, si la auditoría falla por un error
de validación, el rollback incluya un log en stderr con los montos involucrados.
Usar `Multi.run/3` con lógica de compensación.

### Ejercicio D — Stream con backpressure

Extender `CommissionJob.recalculate_all/0` para reportar progreso cada 10,000 registros
usando `Stream.with_index/1` y emitir un evento de telemetría con
`:telemetry.execute([:commission_job, :progress], %{count: n})`.

---

## Patrones de Producción — Checklist

- [ ] Nunca usar `Repo.all` sobre tablas sin límite en producción — siempre `limit/2` o `stream`
- [ ] Window functions van en `fragment` — no hay macros Ecto para ellas
- [ ] `dynamic/2` para filtros opcionales — nunca concatenar strings SQL
- [ ] `Ecto.Multi` para cualquier operación que modifique más de una tabla
- [ ] `Repo.stream` requiere transacción explícita y `timeout: :infinity` para jobs largos
- [ ] `subquery/1` evita round-trips — úsalo cuando el valor depende de la misma base de datos
- [ ] Siempre verificar el SQL generado con `Repo.to_sql(:all, query)` antes de deploy

```elixir
# Debug de query en iex
iex> {sql, params} = Repo.to_sql(:all, Store.Analytics.sales_ranking_by_category())
iex> IO.puts(sql)
SELECT u0."name", c1."name", sum(s0."amount"), rank() OVER (PARTITION BY s0."category_id" ORDER BY sum(s0."amount") DESC)
FROM "sales" AS s0 ...
```

---

## Referencias

- [Ecto.Query — fragment/1](https://hexdocs.pm/ecto/Ecto.Query.API.html#fragment/1)
- [Ecto.Multi](https://hexdocs.pm/ecto/Ecto.Multi.html)
- [Repo.stream/2](https://hexdocs.pm/ecto/Ecto.Repo.html#c:stream/2)
- [dynamic/2](https://hexdocs.pm/ecto/Ecto.Query.html#dynamic/2)
- PostgreSQL Window Functions: https://www.postgresql.org/docs/current/tutorial-window.html
