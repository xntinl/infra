# 59. Absinthe GraphQL — Schema y Resolvers

**Difficulty**: Avanzado

## Prerequisites
- Phoenix framework y Plug
- Ecto y changesets
- Pattern matching avanzado
- Comprensión de APIs REST (para comparar con GraphQL)

## Learning Objectives
After completing this exercise, you will be able to:
- Definir un schema GraphQL completo con `use Absinthe.Schema`
- Crear types: `object`, `input_object`, `enum`, y `scalar`
- Implementar queries con argumentos y paginación
- Implementar mutations que mapean errores de changeset a errores GraphQL
- Configurar subscriptions para actualizaciones en tiempo real
- Manejar resolvers con contexto y error handling idiomático

## Concepts

### GraphQL vs REST: El cambio de paradigma

REST expone recursos fijos. El cliente recibe lo que el servidor decide. GraphQL invierte esto: el cliente declara exactamente qué datos necesita y el servidor devuelve exactamente eso. Ni más ni menos.

El trade-off clave: REST es simple de cachear (HTTP cache funciona por URL), GraphQL es más flexible pero requiere caché explícita (persisted queries, CDN layer). Para aplicaciones con muchos tipos de clientes (web, mobile, partners) GraphQL gana claramente.

Absinthe es la implementación GraphQL más madura para Elixir. Está construida sobre Plug y se integra nativamente con Phoenix.

### Setup del proyecto

```elixir
# mix.exs
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:absinthe, "~> 1.7"},
    {:absinthe_plug, "~> 1.5"},
    {:absinthe_phoenix, "~> 2.0"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, ">= 0.0.0"}
  ]
end
```

```elixir
# lib/my_app_web/router.ex
defmodule MyAppWeb.Router do
  use Phoenix.Router
  use Absinthe.Phoenix.Router

  pipeline :api do
    plug :accepts, ["json"]
  end

  scope "/api" do
    pipe_through :api

    forward "/graphql", Absinthe.Plug,
      schema: MyAppWeb.Schema,
      context: &MyAppWeb.Schema.build_context/1
  end

  # GraphiQL — UI interactiva para desarrollo
  if Mix.env() == :dev do
    forward "/graphiql", Absinthe.Plug.GraphiQL,
      schema: MyAppWeb.Schema,
      socket: MyAppWeb.UserSocket
  end
end
```

### Schema principal: el contrato de la API

El schema es el punto central de Absinthe. Define todos los types disponibles, qué queries existen, qué mutations el cliente puede ejecutar, y qué subscriptions puede escuchar.

```elixir
# lib/my_app_web/schema.ex
defmodule MyAppWeb.Schema do
  use Absinthe.Schema

  import_types MyAppWeb.Schema.Types.Post
  import_types MyAppWeb.Schema.Types.User
  import_types MyAppWeb.Schema.Types.Scalars

  query do
    import_fields :post_queries
    import_fields :user_queries
  end

  mutation do
    import_fields :post_mutations
  end

  subscription do
    import_fields :post_subscriptions
  end

  # Inyecta current_user en el contexto de todos los resolvers
  def build_context(conn) do
    %{current_user: conn.assigns[:current_user]}
  end
end
```

### Types: la anatomía de los datos

```elixir
# lib/my_app_web/schema/types/post.ex
defmodule MyAppWeb.Schema.Types.Post do
  use Absinthe.Schema.Notation

  enum :post_status do
    value :draft,     description: "No publicado aún"
    value :published, description: "Visible para todos"
    value :archived,  description: "Retirado"
  end

  object :post do
    field :id,         :id
    field :title,      :string
    field :body,       :string
    field :status,     :post_status
    field :view_count, :integer
    field :inserted_at, :datetime

    # Resolver inline: calcula datos derivados
    field :excerpt, :string do
      resolve fn post, _, _ ->
        excerpt = post.body |> String.slice(0, 200) |> Kernel.<>("...")
        {:ok, excerpt}
      end
    end

    # Resolver de asociación: cargado bajo demanda
    # (sin DataLoader esto causa N+1 — ver ejercicio 60)
    field :author, :user do
      resolve fn post, _, _ ->
        user = MyApp.Accounts.get_user!(post.author_id)
        {:ok, user}
      end
    end

    field :tags, list_of(:tag) do
      resolve fn post, _, _ ->
        {:ok, MyApp.Blog.list_tags_for_post(post.id)}
      end
    end
  end

  object :tag do
    field :id,   :id
    field :name, :string
    field :slug, :string
  end

  # Input types: solo se usan en argumentos de mutations
  input_object :create_post_input do
    field :title,  non_null(:string)
    field :body,   non_null(:string)
    field :status, :post_status, default_value: :draft
    field :tags,   list_of(non_null(:string))
  end

  input_object :update_post_input do
    field :title,  :string
    field :body,   :string
    field :status, :post_status
  end

  # Tipo de respuesta paginada
  object :post_connection do
    field :entries,    list_of(:post)
    field :total,      :integer
    field :page,       :integer
    field :page_size,  :integer
    field :total_pages, :integer
  end

  object :post_queries do
    @desc "Lista posts con paginación"
    field :posts, :post_connection do
      arg :page,      :integer, default_value: 1
      arg :page_size, :integer, default_value: 10
      arg :status,    :post_status
      arg :author_id, :id

      resolve &MyAppWeb.Resolvers.Post.list_posts/3
    end

    @desc "Obtiene un post por ID"
    field :post, :post do
      arg :id, non_null(:id)
      resolve &MyAppWeb.Resolvers.Post.get_post/3
    end

    @desc "Busca posts por texto"
    field :search_posts, list_of(:post) do
      arg :query, non_null(:string)
      resolve &MyAppWeb.Resolvers.Post.search_posts/3
    end
  end

  object :post_mutations do
    field :create_post, :post do
      arg :input, non_null(:create_post_input)
      resolve &MyAppWeb.Resolvers.Post.create_post/3
    end

    field :update_post, :post do
      arg :id,    non_null(:id)
      arg :input, non_null(:update_post_input)
      resolve &MyAppWeb.Resolvers.Post.update_post/3
    end

    field :delete_post, :boolean do
      arg :id, non_null(:id)
      resolve &MyAppWeb.Resolvers.Post.delete_post/3
    end

    field :publish_post, :post do
      arg :id, non_null(:id)
      resolve &MyAppWeb.Resolvers.Post.publish_post/3
    end
  end

  object :post_subscriptions do
    field :post_added, :post do
      config fn _, _ -> {:ok, topic: "posts"} end
    end

    field :post_updated, :post do
      arg :id, non_null(:id)

      config fn args, _ ->
        {:ok, topic: "posts:#{args.id}"}
      end
    end
  end
end
```

### Resolvers: la lógica de negocio

Los resolvers reciben siempre tres argumentos: `parent` (el objeto padre del campo), `args` (argumentos pasados en la query), y `resolution` (contexto de Absinthe, incluyendo el contexto de conexión).

```elixir
# lib/my_app_web/resolvers/post_resolver.ex
defmodule MyAppWeb.Resolvers.Post do
  alias MyApp.Blog
  alias MyApp.Blog.Post

  def list_posts(_, args, _) do
    result = Blog.list_posts(
      page:      args.page,
      page_size: args.page_size,
      status:    Map.get(args, :status),
      author_id: Map.get(args, :author_id)
    )
    {:ok, result}
  end

  def get_post(_, %{id: id}, _) do
    case Blog.get_post(id) do
      nil  -> {:error, "Post #{id} no encontrado"}
      post -> {:ok, post}
    end
  end

  def search_posts(_, %{query: query}, _) do
    {:ok, Blog.search_posts(query)}
  end

  def create_post(_, %{input: input}, %{context: %{current_user: user}})
      when not is_nil(user) do
    input
    |> Map.put(:author_id, user.id)
    |> Blog.create_post()
    |> handle_result()
  end

  def create_post(_, _, _), do: {:error, "Debes iniciar sesión"}

  def update_post(_, %{id: id, input: input}, %{context: %{current_user: user}})
      when not is_nil(user) do
    with {:ok, post} <- fetch_post(id),
         :ok         <- authorize_edit(post, user) do
      post
      |> Blog.update_post(input)
      |> handle_result()
    end
  end

  def delete_post(_, %{id: id}, %{context: %{current_user: user}})
      when not is_nil(user) do
    with {:ok, post} <- fetch_post(id),
         :ok         <- authorize_edit(post, user),
         {:ok, _}    <- Blog.delete_post(post) do
      {:ok, true}
    end
  end

  def publish_post(_, %{id: id}, %{context: %{current_user: user}})
      when not is_nil(user) do
    with {:ok, post} <- fetch_post(id),
         :ok         <- authorize_edit(post, user) do
      post
      |> Blog.update_post(%{status: :published})
      |> handle_result()
      |> tap(fn {:ok, _} ->
        Absinthe.Subscription.publish(
          MyAppWeb.Endpoint,
          post,
          post_added: "posts"
        )
      end)
    end
  end

  defp fetch_post(id) do
    case Blog.get_post(id) do
      nil  -> {:error, "Post #{id} no encontrado"}
      post -> {:ok, post}
    end
  end

  defp authorize_edit(%Post{author_id: author_id}, %{id: user_id})
       when author_id == user_id, do: :ok
  defp authorize_edit(_, _), do: {:error, "No tienes permiso para editar este post"}

  # Mapea errores de changeset a mensajes legibles
  defp handle_result({:ok, post}), do: {:ok, post}

  defp handle_result({:error, %Ecto.Changeset{} = changeset}) do
    errors =
      Ecto.Changeset.traverse_errors(changeset, fn {msg, opts} ->
        Enum.reduce(opts, msg, fn {key, value}, acc ->
          String.replace(acc, "%{#{key}}", to_string(value))
        end)
      end)

    {:error,
     message: "Validación fallida",
     extensions: %{
       code: :validation_error,
       fields: errors
     }}
  end
end
```

### Scalars custom

Absinthe tiene scalars built-in para tipos Elixir comunes, pero a veces necesitas definir los tuyos:

```elixir
# lib/my_app_web/schema/types/scalars.ex
defmodule MyAppWeb.Schema.Types.Scalars do
  use Absinthe.Schema.Notation

  scalar :datetime, description: "ISO8601 datetime string" do
    serialize fn
      %DateTime{} = dt -> DateTime.to_iso8601(dt)
      %NaiveDateTime{} = dt -> NaiveDateTime.to_iso8601(dt)
    end

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} ->
        case DateTime.from_iso8601(value) do
          {:ok, dt, _offset} -> {:ok, dt}
          {:error, _}        -> :error
        end

      _ -> :error
    end
  end

  scalar :uuid, description: "UUID v4 string" do
    serialize &to_string/1

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} ->
        case Ecto.UUID.cast(value) do
          {:ok, uuid} -> {:ok, uuid}
          :error      -> :error
        end

      _ -> :error
    end
  end

  scalar :json, description: "Arbitrary JSON value" do
    serialize &Jason.encode!/1

    parse fn
      %Absinthe.Blueprint.Input.String{value: value} ->
        Jason.decode(value)

      %Absinthe.Blueprint.Input.Object{} = object ->
        {:ok, Absinthe.Blueprint.Input.Object.to_map(object)}

      _ -> :error
    end
  end
end
```

### Subscriptions con Phoenix Channels

```elixir
# lib/my_app_web/user_socket.ex
defmodule MyAppWeb.UserSocket do
  use Phoenix.Socket
  use Absinthe.Phoenix.Socket, schema: MyAppWeb.Schema

  @impl true
  def connect(%{"token" => token}, socket, _connect_info) do
    case MyApp.Auth.verify_token(token) do
      {:ok, user_id} ->
        user = MyApp.Accounts.get_user!(user_id)
        socket = Absinthe.Phoenix.Socket.put_options(socket, context: %{current_user: user})
        {:ok, socket}

      {:error, _} ->
        :error
    end
  end

  def connect(_, _socket, _), do: :error

  @impl true
  def id(_socket), do: nil
end
```

```elixir
# Publicar desde cualquier parte del sistema
defmodule MyApp.Blog do
  def create_post(attrs) do
    %Post{}
    |> Post.changeset(attrs)
    |> Repo.insert()
    |> tap(fn
      {:ok, post} ->
        Absinthe.Subscription.publish(
          MyAppWeb.Endpoint,
          post,
          post_added: "posts"
        )
      _ -> :ok
    end)
  end
end
```

### Testing de schemas Absinthe

```elixir
defmodule MyAppWeb.Schema.PostTest do
  use MyAppWeb.ConnCase

  @list_posts_query """
  query ListPosts($page: Int, $status: PostStatus) {
    posts(page: $page, status: $status) {
      entries {
        id
        title
        status
        author {
          id
          name
        }
      }
      total
      page
    }
  }
  """

  @create_post_mutation """
  mutation CreatePost($input: CreatePostInput!) {
    createPost(input: $input) {
      id
      title
      status
    }
  }
  """

  test "list_posts retorna posts publicados", %{conn: conn} do
    user = insert(:user)
    insert_list(3, :post, status: :published, author: user)
    insert(:post, status: :draft, author: user)

    conn = authenticate(conn, user)

    response =
      conn
      |> post("/api/graphql", %{
        query: @list_posts_query,
        variables: %{status: "PUBLISHED"}
      })
      |> json_response(200)

    assert is_nil(response["errors"])
    assert response["data"]["posts"]["total"] == 3
    assert length(response["data"]["posts"]["entries"]) == 3
  end

  test "create_post con input invalido retorna errores de validacion", %{conn: conn} do
    user = insert(:user)
    conn = authenticate(conn, user)

    response =
      conn
      |> post("/api/graphql", %{
        query: @create_post_mutation,
        variables: %{input: %{title: "", body: "x"}}
      })
      |> json_response(200)

    assert [error] = response["errors"]
    assert error["extensions"]["code"] == "VALIDATION_ERROR"
    assert error["extensions"]["fields"]["title"]
  end

  test "create_post sin auth retorna error", %{conn: conn} do
    response =
      conn
      |> post("/api/graphql", %{
        query: @create_post_mutation,
        variables: %{input: %{title: "Test", body: "Body"}}
      })
      |> json_response(200)

    assert [error] = response["errors"]
    assert error["message"] =~ "sesión"
  end
end
```

## Exercises

### Ejercicio 1: Blog API completa

Construye un schema Absinthe para una API de blog con las siguientes especificaciones.

**Schema requerido:**

El sistema gestiona `Post`, `User`, y `Comment`. Un post tiene autor (User), y puede tener muchos comentarios. Cada comentario tiene autor y pertenece a un post.

```
# Queries que debes implementar:
posts(page, pageSize, status, authorId) -> PostConnection
post(id) -> Post
user(id) -> User
me -> User  # current_user desde context

# Mutations:
createPost(input: CreatePostInput!) -> Post
updatePost(id: ID!, input: UpdatePostInput!) -> Post
deletePost(id: ID!) -> Boolean
createComment(postId: ID!, body: String!) -> Comment
```

**Restricciones:**
- `deletePost` solo lo puede ejecutar el autor o un admin
- `createComment` requiere autenticación
- Los posts en status `:draft` solo los ve el autor

**Pasos:**

1. Define el type `User` con campos: `id`, `name`, `email`, `role` (enum: `:user`, `:admin`), `posts` (lista de posts del usuario)

2. Define el type `Post` con campos: `id`, `title`, `body`, `status`, `insertedAt`, `author`, `comments`, `commentCount` (campo calculado)

3. Define el type `Comment` con: `id`, `body`, `author`, `post`, `insertedAt`

4. Implementa el resolver `list_posts/3` que:
   - Acepta `page`, `page_size`, `status`, `author_id`
   - Filtra posts draft si el `current_user` no es el autor
   - Retorna una struct con `entries`, `total`, `page`, `page_size`, `total_pages`

5. Implementa `create_post/3` que:
   - Requiere `current_user` en contexto
   - Llama a `Blog.create_post/1`
   - Mapea errores de changeset usando `Ecto.Changeset.traverse_errors/2`

```elixir
# Esqueleto del resolver — completa la implementación
defmodule MyAppWeb.Resolvers.Post do
  alias MyApp.{Blog, Accounts}

  def list_posts(_, args, %{context: context}) do
    # TODO: extraer current_user del context
    # TODO: aplicar filtros
    # TODO: retornar {:ok, %{entries: ..., total: ..., page: ..., page_size: ..., total_pages: ...}}
  end

  def get_post(_, %{id: id}, %{context: %{current_user: current_user}}) do
    # TODO: obtener post, verificar visibilidad (draft solo para autor/admin)
  end

  def create_post(_, %{input: input}, %{context: %{current_user: user}})
      when not is_nil(user) do
    # TODO: crear post, mapear errores de changeset
  end

  def create_post(_, _, _), do: {:error, "Autenticación requerida"}

  # TODO: implementar el resto de resolvers
end
```

### Ejercicio 2: Mutations con manejo de errores rico

Implementa el sistema de manejo de errores para que los clientes puedan distinguir tipos de error:

**Estructura de errores esperada:**

```json
{
  "errors": [
    {
      "message": "Validación fallida",
      "locations": [{"line": 2, "column": 3}],
      "path": ["createPost"],
      "extensions": {
        "code": "VALIDATION_ERROR",
        "fields": {
          "title": ["no puede estar vacío"],
          "body": ["debe tener al menos 10 caracteres"]
        }
      }
    }
  ]
}
```

Absinthe permite agregar extensiones a los errores usando el formato de mapa:

```elixir
{:error,
  message: "Validación fallida",
  extensions: %{
    code: :validation_error,
    fields: errors_map
  }
}
```

**Tarea:** Crea un módulo `MyAppWeb.Helpers.ErrorHelper` con una función `format_changeset_error/1` que convierta un changeset en el formato anterior. Úsala en todos los resolvers de mutation.

```elixir
defmodule MyAppWeb.Helpers.ErrorHelper do
  def format_changeset_error(%Ecto.Changeset{} = changeset) do
    # TODO: usar Ecto.Changeset.traverse_errors/2
    # TODO: retornar {:error, message: ..., extensions: %{code: ..., fields: ...}}
  end
end
```

### Ejercicio 3: Subscriptions en tiempo real

Implementa subscriptions para que los clientes puedan recibir actualizaciones en tiempo real.

**Subscriptions requeridas:**

```graphql
subscription {
  postAdded {
    id
    title
    author { name }
  }
}

subscription {
  commentAdded(postId: "123") {
    id
    body
    author { name }
  }
}

subscription {
  postUpdated(id: "456") {
    id
    title
    status
  }
}
```

**Implementación del schema:**

```elixir
object :post_subscriptions do
  field :post_added, :post do
    # Sin filtro — todos los posts nuevos
    config fn _, _ -> {:ok, topic: "posts:new"} end
  end

  field :comment_added, :comment do
    arg :post_id, non_null(:id)

    # Con filtro por post
    config fn %{post_id: post_id}, _ ->
      {:ok, topic: "posts:#{post_id}:comments"}
    end
  end

  field :post_updated, :post do
    arg :id, non_null(:id)

    config fn %{id: id}, _ ->
      {:ok, topic: "posts:#{id}:updated"}
    end

    # trigger: solo emite si el post realmente cambió
    trigger :update_post, topic: fn post ->
      "posts:#{post.id}:updated"
    end
  end
end
```

**Tarea:** Modifica `Blog.create_post/1` y `Blog.create_comment/2` para que publiquen al topic correspondiente después de cada operación exitosa. Usa `Absinthe.Subscription.publish/3`.

## Expected Results

Después de completar los ejercicios deberías poder ejecutar estas queries en GraphiQL:

```graphql
# Query con fragmentos
fragment PostDetails on Post {
  id
  title
  status
  insertedAt
  author {
    id
    name
  }
  commentCount
}

query GetPosts {
  posts(page: 1, pageSize: 5, status: PUBLISHED) {
    entries {
      ...PostDetails
    }
    total
    totalPages
  }
}

# Mutation
mutation {
  createPost(input: {
    title: "Introducción a Absinthe"
    body: "Absinthe es el framework GraphQL para Elixir..."
    status: DRAFT
  }) {
    id
    title
    status
  }
}

# Subscription (desde WebSocket)
subscription {
  postAdded {
    id
    title
    author { name }
  }
}
```

## Key Takeaways

- El schema Absinthe es el contrato público de la API: types, queries, mutations, subscriptions
- Los resolvers siempre retornan `{:ok, value}` o `{:error, reason}` — Absinthe traduce estos al formato GraphQL
- `input_object` para argumentos de mutations, `object` para tipos de respuesta
- Los errores de changeset deben mapearse explícitamente — Absinthe no los convierte automáticamente
- Las subscriptions usan topics string — `Absinthe.Subscription.publish/3` envía a todos los suscriptores de un topic
- El contexto (`%{context: %{current_user: user}}`) es la forma correcta de pasar información de autenticación a los resolvers
- Separar los types en módulos distintos con `import_types` mantiene el schema manejable
