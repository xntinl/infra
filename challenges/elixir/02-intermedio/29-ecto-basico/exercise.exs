# =============================================================================
# Ejercicio 29: Ecto Básico — Schemas, Changesets y Queries
# Nivel: Intermedio
# =============================================================================
#
# Ecto es la librería de base de datos y validación de datos de Elixir.
# Incluso sin base de datos, Ecto.Changeset es una herramienta poderosa
# para validar y transformar datos de forma estructurada y componible.
#
# Conceptos clave:
#   - Ecto.Schema / embedded_schema
#   - Ecto.Changeset (cast, validate_required, validate_length)
#   - validate_change para validaciones custom
#   - Schemas embebidos para validación sin DB
#
# Para correr: mix run exercise.exs (dentro de un proyecto Mix con Ecto)
# O en IEx:    iex -S mix
# =============================================================================

# Dependencias necesarias en mix.exs:
#   {:ecto, "~> 3.11"}
#   {:ecto_sql, "~> 3.11"}  # solo si necesitas DB real

# =============================================================================
# SECCIÓN 1: Schema básico con Ecto
# =============================================================================
#
# Ecto.Schema mapea estructuras Elixir a tablas de base de datos.
# `schema/2` define el nombre de tabla y sus campos.
# `field/3` declara un campo con su tipo.
#
# Tipos primitivos soportados: :string, :integer, :float, :boolean,
# :date, :time, :naive_datetime, :utc_datetime, :map, :array, etc.

IO.puts("=== Sección 1: Schema Básico ===\n")

defmodule MyApp.User do
  use Ecto.Schema
  import Ecto.Changeset

  # TODO 1: Define el schema "users" con los campos:
  #   - :name  tipo :string
  #   - :email tipo :string
  #   - :age   tipo :integer
  #   - :active tipo :boolean, default: true
  #
  # Estructura esperada:
  #   schema "users" do
  #     field :name,   :string
  #     field :email,  :string
  #     field :age,    :integer
  #     field :active, :boolean, default: true
  #   end
  #
  # Tu código aquí:

  # --- FIN TODO 1 ---

  # El changeset actúa como puerta de entrada para modificar datos.
  # cast/3 toma params externos (usualmente strings de un formulario)
  # y los convierte a los tipos correctos definidos en el schema.
  def changeset(user, params) do
    user
    |> cast(params, [:name, :email, :age, :active])
    |> validate_required([:name, :email])
    |> validate_format(:email, ~r/@/)
    |> validate_number(:age, greater_than: 0, less_than: 150)
  end
end

# Crear un usuario válido
valid_params = %{name: "Alice", email: "alice@example.com", age: 30}
changeset = MyApp.User.changeset(%MyApp.User{}, valid_params)
IO.puts("Changeset válido: #{changeset.valid?}")
IO.puts("Datos: #{inspect(changeset.changes)}\n")

# Crear un usuario inválido
invalid_params = %{name: "", age: -5}
bad_changeset = MyApp.User.changeset(%MyApp.User{}, invalid_params)
IO.puts("Changeset inválido: #{bad_changeset.valid?}")
IO.puts("Errores: #{inspect(bad_changeset.errors)}\n")

# =============================================================================
# SECCIÓN 2: Changeset con validaciones compuestas
# =============================================================================
#
# Los changesets son componibles: cada función de validación recibe
# un changeset y devuelve otro. Esto permite construir pipelines
# de validación claros y reutilizables.

IO.puts("=== Sección 2: Validaciones Compuestas ===\n")

defmodule MyApp.Post do
  use Ecto.Schema
  import Ecto.Changeset

  schema "posts" do
    field :title,   :string
    field :body,    :string
    field :status,  :string, default: "draft"
    field :tags,    {:array, :string}, default: []
  end

  # TODO 2: Implementa el changeset con:
  #   - cast de todos los campos
  #   - validate_required para :title y :body
  #   - validate_length para :title (min: 5, max: 100)
  #   - validate_length para :body (min: 20)
  #   - validate_inclusion para :status en ["draft", "published", "archived"]
  #
  # Pista: validate_inclusion(changeset, :campo, lista_valida)
  #
  # Tu código aquí:
  def changeset(post, params) do
    # --- FIN TODO 2 ---
  end
end

post_params = %{title: "Hola Elixir", body: "Este es un post sobre Elixir y sus maravillas.", status: "published"}
post_cs = MyApp.Post.changeset(%MyApp.Post{}, post_params)
IO.puts("Post válido: #{post_cs.valid?}")

short_post = %{title: "Hi", body: "Corto", status: "borrador"}
bad_post_cs = MyApp.Post.changeset(%MyApp.Post{}, short_post)
IO.puts("Post inválido: #{bad_post_cs.valid?}")
IO.puts("Errores del post: #{inspect(bad_post_cs.errors)}\n")

# =============================================================================
# SECCIÓN 3: validate_required y manejo de campos faltantes
# =============================================================================
#
# validate_required/2 falla si el campo:
#   1. No está presente en los params
#   2. Es nil después del cast
#   3. Es una string vacía ""
#
# Esto lo hace diferente a solo revisar `!= nil`.

IO.puts("=== Sección 3: validate_required ===\n")

defmodule MyApp.Profile do
  use Ecto.Schema
  import Ecto.Changeset

  # TODO 3: Define un schema "profiles" con:
  #   - :username   :string
  #   - :bio        :string
  #   - :avatar_url :string
  #   - :website    :string
  #
  # Luego implementa dos changesets:
  #   1. `create_changeset/2` — requiere :username y :bio
  #   2. `update_changeset/2` — solo requiere :username (bio es opcional en updates)
  #
  # Tu código aquí:

  # --- FIN TODO 3 ---
end

# Probar create_changeset
create_ok = MyApp.Profile.create_changeset(%MyApp.Profile{}, %{username: "bob", bio: "Elixir dev"})
IO.puts("Create válido: #{create_ok.valid?}")

create_fail = MyApp.Profile.create_changeset(%MyApp.Profile{}, %{username: "bob"})
IO.puts("Create sin bio: #{create_fail.valid?} — errores: #{inspect(create_fail.errors)}")

# Probar update_changeset
update_ok = MyApp.Profile.update_changeset(%MyApp.Profile{username: "bob", bio: "old"}, %{bio: ""})
IO.puts("Update sin bio (permitido): #{update_ok.valid?}\n")

# =============================================================================
# SECCIÓN 4: validate_change — validaciones personalizadas
# =============================================================================
#
# validate_change/3 permite agregar lógica de validación arbitraria.
# Recibe el changeset, el nombre del campo, y una función que retorna
# [] (sin errores) o [{campo, {mensaje, opts}}] (con errores).

IO.puts("=== Sección 4: Validaciones Custom ===\n")

defmodule MyApp.Product do
  use Ecto.Schema
  import Ecto.Changeset

  schema "products" do
    field :name,       :string
    field :price,      :float
    field :sku,        :string
    field :stock,      :integer
  end

  def changeset(product, params) do
    product
    |> cast(params, [:name, :price, :sku, :stock])
    |> validate_required([:name, :price, :sku])
    |> validate_sku_format()
    |> validate_price_not_negative()
  end

  # TODO 4: Implementa validate_sku_format/1 usando validate_change/3
  #   El SKU debe cumplir el formato: 3 letras mayúsculas + "-" + 4 dígitos
  #   Ejemplo válido: "ABC-1234"
  #   Ejemplo inválido: "abc-12", "1234-ABC"
  #
  #   Pista:
  #     defp validate_sku_format(changeset) do
  #       validate_change(changeset, :sku, fn :sku, sku ->
  #         if Regex.match?(~r/^[A-Z]{3}-\d{4}$/, sku),
  #           do: [],
  #           else: [sku: {"formato inválido, esperado XXX-0000", []}]
  #       end)
  #     end
  #
  # Tu código aquí:
  defp validate_sku_format(changeset) do
    # --- FIN TODO 4 ---
  end

  defp validate_price_not_negative(changeset) do
    validate_change(changeset, :price, fn :price, price ->
      if price >= 0,
        do: [],
        else: [price: {"no puede ser negativo", [validation: :custom]}]
    end)
  end
end

good_product = %{name: "Widget", price: 9.99, sku: "WDG-0042", stock: 100}
bad_sku_product = %{name: "Gadget", price: 5.0, sku: "gad-42", stock: 10}
neg_price_product = %{name: "Thing", price: -1.0, sku: "THG-0001"}

IO.puts("Producto válido: #{MyApp.Product.changeset(%MyApp.Product{}, good_product).valid?}")
IO.puts("SKU inválido: #{inspect(MyApp.Product.changeset(%MyApp.Product{}, bad_sku_product).errors)}")
IO.puts("Precio negativo: #{inspect(MyApp.Product.changeset(%MyApp.Product{}, neg_price_product).errors)}\n")

# =============================================================================
# SECCIÓN 5: embedded_schema — validación sin base de datos
# =============================================================================
#
# embedded_schema define un schema SIN tabla en la base de datos.
# Es ideal para validar datos externos (formularios, APIs, webhooks)
# usando todo el poder de Ecto.Changeset sin necesitar un Repo.
#
# Diferencias con schema/2:
#   - No genera :id automáticamente
#   - No necesita Ecto.Repo para funcionar
#   - Perfecto para DTOs (Data Transfer Objects)

IO.puts("=== Sección 5: embedded_schema ===\n")

defmodule MyApp.RegistrationForm do
  use Ecto.Schema
  import Ecto.Changeset

  # TODO 5: Implementa un embedded_schema para un formulario de registro con:
  #   - :name            :string
  #   - :email           :string
  #   - :password        :string (virtual: true — no se persiste)
  #   - :password_confirm :string (virtual: true)
  #   - :terms_accepted  :boolean
  #   - :age             :integer
  #
  # Luego implementa changeset/2 que:
  #   1. Hace cast de todos los campos
  #   2. Requiere :name, :email, :password, :password_confirm, :terms_accepted
  #   3. Valida que :email tenga formato válido
  #   4. Valida que :password tenga min 8 caracteres
  #   5. Valida que :terms_accepted sea true (usa validate_acceptance/2)
  #   6. Valida que password y password_confirm coincidan (usa validate_confirmation/2)
  #
  # Pistas:
  #   embedded_schema do ... end  (sin nombre de tabla)
  #   field :password, :string, virtual: true
  #   validate_acceptance(changeset, :terms_accepted)
  #   validate_confirmation(changeset, :password, message: "las contraseñas no coinciden")
  #
  # Tu código aquí:

  # --- FIN TODO 5 ---
end

valid_registration = %{
  name: "Carol",
  email: "carol@example.com",
  password: "supersecret123",
  password_confirmation: "supersecret123",
  terms_accepted: true,
  age: 25
}

reg_cs = MyApp.RegistrationForm.changeset(%MyApp.RegistrationForm{}, valid_registration)
IO.puts("Registro válido: #{reg_cs.valid?}")

invalid_registration = %{
  name: "Dave",
  email: "not-an-email",
  password: "short",
  password_confirmation: "different",
  terms_accepted: false
}

bad_reg_cs = MyApp.RegistrationForm.changeset(%MyApp.RegistrationForm{}, invalid_registration)
IO.puts("Registro inválido: #{bad_reg_cs.valid?}")
IO.puts("Errores de registro: #{inspect(bad_reg_cs.errors)}\n")

# =============================================================================
# SECCIÓN 6: Aplicar changeset y extraer datos
# =============================================================================
#
# apply_changes/1 extrae los datos del changeset si es válido.
# get_change/3 obtiene un cambio específico (o el default si no hay cambio).
# fetch_change/2 retorna {:ok, value} o :error.

IO.puts("=== Sección 6: Extraer datos del Changeset ===\n")

user_cs = MyApp.User.changeset(%MyApp.User{}, %{name: "Eve", email: "eve@test.com", age: 28})

if user_cs.valid? do
  user = Ecto.Changeset.apply_changes(user_cs)
  IO.puts("Usuario aplicado: #{inspect(user)}")
  IO.puts("Nombre: #{Ecto.Changeset.get_change(user_cs, :name)}")
  IO.puts("Email: #{Ecto.Changeset.get_field(user_cs, :email)}")
end

# get_field/3 busca primero en changes, luego en los datos del struct
existing_user = %MyApp.User{name: "Frank", email: "frank@test.com", active: true}
partial_cs = MyApp.User.changeset(existing_user, %{age: 40})
IO.puts("\nCambios parciales:")
IO.puts("  name (de datos): #{Ecto.Changeset.get_field(partial_cs, :name)}")
IO.puts("  age (de changes): #{Ecto.Changeset.get_field(partial_cs, :age)}\n")

# =============================================================================
# SECCIÓN 7: Errores de changeset — traducir y formatear
# =============================================================================
#
# Ecto.Changeset.traverse_errors/2 permite convertir los errores
# a un mapa de listas de strings, útil para APIs JSON.

IO.puts("=== Sección 7: Formatear Errores ===\n")

defmodule MyApp.ErrorHelpers do
  def translate_errors(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {msg, opts} ->
      Enum.reduce(opts, msg, fn {key, value}, acc ->
        String.replace(acc, "%{#{key}}", to_string(value))
      end)
    end)
  end
end

failing_cs = MyApp.User.changeset(%MyApp.User{}, %{email: "bad", age: -1})
errors_map = MyApp.ErrorHelpers.translate_errors(failing_cs)
IO.puts("Errores formateados: #{inspect(errors_map)}\n")

# =============================================================================
# SECCIÓN 8: Changesets anidados con embeds
# =============================================================================
#
# Ecto permite anidar schemas con `embeds_one` y `embeds_many`.
# cast_embed/3 propaga el changeset a los schemas embebidos.

IO.puts("=== Sección 8: Schemas Anidados ===\n")

defmodule MyApp.Address do
  use Ecto.Schema
  import Ecto.Changeset

  embedded_schema do
    field :street, :string
    field :city,   :string
    field :zip,    :string
  end

  def changeset(address, params) do
    address
    |> cast(params, [:street, :city, :zip])
    |> validate_required([:street, :city])
    |> validate_format(:zip, ~r/^\d{5}$/, message: "debe ser 5 dígitos")
  end
end

defmodule MyApp.Customer do
  use Ecto.Schema
  import Ecto.Changeset

  schema "customers" do
    field :name,  :string
    field :email, :string
    embeds_one :address, MyApp.Address
  end

  def changeset(customer, params) do
    customer
    |> cast(params, [:name, :email])
    |> validate_required([:name, :email])
    |> cast_embed(:address, required: true)
  end
end

customer_params = %{
  name: "Grace",
  email: "grace@example.com",
  address: %{street: "123 Elm St", city: "Springfield", zip: "12345"}
}

customer_cs = MyApp.Customer.changeset(%MyApp.Customer{}, customer_params)
IO.puts("Customer con address válido: #{customer_cs.valid?}")

bad_customer = %{
  name: "Hank",
  email: "hank@example.com",
  address: %{street: "1 Main", city: ""}
}

bad_customer_cs = MyApp.Customer.changeset(%MyApp.Customer{}, bad_customer)
IO.puts("Customer con address inválido: #{bad_customer_cs.valid?}")
IO.puts("Errores anidados: #{inspect(bad_customer_cs.errors)}\n")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa un sistema de validación de formulario de registro completo
# para una aplicación de eventos. Sin base de datos — solo Ecto.Changeset.
#
# El formulario debe validar:
#
# EventRegistration:
#   - :attendee_name   :string   (requerido, min 2 chars)
#   - :email           :string   (requerido, formato válido)
#   - :ticket_type     :string   (requerido, uno de: "general", "vip", "student")
#   - :quantity        :integer  (requerido, entre 1 y 10)
#   - :promo_code      :string   (opcional)
#   - :newsletter      :boolean  (default: false)
#
# Regla de negocio adicional (usa validate_change):
#   - Si :ticket_type es "student", :quantity no puede ser mayor a 2
#
# Función de conveniencia:
#   - `register/1` que recibe params y devuelve {:ok, datos} o {:error, changeset}

IO.puts("=== SECCIÓN 9: Try It Yourself ===\n")
IO.puts("Implementa MyApp.EventRegistration abajo:\n")

defmodule MyApp.EventRegistration do
  use Ecto.Schema
  import Ecto.Changeset

  # Tu implementación aquí
  # Recuerda: embedded_schema (sin tabla), changeset/2 con todas las validaciones,
  # y register/1 que retorna {:ok, struct} o {:error, changeset}
end

# Tests de tu implementación
IO.puts("--- Tests de EventRegistration ---")

# Test 1: registro válido general
result1 = MyApp.EventRegistration.register(%{
  attendee_name: "Ivan",
  email: "ivan@test.com",
  ticket_type: "general",
  quantity: 3
})
IO.puts("Test 1 (válido general): #{inspect(elem(result1, 0))}")

# Test 2: estudiante con demasiados tickets
result2 = MyApp.EventRegistration.register(%{
  attendee_name: "Jane",
  email: "jane@student.edu",
  ticket_type: "student",
  quantity: 5
})
IO.puts("Test 2 (student > 2): #{inspect(elem(result2, 0))} — #{inspect(elem(result2, 1))}")

# Test 3: ticket_type inválido
result3 = MyApp.EventRegistration.register(%{
  attendee_name: "Karl",
  email: "karl@test.com",
  ticket_type: "platinum",
  quantity: 1
})
IO.puts("Test 3 (tipo inválido): #{inspect(elem(result3, 0))}")

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. Olvidar `import Ecto.Changeset` — las funciones de validación no estarán disponibles

2. Confundir cast/3 con validate_required/2:
   - cast: convierte params externos al tipo correcto (puede dejar nil)
   - validate_required: verifica que el campo NO sea nil/vacío DESPUÉS del cast

3. validate_change vs validate_number:
   - Para rangos numéricos simples, usa validate_number/3 con opciones
   - validate_change es para lógica condicional o cross-field

4. apply_changes/1 NO valida — aplica los cambios sin importar si el changeset es válido.
   Siempre verifica changeset.valid? antes de aplicar.

5. get_change/3 retorna nil si el campo no fue modificado (usa get_field/3 para
   obtener el valor actual, ya sea del cambio o del struct original).

6. embedded_schema NO tiene campo :id por defecto. Si necesitas un ID,
   agrégalo explícitamente: field :id, :binary_id, primary_key: true
""")
