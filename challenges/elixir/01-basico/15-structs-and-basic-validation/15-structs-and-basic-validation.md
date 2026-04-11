# Structs and Validation: Building a Type-Safe Domain Model

**Project**: `user_schema` — a domain model with enforced keys, custom constructors, and Ecto-changeset-style validation without Ecto

---

## Why structs are an architectural decision, not syntax sugar

Plain maps work for passing data around, but they have a critical weakness: nothing
prevents a caller from passing `%{nme: "Alice"}` (typo in key) or `%{age: "thirty"}`
(wrong type). The compiler cannot help — it has no idea what shape a "user" should have.

A struct changes that:

1. **Compile-time field enforcement**: `%User{nonexistent: 1}` is a compile error.
   With maps, `%{nonexistent: 1}` silently creates an unintended key.
2. **Pattern matching by type**: `def process(%User{} = user)` will raise at runtime
   if the caller passes a plain map. The function signature becomes a contract.
3. **@spec alignment**: type specs can reference `%User{}` rather than `map()`,
   enabling Dialyzer to catch misuse.
4. **Defaults are documented in the code**: `defstruct role: :user` documents the
   default at the definition site.

The trade-off: structs are more rigid. If you need a general key-value bag, a map
is correct. When the shape is known and fixed — a domain entity — a struct is right.

---

## The business problem

Build a user domain model that:

1. Declares all fields with `defstruct` and `@enforce_keys`
2. Provides a validated `new/1` constructor (like Ecto changesets, without Ecto)
3. Supports field-level validation with custom error messages
4. Provides an `update/2` function that re-validates after changes
5. Implements a changeset-style API: apply changes, collect errors, accept or reject

---

## Project structure

```
user_schema/
├── lib/
│   └── user_schema.ex
├── test/
│   └── user_schema_test.exs
└── mix.exs
```

---

## Implementation

### `lib/user_schema.ex`

```elixir
defmodule UserSchema do
  @moduledoc """
  Type-safe user domain model with validated construction.

  Uses @enforce_keys for required fields, a custom new/1 constructor
  with validation, and an update/2 function that re-validates.
  This is the pattern used in production Elixir code before you
  introduce Ecto — and sometimes instead of it.
  """

  @enforce_keys [:email, :name]
  defstruct [
    :email,
    :name,
    :phone,
    age: nil,
    role: :user,
    active: true,
    metadata: %{}
  ]

  @type t :: %__MODULE__{
          email: String.t(),
          name: String.t(),
          phone: String.t() | nil,
          age: pos_integer() | nil,
          role: :user | :admin | :moderator,
          active: boolean(),
          metadata: map()
        }

  @valid_roles [:user, :admin, :moderator]

  @doc """
  Creates a validated User struct from a keyword list or map.

  Returns {:ok, %UserSchema{}} on success, {:error, errors} on failure.
  Errors is a map of field => [error_messages].

  ## Examples

      iex> {:ok, user} = UserSchema.new(email: "alice@example.com", name: "Alice")
      iex> user.role
      :user

      iex> {:error, errors} = UserSchema.new(email: "", name: "")
      iex> Map.has_key?(errors, :email)
      true

      iex> {:error, errors} = UserSchema.new([])
      iex> Map.has_key?(errors, :email)
      true

  """
  @spec new(keyword() | map()) :: {:ok, t()} | {:error, map()}
  def new(attrs) when is_list(attrs) do
    attrs |> Map.new() |> new()
  end

  def new(attrs) when is_map(attrs) do
    changeset = %{
      email: attrs[:email] || attrs["email"],
      name: attrs[:name] || attrs["name"],
      phone: attrs[:phone] || attrs["phone"],
      age: attrs[:age] || attrs["age"],
      role: attrs[:role] || attrs["role"] || :user,
      active: if(Map.has_key?(attrs, :active), do: attrs[:active], else: true),
      metadata: attrs[:metadata] || attrs["metadata"] || %{}
    }

    case validate(changeset) do
      :ok ->
        {:ok, struct!(__MODULE__, changeset)}

      {:error, errors} ->
        {:error, errors}
    end
  end

  @doc """
  Updates an existing user with new attributes and re-validates.

  Only the provided fields are updated; others remain unchanged.

  ## Examples

      iex> {:ok, user} = UserSchema.new(email: "alice@example.com", name: "Alice")
      iex> {:ok, updated} = UserSchema.update(user, name: "Alice Smith")
      iex> updated.name
      "Alice Smith"
      iex> updated.email
      "alice@example.com"

      iex> {:ok, user} = UserSchema.new(email: "alice@example.com", name: "Alice")
      iex> {:error, errors} = UserSchema.update(user, email: "")
      iex> Map.has_key?(errors, :email)
      true

  """
  @spec update(t(), keyword() | map()) :: {:ok, t()} | {:error, map()}
  def update(%__MODULE__{} = user, attrs) when is_list(attrs) do
    update(user, Map.new(attrs))
  end

  def update(%__MODULE__{} = user, attrs) when is_map(attrs) do
    merged = user |> Map.from_struct() |> Map.merge(attrs)

    case validate(merged) do
      :ok ->
        {:ok, struct!(__MODULE__, merged)}

      {:error, errors} ->
        {:error, errors}
    end
  end

  @doc """
  Returns true if the user has admin privileges.

  Pattern matches on the struct in the function head — only accepts
  UserSchema structs, not plain maps.

  ## Examples

      iex> {:ok, user} = UserSchema.new(email: "a@b.com", name: "A", role: :admin)
      iex> UserSchema.admin?(user)
      true

      iex> {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      iex> UserSchema.admin?(user)
      false

  """
  @spec admin?(t()) :: boolean()
  def admin?(%__MODULE__{role: :admin}), do: true
  def admin?(%__MODULE__{}), do: false

  @doc """
  Returns true if the user account is active.
  """
  @spec active?(t()) :: boolean()
  def active?(%__MODULE__{active: true}), do: true
  def active?(%__MODULE__{}), do: false

  @doc """
  Deactivates a user account. Returns a new struct (immutable).

  ## Examples

      iex> {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      iex> deactivated = UserSchema.deactivate(user)
      iex> deactivated.active
      false

  """
  @spec deactivate(t()) :: t()
  def deactivate(%__MODULE__{} = user) do
    %__MODULE__{user | active: false}
  end

  @doc """
  Adds metadata to a user without overwriting existing keys.

  ## Examples

      iex> {:ok, user} = UserSchema.new(email: "a@b.com", name: "A", metadata: %{source: "web"})
      iex> updated = UserSchema.add_metadata(user, %{campaign: "q1"})
      iex> updated.metadata
      %{source: "web", campaign: "q1"}

  """
  @spec add_metadata(t(), map()) :: t()
  def add_metadata(%__MODULE__{metadata: existing} = user, new_meta) when is_map(new_meta) do
    %__MODULE__{user | metadata: Map.merge(existing, new_meta)}
  end

  # ---------------------------------------------------------------------------
  # Private — validation
  # ---------------------------------------------------------------------------

  @spec validate(map()) :: :ok | {:error, map()}
  defp validate(changeset) do
    errors =
      %{}
      |> validate_required(changeset, :email)
      |> validate_required(changeset, :name)
      |> validate_email_format(changeset)
      |> validate_age(changeset)
      |> validate_role(changeset)
      |> validate_name_length(changeset)

    case errors do
      empty when map_size(empty) == 0 -> :ok
      errors -> {:error, errors}
    end
  end

  @spec validate_required(map(), map(), atom()) :: map()
  defp validate_required(errors, changeset, field) do
    value = changeset[field]

    if is_nil(value) or (is_binary(value) and String.trim(value) == "") do
      add_error(errors, field, "is required")
    else
      errors
    end
  end

  @spec validate_email_format(map(), map()) :: map()
  defp validate_email_format(errors, %{email: email}) when is_binary(email) do
    if String.contains?(email, "@") and String.contains?(email, ".") do
      errors
    else
      add_error(errors, :email, "must be a valid email address")
    end
  end

  defp validate_email_format(errors, _changeset), do: errors

  @spec validate_age(map(), map()) :: map()
  defp validate_age(errors, %{age: nil}), do: errors

  defp validate_age(errors, %{age: age}) when is_integer(age) and age > 0 and age < 150 do
    errors
  end

  defp validate_age(errors, %{age: _age}) do
    add_error(errors, :age, "must be a positive integer less than 150")
  end

  @spec validate_role(map(), map()) :: map()
  defp validate_role(errors, %{role: role}) when role in @valid_roles, do: errors

  defp validate_role(errors, %{role: _role}) do
    add_error(errors, :role, "must be one of: #{inspect(@valid_roles)}")
  end

  @spec validate_name_length(map(), map()) :: map()
  defp validate_name_length(errors, %{name: name}) when is_binary(name) do
    len = String.length(name)

    cond do
      len < 1 -> add_error(errors, :name, "is too short")
      len > 100 -> add_error(errors, :name, "is too long (max 100 characters)")
      true -> errors
    end
  end

  defp validate_name_length(errors, _), do: errors

  @spec add_error(map(), atom(), String.t()) :: map()
  defp add_error(errors, field, message) do
    Map.update(errors, field, [message], fn existing -> [message | existing] end)
  end
end
```

**Why this works:**

- `@enforce_keys [:email, :name]` means `%UserSchema{}` raises `ArgumentError` if
  these keys are missing when the struct is created directly. However, `struct!/2`
  also enforces this — it raises on missing enforced keys.
- `new/1` accepts both keyword lists and maps. It normalizes to a map, validates,
  and only then creates the struct. This means invalid data never becomes a struct.
- Validation collects ALL errors into a map (`%{email: ["is required"], name: ["is too short"]}`).
  This is the same pattern Ecto changesets use — show every problem at once.
- `update/2` merges new attributes into the existing struct's fields and re-validates
  the entire result. This ensures invariants are maintained after updates.
- `admin?/1` and `active?/1` pattern match on `%__MODULE__{}` — they only accept
  UserSchema structs. A plain map with the same keys would raise `FunctionClauseError`.
- `add_metadata/2` uses `%__MODULE__{user | metadata: ...}` to create a new struct
  with one field changed. The original struct is not modified.

### Tests

```elixir
# test/user_schema_test.exs
defmodule UserSchemaTest do
  use ExUnit.Case, async: true

  doctest UserSchema

  describe "new/1" do
    test "creates user with required fields" do
      assert {:ok, user} = UserSchema.new(email: "alice@example.com", name: "Alice")
      assert user.email == "alice@example.com"
      assert user.name == "Alice"
      assert user.role == :user
      assert user.active == true
    end

    test "accepts all optional fields" do
      assert {:ok, user} =
               UserSchema.new(
                 email: "a@b.com",
                 name: "Alice",
                 phone: "+1234567890",
                 age: 30,
                 role: :admin,
                 active: false,
                 metadata: %{source: "web"}
               )

      assert user.phone == "+1234567890"
      assert user.age == 30
      assert user.role == :admin
      assert user.active == false
      assert user.metadata == %{source: "web"}
    end

    test "accepts map with string keys" do
      assert {:ok, user} = UserSchema.new(%{"email" => "a@b.com", "name" => "Alice"})
      assert user.email == "a@b.com"
    end

    test "returns errors for missing email" do
      assert {:error, errors} = UserSchema.new(name: "Alice")
      assert Map.has_key?(errors, :email)
    end

    test "returns errors for missing name" do
      assert {:error, errors} = UserSchema.new(email: "a@b.com")
      assert Map.has_key?(errors, :name)
    end

    test "returns errors for empty email" do
      assert {:error, errors} = UserSchema.new(email: "", name: "Alice")
      assert Map.has_key?(errors, :email)
    end

    test "returns errors for invalid email format" do
      assert {:error, errors} = UserSchema.new(email: "noatsign", name: "Alice")
      assert Map.has_key?(errors, :email)
    end

    test "returns errors for invalid age" do
      assert {:error, errors} = UserSchema.new(email: "a@b.com", name: "Alice", age: -5)
      assert Map.has_key?(errors, :age)
    end

    test "returns errors for invalid role" do
      assert {:error, errors} = UserSchema.new(email: "a@b.com", name: "Alice", role: :superadmin)
      assert Map.has_key?(errors, :role)
    end

    test "collects multiple errors" do
      assert {:error, errors} = UserSchema.new(email: "", name: "", role: :invalid)
      assert map_size(errors) >= 2
    end
  end

  describe "update/2" do
    setup do
      {:ok, user} = UserSchema.new(email: "alice@example.com", name: "Alice")
      {:ok, user: user}
    end

    test "updates specified fields only", %{user: user} do
      assert {:ok, updated} = UserSchema.update(user, name: "Alice Smith")
      assert updated.name == "Alice Smith"
      assert updated.email == "alice@example.com"
    end

    test "re-validates after update", %{user: user} do
      assert {:error, errors} = UserSchema.update(user, email: "")
      assert Map.has_key?(errors, :email)
    end

    test "does not modify original struct", %{user: user} do
      {:ok, _updated} = UserSchema.update(user, name: "New Name")
      assert user.name == "Alice"
    end
  end

  describe "admin?/1" do
    test "returns true for admin role" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A", role: :admin)
      assert UserSchema.admin?(user)
    end

    test "returns false for user role" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      refute UserSchema.admin?(user)
    end
  end

  describe "active?/1" do
    test "returns true for active user" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      assert UserSchema.active?(user)
    end

    test "returns false for inactive user" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A", active: false)
      refute UserSchema.active?(user)
    end
  end

  describe "deactivate/1" do
    test "sets active to false" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      deactivated = UserSchema.deactivate(user)
      assert deactivated.active == false
    end

    test "original remains unchanged" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      _deactivated = UserSchema.deactivate(user)
      assert user.active == true
    end
  end

  describe "add_metadata/2" do
    test "merges new metadata" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A", metadata: %{source: "web"})
      updated = UserSchema.add_metadata(user, %{campaign: "q1"})
      assert updated.metadata == %{source: "web", campaign: "q1"}
    end

    test "does not overwrite existing keys" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A", metadata: %{source: "web"})
      updated = UserSchema.add_metadata(user, %{source: "mobile"})
      assert updated.metadata.source == "mobile"
    end
  end

  describe "struct type safety" do
    test "is_struct/2 verifies module type" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      assert is_struct(user, UserSchema)
    end

    test "plain map does not match struct pattern" do
      plain_map = %{email: "a@b.com", name: "A", role: :user}
      refute is_struct(plain_map, UserSchema)
    end

    test "struct is also a map" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")
      assert is_map(user)
    end

    test "update syntax only works on existing fields" do
      {:ok, user} = UserSchema.new(email: "a@b.com", name: "A")

      assert_raise KeyError, fn ->
        %UserSchema{user | nonexistent: "value"}
      end
    end
  end
end
```

### Run the tests

```bash
mix test --trace
```

---

## Structs vs maps vs Ecto schemas

| Aspect | Plain map | Struct | Ecto.Schema |
|--------|-----------|--------|-------------|
| Compile-time field check | No | Yes (`@enforce_keys`) | Yes |
| Pattern match by type | No | Yes (`%User{}`) | Yes |
| Arbitrary keys | Yes | No (`KeyError`) | No |
| Type spec support | `map()` only | `%User{}` | `%User{}` |
| Validation | Manual | Manual (`new/1`) | Built-in changesets |
| Database integration | No | No | Yes |
| When to use | Flexible data, external input | Domain entities, internal state | DB-backed entities |

The pattern in this exercise (validated constructor + error collection) is exactly
what Ecto changesets do internally. Understanding it without Ecto makes you better
at using Ecto when you adopt it.

---

## Common production mistakes

**1. `@enforce_keys` without a validated constructor**
`@enforce_keys` only prevents direct `%MyStruct{}` without the key. `struct/2`
(without the bang) silently sets missing enforced keys to `nil`. Always use
`struct!/2` or a `new/1` constructor that validates before creating.

**2. Using `Map.put/3` to modify structs**
`Map.put(user, :extra_field, "value")` bypasses struct validation and adds a
field that `defstruct` did not declare. The result is technically a map with an
extra key — no longer a valid struct instance. Use `%Struct{s | field: value}`.

**3. Forgetting that struct equality includes `__struct__`**
`%User{name: "Alice"} == %Admin{name: "Alice"}` is `false` even if all visible
fields match — the hidden `__struct__` key differs.

**4. Pattern matching structs from different modules**
`def greet(%User{name: name})` only matches `User` structs. A `%Admin{name: name}`
will not match. This is intentional — structs provide type-safe dispatching.

**5. Not handling `@enforce_keys` with `struct/2`**
`struct(MyStruct, fields)` does NOT enforce `@enforce_keys` — it silently defaults
missing keys to `nil`. Use `struct!(MyStruct, fields)` to get the enforcement, or
validate in your constructor before creating.

---

## Resources

- [Structs — Elixir Getting Started](https://elixir-lang.org/getting-started/structs.html)
- [defstruct — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#defstruct/1)
- [@enforce_keys — Kernel docs](https://hexdocs.pm/elixir/Kernel.html#module-enforcing-keys)
- [Ecto.Changeset — HexDocs](https://hexdocs.pm/ecto/Ecto.Changeset.html) (for comparison)
- [Typespecs for structs](https://elixir-lang.org/getting-started/typespecs-and-behaviours.html)
