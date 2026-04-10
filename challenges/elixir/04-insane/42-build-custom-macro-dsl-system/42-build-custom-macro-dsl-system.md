# 42. Build a Custom Macro DSL System

**Difficulty**: Insane

## Prerequisites

- Metaprogramación avanzada en Elixir: `quote/2`, `unquote/1`, `__using__/1`, `__before_compile__/1`
- AST de Elixir: estructura de `{form, meta, args}` y cómo manipularla
- Módulo `Module` y sus funciones: `put_attribute/3`, `get_attribute/2`, `register_attribute/3`
- Macros de acumulación de atributos (`@doc`, `@spec`, equivalentes propios)
- Compilación de Elixir: qué ocurre en `use`, en `defmacro`, en `__before_compile__`
- Conocimiento de cómo Phoenix, Ecto o Absinthe implementan sus DSLs
- Guard clauses y validación en tiempo de compilación

## Problem Statement

Diseña e implementa un sistema de tres DSLs interoperables en Elixir, cada uno generando código de producción a partir de declaraciones de alto nivel. Los DSLs se implementan como macros que transforman descripciones declarativas en módulos Elixir completos con funciones, callbacks, y documentación generada automáticamente.

El sistema debe detectar errores en los DSLs en tiempo de compilación (no en runtime), generar código legible e inspeccionable, y permitir que los tres DSLs se usen juntos en el mismo módulo.

El objetivo final es que un desarrollador pueda definir un módulo con `use StateMachine`, `use Validation`, y `use Router` de forma limpia, sin boilerplate repetitivo, y con errores descriptivos cuando la configuración es incorrecta.

## Acceptance Criteria

- [ ] StateMachine DSL: las macros `state/1`, `transition/4` (from, to, on: event, guard: function) y `initial/1` definen la máquina; en `__before_compile__`, el framework genera: `transition(state, event)` que retorna `{:ok, new_state}` o `{:error, :invalid_transition}`, `valid_state?/1`, `states/0`, `events/0`; los guards son funciones `fn state -> boolean` evaluadas en runtime
- [ ] Validation DSL: `validates :field, [rule: value, ...]` acumula validaciones por campo; las reglas soportadas incluyen `:required`, `:format` (regex), `:min_length`, `:max_length`, `:inclusion` (enum), `:custom` (función `fn value -> {:ok, value} | {:error, reason}`); en `__before_compile__`, genera `validate/1` que retorna `{:ok, attrs}` o `{:error, %{field => [error_message]}}` con todos los errores acumulados (no fail-fast)
- [ ] Route DSL: `scope path, [do: block]` y `get/post/put/delete path, Controller, :action` dentro del scope; en `__before_compile__`, genera `dispatch(method, path)` que retorna `{:ok, {Controller, :action, path_params}}` o `{:error, :not_found}`; soporta path params (`:id`) y wildcard (`*rest`)
- [ ] Compile-time errors: un `transition` que referencia un estado no declarado con `state/1` produce `CompileError` con mensaje "unknown state :foo in transition from :bar to :foo"; un `validates` con una regla desconocida produce `CompileError` con mensaje "unknown validation rule :baz for field :email"; una ruta duplicada produce warning (no error) con la localización exacta (archivo, línea)
- [ ] Code generation transparency: `mix compile --verbose` muestra un log de cuántas funciones generó cada DSL; una macro `debug_generated_code/0` en cada módulo permite inspeccionar el AST generado en formato legible; el código generado es idéntico al que escribirías a mano (sin artifactos de metaprogramación en el output)
- [ ] Composability: los tres DSLs pueden usarse en el mismo módulo sin conflictos de nombres; el StateMachine puede referenciar validaciones del Validation DSL en sus guards; el Router puede verificar en compile-time que los Controllers referenciados existen y que las actions están definidas
- [ ] Documentation: cada `state/1`, `validates/2`, y ruta genera `@doc` automáticamente para las funciones producidas; `mix docs` genera documentación legible que muestra la máquina de estados, las validaciones por campo, y la tabla de rutas; las funciones generadas aparecen como si fueran escritas manualmente

## What You Will Learn

- Metaprogramación avanzada en Elixir: el sistema de macros como herramienta de diseño de lenguajes
- Cómo funciona el proceso de compilación de Elixir y cuándo se ejecuta cada hook
- Acumulación de atributos de módulo para construir configuración declarativa
- Generación de código AST: escribir código que escribe código correcto
- Errores de compilación descriptivos: cómo guiar al desarrollador cuando el DSL está mal usado
- Por qué Elixir es especialmente bueno para DSLs internos comparado con otros lenguajes

## Hints

- `Module.register_attribute(module, :transitions, accumulate: true)` en `__using__/1` permite acumular con `@transitions {:from, :to, event, guard}`
- En `__before_compile__/1`: `Module.get_attribute(env.module, :transitions)` lee todos los acumulados; luego genera las funciones con `quote do ... end` y las inyecta con `Module.eval_quoted/2`
- Para compile-time errors: `raise CompileError, description: "...", file: env.file, line: env.line` dentro de un macro
- La composabilidad entre DSLs: usa un atributo compartido `@dsl_config` y cada DSL añade su sección; en `__before_compile__` cada DSL solo lee su propia sección
- Para la transparencia: añade `IO.puts` condicional bajo una env var `MIX_DSL_DEBUG=true`; o genera una función `__dsl_debug__/0` que retorna el código como string con `Macro.to_string/1`
- El Router con path params: parsea `"/users/:id/posts/:post_id"` en tiempo de compilación a un pattern con capturas; genera cláusulas de función con pattern matching sobre los segmentos del path

## Reference Material

- "Metaprogramming Elixir" — Chris McCord (capítulos 3-5 sobre DSLs avanzados)
- Código fuente de Ecto `Schema` y `Changeset` (macros acumuladoras reales en producción)
- Código fuente de Phoenix `Router` (cómo genera las rutas con macros)
- Elixir documentation: `Kernel.SpecialForms` (quote, unquote, unquote_splicing)
- "Understanding Elixir Macros" — Saša Jurić (blog series)
- Absinthe source code (DSL de GraphQL en Elixir, muy instructivo)

## Difficulty Rating ★★★★★★★

Las macros de Elixir son poderosas pero tracioneras: los errores de metaprogramación son difíciles de debuggear porque ocurren en un momento diferente al de la ejecución normal. La parte más difícil es la composabilidad entre DSLs sin conflictos y los errores de compilación con contexto preciso (archivo, línea, nombre del campo/estado/ruta problemático).

## Estimated Time

25–40 horas
