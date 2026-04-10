# 60. Absinthe DataLoader y Auth Middleware

**Difficulty**: Avanzado

## Prerequisites
- Ejercicio 59: Absinthe GraphQL Schema y Resolvers
- Ecto y asociaciones (has_many, belongs_to)
- Comprensión del problema N+1 en bases de datos
- Phoenix Plug middleware

## Learning Objectives
After completing this exercise, you will be able to:
- Identificar y resolver el problema N+1 con DataLoader
- Implementar middleware de autenticación con `Absinthe.Middleware`
- Crear middleware de autorización por campo
- Definir scalars custom (DateTime, UUID, JSON)
- Analizar la complejidad de queries para prevenir abuse
- Configurar el contexto de Absinthe con fuentes DataLoader

## Concepts

### El problema N+1: por qué DataLoader existe

Considera este resolver para el campo `author` en `Post`:

```elixir
# Resolver naive — O(N) queries a la base de datos
field :author, :user do
  resolve fn post, _, _ ->
    user = MyApp.Accounts.get_user!(post.author_id)
    {:ok, user}
  end
end
```

Cuando el cliente pide 10 posts con sus autores, Elixir ejecuta:
1. `SELECT * FROM posts LIMIT 10` — 1 query
2. `SELECT * FROM users WHERE id = 1` — por post.author_id = 1
3. `SELECT * FROM users WHERE id = 2` — por post.author_id = 2
4. ...10 queries más

Total: 11 queries para una operación que debería ser 2. En producción con 100 posts son 101 queries. Esto es el problema N+1.

DataLoader soluciona esto con batching: acumula todas las claves que necesita cargar durante la resolución de un nivel del árbol GraphQL, y las carga en una sola query al final.

```
Post 1 → author_id: 1 ─┐
Post 2 → author_id: 2 ─┤─ DataLoader: SELECT * FROM users WHERE id IN (1,2,3,...) → 1 query
Post 3 → author_id: 1 ─┘  (author_id=1 se deduplica automáticamente)
```

### Setup de DataLoader con Ecto

```elixir
# mix.exs
defp deps do
  [
    {:absinthe, "~> 1.7"},
    {:dataloader, "~> 2.0"},
    # dataloader viene con soporte Ecto built-in
  ]
end
```

```elixir
# lib/my_app/loader.ex
defmodule MyApp.Loader do
  def data do
    # Dataloader.Ecto usa el Repo y carga asociaciones automáticamente
    Dataloader.Ecto.new(MyApp.Repo, query: &query/2)
  end

  # Permite personalizar la query por source y args
  defp query(MyApp.Blog.Post, %{status: :published}) do
    import Ecto.Query
    from p in MyApp.Blog.Post, where: p.status == :published
  end

  defp query(queryable, _), do: queryable
end
```

```elixir
# lib/my_app_web/schema.ex
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  import Absinthe.Resolution.Helpers, only: [dataloader: 1, dataloader: 2]

  def context(ctx) do
    loader =
      Dataloader.new()
      |> Dataloader.add_source(MyApp.Repo, MyApp.Loader.data())

    Map.put(ctx, :loader, loader)
  end

  def plugins do
    [Absinthe.Middleware.Dataloader | Absinthe.Plugin.defaults()]
  end

  # ... resto del schema
end
```

### DataLoader en resolvers

```elixir
# lib/my_app_web/schema/types/post.ex
defmodule MyAppWeb.Schema.Types.Post do
  use Absinthe.Schema.Notation
  import Absinthe.Resolution.Helpers, only: [dataloader: 1]

  object :post do
    field :id,    :id
    field :title, :string
    field :body,  :string

    # BIEN: DataLoader batchea todas las cargas de :author
    # Una sola query para N posts
    field :author, :user do
      resolve dataloader(MyApp.Repo)
    end

    # DataLoader también maneja has_many
    field :comments, list_of(:comment) do
      resolve dataloader(MyApp.Repo)
    end

    # DataLoader con args: filtra comentarios aprobados
    field :approved_comments, list_of(:comment) do
      resolve dataloader(MyApp.Repo, :comments, args: %{status: :approved})
    end

    # DataLoader con función de query personalizada
    field :recent_comments, list_of(:comment) do
      arg :limit, :integer, default_value: 5

      resolve fn post, %{limit: limit}, %{context: %{loader: loader}} ->
        loader
        |> Dataloader.load(MyApp.Repo, {:many, MyApp.Blog.Comment}, [
          post_id: post.id,
          limit: limit,
          order_by: [desc: :inserted_at]
        ])
        |> Absinthe.Resolution.Helpers.on_load(fn loader ->
          comments = Dataloader.get(loader, MyApp.Repo, {:many, MyApp.Blog.Comment}, [
            post_id: post.id,
            limit: limit,
            order_by: [desc: :inserted_at]
          ])
          {:ok, comments}
        end)
      end
    end
  end
end
```

### Absinthe.Middleware: autenticación y autorización

Middleware en Absinthe es una función que envuelve la resolución de un campo. Puede ejecutar lógica antes de llamar al resolver, después, o reemplazarlo completamente.

```elixir
# lib/my_app_web/middleware/authenticate.ex
defmodule MyAppWeb.Middleware.Authenticate do
  @behaviour Absinthe.Middleware

  @impl true
  def call(%{context: %{current_user: user}} = resolution, _opts)
      when not is_nil(user) do
    # Usuario autenticado: continuar normalmente
    resolution
  end

  def call(resolution, _opts) do
    # Sin usuario: abortar con error
    Absinthe.Resolution.put_result(resolution, {:error, "Autenticación requerida"})
  end
end
```

```elixir
# lib/my_app_web/middleware/authorize.ex
defmodule MyAppWeb.Middleware.Authorize do
  @behaviour Absinthe.Middleware

  @impl true
  def call(%{context: %{current_user: user}} = resolution, role) do
    if user_has_role?(user, role) do
      resolution
    else
      Absinthe.Resolution.put_result(resolution, {:error, "Permiso denegado"})
    end
  end

  def call(resolution, _role) do
    Absinthe.Resolution.put_result(resolution, {:error, "Autenticación requerida"})
  end

  defp user_has_role?(%{role: :admin}, _), do: true
  defp user_has_role?(%{role: role}, required), do: role == required
end
```

```elixir
# Uso en el schema
object :post_mutations do
  field :create_post, :post do
    arg :input, non_null(:create_post_input)

    # El orden importa: Authenticate primero, luego el resolver
    middleware MyAppWeb.Middleware.Authenticate
    resolve &MyAppWeb.Resolvers.Post.create_post/3
  end

  field :delete_post, :boolean do
    arg :id, non_null(:id)

    middleware MyAppWeb.Middleware.Authenticate
    middleware MyAppWeb.Middleware.Authorize, :admin
    resolve &MyAppWeb.Resolvers.Post.delete_post/3
  end
end
```

### Middleware global con `middleware/2` callback

En lugar de agregar middleware campo por campo, puedes aplicarlo globalmente:

```elixir
# lib/my_app_web/schema.ex
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  # Este callback se llama para cada campo del schema
  def middleware(middleware, field, object) do
    middleware
    |> apply_auth_middleware(field, object)
    |> apply_error_handler(field, object)
  end

  # Agrega auth a todas las mutations automáticamente
  defp apply_auth_middleware(middleware, _field, %Absinthe.Type.Object{identifier: :mutation}) do
    [MyAppWeb.Middleware.Authenticate | middleware]
  end

  defp apply_auth_middleware(middleware, _field, _object), do: middleware

  # Agrega manejo de errores al final de toda resolución
  defp apply_error_handler(middleware, _field, _object) do
    middleware ++ [MyAppWeb.Middleware.HandleErrors]
  end
end
```

```elixir
# lib/my_app_web/middleware/handle_errors.ex
# Middleware que se ejecuta DESPUÉS del resolver para normalizar errores
defmodule MyAppWeb.Middleware.HandleErrors do
  @behaviour Absinthe.Middleware

  @impl true
  def call(%{errors: []} = resolution, _opts), do: resolution

  def call(%{errors: errors} = resolution, _opts) do
    normalized =
      Enum.map(errors, fn
        %Ecto.Changeset{} = cs -> format_changeset(cs)
        error -> error
      end)

    %{resolution | errors: normalized}
  end

  defp format_changeset(changeset) do
    errors = Ecto.Changeset.traverse_errors(changeset, fn {msg, opts} ->
      Enum.reduce(opts, msg, fn {k, v}, acc ->
        String.replace(acc, "%{#{k}}", to_string(v))
      end)
    end)

    %{
      message: "Validación fallida",
      extensions: %{code: "VALIDATION_ERROR", fields: errors}
    }
  end
end
```

### Query complexity: prevenir queries abusivas

Un cliente podría pedir posts → comments → author → posts → comments en loop infinito. La complexity analysis detiene esto.

```elixir
# lib/my_app_web/schema.ex
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  # Complexity máxima permitida por query
  @max_complexity 1000

  # Absinthe.Plug configuración
  def complexity_limit, do: @max_complexity
end
```

```elixir
# lib/my_app_web/router.ex
forward "/graphql", Absinthe.Plug,
  schema: MyAppWeb.Schema,
  analyze_complexity: true,
  max_complexity: MyAppWeb.Schema.complexity_limit()
```

```elixir
# Asignar complexity a campos costosos
object :post do
  field :id,    :id
  field :title, :string

  # Un post tiene complexity 1 (default)
  # Sus comentarios cuestan 5 + n * complejidad_de_comment
  field :comments, list_of(:comment) do
    arg :limit, :integer, default_value: 10

    complexity fn %{limit: limit}, child_complexity ->
      5 + limit * child_complexity
    end

    resolve dataloader(MyApp.Repo)
  end
end
```

### Rate limiting con middleware

```elixir
# lib/my_app_web/middleware/rate_limit.ex
defmodule MyAppWeb.Middleware.RateLimit do
  @behaviour Absinthe.Middleware

  # Límite: 100 mutations por minuto por usuario
  @limit 100
  @window_ms 60_000

  @impl true
  def call(%{context: %{current_user: user}} = resolution, _opts) do
    key = "rate_limit:mutations:#{user.id}"

    case check_rate(key) do
      :ok ->
        resolution

      {:error, :rate_limited} ->
        Absinthe.Resolution.put_result(
          resolution,
          {:error, message: "Rate limit excedido", extensions: %{code: "RATE_LIMITED"}}
        )
    end
  end

  def call(resolution, _opts), do: resolution

  defp check_rate(key) do
    # Usa ETS, Redis, o Hammer para implementar sliding window
    case :ets.lookup(:rate_limits, key) do
      [{_, count, window_start}] when count < @limit ->
        now = System.monotonic_time(:millisecond)

        if now - window_start < @window_ms do
          :ets.update_counter(:rate_limits, key, {2, 1})
          :ok
        else
          :ets.insert(:rate_limits, {key, 1, now})
          :ok
        end

      [{_, count, _}] when count >= @limit ->
        {:error, :rate_limited}

      [] ->
        now = System.monotonic_time(:millisecond)
        :ets.insert(:rate_limits, {key, 1, now})
        :ok
    end
  end
end
```

### Scalars avanzados

```elixir
# lib/my_app_web/schema/types/scalars.ex
defmodule MyAppWeb.Schema.Types.Scalars do
  use Absinthe.Schema.Notation

  scalar :naive_datetime, description: "NaiveDateTime como string ISO8601" do
    serialize fn
      %NaiveDateTime{} = dt -> NaiveDateTime.to_iso8601(dt)
      value -> to_string(value)
    end

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} ->
        case NaiveDateTime.from_iso8601(value) do
          {:ok, dt} -> {:ok, dt}
          {:error, _} -> :error
        end

      _ -> :error
    end
  end

  scalar :date, description: "Date como string YYYY-MM-DD" do
    serialize fn
      %Date{} = d -> Date.to_iso8601(d)
      value -> to_string(value)
    end

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} ->
        case Date.from_iso8601(value) do
          {:ok, date} -> {:ok, date}
          {:error, _} -> :error
        end

      _ -> :error
    end
  end

  scalar :upload, description: "File upload" do
    serialize &Function.identity/1

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} -> {:ok, value}
      %Plug.Upload{} = upload -> {:ok, upload}
      _ -> :error
    end
  end
end
```

### Testing de DataLoader y middleware

```elixir
defmodule MyAppWeb.Schema.DataLoaderTest do
  use MyAppWeb.ConnCase

  @posts_with_authors_query """
  query {
    posts(pageSize: 10) {
      entries {
        title
        author {
          name
          email
        }
        commentCount
        comments {
          body
          author { name }
        }
      }
    }
  }
  """

  test "resuelve N posts con autores en 2 queries (no N+1)", %{conn: conn} do
    user1 = insert(:user)
    user2 = insert(:user)
    insert_list(5, :post, author: user1, status: :published)
    insert_list(5, :post, author: user2, status: :published)

    # Ecto.Sandbox intercepta queries — podemos contarlas
    parent = self()
    :telemetry.attach(
      "test-query-counter",
      [:my_app, :repo, :query],
      fn _, _, _, _ -> send(parent, :db_query) end,
      nil
    )

    conn
    |> authenticate(user1)
    |> post("/api/graphql", %{query: @posts_with_authors_query})
    |> json_response(200)

    query_count =
      receive do :db_query -> 1 after 0 -> 0 end
      |> then(fn _ -> count_received_messages(:db_query) end)

    # Con DataLoader: 1 query posts + 1 query users + 1 query comments = 3
    # Sin DataLoader: 1 + 10 + 10 = 21
    assert query_count <= 5

    :telemetry.detach("test-query-counter")
  end

  @create_post_mutation """
  mutation CreatePost($input: CreatePostInput!) {
    createPost(input: $input) {
      id
      title
    }
  }
  """

  test "mutation sin auth retorna 200 con error en data", %{conn: conn} do
    response =
      conn
      |> post("/api/graphql", %{
        query: @create_post_mutation,
        variables: %{input: %{title: "Test", body: "Body content here"}}
      })
      |> json_response(200)

    # GraphQL siempre retorna 200 — los errores van en el body
    assert [error] = response["errors"]
    assert error["message"] == "Autenticación requerida"
  end

  test "middleware authorize bloquea usuarios sin rol admin", %{conn: conn} do
    regular_user = insert(:user, role: :user)

    response =
      conn
      |> authenticate(regular_user)
      |> post("/api/graphql", %{
        query: """
        mutation {
          deletePost(id: "123")
        }
        """
      })
      |> json_response(200)

    assert [error] = response["errors"]
    assert error["message"] == "Permiso denegado"
  end
end
```

## Exercises

### Ejercicio 1: Migrar resolvers naive a DataLoader

Tienes un schema con el problema N+1. Tu tarea es migrarlo a DataLoader.

**Código actual (con N+1):**

```elixir
defmodule MyAppWeb.Schema.Types.Post do
  use Absinthe.Schema.Notation

  object :post do
    field :id,    :id
    field :title, :string

    # BUG: N+1 — ejecuta 1 query por cada post
    field :author, :user do
      resolve fn post, _, _ ->
        {:ok, MyApp.Accounts.get_user!(post.author_id)}
      end
    end

    # BUG: N+1 — ejecuta 1 query por cada post
    field :comments, list_of(:comment) do
      resolve fn post, _, _ ->
        {:ok, MyApp.Blog.list_comments(post_id: post.id)}
      end
    end

    # BUG: N+1 — calcula count con query separada por post
    field :comment_count, :integer do
      resolve fn post, _, _ ->
        {:ok, MyApp.Blog.count_comments(post.id)}
      end
    end
  end
end
```

**Pasos para migrar:**

1. Agrega DataLoader al contexto del schema:

```elixir
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  def context(ctx) do
    loader =
      Dataloader.new()
      |> Dataloader.add_source(MyApp.Repo, MyApp.Loader.data())

    Map.put(ctx, :loader, loader)
  end

  def plugins do
    [Absinthe.Middleware.Dataloader | Absinthe.Plugin.defaults()]
  end
end
```

2. Crea `MyApp.Loader` con una función `data/0` que devuelva un `Dataloader.Ecto` source

3. Reemplaza los tres resolvers usando `dataloader(MyApp.Repo)`:
   - `author`: asociación `belongs_to`
   - `comments`: asociación `has_many`
   - `comment_count`: usa `dataloader` con una query que cuente

Para `comment_count`, necesitas un source personalizado (no Ecto directo):

```elixir
# En MyApp.Loader
def data do
  Dataloader.KV.new(&fetch_counts/2)
end

defp fetch_counts(:comment_count, post_ids) do
  import Ecto.Query

  counts =
    from(c in MyApp.Blog.Comment,
      where: c.post_id in ^MapSet.to_list(post_ids),
      group_by: c.post_id,
      select: {c.post_id, count(c.id)}
    )
    |> MyApp.Repo.all()
    |> Map.new()

  # Retorna un mapa post_id -> count (0 para posts sin comentarios)
  Map.new(post_ids, fn id -> {id, Map.get(counts, id, 0)} end)
end
```

### Ejercicio 2: Sistema de autorización por campo

Implementa un sistema de autorización donde ciertos campos solo son visibles para ciertos usuarios.

**Requisito:** El campo `email` de `User` solo debe ser visible para el propio usuario o para admins. Si otro usuario intenta verlo, debe recibir `nil` (no un error — el campo es opcional).

```elixir
# lib/my_app_web/middleware/field_policy.ex
defmodule MyAppWeb.Middleware.FieldPolicy do
  @behaviour Absinthe.Middleware

  @impl true
  def call(resolution, policy_fn) do
    # TODO: llamar policy_fn con (resolution.source, context)
    # Si retorna :ok -> continuar con resolution
    # Si retorna {:error, :unauthorized} -> poner nil como resultado (no error)
    # Si retorna {:error, message} -> poner error en resolution
  end
end
```

```elixir
# Uso en el type
object :user do
  field :id,   :id
  field :name, :string

  field :email, :string do
    middleware MyAppWeb.Middleware.FieldPolicy, fn user, %{current_user: current_user} ->
      cond do
        is_nil(current_user)                 -> {:error, :unauthorized}
        current_user.id == user.id           -> :ok
        current_user.role == :admin          -> :ok
        true                                 -> {:error, :unauthorized}
      end
    end

    resolve fn user, _, _ -> {:ok, user.email} end
  end

  field :role, :user_role do
    middleware MyAppWeb.Middleware.FieldPolicy, fn _user, %{current_user: current_user} ->
      if current_user && current_user.role == :admin, do: :ok, else: {:error, :unauthorized}
    end

    resolve fn user, _, _ -> {:ok, user.role} end
  end
end
```

**Tarea:** Implementa `FieldPolicy` y escribe tests que verifiquen:
1. El propio usuario puede ver su email
2. Un admin puede ver el email de cualquier usuario
3. Otro usuario recibe `null` en el campo email (sin error GraphQL)
4. Un usuario sin auth recibe `null` en el campo email

### Ejercicio 3: Scalar DateTime con zona horaria

Implementa un scalar `datetime_with_tz` que:
- Serializa a RFC 3339 con zona horaria (ej: `"2024-01-15T10:30:00+02:00"`)
- Parsea strings RFC 3339 a `DateTime`
- En la serialización, convierte siempre a UTC antes de mostrar
- Retorna `:error` para strings que no sean fechas válidas

```elixir
scalar :datetime_with_tz, description: "RFC 3339 datetime con timezone" do
  serialize fn datetime ->
    # TODO: convertir a UTC y formatear como RFC 3339
  end

  parse fn
    %Absinthe.Blueprint.Input.String{value: value} ->
      # TODO: parsear RFC 3339, retornar {:ok, %DateTime{}} o :error

    _ -> :error
  end
end
```

Tests que debe pasar:

```elixir
test "serializa DateTime a RFC 3339 UTC" do
  dt = ~U[2024-01-15 10:30:00Z]
  assert MyAppWeb.Schema.Types.Scalars.serialize_datetime(dt) == "2024-01-15T10:30:00Z"
end

test "parsea string RFC 3339 valido" do
  assert {:ok, %DateTime{}} =
    MyAppWeb.Schema.Types.Scalars.parse_datetime("2024-01-15T10:30:00+02:00")
end

test "retorna error para string invalido" do
  assert :error = MyAppWeb.Schema.Types.Scalars.parse_datetime("not-a-date")
end
```

## Expected Results

Con DataLoader activo, una query que pide 100 posts con autores, comentarios y conteos debería ejecutar exactamente:
- 1 query para posts
- 1 query para usuarios (batch de todos los `author_id`)
- 1 query para comentarios (batch de todos los `post_id`)
- 1 query para conteos (batch agregado)

Total: 4 queries independientemente de cuántos posts haya.

El middleware de auth debe aplicarse automáticamente a todas las mutations sin repetir código campo por campo.

Los scalars custom deben ser transparentes para el resolver: el resolver trabaja con tipos Elixir nativos (`%DateTime{}`, `%Date{}`), y Absinthe maneja la serialización/deserialización.

## Key Takeaways

- DataLoader resuelve N+1 acumulando claves y cargándolas en batch al finalizar cada nivel del árbol GraphQL
- `Absinthe.Middleware.Dataloader` debe estar en la lista de `plugins/0` del schema — sin esto DataLoader no funciona
- El middleware se ejecuta en orden: `[Authenticate, Authorize, resolver_fn, HandleErrors]`
- El callback `middleware/3` del schema permite aplicar middleware globalmente por tipo de objeto (queries, mutations, subscriptions)
- Para campos opcionales con auth, prefer `nil` sobre error — es mejor UX para el cliente
- Los scalars desacoplan la representación (string en el wire) de la implementación (tipo Elixir nativo)
- Complexity analysis es una defensa necesaria en APIs públicas: sin límite, una query anidada puede explotar la BD
