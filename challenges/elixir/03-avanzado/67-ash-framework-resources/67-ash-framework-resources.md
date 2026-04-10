# Ejercicio 67: Ash Framework — Resources y Actions

## Objetivo

Modelar un dominio de negocio usando Ash Framework: definir recursos declarativos,
acciones con validaciones, relaciones entre recursos y queries avanzadas con filtros,
ordenamiento y paginación. Ash reemplaza el imperativo Ecto por un DSL declarativo
donde el framework genera comportamiento a partir de la declaración.

## Conceptos clave

- `use Ash.Resource` — define un recurso con atributos, acciones y relaciones
- `use Ash.Domain` — agrupa recursos y expone la API pública del dominio
- Atributos tipados con validaciones inline (`allow_nil?`, `constraints`)
- Actions por defecto (`defaults [:create, :read, :update, :destroy]`)
- Custom actions con `argument` y `change`
- `Ash.Query` para filtrar, ordenar y paginar
- `AshPostgres.DataLayer` para persistencia en PostgreSQL

---

## Contexto del ejercicio

Estás construyendo el backend de un e-commerce. El dominio tiene tres recursos:
`Category`, `Product` y `Review`. Debes modelarlos con Ash, implementar acciones
custom y escribir queries que el frontend consumirá.

---

## Parte 1: Domain y recursos base

### El dominio

```elixir
# lib/shop/shop.ex
defmodule Shop do
  use Ash.Domain

  resources do
    resource Shop.Category
    resource Shop.Product
    resource Shop.Review
  end
end
```

El dominio es el punto de entrada. Todo código externo llama `Shop.create!(...)`,
`Shop.read!(...)`, nunca accede al recurso directamente.

### Recurso Category

```elixir
# lib/shop/resources/category.ex
defmodule Shop.Category do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "categories"
    repo Shop.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :name, :string, allow_nil?: false, public?: true
    attribute :slug, :string, allow_nil?: false, public?: true
    attribute :active, :boolean, default: true, public?: true
    timestamps()
  end

  actions do
    defaults [:create, :read, :update, :destroy]

    read :active do
      filter expr(active == true)
    end
  end

  relationships do
    has_many :products, Shop.Product
  end

  identities do
    identity :unique_slug, [:slug]
  end
end
```

`identities` genera constraints de unicidad en la base de datos y validaciones
en el changeset automáticamente. `expr(...)` usa el DSL de Ash para expresiones
que se traducen a SQL.

### Recurso Product completo

```elixir
# lib/shop/resources/product.ex
defmodule Shop.Product do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "products"
    repo Shop.Repo
  end

  attributes do
    uuid_primary_key :id

    attribute :name, :string do
      allow_nil? false
      public? true
      constraints min_length: 2, max_length: 200
    end

    attribute :description, :string, public?: true

    attribute :price, :decimal do
      allow_nil? false
      public? true
      constraints min: Decimal.new("0.01")
    end

    attribute :stock, :integer do
      default 0
      allow_nil? false
      public? true
      constraints min: 0
    end

    attribute :sku, :string, allow_nil?: false, public?: true
    attribute :active, :boolean, default: true, public?: true

    timestamps()
  end

  actions do
    defaults [:create, :read, :update, :destroy]

    # Acción custom: incrementar stock
    update :restock do
      description "Añade unidades al stock existente"

      argument :quantity, :integer do
        allow_nil? false
        constraints min: 1
      end

      change fn changeset, _ ->
        current = Ash.Changeset.get_data(changeset, :stock)
        added = Ash.Changeset.get_argument(changeset, :quantity)
        Ash.Changeset.change_attribute(changeset, :stock, current + added)
      end
    end

    # Acción custom: desactivar producto
    update :deactivate do
      description "Marca el producto como inactivo sin borrarlo"
      change set_attribute(:active, false)
    end

    # Read con filtros por defecto
    read :available do
      description "Productos activos con stock > 0"
      filter expr(active == true and stock > 0)
    end

    read :by_category do
      argument :category_id, :uuid, allow_nil?: false
      filter expr(category_id == ^arg(:category_id))
    end
  end

  relationships do
    belongs_to :category, Shop.Category do
      allow_nil? false
      public? true
    end

    has_many :reviews, Shop.Review
  end

  identities do
    identity :unique_sku, [:sku]
  end
end
```

Los `constraints` en atributos se validan antes de tocar la base de datos.
`set_attribute/2` es un `change` built-in de Ash que evita escribir la función
manualmente.

---

## Parte 2: Recurso con validaciones custom

### Review con validaciones explícitas

```elixir
# lib/shop/resources/review.ex
defmodule Shop.Review do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer

  postgres do
    table "reviews"
    repo Shop.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :rating, :integer, allow_nil?: false, public?: true
    attribute :body, :string, public?: true
    attribute :verified_purchase, :boolean, default: false, public?: true
    timestamps()
  end

  validations do
    # Validación inline con expr DSL
    validate numericality(:rating, greater_than_or_equal_to: 1, less_than_or_equal_to: 5)

    # Validación custom con módulo
    validate {Shop.Review.Validations.BodyLength, []}
  end

  actions do
    defaults [:create, :read, :destroy]

    update :mark_verified do
      change set_attribute(:verified_purchase, true)
    end
  end

  relationships do
    belongs_to :product, Shop.Product, allow_nil?: false, public?: true
  end
end
```

### Validación custom como módulo

```elixir
# lib/shop/resources/review/validations/body_length.ex
defmodule Shop.Review.Validations.BodyLength do
  use Ash.Resource.Validation

  @impl true
  def validate(changeset, _opts, _context) do
    body = Ash.Changeset.get_attribute(changeset, :body)

    cond do
      is_nil(body) ->
        :ok

      String.length(body) < 10 ->
        {:error,
         field: :body,
         message: "debe tener al menos 10 caracteres si se incluye"}

      String.length(body) > 2000 ->
        {:error, field: :body, message: "no puede superar 2000 caracteres"}

      true ->
        :ok
    end
  end
end
```

Las validaciones custom implementan `Ash.Resource.Validation` y devuelven `:ok`
o `{:error, keyword_list}`. Ash las ejecuta en el orden declarado antes de
persistir.

---

## Parte 3: Ash.Query — filtros, ordenamiento y paginación

### Queries básicas

```elixir
# lib/shop/queries.ex
defmodule Shop.Queries do
  import Ash.Query

  # Productos baratos con stock
  def cheap_available(max_price) do
    Shop.Product
    |> filter(active == true and stock > 0 and price <= ^max_price)
    |> sort(price: :asc)
    |> limit(20)
  end

  # Búsqueda por nombre (case-insensitive)
  def search_by_name(term) do
    Shop.Product
    |> filter(contains(name, ^term))
    |> sort(inserted_at: :desc)
  end

  # Productos de una categoría, ordenados por rating promedio
  def by_category_sorted(category_id) do
    Shop.Product
    |> filter(category_id == ^category_id and active == true)
    |> sort(name: :asc)
  end
end
```

### Paginación con keyset

```elixir
defmodule Shop.ProductList do
  def paginated(params) do
    page_opts = [
      count: true,         # incluye total en el resultado
      limit: params[:limit] || 20,
      after: params[:cursor]  # keyset cursor para paginación eficiente
    ]

    Shop.Product
    |> Ash.Query.filter(active == true)
    |> Ash.Query.sort(inserted_at: :desc)
    |> Shop.read!(page: page_opts)
  end
end
```

Ash soporta dos modos de paginación: `offset` (simple, menos eficiente) y
`keyset` (cursor-based, O(1) en seek). El recurso debe declarar qué modo admite:

```elixir
# Dentro del bloque actions del recurso:
read :list do
  pagination do
    keyset? true
    default_limit 20
    max_page_size 100
    countable true
  end
end
```

---

## Parte 4: Uso desde código de aplicación

### Crear y actualizar recursos

```elixir
defmodule Shop.ProductService do
  # Crear un producto
  def create_product(attrs) do
    Shop.Product
    |> Ash.Changeset.for_create(:create, attrs)
    |> Shop.create()
    # Devuelve {:ok, product} | {:error, %Ash.Error{}}
  end

  # Restockear (acción custom)
  def restock(product_id, quantity) do
    with {:ok, product} <- get_product(product_id) do
      product
      |> Ash.Changeset.for_update(:restock, %{quantity: quantity})
      |> Shop.update()
    end
  end

  # Leer con query
  def available_under(max_price) do
    Shop.Product
    |> Ash.Query.filter(active == true and price <= ^max_price)
    |> Ash.Query.sort(:price)
    |> Shop.read!()
  end

  defp get_product(id) do
    Shop.get(Shop.Product, id)
    # {:ok, product} | {:error, %Ash.Error.Query.NotFound{}}
  end
end
```

### Manejo de errores de Ash

```elixir
defmodule Shop.ErrorHandler do
  def handle({:error, %Ash.Error.Invalid{} = error}) do
    # Errores de validación: atributos inválidos, constraints, etc.
    errors =
      error.errors
      |> Enum.map(&format_invalid_error/1)

    {:unprocessable, errors}
  end

  def handle({:error, %Ash.Error.Query.NotFound{}}) do
    {:not_found, "Recurso no encontrado"}
  end

  def handle({:error, error}) do
    {:internal_error, Exception.message(error)}
  end

  defp format_invalid_error(%{field: field, message: msg}),
    do: %{field: field, message: msg}

  defp format_invalid_error(error),
    do: %{field: nil, message: Exception.message(error)}
end
```

---

## Parte 5: Migraciones con AshPostgres

```bash
# Generar migración desde los recursos Ash
mix ash_postgres.generate_migrations --name add_shop_resources

# Ash analiza los recursos y genera la migración automáticamente
# lib/shop/repo/migrations/20260101000001_add_shop_resources.exs
```

```elixir
defmodule Shop.Repo.Migrations.AddShopResources do
  use Ecto.Migration

  def change do
    create table(:categories, primary_key: false) do
      add :id, :uuid, null: false, primary_key: true
      add :name, :string, null: false
      add :slug, :string, null: false
      add :active, :boolean, default: true, null: false
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:categories, [:slug])

    create table(:products, primary_key: false) do
      add :id, :uuid, null: false, primary_key: true
      add :name, :string, null: false
      add :description, :text
      add :price, :decimal, null: false
      add :stock, :integer, default: 0, null: false
      add :sku, :string, null: false
      add :active, :boolean, default: true, null: false
      add :category_id, references(:categories, type: :uuid, on_delete: :restrict), null: false
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:products, [:sku])
    create index(:products, [:category_id])
    create index(:products, [:active, :price])

    create table(:reviews, primary_key: false) do
      add :id, :uuid, null: false, primary_key: true
      add :rating, :integer, null: false
      add :body, :text
      add :verified_purchase, :boolean, default: false, null: false
      add :product_id, references(:products, type: :uuid, on_delete: :delete_all), null: false
      timestamps(type: :utc_datetime_usec)
    end

    create index(:reviews, [:product_id])
  end
end
```

Ash genera y gestiona las migraciones. Si cambias un atributo en el recurso,
el siguiente `generate_migrations` detecta el diff y crea la migración delta.

---

## Ejercicio propuesto

Implementa lo siguiente:

1. Añade al recurso `Product` una acción `:apply_discount` que reciba un
   argumento `percent` (integer, 1..99) y reduzca el precio proporcionalmente.
   Usa `Decimal.mult/2` y `Decimal.div/2`.

2. Escribe una query `Shop.Queries.top_reviewed/1` que dado un `category_id`
   devuelva los productos con al menos una review, ordenados por `rating` desc.
   Pista: necesitas cargar la relación `:reviews` con `Ash.Query.load/2`.

3. Añade una acción `read :search` en `Product` que acepte un argumento
   `:term` y filtre por `contains(name, ^arg(:term))`.

4. Implementa `Shop.ErrorHandler.handle/1` para el caso
   `%Ash.Error.Changes.InvalidAttribute{}` (campo inválido específico).

---

## Configuración del proyecto

```elixir
# mix.exs
defp deps do
  [
    {:ash, "~> 3.0"},
    {:ash_postgres, "~> 2.0"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.18"}
  ]
end
```

```elixir
# config/config.exs
config :my_app, :ash_domains, [Shop]

config :ash, :use_all_identities_in_manage_relationship?, false
```

---

## Referencias

- [Ash Framework Docs](https://hexdocs.pm/ash)
- [AshPostgres](https://hexdocs.pm/ash_postgres)
- [Ash.Query](https://hexdocs.pm/ash/Ash.Query.html)
- [Ash.Changeset](https://hexdocs.pm/ash/Ash.Changeset.html)
