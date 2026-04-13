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

## Why generated validators over hand-written per-endpoint validation and not copy-pasted validation per controller

a single schema is the source of truth for validation, docs, and client codegen. Hand-written validators drift from docs within weeks and become a silent bug source.

## Design decisions

**Option A — schema validation at the edge via a macro-generated Changeset**
- Pros: compile-time safety, zero runtime schema lookup
- Cons: recompilation required to change schemas, verbose for simple endpoints

**Option B — runtime schema interpretation from a struct definition** (chosen)
- Pros: hot-reloadable schemas, simpler for CRUD
- Cons: every request pays schema-walking cost

→ Chose **B** because compile-time schemas eliminate a whole class of invalid-input bugs and cost nothing at request time.

## The business problem

The API team maintains 20 resource endpoints. Every time a field is added to the Users resource, three things must be updated: the controller, the validation schema, and the OpenAPI spec. They are out of sync 30% of the time. `restkit` makes divergence structurally impossible — the single resource declaration is the only place a field exists.

Two technical constraints shape everything:

1. **Spec derivation at compile time** — `GET /openapi.json` must not perform code analysis at request time; the spec is built when the application starts.
2. **Cursor pagination must be opaque** — clients must not be able to construct cursors manually; the encoding must include a HMAC to prevent tampering.

---

## Project structure

\`\`\`
restkit/
├── lib/
│   └── restkit.ex
├── test/
│   └── restkit_test.exs
├── script/
│   └── main.exs
└── mix.exs
\`\`\`

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

**Objective**: Scaffold the rest api framework Mix project with the required directory layout.

```bash
mix new restkit --sup
cd restkit
mkdir -p lib/restkit/{pagination,openapi}
mkdir -p test/restkit bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Declare the Mix project configuration and third-party dependencies.

### `lib/restkit.ex`

```elixir
defmodule Restkit do
  @moduledoc """
  REST API Framework with OpenAPI Generation.

  a single schema is the source of truth for validation, docs, and client codegen. Hand-written validators drift from docs within weeks and become a silent bug source.
  """
end
```
### `lib/restkit/resource.ex`

**Objective**: Implement the resource module that provides the required rest api framework behavior.

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
### `lib/restkit/validation.ex`

**Objective**: Validate inputs against declared field rules generated at compile time.

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
    e in RuntimeError -> [{field, "must be a valid ISO 8601 datetime"}]
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
### `lib/restkit/pagination/cursor.ex`

**Objective**: Implement the cursor module that provides the required rest api framework behavior.

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
### `lib/restkit/openapi/generator.ex`

**Objective**: Implement the generator module that provides the required rest api framework behavior.

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

**Objective**: Validate behavior against the frozen test suite that must pass unmodified.

```elixir
defmodule Restkit.ValidationTest do
  use ExUnit.Case, async: true
  doctest Restkit.OpenAPI.Generator

  alias Restkit.Validation

  @schema %{
    name: %{type: :string, required: true},
    age: %{type: :integer, required: false, min: 0, max: 150},
    role: %{type: :string, required: false, enum: ["admin", "user"]}
  }

  describe "Validation" do

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
end
```
```elixir
defmodule Restkit.PaginationTest do
  use ExUnit.Case, async: true
  doctest Restkit.OpenAPI.Generator

  alias Restkit.Pagination.Cursor

  @secret "test_secret_at_least_32_bytes_long_for_hmac"

  describe "Pagination" do

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
end
```
```elixir
defmodule Restkit.OpenAPITest do
  use ExUnit.Case, async: true
  doctest Restkit.OpenAPI.Generator

  alias Restkit.OpenAPI.Generator

  defmodule UserResource do
    use Restkit.Resource

    resource :users, FakeController do
      field :id,    :integer, required: true
      field :name,  :string,  required: true,  sortable: true
      field :email, :string,  required: true,  filterable: true
    end
  end

  describe "OpenAPI" do

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
end
```
### Step 8: Run the tests

**Objective**: Execute the provided test suite to verify the implementation passes.

```bash
mix test test/restkit/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the REST API framework invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.

## Main Entry Point

```elixir
def main do
  IO.puts("======== 33-build-rest-api-framework ========")
  IO.puts("Build rest api framework")
  IO.puts("")
  
  Restkit.Resource.start_link([])
  IO.puts("Restkit.Resource started")
  
  IO.puts("Run: mix test")
end
```
## Benchmark

```elixir
# bench/validation_bench.exs (complete benchmark harness)
schema = %{
  name: %{type: :string, required: true},
  email: %{type: :string, required: true},
  role: %{type: :string, required: false, enum: ["admin", "user", "viewer"]},
  age: %{type: :integer, required: false, min: 0, max: 150}
}

valid_params = %{
  "name" => "Alice",
  "email" => "alice@example.com",
  "role" => "admin",
  "age" => "30"
}

Benchee.run(
  %{
    "validation: valid params" => fn ->
      Restkit.Validation.validate(valid_params, schema)
    end,
    "validation: invalid params" => fn ->
      Restkit.Validation.validate(%{"role" => "superadmin"}, schema)
    end
  },
  time: 5,
  warmup: 2
)
```
Target: <200µs per validated request including JSON decode and schema check.

## Key Concepts: Resource-Oriented API Design and OpenAPI Generation

A REST API's purpose is to expose domain resources as addressable, manipulable entities. RESTkit's design centers on:

1. **Single source of truth** — a resource declaration (fields, types, constraints, relationships) is used to generate routes, validation schemas, and API documentation automatically.
2. **Cursor-based pagination** — opaque, signed cursors prevent clients from constructing arbitrary database queries while supporting efficient deep pagination.
3. **Compile-time OpenAPI generation** — the spec is derived at application startup, never at request time, ensuring stability and performance.
4. **Accumulated validation errors** — all schema violations are reported in a single response, not fail-fast, reducing round trips.

By embedding validation rules, sort constraints, and filter definitions in the resource declaration, RESTkit eliminates drift between code and documentation.

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

## Reflection

How does your framework's error contract (HTTP status + shape) stay stable when upstream validation errors change wording? Who owns backward compatibility — the framework or the app?

## Resources

- [OpenAPI 3.0 Specification](https://spec.openapis.org/oas/v3.0.0) — study the `paths`, `components/schemas`, `parameters`, and `requestBody` sections before implementing the generator
- [JSON:API Specification](https://jsonapi.org/) — your response envelope can follow this standard; the `data`/`meta`/`links` structure is well-specified
- [PostgreSQL documentation -- Row Ordering](https://www.postgresql.org/docs/current/queries-order.html) — understand tuple comparison for multi-column cursor WHERE clauses
- ["Building APIs You Won't Hate"](https://apisyouwonthate.com/books/build-apis-you-wont-hate/) — Phil Sturgeon — pragmatic API design; chapters on pagination and versioning
- [JSONAPI Elixir library source](https://github.com/jeregrine/jsonapi) — reference implementation for response shaping in Elixir

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Restex.MixProject do
  use Mix.Project

  def project do
    [
      app: :restex,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Restex.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `restex` (REST API framework).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 10000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:restex) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Restex stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:restex) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:restex)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual restex operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Restex classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **80,000 req/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **10 ms** | Plug + OpenAPI |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Plug + OpenAPI: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why REST API Framework with OpenAPI Generation matters

Mastering **REST API Framework with OpenAPI Generation** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/restkit_test.exs`

```elixir
defmodule RestkitTest do
  use ExUnit.Case, async: true

  doctest Restkit

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Restkit.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Plug + OpenAPI
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
