# Ejercicio 68: Ash — Extensions, Calculations, Aggregates y API Generation

## Objetivo

Extender recursos Ash con extensions que auto-generan APIs REST y GraphQL,
añadir campos computados con calculations, métricas sobre relaciones con
aggregates, y autenticación completa con `AshAuthentication`. Ash convierte
declaraciones en comportamiento: cero código boilerplate de routing o resolvers.

## Conceptos clave

- `AshJsonApi.Resource` — endpoints REST generados automáticamente desde el recurso
- `AshGraphql.Resource` — resolvers GraphQL generados desde el recurso
- `calculations` — campos computados: fórmulas en `expr/1` o módulos Elixir
- `aggregates` — count, sum, avg, min, max sobre relaciones, traducidos a SQL
- `AshAuthentication` — estrategias de autenticación declarativas (password, OAuth2)
- `validate` con expresiones DSL: `match(:email, ~r/@/)`, `numericality/2`

---

## Contexto del ejercicio

Extiendes el dominio del ejercicio 67 (Shop) con una API REST pública, una API
GraphQL interna, cálculos de precio con descuento, métricas de reviews por
producto y autenticación de usuarios con email/password.

---

## Parte 1: AshJsonApi — REST automático

### Añadir la extension al recurso

```elixir
# lib/shop/resources/product.ex
defmodule Shop.Product do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource]  # <-- extension REST

  # ... atributos y relaciones igual que ejercicio 67 ...

  json_api do
    type "products"  # JSON:API resource type

    routes do
      base "/products"

      # GET /products
      index :available do
        default_fields [:id, :name, :price, :stock, :sku]
      end

      # GET /products/:id
      get :read

      # POST /products
      post :create

      # PATCH /products/:id
      patch :update

      # DELETE /products/:id
      delete :destroy

      # PATCH /products/:id/restock  (acción custom)
      patch :restock, route: "/:id/restock"
    end
  end
end
```

### Router Phoenix

```elixir
# lib/shop_web/router.ex
defmodule ShopWeb.Router do
  use ShopWeb, :router
  use AshJsonApi.Router,
    domains: [Shop],
    json_schema: "/json_schema",
    open_api: "/open_api"

  pipeline :api do
    plug :accepts, ["json"]
    plug AshJsonApi.Plug.Parser
  end

  scope "/api" do
    pipe_through :api
    forward "/", AshJsonApi.Router, domains: [Shop]
  end
end
```

Con esto, el router genera automáticamente:

```
GET    /api/products          → index :available
GET    /api/products/:id      → get :read
POST   /api/products          → post :create
PATCH  /api/products/:id      → patch :update
DELETE /api/products/:id      → delete :destroy
PATCH  /api/products/:id/restock → patch :restock
```

Formato JSON:API automático — sin escribir controllers ni serializers.

---

## Parte 2: AshGraphql — GraphQL automático

```elixir
# lib/shop/resources/product.ex
defmodule Shop.Product do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshJsonApi.Resource, AshGraphql.Resource]  # ambas

  graphql do
    type :product

    queries do
      # query { products(filter: ..., sort: ...) { id name price } }
      list :products, :available

      # query { product(id: "...") { id name } }
      get :product, :read
    end

    mutations do
      # mutation { createProduct(input: { name: "..." price: 9.99 }) { id } }
      create :create_product, :create

      # mutation { updateProduct(id: "...", input: { price: 8.99 }) { id } }
      update :update_product, :update

      # mutation { restockProduct(id: "...", input: { quantity: 50 }) { stock } }
      update :restock_product, :restock

      # mutation { deleteProduct(id: "...") { id } }
      destroy :delete_product, :destroy
    end
  end
end
```

### Schema GraphQL

```elixir
# lib/shop_web/schema.ex
defmodule ShopWeb.Schema do
  use Absinthe.Schema
  use AshGraphql, domains: [Shop]

  # Ash genera los tipos, queries y mutations automáticamente
  # Solo añade aquí tipos custom que no vienen de Ash
  query do
  end

  mutation do
  end
end
```

```elixir
# lib/shop_web/router.ex — añadir el endpoint GraphQL
forward "/graphql", Absinthe.Plug,
  schema: ShopWeb.Schema

forward "/graphiql", Absinthe.Plug.GraphiQL,
  schema: ShopWeb.Schema,
  interface: :simple
```

---

## Parte 3: Calculations — campos computados

### Calculations con expr DSL (traducidas a SQL)

```elixir
defmodule Shop.Product do
  # ... resto del recurso ...

  calculations do
    # Precio con descuento: calculado en SQL, eficiente en queries masivas
    calculate :discounted_price, :decimal,
      expr(price * ^arg(:percent) / 100) do
      argument :percent, :integer, default: 10
    end

    # Precio formateado (no se puede hacer en SQL, usa módulo Elixir)
    calculate :price_display, :string, Shop.Product.Calculations.PriceDisplay

    # ¿Hay stock disponible?
    calculate :in_stock, :boolean, expr(stock > 0)
  end
end
```

### Calculation como módulo Elixir

```elixir
# lib/shop/resources/product/calculations/price_display.ex
defmodule Shop.Product.Calculations.PriceDisplay do
  use Ash.Resource.Calculation

  @impl true
  def calculate(records, _opts, _context) do
    Enum.map(records, fn product ->
      price_str = Decimal.to_string(product.price, :normal)
      "€#{price_str}"
    end)
  end

  # Declara qué campos necesita — Ash los carga automáticamente
  @impl true
  def load(_query, _opts, _context), do: [:price]
end
```

### Usar calculations en queries

```elixir
# Las calculations NO se cargan por defecto — hay que pedirlas explícitamente
products =
  Shop.Product
  |> Ash.Query.filter(active == true)
  |> Ash.Query.load([:in_stock, discounted_price: [percent: 20]])
  |> Shop.read!()

# Acceder al valor calculado
Enum.each(products, fn p ->
  IO.puts("#{p.name}: #{p.price_display} (20% off: #{p.discounted_price})")
end)
```

---

## Parte 4: Aggregates — métricas sobre relaciones

```elixir
defmodule Shop.Product do
  # ... resto del recurso ...

  aggregates do
    # Número total de reviews
    count :review_count, :reviews

    # Número de reviews verificadas
    count :verified_review_count, :reviews do
      filter expr(verified_purchase == true)
    end

    # Promedio de rating
    avg :average_rating, :reviews, :rating

    # Rating más alto recibido
    max :best_rating, :reviews, :rating

    # Rating más bajo recibido
    min :worst_rating, :reviews, :rating
  end
end
```

Ash traduce los aggregates a subconsultas SQL o JOINs según el data layer.
Se cargan igual que las calculations:

```elixir
products =
  Shop.Product
  |> Ash.Query.filter(active == true)
  |> Ash.Query.sort(average_rating: :desc)  # ordenar por aggregate
  |> Ash.Query.load([:review_count, :average_rating, :best_rating])
  |> Shop.read!()

Enum.each(products, fn p ->
  IO.puts("#{p.name} — #{p.review_count} reviews, avg: #{p.average_rating}")
end)
```

Filtrar por aggregate:

```elixir
# Productos con más de 5 reviews y rating promedio >= 4
Shop.Product
|> Ash.Query.filter(review_count > 5 and average_rating >= 4)
|> Ash.Query.load([:review_count, :average_rating])
|> Shop.read!()
```

---

## Parte 5: AshAuthentication — autenticación declarativa

### Recurso User con autenticación

```elixir
# lib/shop/resources/user.ex
defmodule Shop.User do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshAuthentication]

  postgres do
    table "users"
    repo Shop.Repo
  end

  attributes do
    uuid_primary_key :id
    attribute :email, :ci_string, allow_nil?: false, public?: true
    attribute :name, :string, public?: true
    attribute :role, :atom do
      constraints one_of: [:customer, :admin]
      default :customer
      public? true
    end
    timestamps()
  end

  validations do
    validate match(:email, ~r/^[^\s]+@[^\s]+\.[^\s]+$/),
      message: "debe ser un email válido"
  end

  authentication do
    # Estrategia email + password con hashing bcrypt
    strategies do
      password :password do
        identity_field :email

        resettable do
          sender Shop.User.Emails.PasswordResetSender
        end
      end
    end

    tokens do
      enabled? true
      token_resource Shop.Token
      signing_secret fn _, _ ->
        Application.fetch_env(:shop, :token_signing_secret)
      end
    end
  end

  actions do
    defaults [:read, :destroy]

    # AshAuthentication genera automáticamente :register_with_password,
    # :sign_in_with_password, :reset_password_with_password
    # Solo añadimos acciones de dominio propias:

    update :promote_to_admin do
      change set_attribute(:role, :admin)
    end
  end

  identities do
    identity :unique_email, [:email]
  end
end
```

### Token resource (requerido por AshAuthentication)

```elixir
defmodule Shop.Token do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer,
    extensions: [AshAuthentication.TokenResource]

  postgres do
    table "tokens"
    repo Shop.Repo
  end
end
```

### Uso en el controlador Phoenix

```elixir
defmodule ShopWeb.AuthController do
  use ShopWeb, :controller

  def register(conn, %{"email" => email, "password" => password, "name" => name}) do
    params = %{email: email, password: password, password_confirmation: password, name: name}

    case AshAuthentication.Strategy.action(
           AshAuthentication.Info.strategy!(Shop.User, :password),
           :register,
           params
         ) do
      {:ok, user} ->
        token = AshAuthentication.generate_token_for(user)
        json(conn, %{token: token, user: %{id: user.id, email: user.email}})

      {:error, error} ->
        conn
        |> put_status(:unprocessable_entity)
        |> json(%{errors: format_errors(error)})
    end
  end

  def sign_in(conn, %{"email" => email, "password" => password}) do
    case AshAuthentication.Strategy.action(
           AshAuthentication.Info.strategy!(Shop.User, :password),
           :sign_in,
           %{email: email, password: password}
         ) do
      {:ok, user} ->
        token = AshAuthentication.generate_token_for(user)
        json(conn, %{token: token})

      {:error, _} ->
        conn
        |> put_status(:unauthorized)
        |> json(%{error: "credenciales inválidas"})
    end
  end

  defp format_errors(%Ash.Error.Invalid{errors: errors}) do
    Enum.map(errors, &%{field: Map.get(&1, :field), message: Exception.message(&1)})
  end
end
```

### Plug de autenticación para rutas protegidas

```elixir
# lib/shop_web/plugs/require_auth.ex
defmodule ShopWeb.Plugs.RequireAuth do
  import Plug.Conn

  def init(opts), do: opts

  def call(conn, _opts) do
    with ["Bearer " <> token] <- get_req_header(conn, "authorization"),
         {:ok, user} <- AshAuthentication.verify_token(Shop.User, token) do
      assign(conn, :current_user, user)
    else
      _ ->
        conn
        |> put_status(:unauthorized)
        |> Phoenix.Controller.json(%{error: "no autorizado"})
        |> halt()
    end
  end
end
```

```elixir
# En el router — rutas protegidas
pipeline :authenticated do
  plug :accepts, ["json"]
  plug ShopWeb.Plugs.RequireAuth
end

scope "/api/admin", ShopWeb do
  pipe_through :authenticated

  resources "/products", ProductController, except: [:new, :edit]
end
```

---

## Parte 6: Policies — autorización declarativa

```elixir
defmodule Shop.Product do
  use Ash.Resource,
    domain: Shop,
    data_layer: AshPostgres.DataLayer,
    authorizers: [Ash.Policy.Authorizer]  # activar autorización

  # ... resto del recurso ...

  policies do
    # Cualquiera puede leer productos activos
    policy action_type(:read) do
      authorize_if expr(active == true)
    end

    # Solo admins pueden crear, actualizar y borrar
    policy action_type([:create, :update, :destroy]) do
      authorize_if actor_attribute_equals(:role, :admin)
    end

    # Cualquier usuario autenticado puede ver cualquier producto (admin ve todo)
    policy action(:read) do
      authorize_if actor_attribute_equals(:role, :admin)
      authorize_if expr(active == true)
    end
  end
end
```

### Pasar el actor al llamar acciones

```elixir
# El actor se pasa via opción, Ash lo propaga a los policies automáticamente
def get_product_as(user, product_id) do
  Shop.get(Shop.Product, product_id, actor: user)
  # Si el policy lo deniega → {:error, %Ash.Error.Forbidden{}}
end

def create_product_as(user, attrs) do
  Shop.Product
  |> Ash.Changeset.for_create(:create, attrs, actor: user)
  |> Shop.create()
end
```

---

## Ejercicio propuesto

1. Añade al recurso `Review` una calculation `:sentiment` de tipo `:atom`
   (`:positive` | `:neutral` | `:negative`) que dependa del campo `rating`:
   rating >= 4 → `:positive`, rating == 3 → `:neutral`, rating < 3 → `:negative`.
   Impleméntala como módulo (no como `expr`) porque requiere lógica condicional.

2. Añade un aggregate `:revenue_potential` a `Product` que sume
   `price * stock` para representar el valor de inventario. Pista: Ash soporta
   `sum` con `field:` y `filter:`.

3. Configura en `Shop.User` una segunda estrategia: `magic_link` con
   `AshAuthentication.Strategy.MagicLink`. El sender puede ser un módulo stub
   que solo hace `IO.puts`.

4. Escribe un policy en `Review` donde:
   - Cualquiera puede leer reviews de productos activos.
   - Solo el dueño de la review (campo `user_id`) puede borrarla.
   - Admins pueden borrar cualquier review.

---

## Configuración del proyecto

```elixir
# mix.exs
defp deps do
  [
    {:ash, "~> 3.0"},
    {:ash_postgres, "~> 2.0"},
    {:ash_json_api, "~> 1.0"},
    {:ash_graphql, "~> 1.0"},
    {:ash_authentication, "~> 4.0"},
    {:ash_authentication_phoenix, "~> 2.0"},
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:bcrypt_elixir, "~> 3.0"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.18"},
    {:jason, "~> 1.4"}
  ]
end
```

```elixir
# config/config.exs
config :my_app, :ash_domains, [Shop]

config :shop, :token_signing_secret,
  System.get_env("TOKEN_SIGNING_SECRET") ||
    raise("TOKEN_SIGNING_SECRET no configurada")

config :ash_authentication,
  debug_authentication_failures?: Mix.env() == :dev
```

---

## Diferencia clave: calculation vs aggregate

| | Calculation | Aggregate |
|---|---|---|
| **Fuente** | El propio registro o lógica Elixir | Registros relacionados |
| **SQL** | Puede o no ir a SQL (depende de `expr` vs módulo) | Siempre subconsulta o JOIN SQL |
| **Ejemplos** | precio formateado, nombre completo, descuento | count, avg, sum sobre relaciones |
| **Carga** | `Ash.Query.load([:campo])` | `Ash.Query.load([:campo])` |
| **Filtrado** | `filter(campo > x)` si es `expr` | `filter(aggregate_field > x)` siempre |

---

## Referencias

- [AshJsonApi](https://hexdocs.pm/ash_json_api)
- [AshGraphql](https://hexdocs.pm/ash_graphql)
- [AshAuthentication](https://hexdocs.pm/ash_authentication)
- [Ash Calculations](https://hexdocs.pm/ash/calculations.html)
- [Ash Aggregates](https://hexdocs.pm/ash/aggregates.html)
- [Ash Policies](https://hexdocs.pm/ash/policies.html)
