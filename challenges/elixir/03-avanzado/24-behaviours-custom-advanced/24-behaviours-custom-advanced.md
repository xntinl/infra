# 24 — Advanced Behaviours with Compile-Time Validation

**Nivel**: Avanzado  
**Tema**: Behaviours avanzados con `@optional_callbacks`, validación en compile-time, `@macrocallback`, comparación con Protocols

---

## Contexto

Un **Behaviour** en Elixir es un contrato que define qué funciones (o macros) debe
implementar un módulo. A diferencia de los Protocols, que polimorfismizan sobre el
*tipo de un valor*, los Behaviours definen la *interfaz de un módulo*.

### Anatomía de un Behaviour

```elixir
defmodule MyBehaviour do
  # Callback obligatorio con spec completa
  @callback init(opts :: keyword()) :: {:ok, state :: term()} | {:error, reason :: term()}

  # Callback con tipos complejos
  @callback handle_event(event :: event_t(), state :: state_t()) ::
              {:ok, new_state :: state_t()}
              | {:error, reason :: term(), state :: state_t()}

  # Callback opcional — no genera warning si no se implementa
  @optional_callbacks [format_error: 1, on_exit: 1]
  @callback format_error(error :: term()) :: String.t()
  @callback on_exit(state :: term()) :: :ok

  # Macrocallback — la implementación debe ser una macro, no una función
  @macrocallback transform(ast :: Macro.t()) :: Macro.t()
end
```

### `@optional_callbacks`

Los callbacks opcionales no generan warning del compilador cuando no se implementan.
Útil para hooks de extensión que tienen comportamiento default razonable.

```elixir
@optional_callbacks [
  handle_timeout: 2,   # función con aridad 2
  on_start: 1
]
```

### `__behaviour__/1` — introspección

```elixir
# En módulos que definen @callback, Elixir genera automáticamente:
MyBehaviour.behaviour_info(:callbacks)
# => [init: 1, handle_event: 2, format_error: 1, on_exit: 1, transform: 1]

MyBehaviour.behaviour_info(:optional_callbacks)
# => [format_error: 1, on_exit: 1]
```

### Default implementations via `__using__/1`

```elixir
defmodule MyBehaviour do
  @callback required_callback(term()) :: term()
  @optional_callbacks [optional_hook: 0]
  @callback optional_hook() :: :ok

  defmacro __using__(_opts) do
    quote do
      @behaviour MyBehaviour

      # Implementación por defecto para callbacks opcionales
      def optional_hook, do: :ok

      # Permite que el módulo usuario la sobreescriba
      defoverridable [optional_hook: 0]
    end
  end
end
```

### Behaviour vs Protocol — cuándo usar cada uno

| | Behaviour | Protocol |
|---|---|---|
| Pregunta | "¿Este módulo tiene esta interfaz?" | "¿Este tipo de valor tiene esta operación?" |
| Dispatch | Estático — el módulo se pasa explícitamente | Dinámico — por tipo del valor |
| Composición | `use` inyecta implementación default | `@derive` genera implementación |
| Ejemplo típico | Worker, Adapter, Plugin, GenServer | Stringify, Serialize, Compare |
| Verificación | Compile-time (warnings) o runtime | Runtime (Protocol.impl_for) |

---

## Ejercicio 1 — Plugin Behaviour con callbacks required + optional

Implementa un sistema de plugins donde cada plugin debe implementar un contrato
bien definido con callbacks obligatorios y opcionales.

### El Behaviour

```elixir
defmodule Plugin do
  # Required
  @callback name() :: String.t()
  @callback version() :: String.t()
  @callback execute(input :: map(), config :: keyword()) ::
              {:ok, output :: map()} | {:error, reason :: String.t()}

  # Optional — con default implementations
  @callback priority() :: integer()          # default: 0
  @callback validate_config(config :: keyword()) :: :ok | {:error, String.t()}  # default: siempre :ok
  @callback on_load(config :: keyword()) :: :ok  # default: :ok
  @callback on_unload() :: :ok               # default: :ok
  @callback description() :: String.t()      # default: ""
end
```

### Pipeline de plugins

```elixir
defmodule PluginRunner do
  # Ejecuta todos los plugins en orden de prioridad
  # Cada plugin recibe el output del anterior como input
  @spec run(plugins :: [module()], input :: map(), config :: keyword()) ::
        {:ok, final_output :: map()} | {:error, {plugin :: module(), reason :: String.t()}}
  def run(plugins, input, config \\ [])

  # Lista plugins ordenados por prioridad
  @spec sorted(plugins :: [module()]) :: [module()]
  def sorted(plugins)

  # Verifica que un módulo implementa el behaviour correctamente
  @spec valid_plugin?(module :: module()) :: boolean()
  def valid_plugin?(module)
end
```

### Ejemplo de plugin

```elixir
defmodule UppercasePlugin do
  use Plugin  # inyecta behaviour + defaults

  @impl Plugin
  def name, do: "uppercase"

  @impl Plugin
  def version, do: "1.0.0"

  @impl Plugin
  def priority, do: 10  # sobreescribe el default de 0

  @impl Plugin
  def execute(%{text: text} = input, _config) do
    {:ok, Map.put(input, :text, String.upcase(text))}
  end
  def execute(_input, _config), do: {:error, "input must have :text key"}
end
```

### Hints

<details>
<summary>Hint 1 — Registrar @optional_callbacks correctamente</summary>

```elixir
defmodule Plugin do
  @optional_callbacks [
    priority: 0,
    validate_config: 1,
    on_load: 1,
    on_unload: 0,
    description: 0
  ]

  @callback priority() :: integer()
  # ... más callbacks opcionales

  defmacro __using__(_opts) do
    quote do
      @behaviour Plugin

      # Defaults para callbacks opcionales
      def priority, do: 0
      def validate_config(_config), do: :ok
      def on_load(_config), do: :ok
      def on_unload, do: :ok
      def description, do: ""

      defoverridable [priority: 0, validate_config: 1, on_load: 1, on_unload: 0, description: 0]
    end
  end
end
```
</details>

<details>
<summary>Hint 2 — valid_plugin? usando behaviour_info</summary>

```elixir
def valid_plugin?(module) do
  # Verificar que el módulo exporta @behaviour Plugin
  behaviours = module.__info__(:attributes) |> Keyword.get(:behaviour, [])

  if Plugin not in behaviours do
    false
  else
    required = Plugin.behaviour_info(:callbacks) -- Plugin.behaviour_info(:optional_callbacks)
    Enum.all?(required, fn {fun, arity} ->
      function_exported?(module, fun, arity)
    end)
  end
end
```
</details>

<details>
<summary>Hint 3 — Pipeline con acumulación de errores</summary>

```elixir
def run(plugins, input, config) do
  sorted_plugins = sorted(plugins)

  Enum.reduce_while(sorted_plugins, {:ok, input}, fn plugin, {:ok, current_input} ->
    case plugin.execute(current_input, config) do
      {:ok, output}     -> {:cont, {:ok, output}}
      {:error, reason}  -> {:halt, {:error, {plugin, reason}}}
    end
  end)
end
```
</details>

---

## Ejercicio 2 — Validación en compile-time

Implementa un macro `assert_behaviour/1` que **en compile time** verifique que
el módulo donde se usa implementa todos los callbacks required de un behaviour dado.
Si falta alguno, debe generar un `CompileError` con un mensaje descriptivo.

### API requerida

```elixir
defmodule MyPlugin do
  @behaviour Plugin

  # Esta línea verifica en compile time:
  Plugin.assert_implemented(__MODULE__)
  # Si falta algún callback, lanza CompileError antes de terminar la compilación

  def name, do: "my-plugin"
  def version, do: "1.0.0"
  def execute(input, _config), do: {:ok, input}
end
```

Alternativamente, como macro standalone:

```elixir
defmodule MyPlugin do
  use Plugin  # esto ya llama assert_implemented internamente en @before_compile
  # ...
end
```

### Variante: `@before_compile` automático

Modifica el `__using__/1` del behaviour para que el `@before_compile` hook
verifique automáticamente que todos los callbacks required están definidos
en el módulo usuario antes de terminar la compilación.

### Hints

<details>
<summary>Hint 1 — ¿Qué información está disponible en @before_compile?</summary>

En `@before_compile`, tienes acceso al entorno del módulo (`env`):

```elixir
defmacro __before_compile__(env) do
  module = env.module

  # Ver qué funciones están definidas hasta este momento
  defined_fns = Module.definitions_in(module)
  # => [{:name, 0}, {:version, 0}, {:execute, 2}, ...]

  # Ver qué behaviours declaró el módulo
  behaviours = Module.get_attribute(module, :behaviour)

  # Callbacks required del behaviour
  required = Plugin.behaviour_info(:callbacks) -- Plugin.behaviour_info(:optional_callbacks)

  missing = required -- defined_fns

  unless missing == [] do
    missing_str = Enum.map_join(missing, ", ", fn {f, a} -> "#{f}/#{a}" end)
    raise CompileError,
      file: env.file,
      line: env.line,
      description: "#{module} no implementa los callbacks required de Plugin: #{missing_str}"
  end

  # No generar código adicional — sólo validar
  :ok
end
```
</details>

<details>
<summary>Hint 2 — Integrar en __using__/1</summary>

```elixir
defmacro __using__(_opts) do
  quote do
    @behaviour Plugin
    @before_compile Plugin  # registra el hook

    # ... defaults ...
  end
end
```

La clave: `@before_compile Plugin` hace que Elixir llame a `Plugin.__before_compile__/1`
justo antes de compilar el módulo usuario. En ese momento,
`Module.definitions_in/1` ya contiene todas las funciones definidas.
</details>

---

## Ejercicio 3 — Macro Callback: DSL Generation Behaviour

Implementa un behaviour donde uno de los callbacks es un **macro** (`@macrocallback`).
El caso de uso: un behaviour para "transformadores de código" donde cada implementación
define cómo transformar un bloque de código en compile time.

### El Behaviour

```elixir
defmodule CodeTransformer do
  @doc """
  Transforma un bloque de código AST en compile time.
  La implementación debe ser un macro porque recibe y retorna AST.
  """
  @macrocallback transform(ast :: Macro.t()) :: Macro.t()

  @doc "Nombre descriptivo del transformador"
  @callback name() :: String.t()

  @doc "Prioridad de aplicación cuando se encadenan múltiples transformadores"
  @callback priority() :: integer()
  @optional_callbacks [priority: 0]
end
```

### Implementaciones de ejemplo

```elixir
defmodule LoggingTransformer do
  @behaviour CodeTransformer

  def name, do: "logging"

  # Este es un macrocallback — debe ser defmacro
  defmacro transform(ast) do
    # Envuelve cada expresión del bloque con logging
    quote do
      result = unquote(ast)
      IO.puts("[LOG] expresión evaluada: #{inspect(result)}")
      result
    end
  end
end

defmodule TimingTransformer do
  @behaviour CodeTransformer

  def name, do: "timing"
  def priority, do: 100  # alta prioridad → se aplica primero

  defmacro transform(ast) do
    quote do
      start = System.monotonic_time(:microsecond)
      result = unquote(ast)
      elapsed = System.monotonic_time(:microsecond) - start
      IO.puts("[TIMING] #{elapsed} µs")
      result
    end
  end
end
```

### El orquestador de transformadores

```elixir
defmodule Transform do
  @doc """
  Aplica una lista de transformadores al bloque dado, en orden de prioridad.
  Cada transformador es un módulo que implementa CodeTransformer.
  """
  defmacro apply_transformers(transformers, do: block) do
    # transformers es una lista de módulos evaluada en compile time
    # Ordenar por priority (si implementan el callback opcional)
    # Aplicar en cadena: cada transformador envuelve el resultado del anterior
    # ...
  end
end
```

### Uso

```elixir
defmodule MyApp do
  require Transform
  require LoggingTransformer
  require TimingTransformer

  def run do
    Transform.apply_transformers([TimingTransformer, LoggingTransformer]) do
      heavy_computation(1..1000)
    end
    # Aplica primero TimingTransformer (prioridad 100), luego LoggingTransformer (prioridad 0)
    # Equivalente a: TimingTransformer.transform(LoggingTransformer.transform(heavy_computation(1..1000)))
  end
end
```

### Hints

<details>
<summary>Hint 1 — ¿Cómo verificar macrocallbacks?</summary>

`@macrocallback` en un behaviour espera que la implementación use `defmacro`.
En el módulo implementador, Elixir espera `MACRO-name/arity`:

```elixir
# Para verificar que un módulo implementa el macrocallback:
macro_exports = module.__info__(:macros)
# => [transform: 1]  (si está definido con defmacro)

# O usando macro_exported?/3:
macro_exported?(module, :transform, 1)
```
</details>

<details>
<summary>Hint 2 — Encadenar transformadores en compile time</summary>

El trick de `apply_transformers` es construir el AST final en compile time
anidando las llamadas a los macros de cada transformador:

```elixir
defmacro apply_transformers(transformers, do: block) do
  # Evaluar la lista de módulos en compile time
  mods = Code.eval_quoted(transformers, [], __CALLER__) |> elem(0)

  # Ordenar por prioridad
  sorted = Enum.sort_by(mods, fn mod ->
    if function_exported?(mod, :priority, 0), do: mod.priority(), else: 0
  end, :desc)

  # Construir AST anidado: t1.transform(t2.transform(block))
  Enum.reduce(sorted, block, fn mod, inner_ast ->
    quote do
      require unquote(mod)
      unquote(mod).transform(unquote(inner_ast))
    end
  end)
end
```
</details>

<details>
<summary>Hint 3 — @impl para macrocallbacks</summary>

Cuando implementas un `@macrocallback`, el `@impl` funciona igual:

```elixir
defmodule MyTransformer do
  @behaviour CodeTransformer

  @impl CodeTransformer
  def name, do: "my-transformer"

  @impl CodeTransformer
  defmacro transform(ast) do
    # ...
  end
end
```

`@impl` verifica en compile time que el nombre y aridad coinciden con
el callback declarado en el behaviour, generando warnings si no coinciden.
</details>

---

## Trade-offs a considerar

### `@macrocallback` vs `@callback`

`@macrocallback` es poderoso pero raro. Úsalo cuando el comportamiento
polimórfico necesita operar sobre AST (transformadores de código, DSL builders).
Para todo lo demás, `@callback` con funciones normales es más simple, testeable
y debuggeable.

### Validación en compile-time: ¿cuándo es suficiente el warning del compilador?

Elixir ya genera warnings cuando un módulo declara `@behaviour X` pero no
implementa todos los callbacks required. En muchos casos esto es suficiente.
El `@before_compile` que lanza `CompileError` es más estricto: convierte un
warning (ignorable) en un error (no-ignorable). Úsalo cuando:
- El contrato es crítico y no puede romperse silenciosamente
- Tienes un sistema de plugins donde las implementaciones incompletas causarían
  errores en runtime difíciles de trazar

### `defoverridable` y la trampa del default

Cuando defines defaults en `__using__/1` y los marcas como `defoverridable`,
el módulo usuario puede olvidar sobreescribir algo importante. El error aparece
en runtime, no en compile time. Considera usar callbacks required para todo
lo que sea verdaderamente esencial, y opcionales sólo para hooks secundarios.

### `Module.definitions_in/1` y el orden de compilación

`Module.definitions_in/1` en `@before_compile` refleja **todas** las funciones
definidas hasta ese momento, incluyendo las inyectadas por `use`. Esto significa
que tus defaults (de `__using__`) aparecerán en la lista incluso si el usuario
no los sobreescribió. Para distinguir "usuario implementó" vs "default inyectado",
necesitas una estrategia adicional (e.g., marcar con un module attribute qué
funciones fueron sobreescritas).

### Performance: Behaviours no tienen overhead de dispatch

A diferencia de los Protocols, los Behaviours no tienen overhead en runtime:
llamas `ModuloConcreto.funcion()` directamente. El "polimorfismo" es resuelto
por el programador (pasando el módulo como parámetro), no por el runtime.
Esto hace que los Behaviours sean ideales para sistemas de plugins donde el
módulo concreto se conoce en configuración.

---

## One possible solution

<details>
<summary>Ver solución Plugin Behaviour (spoiler)</summary>

```elixir
defmodule Plugin do
  @callback name() :: String.t()
  @callback version() :: String.t()
  @callback execute(map(), keyword()) :: {:ok, map()} | {:error, String.t()}
  @callback priority() :: integer()
  @callback validate_config(keyword()) :: :ok | {:error, String.t()}
  @callback on_load(keyword()) :: :ok
  @callback on_unload() :: :ok
  @callback description() :: String.t()

  @optional_callbacks [priority: 0, validate_config: 1, on_load: 1, on_unload: 0, description: 0]

  defmacro __using__(_opts) do
    quote do
      @behaviour Plugin
      @before_compile Plugin

      def priority, do: 0
      def validate_config(_), do: :ok
      def on_load(_), do: :ok
      def on_unload, do: :ok
      def description, do: ""

      defoverridable [priority: 0, validate_config: 1, on_load: 1, on_unload: 0, description: 0]
    end
  end

  defmacro __before_compile__(env) do
    required = Plugin.behaviour_info(:callbacks) -- Plugin.behaviour_info(:optional_callbacks)
    defined  = Module.definitions_in(env.module)
    missing  = required -- defined

    unless missing == [] do
      missing_str = Enum.map_join(missing, ", ", fn {f, a} -> "#{f}/#{a}" end)
      raise CompileError,
        file: env.file,
        line: env.line,
        description: "#{env.module} no implementa: #{missing_str}"
    end
  end
end

defmodule PluginRunner do
  def run(plugins, input, config \\ []) do
    plugins
    |> sorted()
    |> Enum.reduce_while({:ok, input}, fn plugin, {:ok, acc} ->
      case plugin.execute(acc, config) do
        {:ok, out}       -> {:cont, {:ok, out}}
        {:error, reason} -> {:halt, {:error, {plugin, reason}}}
      end
    end)
  end

  def sorted(plugins) do
    Enum.sort_by(plugins, & &1.priority(), :desc)
  end

  def valid_plugin?(module) do
    behaviours = module.__info__(:attributes) |> Keyword.get(:behaviour, [])
    Plugin in behaviours and
      Enum.all?(
        Plugin.behaviour_info(:callbacks) -- Plugin.behaviour_info(:optional_callbacks),
        fn {f, a} -> function_exported?(module, f, a) end
      )
  end
end
```

</details>
