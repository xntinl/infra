# Ejercicio 56 — Ecto Schemas Avanzados y Associations

**Nivel**: Avanzado  
**Tema**: Polymorphic associations, embeds, multi-tenancy, preload optimization

---

## Contexto

Los schemas básicos de Ecto (`belongs_to`, `has_many`) cubren el 80% de los casos.
El 20% restante — comentarios polimórficos, SaaS multi-tenant, datos embebidos, y
el problema del N+1 — aparece en cualquier aplicación real de producción. Este
ejercicio aborda esos patrones con implementaciones concretas.

---

## Parte 1 — Polymorphic Associations

### Problema

La plataforma tiene tres tipos de contenido (`Post`, `Photo`, `Video`) y todos
pueden recibir comentarios. El anti-patrón es crear `post_comments`, `photo_comments`,
`video_comments` por separado. El patrón polimórfico usa una sola tabla `comments`
con dos campos: `commentable_id` y `commentable_type`.

### Ecto no tiene polimorfismo nativo — lo implementamos manualmente

```elixir
# lib/platform/comment.ex
defmodule Platform.Comment do
  use Ecto.Schema
  import Ecto.Changeset

  schema "comments" do
    field :content,          :string
    field :commentable_id,   :integer
    field :commentable_type, :string   # "Post" | "Photo" | "Video"
    belongs_to :author, Platform.User
    timestamps()
  end

  def changeset(comment, attrs) do
    comment
    |> cast(attrs, [:content, :commentable_id, :commentable_type, :author_id])
    |> validate_required([:content, :commentable_id, :commentable_type])
    |> validate_inclusion(:commentable_type, ["Post", "Photo", "Video"])
  end
end
```

```sql
-- migration
CREATE TABLE comments (
  id               BIGSERIAL PRIMARY KEY,
  content          TEXT NOT NULL,
  commentable_id   INTEGER NOT NULL,
  commentable_type VARCHAR(50) NOT NULL,
  author_id        INTEGER REFERENCES users(id),
  inserted_at      TIMESTAMP NOT NULL,
  updated_at       TIMESTAMP NOT NULL
);
CREATE INDEX ON comments (commentable_type, commentable_id);
```

### Módulo de consulta polimórfica

```elixir
# lib/platform/comments.ex
defmodule Platform.Comments do
  import Ecto.Query
  alias Platform.{Repo, Comment}

  def for(%Platform.Post{id: id}),  do: query_for("Post", id)
  def for(%Platform.Photo{id: id}), do: query_for("Photo", id)
  def for(%Platform.Video{id: id}), do: query_for("Video", id)

  defp query_for(type, id) do
    Comment
    |> where([c], c.commentable_type == ^type and c.commentable_id == ^id)
    |> order_by([c], desc: c.inserted_at)
    |> preload(:author)
    |> Repo.all()
  end

  def create(commentable, attrs) do
    type = commentable.__struct__ |> Module.split() |> List.last()

    %Comment{}
    |> Comment.changeset(Map.merge(attrs, %{
      "commentable_id"   => commentable.id,
      "commentable_type" => type
    }))
    |> Repo.insert()
  end
end
```

### Uso desde el contexto

```elixir
iex> post = Repo.get!(Post, 1)
iex> Platform.Comments.for(post)
[%Comment{content: "Gran artículo", commentable_type: "Post", ...}, ...]

iex> Platform.Comments.create(post, %{"content" => "Nuevo comentario", "author_id" => 5})
{:ok, %Comment{...}}
```

**Trade-off del patrón polimórfico**: las foreign keys no pueden ser declaradas en la base
de datos (no puedes hacer `REFERENCES` a tres tablas). La integridad referencial recae
en la aplicación. Alternativa más robusta para PostgreSQL: una tabla por tipo con herencia
de tabla, o usar UUIDs compartidos.

---

## Parte 2 — Embeds: Datos Anidados sin Join

### Problema

Un `Order` tiene una dirección de envío que no merece tabla propia (no se consulta
independientemente, no tiene entidad propia). `embeds_one` la almacena como JSONB en
PostgreSQL — con validación Ecto completa.

### Schema con embeds

```elixir
# lib/store/address.ex
defmodule Store.Address do
  use Ecto.Schema
  import Ecto.Changeset

  # embedded_schema: no tiene tabla propia, vive dentro del padre
  embedded_schema do
    field :street,  :string
    field :city,    :string
    field :country, :string
    field :zip,     :string
  end

  def changeset(address, attrs) do
    address
    |> cast(attrs, [:street, :city, :country, :zip])
    |> validate_required([:street, :city, :country])
    |> validate_format(:zip, ~r/^\d{4,10}$/)
  end
end
```

```elixir
# lib/store/order.ex
defmodule Store.Order do
  use Ecto.Schema
  import Ecto.Changeset

  schema "orders" do
    field :total,  :decimal
    field :status, :string, default: "pending"
    belongs_to :user, Store.User
    embeds_one :shipping_address, Store.Address, on_replace: :update
    embeds_many :line_items, Store.LineItem, on_replace: :delete
    timestamps()
  end

  def changeset(order, attrs) do
    order
    |> cast(attrs, [:total, :status, :user_id])
    |> cast_embed(:shipping_address, required: true)
    |> cast_embed(:line_items)
    |> validate_required([:total])
  end
end
```

### Uso y validación

```elixir
attrs = %{
  total: Decimal.new("299.99"),
  user_id: 1,
  shipping_address: %{
    street: "Gran Vía 28",
    city: "Madrid",
    country: "ES",
    zip: "28013"
  }
}

# El changeset valida el embed recursivamente
changeset = Order.changeset(%Order{}, attrs)
changeset.valid?  # true

# Si el zip es inválido:
bad_attrs = put_in(attrs, [:shipping_address, :zip], "ABC")
Order.changeset(%Order{}, bad_attrs).errors
# [] — pero:
Order.changeset(%Order{}, bad_attrs).changes.shipping_address.errors
# [zip: {"has invalid format", [validation: :format]}]
```

**Por qué `embeds_one` sobre `has_one`**:
- Sin JOIN para leer la dirección — un único SELECT
- Datos históricos preservados (si el cliente cambia dirección, el pedido antiguo mantiene la original)
- Validación Ecto completa sin tabla separada

---

## Parte 3 — Multi-Tenancy con `put_query_prefix/2`

### Problema

Un SaaS con 500 clientes necesita aislamiento total de datos. La estrategia de
"schema por tenant" en PostgreSQL (cada tenant tiene su propio schema con sus propias
tablas) es la más segura — no hay riesgo de filtrado de datos entre tenants por error
de query.

### Setup de PostgreSQL

```sql
-- Cada tenant tiene su propio schema
CREATE SCHEMA tenant_acme;
CREATE SCHEMA tenant_globex;

-- Las tablas se crean en cada schema (vía migration por tenant)
CREATE TABLE tenant_acme.users (id BIGSERIAL PRIMARY KEY, name TEXT);
CREATE TABLE tenant_globex.users (id BIGSERIAL PRIMARY KEY, name TEXT);
```

### Implementación en Ecto

```elixir
# lib/saas/tenant.ex
defmodule SaaS.Tenant do
  import Ecto.Query
  alias SaaS.Repo

  # Aplica el prefix a cualquier query Ecto
  def scope(query, tenant_id) when is_binary(tenant_id) do
    prefix = "tenant_#{tenant_id}"
    put_query_prefix(query, prefix)
  end

  # Helper para operaciones de repo con tenant
  def all(query, tenant_id) do
    query |> scope(tenant_id) |> Repo.all()
  end

  def get(schema, id, tenant_id) do
    Repo.get(schema, id, prefix: "tenant_#{tenant_id}")
  end

  def insert(changeset, tenant_id) do
    Repo.insert(changeset, prefix: "tenant_#{tenant_id}")
  end
end
```

### Plug para inyectar el tenant en cada request

```elixir
# lib/saas_web/plugs/tenant_plug.ex
defmodule SaaSWeb.TenantPlug do
  import Plug.Conn

  def init(opts), do: opts

  def call(conn, _opts) do
    tenant_id = get_req_header(conn, "x-tenant-id") |> List.first()

    case validate_tenant(tenant_id) do
      {:ok, tid} -> assign(conn, :tenant_id, tid)
      :error     -> conn |> send_resp(401, "Invalid tenant") |> halt()
    end
  end

  defp validate_tenant(nil), do: :error
  defp validate_tenant(tid) when byte_size(tid) > 0, do: {:ok, tid}
end
```

### Uso en contextos

```elixir
# lib/saas/users.ex
defmodule SaaS.Users do
  alias SaaS.{Repo, User, Tenant}
  import Ecto.Query

  def list_users(tenant_id) do
    User |> Tenant.all(tenant_id)
  end

  def create_user(attrs, tenant_id) do
    %User{}
    |> User.changeset(attrs)
    |> Tenant.insert(tenant_id)
  end
end
```

```elixir
# En el controller:
def index(conn, _params) do
  users = SaaS.Users.list_users(conn.assigns.tenant_id)
  render(conn, :index, users: users)
end
```

**El SQL generado para tenant "acme"**:
```sql
SELECT u0."id", u0."name" FROM "tenant_acme"."users" AS u0
```

---

## Parte 4 — Preload Optimization y N+1

### El problema del N+1

```elixir
# MALO: N+1 queries — 1 para posts + N para autores
posts = Repo.all(Post)
Enum.map(posts, fn post ->
  author = Repo.get!(User, post.author_id)  # query por cada post
  "#{post.title} por #{author.name}"
end)
```

Con 100 posts = 101 queries. Con 1000 posts = 1001 queries.

### Solución 1: `preload` (query separada)

```elixir
# 2 queries: 1 para posts, 1 para todos los autores
posts = Post |> preload(:author) |> Repo.all()

Enum.map(posts, fn post ->
  "#{post.title} por #{post.author.name}"   # sin query adicional
end)
```

Ecto ejecuta: `SELECT * FROM posts` + `SELECT * FROM users WHERE id IN (1, 2, 3, ...)`

### Solución 2: `join` + `preload` (una sola query)

```elixir
# 1 sola query con JOIN — mejor cuando necesitas filtrar por el asociado
from(p in Post,
  join: a in assoc(p, :author),
  where: a.active == true,
  preload: [author: a]
)
|> Repo.all()
```

**Cuándo usar cada uno**:
- `preload(:author)` sin join → cuando no filtras por el asociado
- `join` + `preload: [author: a]` → cuando filtras, ordenas, o seleccionas campos del asociado

### Preload condicional anidado

```elixir
# Preload de associations anidadas en una sola llamada
posts = Post
|> preload([:tags, comments: :author])
|> Repo.all()

# Acceso sin queries adicionales:
post.comments         # lista de Comment
post.comments |> hd() |> Map.get(:author)  # User — ya cargado
```

### Detección de N+1 con Ecto DevLogger

En `config/dev.exs`:
```elixir
config :my_app, MyApp.Repo,
  log: :debug

# O instalar ecto_dev_logger para queries más legibles:
# {:ecto_dev_logger, "~> 0.13", only: :dev}
```

Con `EctoDevLogger` instalado, verás en la consola:
```
[debug] QUERY OK source="posts" db=2.1ms
SELECT p0."id", p0."title", p0."author_id" FROM "posts" AS p0

[debug] QUERY OK source="users" db=0.8ms    ← 1 sola query para todos los autores
SELECT u0."id", u0."name" FROM "users" AS u0 WHERE u0."id" = ANY($1) [...]
```

Si ves la misma query repetida con IDs distintos en el log, tienes N+1.

---

## Parte 5 — `prepare_changes/1` para Lógica de Changeset Avanzada

### Problema

Al actualizar el email de un usuario, necesitamos invalidar todas sus sesiones activas
dentro del mismo changeset (no en el contexto, dentro de la transacción del changeset).

```elixir
# lib/platform/user.ex
defmodule Platform.User do
  use Ecto.Schema
  import Ecto.Changeset
  import Ecto.Query

  schema "users" do
    field :email,    :string
    field :name,     :string
    has_many :sessions, Platform.Session
    timestamps()
  end

  def update_email_changeset(user, attrs) do
    user
    |> cast(attrs, [:email])
    |> validate_required(:email)
    |> validate_format(:email, ~r/@/)
    |> unique_constraint(:email)
    |> prepare_changes(&invalidate_sessions/1)
  end

  # Se ejecuta dentro de la transacción del changeset, solo si el changeset es válido
  defp invalidate_sessions(changeset) do
    if get_change(changeset, :email) do
      user_id = changeset.data.id
      # changeset.repo está disponible dentro de prepare_changes
      changeset.repo.update_all(
        from(s in Platform.Session, where: s.user_id == ^user_id),
        set: [revoked: true]
      )
    end
    changeset   # siempre devolver el changeset
  end
end
```

**Por qué `prepare_changes/1`**: se ejecuta dentro de la misma transacción que el `Repo.update`.
Si la actualización del email falla (ej: email duplicado), las sesiones NO se invalidan.
La alternativa con `Ecto.Multi` es más explícita pero más verbosa para este caso.

---

## Ejercicios Propuestos

### Ejercicio A — Polymorphism con protocolo

Implementar un protocolo `Commentable` con función `commentable_type/1` para que
`Platform.Comments.for/1` funcione con cualquier struct sin pattern matching explícito.

```elixir
defprotocol Platform.Commentable do
  def type(struct)
end

defimpl Platform.Commentable, for: Platform.Post do
  def type(_), do: "Post"
end
```

### Ejercicio B — Tenant migration automática

Escribir un Mix task `mix saas.create_tenant TENANT_ID` que cree el schema PostgreSQL
y ejecute las migraciones en ese schema usando `Ecto.Migrator.run/4` con el prefix.

**Pista**:
```elixir
Ecto.Migrator.run(Repo, migrations_path, :up, prefix: "tenant_#{id}", all: true)
```

### Ejercicio C — Preload con función de filtro

Precargar solo los comentarios activos (no borrados) de un post usando `preload` con query:

```elixir
active_comments = from(c in Comment, where: c.deleted == false)
Post |> preload(comments: ^active_comments) |> Repo.all()
```

Verificar que comentarios borrados no aparezcan en `post.comments`.

### Ejercicio D — Embed con validación cross-field

Extender `Store.Address` para validar que si `country == "ES"`, el zip debe ser
exactamente 5 dígitos. Implementarlo como validate_change personalizado.

---

## Patrones de Producción — Checklist

- [ ] Polymorphic: siempre índice compuesto en `(commentable_type, commentable_id)`
- [ ] Embeds: usar `on_replace: :delete` para `embeds_many` (evita orphaned records)
- [ ] Multi-tenancy: nunca construir el prefix con input del usuario sin validación
- [ ] N+1: revisar el log en dev después de cada endpoint nuevo. Si ves queries repetidas, preload
- [ ] Preload con join solo cuando filtras por el asociado — sin filtro, 2 queries es más eficiente
- [ ] `prepare_changes` para efectos secundarios transaccionales simples; `Ecto.Multi` para complejos
- [ ] `embeds_one` preserva datos históricos — ideal para direcciones, configuraciones snapshots

---

## Referencias

- [Ecto.Schema — embeds_one](https://hexdocs.pm/ecto/Ecto.Schema.html#embeds_one/3)
- [Ecto.Query — put_query_prefix](https://hexdocs.pm/ecto/Ecto.Query.html#put_query_prefix/2)
- [Ecto.Changeset — prepare_changes](https://hexdocs.pm/ecto/Ecto.Changeset.html#prepare_changes/2)
- [Preloading Associations](https://hexdocs.pm/ecto/Ecto.html#content)
- PostgreSQL Schemas (multi-tenancy): https://www.postgresql.org/docs/current/ddl-schemas.html
- Apartnik — Ecto Multi-tenancy: https://hexdocs.pm/triplex/readme.html
