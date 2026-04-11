# REST API Framework with OpenAPI Generation

**Project**: `restkit` — a resource-oriented API framework that derives its OpenAPI spec from the same source of truth

---

## Project context

You are building `restkit`, a framework the API team will use to define REST resources declaratively. The same DSL that registers routes must produce the OpenAPI 3.0 spec — no duplication, no drift between code and documentation. The framework handles validation, response shaping, filtering, sorting, pagination, and spec generation automatically.

Project structure:

```
restkit/
├── lib/
│   └── restkit/
│       ├── application.ex
│       ├── resource.ex            # ← macro DSL: resource/3, field/3, relationships
│       ├── router.ex              # ← compile-time route generation from resources
│       ├── validation.ex          # ← request param/body validation against schema
│       ├── responder.ex           # ← field selection, embedding, envelope shaping
│       ├── filter.ex              # ← query param filter parsing + application
│       ├── sort.ex                # ← multi-field sort parsing + validation
│       ├── pagination/
│       │   ├── offset.ex          # ← page/per_page + links
│       │   └── cursor.ex          # ← opaque cursor encoding/decoding
│       └── openapi/
│           ├── generator.ex       # ← derives OpenAPI 3.0 from resource registry
│           └── encoder.ex         # ← renders to JSON or YAML
├── test/
│   └── restkit/
│       ├── resource_test.exs
│       ├── validation_test.exs
│       ├── responder_test.exs
│       ├── pagination_test.exs
│       └── openapi_test.exs
├── bench/
│   └── validation_bench.exs
└── mix.exs
```

---

## The business problem

The API team maintains 20 resource endpoints. Every time a field is added to the Users resource, three things must be updated: the controller, the validation schema, and the OpenAPI spec. They are out of sync 30% of the time. `restkit` makes divergence structurally impossible — the single resource declaration is the only place a field exists.

Two technical constraints shape everything:

1. **Spec derivation at compile time** — `GET /openapi.json` must not perform code analysis at request time; the spec is built when the application starts.
2. **Cursor pagination must be opaque** — clients must not be able to construct cursors manually; the encoding must include a HMAC to prevent tampering.

---

## Why cursor pagination beats offset pagination at scale

Offset pagination (`LIMIT 25 OFFSET 100`) scans and discards 100 rows before returning 25. At page 100 (`OFFSET 2500`) the database scans 2525 rows for each request. This degrades linearly.

Cursor pagination encodes the position of the last seen item. The next-page query is:

```sql
WHERE (created_at, id) < (:cursor_created_at, :cursor_id)
ORDER BY created_at DESC, id DESC
LIMIT 25
```

This uses the index efficiently regardless of how deep in the dataset you are. The cost is that you cannot jump to an arbitrary page — cursors only support sequential navigation.

Your cursor must encode enough state to reproduce the "next page" query for any combination of active sort fields and filters.

---

## Implementation

### Step 1: Create the project

```bash
mix new restkit --sup
cd restkit
mkdir -p lib/restkit/{pagination,openapi}
mkdir -p test/restkit bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev},
    {:stream_data, "~> 1.1", only: :test}
  ]
end
```

### Step 3: `lib/restkit/resource.ex`

```elixir
defmodule Restkit.Resource do
  @moduledoc """
  Compile-time DSL for defining REST resources.

  Usage:
    defmodule MyApp.Resources.User do
      use Restkit.Resource

      resource :users, MyApp.UserController do
        field :id,         :integer, required: true, filterable: false, sortable: false
        field :name,       :string,  required: true, filterable: true,  sortable: true
        field :email,      :string,  required: true, filterable: true,  sortable: false
        field :role,       :string,  required: false, filterable: true, sortable: false,
                                     enum: ["admin", "user", "viewer"]
        field :created_at, :datetime, required: false, filterable: true, sortable: true

        has_many :posts, MyApp.Resources.Post, through: :user_id
      end
    end

  At __before_compile__, generates:
  - Route registrations (GET /users, GET /users/:id, POST /users, etc.)
  - Validation schema for each action
  - OpenAPI path items for this resource
  - Filter and sort constraint maps
  """

  defmacro __using__(_) do
    quote do
      import Restkit.Resource
      Module.register_attribute(__MODULE__, :restkit_fields, accumulate: true)
      Module.register_attribute(__MODULE__, :restkit_relationships, accumulate: true)
      @before_compile Restkit.Resource
    end
  end

  defmacro __before_compile__(env) do
    fields = Module.get_attribute(env.module, :restkit_fields) |> Enum.reverse()
    rels = Module.get_attribute(env.module, :restkit_relationships) |> Enum.reverse()
    resource_config = Module.get_attribute(env.module, :restkit_resource)

    filterable =
      fields
      |> Enum.filter(fn f -> f.filterable end)
      |> Enum.map(fn f -> f.name end)

    sortable =
      fields
      |> Enum.filter(fn f -> f.sortable end)
      |> Enum.map(fn f -> f.name end)

    create_schema =
      fields
      |> Enum.reject(fn f -> f.name == :id end)
      |> Enum.map(fn f ->
        schema = %{type: f.type, required: f.required}
        schema = if f.enum, do: Map.put(schema, :enum, f.enum), else: schema
        {f.name, schema}
      end)
      |> Map.new()

    quote do
      def __restkit_fields__, do: unquote(Macro.escape(fields))
      def __restkit_relationships__, do: unquote(Macro.escape(rels))
      def __restkit_resource__, do: unquote(Macro.escape(resource_config))

      def validation_schema(:create), do: unquote(Macro.escape(create_schema))
      def validation_schema(:update), do: unquote(Macro.escape(create_schema))

      def filterable_fields, do: unquote(filterable)
      def sortable_fields, do: unquote(sortable)
    end
  end

  defmacro resource(name, controller, do: block) do
    quote do
      @restkit_resource %{name: unquote(name), controller: unquote(controller)}
      unquote(block)
    end
  end

  defmacro field(name, type, opts \\ []) do
    quote do
      @restkit_fields %{
        name: unquote(name),
        type: unquote(type),
        required: Keyword.get(unquote(opts), :required, false),
        filterable: Keyword.get(unquote(opts), :filterable, false),
        sortable: Keyword.get(unquote(opts), :sortable, false),
        enum: Keyword.get(unquote(opts), :enum, nil)
      }
    end
  end

  defmacro has_many(name, resource_module, opts \\ []) do
    quote do
      @restkit_relationships %{
        type: :has_many,
        name: unquote(name),
        resource: unquote(resource_module),
        through: Keyword.get(unquote(opts), :through)
      }
    end
  end
end
```

### Step 4: `lib/restkit/validation.ex`

```elixir
defmodule Restkit.Validation do
  @moduledoc """
  Validates request parameters against a schema derived from resource field declarations.

  Validation errors are accumulated (not fail-fast): a request with 3 invalid fields
  returns all 3 errors in a single 422 response. Fail-fast would require 3 round trips.
  """

  @type field_error :: {field_name :: atom(), reason :: String.t()}
  @type result :: {:ok, map()} | {:error, [field_error()]}

  @doc """
  Validates params against a schema map.
  Schema format: %{field_name => %{type: :string, required: true, enum: [...]}}
  """
  @spec validate(map(), map()) :: result()
  def validate(params, schema) do
    errors =
      Enum.flat_map(schema, fn {field, field_schema} ->
        value = Map.get(params, to_string(field))
        validate_field(field, value, field_schema)
      end)

    if errors == [] do
      {:ok, coerce_types(params, schema)}
    else
      {:error, errors}
    end
  end

  defp validate_field(field, nil, %{required: true}) do
    [{field, "is required"}]
  end

  defp validate_field(_field, nil, _schema), do: []

  defp validate_field(field, value, %{type: :integer} = schema) do
    errors = []

    errors =
      case parse_integer(value) do
        {:ok, int_val} ->
          min_errors = if schema[:min] && int_val < schema[:min], do: [{field, "must be at least #{schema[:min]}"}], else: []
          max_errors = if schema[:max] && int_val > schema[:max], do: [{field, "must be at most #{schema[:max]}"}], else: []
          min_errors ++ max_errors

        :error ->
          [{field, "must be an integer"}]
      end

    errors
  end

  defp validate_field(field, value, %{type: :string, enum: enum}) when is_list(enum) and enum != [] do
    if value in enum do
      []
    else
      [{field, "must be one of #{inspect(enum)}"}]
    end
  end

  defp validate_field(field, value, %{type: :string} = schema) do
    errors = []

    errors =
      cond do
        not is_binary(value) ->
          [{field, "must be a string"}]

        schema[:min_length] && String.length(value) < schema[:min_length] ->
          [{field, "must be at least #{schema[:min_length]} characters"}]

        schema[:max_length] && String.length(value) > schema[:max_length] ->
          [{field, "must be at most #{schema[:max_length]} characters"}]

        schema[:format] && is_struct(schema[:format], Regex) && not Regex.match?(schema[:format], value) ->
          [{field, "has invalid format"}]

        true ->
          []
      end

    errors
  end

  defp validate_field(field, value, %{type: :datetime}) do
    case DateTime.from_iso8601(value) do
      {:ok, _dt, _offset} -> []
      _ -> [{field, "must be a valid ISO 8601 datetime"}]
    end
  rescue
    _ -> [{field, "must be a valid ISO 8601 datetime"}]
  end

  defp validate_field(_field, _value, _schema), do: []

  defp coerce_types(params, schema) do
    Enum.reduce(schema, params, fn {field, field_schema}, acc ->
      str_key = to_string(field)

      case {Map.get(acc, str_key), field_schema} do
        {nil, _} ->
          acc

        {value, %{type: :integer}} ->
          case parse_integer(value) do
            {:ok, int_val} -> Map.put(acc, str_key, int_val)
            :error -> acc
          end

        {value, %{type: :datetime}} when is_binary(value) ->
          case DateTime.from_iso8601(value) do
            {:ok, dt, _} -> Map.put(acc, str_key, dt)
            _ -> acc
          end

        _ ->
          acc
      end
    end)
  end

  defp parse_integer(value) when is_integer(value), do: {:ok, value}

  defp parse_integer(value) when is_binary(value) do
    case Integer.parse(value) do
      {int, ""} -> {:ok, int}
      _ -> :error
    end
  end

  defp parse_integer(_), do: :error
end
```

### Step 5: `lib/restkit/pagination/cursor.ex`

```elixir
defmodule Restkit.Pagination.Cursor do
  @moduledoc """
  Cursor-based pagination with HMAC-signed opaque cursors.

  A cursor encodes the position of the last item in the current page.
  For a query sorted by (created_at DESC, id DESC), the cursor for the
  item {created_at: "2024-01-15T10:00:00Z", id: 42} is:

    Base64(HMAC-SHA256(secret, payload) || payload)

  where payload = Jason.encode!(%{sort_fields: [created_at: "2024-01-15...", id: 42]})

  Clients cannot construct valid cursors manually because they don't know the HMAC secret.
  This prevents cursor injection attacks where a client forges a cursor to scan
  arbitrary database ranges.
  """

  @doc """
  Encodes a cursor from the sort field values of the last item on the current page.
  sort_values is a keyword list: [created_at: "2024-01-15T10:00:00Z", id: 42]
  """
  @spec encode(keyword(), String.t()) :: String.t()
  def encode(sort_values, secret) do
    payload = Jason.encode!(%{v: sort_values})
    mac = compute_mac(payload, secret)
    Base.url_encode64(mac <> payload, padding: false)
  end

  @doc """
  Decodes and verifies a cursor. Returns {:ok, sort_values} or {:error, reason}.
  """
  @spec decode(String.t(), String.t()) :: {:ok, keyword()} | {:error, atom()}
  def decode(cursor_string, secret) do
    with {:ok, raw} <- Base.url_decode64(cursor_string, padding: false),
         mac_size = 32,
         <<mac::binary-size(mac_size), payload::binary>> when byte_size(payload) > 0 <- raw,
         expected_mac = compute_mac(payload, secret),
         true <- constant_time_compare(mac, expected_mac),
         {:ok, %{"v" => sort_values}} <- Jason.decode(payload) do
      {:ok, sort_values}
    else
      false -> {:error, :tampered}
      _ -> {:error, :invalid}
    end
  end

  @doc """
  Builds the WHERE clause fragment for the next page, given a decoded cursor
  and the active sort fields with directions.

  For sort: [created_at: :desc, id: :desc] and cursor values {created_at: "...", id: 42}:
  Returns the condition: "(created_at, id) < (:cursor_created_at, :cursor_id)"
  (adjusted for ASC sorts)
  """
  @spec build_where_clause(keyword(), keyword()) :: {String.t(), map()}
  def build_where_clause(cursor_values, sort_fields) do
    case sort_fields do
      [{field, direction}] ->
        op = if direction == :desc, do: "<", else: ">"
        param_name = :"cursor_#{field}"
        value = Keyword.get(cursor_values, field) || Map.get(cursor_values, to_string(field))
        {"#{field} #{op} :#{param_name}", %{param_name => value}}

      fields when is_list(fields) ->
        all_same_direction = fields |> Enum.map(fn {_, d} -> d end) |> Enum.uniq() |> length() == 1
        {_, first_dir} = hd(fields)

        if all_same_direction do
          field_names = Enum.map(fields, fn {f, _} -> to_string(f) end)
          param_names = Enum.map(fields, fn {f, _} -> ":cursor_#{f}" end)
          op = if first_dir == :desc, do: "<", else: ">"

          tuple_left = "(#{Enum.join(field_names, ", ")})"
          tuple_right = "(#{Enum.join(param_names, ", ")})"

          params =
            Enum.map(fields, fn {f, _} ->
              value = Keyword.get(cursor_values, f) || Map.get(cursor_values, to_string(f))
              {:"cursor_#{f}", value}
            end)
            |> Map.new()

          {"#{tuple_left} #{op} #{tuple_right}", params}
        else
          build_mixed_direction_clause(cursor_values, fields, [])
        end
    end
  end

  defp build_mixed_direction_clause(_cursor_values, [], clauses) do
    where = Enum.join(Enum.reverse(clauses), " OR ")
    {"(#{where})", %{}}
  end

  defp build_mixed_direction_clause(cursor_values, [{field, direction} | rest], clauses) do
    op = if direction == :desc, do: "<", else: ">"
    value = Keyword.get(cursor_values, field) || Map.get(cursor_values, to_string(field))

    equality_prefix =
      clauses
      |> Enum.reverse()
      |> Enum.map(fn _ -> "" end)

    clause = "#{field} #{op} '#{value}'"

    clause_with_equalities =
      if clauses == [] do
        clause
      else
        prev_fields = Enum.reverse(clauses) |> Enum.take(length(clauses))
        "(" <> clause <> ")"
      end

    build_mixed_direction_clause(cursor_values, rest, [clause_with_equalities | clauses])
  end

  defp compute_mac(data, secret) do
    :crypto.mac(:hmac, :sha256, secret, data)
  end

  defp constant_time_compare(a, b) when byte_size(a) != byte_size(b), do: false

  defp constant_time_compare(a, b) do
    a_bytes = :binary.bin_to_list(a)
    b_bytes = :binary.bin_to_list(b)

    result =
      Enum.zip(a_bytes, b_bytes)
      |> Enum.reduce(0, fn {x, y}, acc -> Bitwise.bor(acc, Bitwise.bxor(x, y)) end)

    result == 0
  end
end
```

### Step 6: `lib/restkit/openapi/generator.ex`

```elixir
defmodule Restkit.OpenAPI.Generator do
  @moduledoc """
  Generates an OpenAPI 3.0 specification from resource module definitions.
  """

  @doc "Generate a complete OpenAPI 3.0 spec from a list of resource modules."
  @spec generate([module()]) :: map()
  def generate(resource_modules) do
    paths =
      Enum.reduce(resource_modules, %{}, fn mod, acc ->
        resource = mod.__restkit_resource__()
        fields = mod.__restkit_fields__()
        resource_name = to_string(resource.name)

        collection_path = "/#{resource_name}"
        item_path = "/#{resource_name}/{id}"

        filterable = Enum.filter(fields, fn f -> f.filterable end)

        filter_params =
          Enum.map(filterable, fn f ->
            %{
              "name" => to_string(f.name),
              "in" => "query",
              "required" => false,
              "schema" => type_to_openapi_schema(f.type, f)
            }
          end)

        schema_properties =
          Enum.map(fields, fn f ->
            {to_string(f.name), type_to_openapi_schema(f.type, f)}
          end)
          |> Map.new()

        required_fields =
          fields
          |> Enum.filter(fn f -> f.required end)
          |> Enum.map(fn f -> to_string(f.name) end)

        schema_ref = "#/components/schemas/#{String.capitalize(String.trim_trailing(resource_name, "s"))}"

        acc
        |> Map.put(collection_path, %{
          "get" => %{
            "summary" => "List #{resource_name}",
            "parameters" => filter_params,
            "responses" => %{
              "200" => %{"description" => "Success", "content" => %{"application/json" => %{"schema" => %{"type" => "array", "items" => %{"$ref" => schema_ref}}}}}
            }
          },
          "post" => %{
            "summary" => "Create #{resource_name}",
            "requestBody" => %{
              "required" => true,
              "content" => %{"application/json" => %{"schema" => %{"$ref" => schema_ref}}}
            },
            "responses" => %{
              "201" => %{"description" => "Created"}
            }
          }
        })
        |> Map.put(item_path, %{
          "get" => %{
            "summary" => "Get #{resource_name} by ID",
            "parameters" => [%{"name" => "id", "in" => "path", "required" => true, "schema" => %{"type" => "integer"}}],
            "responses" => %{"200" => %{"description" => "Success"}}
          },
          "put" => %{
            "summary" => "Update #{resource_name}",
            "parameters" => [%{"name" => "id", "in" => "path", "required" => true, "schema" => %{"type" => "integer"}}],
            "responses" => %{"200" => %{"description" => "Updated"}}
          },
          "delete" => %{
            "summary" => "Delete #{resource_name}",
            "parameters" => [%{"name" => "id", "in" => "path", "required" => true, "schema" => %{"type" => "integer"}}],
            "responses" => %{"204" => %{"description" => "Deleted"}}
          }
        })
      end)

    components =
      Enum.reduce(resource_modules, %{}, fn mod, acc ->
        resource = mod.__restkit_resource__()
        fields = mod.__restkit_fields__()
        resource_name = to_string(resource.name)
        schema_name = String.capitalize(String.trim_trailing(resource_name, "s"))

        properties =
          Enum.map(fields, fn f ->
            {to_string(f.name), type_to_openapi_schema(f.type, f)}
          end)
          |> Map.new()

        required =
          fields
          |> Enum.filter(fn f -> f.required end)
          |> Enum.map(fn f -> to_string(f.name) end)

        schema = %{"type" => "object", "properties" => properties}
        schema = if required != [], do: Map.put(schema, "required", required), else: schema

        Map.put(acc, schema_name, schema)
      end)

    %{
      "openapi" => "3.0.0",
      "info" => %{
        "title" => "Restkit API",
        "version" => "1.0.0"
      },
      "paths" => paths,
      "components" => %{"schemas" => components}
    }
  end

  defp type_to_openapi_schema(:integer, _field), do: %{"type" => "integer"}
  defp type_to_openapi_schema(:string, %{enum: enum}) when is_list(enum) and enum != [], do: %{"type" => "string", "enum" => enum}
  defp type_to_openapi_schema(:string, _field), do: %{"type" => "string"}
  defp type_to_openapi_schema(:datetime, _field), do: %{"type" => "string", "format" => "date-time"}
  defp type_to_openapi_schema(:boolean, _field), do: %{"type" => "boolean"}
  defp type_to_openapi_schema(_, _field), do: %{"type" => "string"}
end
```

### Step 7: Given tests — must pass without modification

```elixir
# test/restkit/validation_test.exs
defmodule Restkit.ValidationTest do
  use ExUnit.Case, async: true

  alias Restkit.Validation

  @schema %{
    name: %{type: :string, required: true},
    age: %{type: :integer, required: false, min: 0, max: 150},
    role: %{type: :string, required: false, enum: ["admin", "user"]}
  }

  test "valid params return {:ok, coerced}" do
    assert {:ok, result} = Validation.validate(%{"name" => "Alice", "age" => "30"}, @schema)
    assert result["name"] == "Alice"
    assert result["age"] == 30  # coerced from string
  end

  test "missing required field returns error" do
    assert {:error, errors} = Validation.validate(%{"age" => "25"}, @schema)
    assert Enum.any?(errors, fn {field, _} -> field == :name end)
  end

  test "invalid enum value returns error" do
    assert {:error, errors} = Validation.validate(%{"name" => "X", "role" => "superadmin"}, @schema)
    assert Enum.any?(errors, fn {field, _} -> field == :role end)
  end

  test "all errors accumulated, not fail-fast" do
    # Both name (required) and role (invalid enum) are wrong
    assert {:error, errors} = Validation.validate(%{"role" => "superadmin"}, @schema)
    field_names = Enum.map(errors, fn {f, _} -> f end)
    assert :name in field_names
    assert :role in field_names
  end
end
```

```elixir
# test/restkit/pagination_test.exs
defmodule Restkit.PaginationTest do
  use ExUnit.Case, async: true

  alias Restkit.Pagination.Cursor

  @secret "test_secret_at_least_32_bytes_long_for_hmac"

  test "encode and decode round-trip" do
    values = [created_at: "2024-01-15T10:00:00Z", id: 42]
    cursor = Cursor.encode(values, @secret)

    assert {:ok, decoded} = Cursor.decode(cursor, @secret)
    assert decoded["created_at"] == "2024-01-15T10:00:00Z"
    assert decoded["id"] == 42
  end

  test "tampered cursor is rejected" do
    cursor = Cursor.encode([id: 1], @secret)
    # Flip one byte in the middle
    <<head::binary-size(10), byte, rest::binary>> = Base.url_decode64!(cursor, padding: false)
    tampered = Base.url_encode64(<<head::binary, byte ^^^ 0xFF, rest::binary>>, padding: false)
    assert {:error, :tampered} = Cursor.decode(tampered, @secret)
  end

  test "wrong secret is rejected" do
    cursor = Cursor.encode([id: 1], @secret)
    assert {:error, :tampered} = Cursor.decode(cursor, "wrong_secret_also_32_bytes_padded!")
  end
end
```

```elixir
# test/restkit/openapi_test.exs
defmodule Restkit.OpenAPITest do
  use ExUnit.Case, async: true

  alias Restkit.OpenAPI.Generator

  defmodule UserResource do
    use Restkit.Resource

    resource :users, FakeController do
      field :id,    :integer, required: true
      field :name,  :string,  required: true,  sortable: true
      field :email, :string,  required: true,  filterable: true
    end
  end

  test "generates paths for all standard CRUD actions" do
    spec = Generator.generate([UserResource])
    paths = spec["paths"]
    assert Map.has_key?(paths, "/users")
    assert Map.has_key?(paths, "/users/{id}")
    assert Map.get_in(paths, ["/users", "get"]) != nil
    assert Map.get_in(paths, ["/users", "post"]) != nil
    assert Map.get_in(paths, ["/users/{id}", "get"]) != nil
  end

  test "filterable fields appear as query parameters" do
    spec = Generator.generate([UserResource])
    params = get_in(spec, ["paths", "/users", "get", "parameters"]) || []
    param_names = Enum.map(params, & &1["name"])
    assert "email" in param_names
    # id is not filterable, should not appear
    refute "id" in param_names
  end

  test "generated spec validates against OpenAPI 3.0 required keys" do
    spec = Generator.generate([UserResource])
    assert spec["openapi"] == "3.0.0"
    assert is_map(spec["info"])
    assert is_map(spec["paths"])
    assert is_map(spec["components"])
  end
end
```

### Step 8: Run the tests

```bash
mix test test/restkit/ --trace
```

---

## Trade-off analysis

| Aspect | Cursor pagination | Offset pagination | Keyset pagination |
|--------|------------------|-------------------|-------------------|
| Deep page performance | O(1) | O(n) -- full scan | O(1) |
| Arbitrary page jumps | no | yes | no |
| Stable under concurrent inserts | yes | no (items shift) | yes |
| Sort field constraints | must be in cursor | none | same as cursor |
| Client complexity | opaque token | page number | similar to cursor |
| Cursor forgery risk | mitigated by HMAC | n/a | possible without signing |

Reflection: when would you choose offset pagination over cursor pagination despite its performance characteristics? (Hint: think about user-visible page numbers and search engine crawlers.)

---

## Common production mistakes

**1. Unsigned cursors**
A cursor without a HMAC lets a client craft arbitrary cursors to scan the database. At minimum, sign the cursor with a server secret. Better: include the active filter state in the signed payload so a cursor from one query cannot be used with a different query.

**2. OpenAPI spec generated lazily at request time**
Running `Generator.generate/1` on every `GET /openapi.json` request re-traverses all resource modules. For 50 resources this is fast, but it defeats the purpose of compile-time derivation. Generate the spec at application startup and cache it.

**3. Non-filterable fields silently ignored vs. rejected**
If a client sends `?nonexistent_field=value` and the framework silently ignores it, the client has no feedback that their query is wrong. Reject unknown filter fields with a 400 error explicitly.

**4. Field selection without depth limiting**
`include=posts.comments.author` can result in N+1 queries cascading 3 levels deep. Implement a maximum include depth (default: 2) and validate at request time.

**5. Cursor state does not include sort direction**
A cursor encodes sort field values but not the sort direction. If the client changes sort direction between pages, the cursor from the previous query is invalid. Include the full sort specification in the cursor payload.

---

## Resources

- [OpenAPI 3.0 Specification](https://spec.openapis.org/oas/v3.0.0) — study the `paths`, `components/schemas`, `parameters`, and `requestBody` sections before implementing the generator
- [JSON:API Specification](https://jsonapi.org/) — your response envelope can follow this standard; the `data`/`meta`/`links` structure is well-specified
- [PostgreSQL documentation -- Row Ordering](https://www.postgresql.org/docs/current/queries-order.html) — understand tuple comparison for multi-column cursor WHERE clauses
- ["Building APIs You Won't Hate"](https://apisyouwonthate.com/books/build-apis-you-wont-hate/) — Phil Sturgeon — pragmatic API design; chapters on pagination and versioning
- [JSONAPI Elixir library source](https://github.com/jeregrine/jsonapi) — reference implementation for response shaping in Elixir
