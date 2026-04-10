# 22 — Custom DSL with Macros

**Nivel**: Avanzado  
**Tema**: Domain Specific Languages con macros de Elixir — `__using__/1`, `@before_compile`, `@on_definition`

---

## Contexto

Un DSL bien diseñado en Elixir se construye sobre tres mecanismos complementarios:

1. **`__using__/1`**: ejecutado cuando el usuario escribe `use MyDSL`. Inyecta código
   en el módulo cliente, define atributos de módulo como acumuladores, e importa macros.

2. **`@before_compile`**: un hook registrado en `__using__` que ejecuta un macro
   justo antes de que el módulo cliente termine de compilar. Es el momento donde
   generas las funciones finales desde los datos acumulados.

3. **`@on_definition`**: hook invocado cada vez que se define una función en el módulo.
   Recibe el `env`, el tipo (`:def`/`:defp`), nombre, args, guards, y body.

4. **Module attributes como acumuladores**: `Module.register_attribute(mod, :rules, accumulate: true)`
   permite que `@rules value` en el módulo cliente vaya añadiendo a una lista.

### Flujo típico de un DSL

```
use MyDSL
   ↓
__using__/1 ejecutado
   ↓ registra @before_compile
   ↓ registra atributos acumuladores
   ↓ importa macros del DSL
   ↓
Usuario llama macros (state :idle, transition ..., etc.)
   ↓ cada macro añade datos al acumulador
   ↓
@before_compile ejecutado
   ↓ lee el acumulador
   ↓ genera funciones def con los datos
   ↓
Módulo compilado
```

### `defmacrop` — macros privadas

```elixir
defmacrop validate_state(name) do
  # Sólo accesible dentro del módulo que lo define
  # Útil para helpers internos del DSL
end
```

---

## Ejercicio 1 — StateMachine DSL

Implementa un módulo `StateMachine` que permita a cualquier módulo definir
una máquina de estados declarativa.

### API del DSL

```elixir
defmodule TrafficLight do
  use StateMachine

  state :red
  state :green
  state :yellow

  transition :red,    to: :green,  on: :go
  transition :green,  to: :yellow, on: :slow
  transition :yellow, to: :red,    on: :stop
end
```

### Funciones generadas (en TrafficLight)

```elixir
# Estado inicial es el primer estado declarado
TrafficLight.initial_state()       # => :red

# Listar estados válidos
TrafficLight.states()              # => [:red, :green, :yellow]

# Ejecutar transición
TrafficLight.transition(:red, :go)      # => {:ok, :green}
TrafficLight.transition(:red, :slow)    # => {:error, :invalid_transition}
TrafficLight.transition(:invalid, :go)  # => {:error, :invalid_state}

# Listar transiciones posibles desde un estado
TrafficLight.transitions_from(:red)    # => [{:go, :green}]
```

### Requisitos de implementación

- `@states` acumulador para estados en orden de declaración
- `@transitions` acumulador para transiciones como `{from, to, event}`
- Validar en `@before_compile` que todas las transiciones referencian estados declarados;
  si no, lanzar `CompileError`
- Generar todas las funciones en `@before_compile`

### Hints

<details>
<summary>Hint 1 — Estructura de __using__/1</summary>

```elixir
defmodule StateMachine do
  defmacro __using__(_opts) do
    quote do
      import StateMachine, only: [state: 1, transition: 3]
      Module.register_attribute(__MODULE__, :states, accumulate: true)
      Module.register_attribute(__MODULE__, :transitions, accumulate: true)
      @before_compile StateMachine
    end
  end

  defmacro state(name) do
    quote do
      @states unquote(name)
    end
  end

  defmacro transition(from, to: to, on: event) do
    quote do
      @transitions {unquote(from), unquote(to), unquote(event)}
    end
  end

  defmacro __before_compile__(env) do
    states     = Module.get_attribute(env.module, :states) |> Enum.reverse()
    transitions = Module.get_attribute(env.module, :transitions) |> Enum.reverse()
    # ... generar código aquí
  end
end
```
</details>

<details>
<summary>Hint 2 — Validar en compile time</summary>

```elixir
defmacro __before_compile__(env) do
  states = Module.get_attribute(env.module, :states) |> Enum.reverse()
  transitions = Module.get_attribute(env.module, :transitions) |> Enum.reverse()
  state_set = MapSet.new(states)

  Enum.each(transitions, fn {from, to, _event} ->
    unless MapSet.member?(state_set, from) do
      raise CompileError,
        description: "StateMachine: estado '#{from}' no declarado (transición desde #{from})",
        file: env.file,
        line: env.line
    end
    unless MapSet.member?(state_set, to) do
      raise CompileError,
        description: "StateMachine: estado '#{to}' no declarado (transición hacia #{to})",
        file: env.file,
        line: env.line
    end
  end)
  # continuar con generación...
end
```
</details>

<details>
<summary>Hint 3 — Generar funciones desde datos</summary>

```elixir
# Dentro de __before_compile__:
transition_clauses =
  Enum.map(transitions, fn {from, to, event} ->
    quote do
      def transition(unquote(from), unquote(event)), do: {:ok, unquote(to)}
    end
  end)

quote do
  def initial_state, do: unquote(hd(states))
  def states, do: unquote(states)
  unquote_splicing(transition_clauses)
  def transition(state, _event) when state in unquote(states), do: {:error, :invalid_transition}
  def transition(_, _), do: {:error, :invalid_state}
end
```
</details>

---

## Ejercicio 2 — Validation DSL

Implementa un módulo `Validatable` que genere validaciones tipo changeset.

### API del DSL

```elixir
defmodule UserSchema do
  use Validatable

  field :name,  :string, required: true,  length: 2..50
  field :age,   :integer, min: 0, max: 150
  field :email, :string, required: true,  format: ~r/@/
  field :role,  :atom,   inclusion: [:admin, :user, :guest]
end
```

### Funciones generadas

```elixir
# Valida un mapa de datos, retorna {:ok, data} | {:error, errors}
UserSchema.validate(%{name: "Ana", email: "ana@example.com", age: 25, role: :user})
# => {:ok, %{name: "Ana", email: "ana@example.com", age: 25, role: :user}}

UserSchema.validate(%{name: "A", email: "no-email", role: :superadmin})
# => {:error, [
#   name: "must have length between 2 and 50",
#   email: "must match format",
#   role: "must be one of [:admin, :user, :guest]",
#   age: "is required"  # si required: true estuviera en age
# ]}

# Introspección
UserSchema.fields()
# => [:name, :age, :email, :role]

UserSchema.field_type(:name)
# => :string
```

### Requisitos

- `@fields` acumulador: `{name, type, opts}`
- Generar `validate/1` que ejecute todas las reglas en secuencia y acumule errores
- Reglas soportadas: `required`, `length` (Range), `min`, `max`, `format` (Regex), `inclusion`
- Type coercion básica: si el tipo es `:integer` y el valor es un string numérico, convertirlo

### Hints

<details>
<summary>Hint 1 — Separar definición de reglas de ejecución</summary>

En `@before_compile`, en lugar de generar código inline para cada regla, genera
una función `validate_field/3` por cada campo que llame a helpers privados.

```elixir
defp validate_rule({:required, true}, name, nil),
  do: {name, "is required"}
defp validate_rule({:required, true}, _name, _val), do: nil
defp validate_rule({:length, first..last}, name, val) when is_binary(val) do
  len = String.length(val)
  if len >= first and len <= last, do: nil, else: {name, "must have length between #{first} and #{last}"}
end
defp validate_rule(_, _name, _val), do: nil
```

Esto mantiene la lógica de validación fuera del código generado, facilitando pruebas.
</details>

<details>
<summary>Hint 2 — Estructura del validate/1 generado</summary>

```elixir
# En __before_compile__:
quote do
  def validate(data) do
    errors =
      unquote(fields)  # lista de {name, type, opts}
      |> Enum.flat_map(fn {name, type, opts} ->
        value = Map.get(data, name)
        coerced = __MODULE__.__coerce__(type, value)
        Enum.map(opts, &__MODULE__.__validate_rule__(&1, name, coerced))
      end)
      |> Enum.reject(&is_nil/1)

    if errors == [], do: {:ok, data}, else: {:error, errors}
  end
end
```
</details>

---

## Ejercicio 3 — Route DSL

Implementa un `Router` DSL que genere una tabla de dispatch para HTTP.

### API del DSL

```elixir
defmodule MyApp.Router do
  use Router

  get    "/users",        to: MyApp.UsersController,  action: :index
  post   "/users",        to: MyApp.UsersController,  action: :create
  get    "/users/:id",    to: MyApp.UsersController,  action: :show
  put    "/users/:id",    to: MyApp.UsersController,  action: :update
  delete "/users/:id",    to: MyApp.UsersController,  action: :delete

  scope "/admin" do
    get  "/dashboard",    to: MyApp.AdminController,  action: :index
  end
end
```

### Función generada: `dispatch/2`

```elixir
MyApp.Router.dispatch(:get, "/users")
# => {:ok, {MyApp.UsersController, :index, %{}}}

MyApp.Router.dispatch(:get, "/users/42")
# => {:ok, {MyApp.UsersController, :show, %{"id" => "42"}}}

MyApp.Router.dispatch(:get, "/admin/dashboard")
# => {:ok, {MyApp.AdminController, :index, %{}}}

MyApp.Router.dispatch(:get, "/nonexistent")
# => {:error, :not_found}

MyApp.Router.dispatch(:post, "/users/42")
# => {:error, :method_not_allowed}  (ruta existe pero no el método)

# Introspección
MyApp.Router.routes()
# => [{:get, "/users", MyApp.UsersController, :index}, ...]
```

### Requisitos

- Macros `get/2`, `post/2`, `put/2`, `delete/2` como `defmacrop` wrapper sobre `route/3`
- Macro `scope/2` que prefija el path a todas las rutas dentro de su bloque.
  Usar `@scope_prefix` como atributo de módulo (no acumulador, sólo valor)
- Path params: segmentos que empiecen con `:` se convierten en captures
- `dispatch/2` generado con pattern matching — una cláusula por ruta, más dos
  cláusulas catch-all (`:not_found`, `:method_not_allowed`)
- Para `:method_not_allowed`, necesitas saber qué métodos SÍ existen para un path

### Hints

<details>
<summary>Hint 1 — Matching de path params en runtime</summary>

Convierte el path template en segmentos y genera un pattern de matching:

```elixir
# "/users/:id/posts/:post_id" →
# fn "/users/" <> rest → match "id" hasta "/" → match "posts/" → match "post_id"

# Más simple: dividir por "/" y comparar segmentos
defp match_path(template_segments, actual_segments) do
  match_segments(template_segments, actual_segments, %{})
end

defp match_segments([], [], params), do: {:ok, params}
defp match_segments([":" <> param | rest_t], [val | rest_a], params),
  do: match_segments(rest_t, rest_a, Map.put(params, param, val))
defp match_segments([seg | rest_t], [seg | rest_a], params),
  do: match_segments(rest_t, rest_a, params)
defp match_segments(_, _, _), do: :no_match
```
</details>

<details>
<summary>Hint 2 — scope/2 con atributo mutable</summary>

```elixir
defmacro scope(prefix, do: block) do
  quote do
    old_prefix = Module.get_attribute(__MODULE__, :scope_prefix) || ""
    Module.put_attribute(__MODULE__, :scope_prefix, old_prefix <> unquote(prefix))
    unquote(block)
    Module.put_attribute(__MODULE__, :scope_prefix, old_prefix)
  end
end
```

Importante: el atributo `@scope_prefix` NO es un acumulador — se usa como
variable temporal durante la compilación del módulo.
</details>

<details>
<summary>Hint 3 — method_not_allowed detection</summary>

Al generar `dispatch/2`, agrupa las rutas por path template (ignorando método).
Si un path tiene matches en algún método pero no en el solicitado, retorna
`:method_not_allowed`.

```elixir
# En __before_compile__:
routes_by_path = Enum.group_by(routes, fn {_method, path, _ctrl, _action} -> path end)

# Para cada path, generar claúsulas para los métodos que existen,
# luego una cláusula con _ para el método → :method_not_allowed
```
</details>

---

## Trade-offs a considerar

### `@before_compile` vs función de tiempo de ejecución

La validación en `@before_compile` tiene errores en compile time, lo cual es ideal.
Sin embargo, si la información sólo está disponible en runtime (e.g., configuración
de DB), necesitas un enfoque diferente. El DSL es mejor para esquemas estáticos.

### Acumuladores: orden de inserción

Los atributos acumuladores de Elixir insertan en orden LIFO. Si el orden
importa (como en `initial_state()`), siempre aplica `Enum.reverse/1` al leer
el acumulador en `@before_compile`.

### `defmacrop` vs `defmacro`

`defmacrop` crea macros que sólo pueden usarse en el mismo módulo donde se
definen. Útil para helpers internos del DSL que el usuario no debe usar directamente.
El trade-off: no puedes compartirlas ni testearlas desde fuera.

### Complejidad de compile time

Cada usuario de `use StateMachine` ejecuta `__before_compile__`. Si este hook
hace trabajo costoso (O(n²) o más), escala mal en proyectos grandes.
Diseña tus acumuladores para que la generación sea O(n).

---

## One possible solution

<details>
<summary>Ver solución StateMachine (spoiler)</summary>

```elixir
defmodule StateMachine do
  defmacro __using__(_opts) do
    quote do
      import StateMachine, only: [state: 1, transition: 3]
      Module.register_attribute(__MODULE__, :states, accumulate: true)
      Module.register_attribute(__MODULE__, :transitions, accumulate: true)
      @before_compile StateMachine
    end
  end

  defmacro state(name) do
    quote do: @states unquote(name)
  end

  defmacro transition(from, to: to, on: event) do
    quote do: @transitions {unquote(from), unquote(to), unquote(event)}
  end

  defmacro __before_compile__(env) do
    states = Module.get_attribute(env.module, :states) |> Enum.reverse()
    transitions = Module.get_attribute(env.module, :transitions) |> Enum.reverse()
    state_set = MapSet.new(states)

    Enum.each(transitions, fn {from, to, _} ->
      for s <- [from, to], not MapSet.member?(state_set, s) do
        raise CompileError,
          description: "StateMachine: estado desconocido '#{s}'",
          file: env.file, line: env.line
      end
    end)

    transition_clauses =
      Enum.map(transitions, fn {from, to, event} ->
        quote do
          def transition(unquote(from), unquote(event)), do: {:ok, unquote(to)}
        end
      end)

    from_clauses =
      transitions
      |> Enum.group_by(fn {from, _, _} -> from end)
      |> Enum.map(fn {from, ts} ->
        pairs = Enum.map(ts, fn {_, to, event} -> {event, to} end)
        quote do
          def transitions_from(unquote(from)), do: unquote(pairs)
        end
      end)

    quote do
      def initial_state, do: unquote(hd(states))
      def states, do: unquote(states)
      unquote_splicing(transition_clauses)
      def transition(s, _) when s in unquote(states), do: {:error, :invalid_transition}
      def transition(_, _), do: {:error, :invalid_state}
      unquote_splicing(from_clauses)
      def transitions_from(_), do: []
    end
  end
end
```

</details>
