# 29 - Ecto Básico

## Prerequisites

- Elixir intermedio: módulos, structs, pattern matching
- PostgreSQL instalado y corriendo localmente
- Familiaridad con Mix y dependencias Hex
- Conceptos básicos de bases de datos relacionales (tablas, columnas, claves foráneas)

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Configurar Ecto con PostgreSQL en un proyecto Mix
2. Definir schemas con `@primary_key`, tipos y asociaciones
3. Validar datos con changesets usando `cast/3`, `validate_required/3`, `validate_format/3`
4. Escribir migrations reproducibles con `Ecto.Migration`
5. Consultar la base de datos con el DSL de `Ecto.Query`
6. Usar `Repo.get/2`, `Repo.all/1`, `Repo.insert/1`, `Repo.update/1`, `Repo.delete/1`
7. Construir filtros dinámicos sobre queries base

---

## Concepts

### Repo: el punto de entrada a la base de datos

`Ecto.Repo` es el módulo que maneja la conexión al adaptador (Postgrex para PostgreSQL). Toda operación de base de datos pasa por él.

```elixir
# config/config.exs
import Config

config :mi_app, MiApp.Repo,
  username: "postgres",
  password: "postgres",
  hostname: "localhost",
  database: "mi_app_dev",
  stacktrace: true,
  show_sensitive_data_on_connection_error: true,
  pool_size: 10
```

```elixir
# lib/mi_app/repo.ex
defmodule MiApp.Repo do
  use Ecto.Repo,
    otp_app: :mi_app,
    adapter: Ecto.Adapters.Postgres
end
```

```elixir
# lib/mi_app/application.ex
defmodule MiApp.Application do
  use Application

  def start(_type, _args) do
    children = [MiApp.Repo]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Schema: mapeo entre tablas y structs

`Ecto.Schema` declara la estructura de los datos y cómo se mapean a columnas de la base de datos.

```elixir
defmodule MiApp.Accounts.User do
  use Ecto.Schema
  import Ecto.Changeset

  # Sobrescribir la clave primaria por defecto
  @primary_key {:id, :binary_id, autogenerate: true}
  # Indicar el tipo de claves foráneas en asociaciones
  @foreign_key_type :binary_id

  schema "users" do
    field :name,  :string
    field :email, :string
    field :age,   :integer
    field :role,  :string, default: "user"

    # Campo virtual: no se persiste en la base de datos
    field :password, :string, virtual: true
    field :password_hash, :string

    timestamps()  # inserted_at y updated_at automáticos
  end
end
```

### Changeset: validación y transformación de datos

El changeset es el mecanismo de Ecto para validar y castear datos externos antes de persistirlos. Nunca se manipulan structs directamente para operaciones de escritura.

```elixir
defmodule MiApp.Accounts.User do
  # ... schema arriba ...

  def changeset(user, attrs) do
    user
    |> cast(attrs, [:name, :email, :age, :role, :password])
    |> validate_required([:name, :email])
    |> validate_format(:email, ~r/^[^\s]+@[^\s]+\.[^\s]+$/,
         message: "debe ser un email válido")
    |> validate_length(:name, min: 2, max: 100)
    |> validate_number(:age, greater_than: 0, less_than: 150)
    |> validate_inclusion(:role, ["user", "admin", "moderator"])
    |> unique_constraint(:email)
    |> put_password_hash()
  end

  defp put_password_hash(changeset) do
    case get_change(changeset, :password) do
      nil -> changeset
      password -> put_change(changeset, :password_hash, hash(password))
    end
  end

  defp hash(password), do: :crypto.hash(:sha256, password) |> Base.encode16()
end
```

### Migrations: evolución del esquema de base de datos

```elixir
# priv/repo/migrations/20240101000000_create_users.exs
defmodule MiApp.Repo.Migrations.CreateUsers do
  use Ecto.Migration

  def change do
    create table(:users, primary_key: false) do
      add :id,            :binary_id, primary_key: true
      add :name,          :string,    null: false
      add :email,         :string,    null: false
      add :age,           :integer
      add :role,          :string,    default: "user", null: false
      add :password_hash, :string

      timestamps()
    end

    create unique_index(:users, [:email])
    create index(:users, [:role])
  end
end
```

```bash
mix ecto.create
mix ecto.migrate
# Para deshacer la última migration:
mix ecto.rollback
```

### Ecto.Query: consultas DSL

```elixir
import Ecto.Query

# Forma de keyword list
query = from u in "users",
          where: u.role == "admin",
          select: %{id: u.id, name: u.name}

# Forma de pipe (composable)
query =
  MiApp.Accounts.User
  |> where([u], u.age > 18)
  |> order_by([u], asc: u.name)
  |> limit(10)
  |> select([u], {u.id, u.name, u.email})

# Ejecutar
MiApp.Repo.all(query)
```

### Operaciones básicas del Repo

```elixir
alias MiApp.{Repo, Accounts.User}
import Ecto.Query

# Insertar
{:ok, user} =
  %User{}
  |> User.changeset(%{name: "Ana", email: "ana@example.com", age: 30})
  |> Repo.insert()

# Obtener por ID (devuelve nil si no existe)
user = Repo.get(User, "uuid-aqui")

# Obtener o lanzar excepción
user = Repo.get!(User, "uuid-aqui")

# Buscar por campo
user = Repo.get_by(User, email: "ana@example.com")

# Listar todos
users = Repo.all(User)

# Actualizar
{:ok, updated} =
  user
  |> User.changeset(%{name: "Ana García"})
  |> Repo.update()

# Eliminar
{:ok, deleted} = Repo.delete(user)
```

### Asociaciones

```elixir
defmodule MiApp.Blog.Post do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "posts" do
    field :title,   :string
    field :body,    :string
    field :published, :boolean, default: false

    belongs_to :author, MiApp.Accounts.User
    many_to_many :tags, MiApp.Blog.Tag, join_through: "posts_tags"

    timestamps()
  end

  def changeset(post, attrs) do
    post
    |> cast(attrs, [:title, :body, :published, :author_id])
    |> validate_required([:title, :body, :author_id])
    |> validate_length(:title, min: 5, max: 200)
    |> foreign_key_constraint(:author_id)
  end
end
```

```elixir
# Preloading de asociaciones
post = Repo.get!(Post, id) |> Repo.preload([:author, :tags])
post.author.name  # ya cargado, no lazy-load
```

---

## Exercises

### Ejercicio 1: User schema con validaciones de email

Implementa el módulo `Accounts.User` completo con validaciones robustas.

```elixir
# lib/mi_app/accounts/user.ex
defmodule MiApp.Accounts.User do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  # TODO: Define el schema con los campos:
  # - name (string, requerido)
  # - email (string, requerido, único)
  # - age (integer, opcional)
  # - role (string, default: "user")
  # - password (virtual, string)
  # - password_hash (string)
  # - timestamps()
  schema "users" do
  end

  @doc """
  Changeset para creación de usuario.
  Valida: name (2-100 chars), email (formato), age (1-150), role (enum).
  """
  def changeset(user, attrs) do
    # TODO: implementar con cast, validate_required, validate_format para email,
    # validate_length para name, validate_number para age,
    # validate_inclusion para role, unique_constraint para email
  end

  @doc """
  Changeset para actualización parcial (solo campos provistos).
  """
  def update_changeset(user, attrs) do
    # TODO: similar a changeset/2 pero sin validate_required
    # (permite actualizaciones parciales)
  end

  # Prueba en iex:
  # alias MiApp.Accounts.User
  # cs = User.changeset(%User{}, %{name: "Ana", email: "ana@example.com", age: 25})
  # cs.valid?   # true
  # cs.errors   # []
  #
  # bad = User.changeset(%User{}, %{name: "A", email: "no-es-email"})
  # bad.valid?  # false
  # bad.errors  # [{:name, ...}, {:email, ...}]
end
```

```elixir
# priv/repo/migrations/20240101000001_create_users.exs
defmodule MiApp.Repo.Migrations.CreateUsers do
  use Ecto.Migration

  def change do
    # TODO: crear tabla "users" con los campos del schema
    # Incluir: unique_index en email, index en role
  end
end
```

---

### Ejercicio 2: Blog con Posts y Tags (asociaciones)

Implementa el sistema de blog con asociaciones `belongs_to`, `has_many`, y `many_to_many`.

```elixir
# lib/mi_app/blog/tag.ex
defmodule MiApp.Blog.Tag do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "tags" do
    field :name, :string
    field :slug, :string

    # TODO: declarar asociación many_to_many con Post
    # usando la tabla join "posts_tags"

    timestamps()
  end

  def changeset(tag, attrs) do
    tag
    |> cast(attrs, [:name])
    |> validate_required([:name])
    |> validate_length(:name, min: 1, max: 50)
    # TODO: generar slug automáticamente desde name usando put_change
    # slug = name |> String.downcase() |> String.replace(~r/[^a-z0-9]+/, "-")
    |> unique_constraint(:slug)
  end
end
```

```elixir
# lib/mi_app/blog/post.ex
defmodule MiApp.Blog.Post do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id

  schema "posts" do
    field :title,     :string
    field :body,      :string
    field :published, :boolean, default: false

    # TODO: belongs_to :author, MiApp.Accounts.User
    # TODO: many_to_many :tags, MiApp.Blog.Tag, join_through: "posts_tags"

    timestamps()
  end

  def changeset(post, attrs) do
    # TODO: cast [:title, :body, :published, :author_id]
    # validate_required [:title, :body, :author_id]
    # validate_length :title, min: 5, max: 200
    # foreign_key_constraint :author_id
  end

  @doc """
  Changeset que permite asociar tags al post.
  """
  def with_tags_changeset(post, attrs, tags) do
    post
    |> changeset(attrs)
    # TODO: usar put_assoc(:tags, tags) para asociar los tags
  end
end
```

```elixir
# priv/repo/migrations/20240101000002_create_blog.exs
defmodule MiApp.Repo.Migrations.CreateBlog do
  use Ecto.Migration

  def change do
    create table(:tags, primary_key: false) do
      # TODO: campos id (binary_id), name, slug
      timestamps()
    end

    # TODO: unique_index en tags.slug

    create table(:posts, primary_key: false) do
      # TODO: campos id, title, body, published, references(:users)
      timestamps()
    end

    # TODO: tabla join posts_tags con post_id y tag_id
    # Índice único compuesto (post_id, tag_id)
  end
end
```

```elixir
# Uso esperado en iex:
# alias MiApp.{Repo, Accounts.User, Blog.Post, Blog.Tag}
# import Ecto.Query
#
# {:ok, user} = %User{} |> User.changeset(%{name: "Ana", email: "ana@ex.com", age: 30}) |> Repo.insert()
# {:ok, tag}  = %Tag{}  |> Tag.changeset(%{name: "Elixir"}) |> Repo.insert()
# {:ok, post} = %Post{} |> Post.with_tags_changeset(%{title: "Intro a Ecto", body: "...", author_id: user.id}, [tag]) |> Repo.insert()
#
# post_cargado = Repo.get!(Post, post.id) |> Repo.preload([:author, :tags])
# post_cargado.author.name  # "Ana"
# post_cargado.tags |> Enum.map(& &1.name)  # ["Elixir"]
```

---

### Ejercicio 3: Búsqueda con filtros dinámicos

Implementa un sistema de filtrado dinámico donde los filtros se componen sobre una query base.

```elixir
# lib/mi_app/blog/query.ex
defmodule MiApp.Blog.Query do
  import Ecto.Query
  alias MiApp.Blog.Post

  @doc """
  Construye una query filtrada dinámicamente.

  Filtros soportados:
  - :published (boolean) — solo posts publicados/no publicados
  - :author_id (binary_id) — posts de un autor específico
  - :tag_slug (string) — posts con un tag específico
  - :search (string) — búsqueda en título (ILIKE)
  - :order_by (:asc | :desc) — ordenar por inserted_at
  - :limit (integer) — limitar resultados

  ## Ejemplo
      iex> filters = [published: true, search: "ecto", limit: 5]
      iex> Query.filter(filters) |> Repo.all()
  """
  def filter(filters) when is_list(filters) do
    # Query base: todos los posts con autor precargado
    base_query = from p in Post, preload: [:author, :tags]

    # TODO: reducir los filtros sobre la query base
    # Pista: Enum.reduce(filters, base_query, fn {key, value}, query ->
    #           apply_filter(query, key, value)
    #        end)
  end

  # TODO: implementar cada cláusula apply_filter/3:

  defp apply_filter(query, :published, value) do
    # TODO: where published == ^value
  end

  defp apply_filter(query, :author_id, author_id) do
    # TODO: where author_id == ^author_id
  end

  defp apply_filter(query, :tag_slug, slug) do
    # TODO: join con tags donde tags.slug == ^slug
    # Pista: join :inner, usando many_to_many a través de posts_tags
  end

  defp apply_filter(query, :search, term) do
    # TODO: where ilike(p.title, ^"%#{term}%")
  end

  defp apply_filter(query, :order_by, :asc) do
    # TODO: order_by [asc: :inserted_at]
  end

  defp apply_filter(query, :order_by, :desc) do
    # TODO: order_by [desc: :inserted_at]
  end

  defp apply_filter(query, :limit, n) do
    # TODO: limit ^n
  end

  # Filtro desconocido: ignorar silenciosamente
  defp apply_filter(query, _key, _value), do: query
end
```

```elixir
# Uso esperado:
# alias MiApp.{Repo, Blog.Query}
#
# Query.filter([published: true, search: "elixir", limit: 10, order_by: :desc])
# |> Repo.all()
# |> Enum.each(fn p -> IO.puts("#{p.title} — #{p.author.name}") end)
```

---

## Common Mistakes

**1. Manipular structs directamente en lugar de usar changesets**

```elixir
# MAL: salta las validaciones
user = %User{email: "no-es-email"}
Repo.insert(user)

# BIEN: el changeset rechaza el email inválido
{:error, changeset} =
  %User{}
  |> User.changeset(%{email: "no-es-email"})
  |> Repo.insert()
```

**2. Olvidar `preload` antes de acceder asociaciones**

```elixir
# MAL: lanza %Ecto.Association.NotLoaded{}
post = Repo.get!(Post, id)
post.author.name  # error!

# BIEN
post = Repo.get!(Post, id) |> Repo.preload(:author)
post.author.name  # "Ana"
```

**3. N+1 queries**

```elixir
# MAL: una query por post para cargar el autor
posts = Repo.all(Post)
Enum.each(posts, fn p ->
  p = Repo.preload(p, :author)  # N queries!
  IO.puts(p.author.name)
end)

# BIEN: preload en batch
posts = Post |> Repo.all() |> Repo.preload(:author)  # 2 queries
```

**4. Confundir `cast` con `put_change`**

```elixir
# cast: para datos externos (usuario, API) — aplica filtering de campos
# put_change: para datos internos del sistema — siempre se aplica
changeset
|> cast(attrs, [:name, :email])       # solo acepta estos campos de attrs
|> put_change(:slug, slugify(name))   # campo calculado internamente
```

**5. Migrations irreversibles sin `down`**

```elixir
# Si usas `up/down` en lugar de `change`, define ambos:
def up do
  alter table(:users) do
    add :verified, :boolean, default: false
  end
end

def down do
  alter table(:users) do
    remove :verified
  end
end
```

---

## Verification

```bash
# Setup inicial
mix deps.get
mix ecto.create
mix ecto.migrate

# En iex -S mix
alias MiApp.{Repo, Accounts.User, Blog.Post, Blog.Tag, Blog.Query}
import Ecto.Query

# Verificar inserción con validación
cs = User.changeset(%User{}, %{name: "Ana", email: "ana@example.com", age: 30})
cs.valid?   # true

{:ok, user} = Repo.insert(cs)
user.id     # UUID generado

# Verificar validación fallida
bad = User.changeset(%User{}, %{name: "A", email: "malformado"})
bad.valid?         # false
bad.errors         # [{:name, {"should be at least %{count} character(s)", ...}}, {:email, ...}]

# Verificar query básica
from(u in User, where: u.age > 18) |> Repo.all() |> length()  # >= 1

# Verificar filtros dinámicos
Query.filter([published: true, limit: 5]) |> Repo.all()
```

---

## Summary

Ecto separa claramente tres responsabilidades:

| Componente | Responsabilidad |
|------------|----------------|
| `Schema` | Estructura de datos, mapeo tabla ↔ struct |
| `Changeset` | Validación y transformación de datos externos |
| `Repo` | Acceso a base de datos (queries, insert, update, delete) |
| `Migration` | Evolución del esquema de base de datos |

Los changesets son el núcleo de la seguridad de datos: nunca persistas datos sin pasar por uno. Las queries se componen de forma funcional con el pipe operator, lo que permite filtros dinámicos elegantes.

---

## What's Next

- **30**: Process Dictionary — estado por proceso con `:erlang.put/get`
- **Ecto.Multi**: transacciones atómicas con múltiples operaciones
- **Ecto.Repo.transaction/2**: transacciones manuales
- **Fragmentos SQL**: `fragment("lower(?)", u.email)` para funciones nativas
- **Ecto.Query.API**: funciones agregadas (`count`, `sum`, `avg`, `max`)

---

## Resources

- [Ecto Getting Started](https://hexdocs.pm/ecto/getting-started.html)
- [Ecto.Changeset docs](https://hexdocs.pm/ecto/Ecto.Changeset.html)
- [Ecto.Query DSL](https://hexdocs.pm/ecto/Ecto.Query.html)
- [Programming Ecto (Pragmatic Bookshelf)](https://pragprog.com/titles/wmecto/programming-ecto/)
- [Postgrex adapter](https://hexdocs.pm/postgrex/readme.html)
