# 21 — Advanced Macros & AST Manipulation

**Nivel**: Avanzado  
**Tema**: Manipulación directa del Abstract Syntax Tree y el módulo `Macro`

---

## Contexto

En Elixir, todo el código fuente se representa como una estructura de datos uniforme antes de compilarse:
el **AST** (Abstract Syntax Tree). Entender cómo leerlo, traversarlo y transformarlo te permite escribir
macros que operan a nivel semántico profundo, no sólo de texto.

El AST de Elixir sigue un formato estricto de tres elementos:

```elixir
# {función_o_operador, metadata, argumentos}
quote do: 1 + 2
#=> {:+, [context: Elixir, imports: [...]}, [1, 2]}

quote do: foo(bar)
#=> {:foo, [line: 1], [{:bar, [line: 1], Elixir}]}

# Las variables son tres-element tuples con el módulo como tercer elemento
quote do: x
#=> {:x, [], Elixir}
```

### Herramientas clave del módulo `Macro`

| Función | Propósito |
|---|---|
| `Macro.traverse/4` | Pre y post walk simultáneos con acumulador |
| `Macro.prewalk/3` | Visita el nodo ANTES de visitar sus hijos |
| `Macro.postwalk/3` | Visita el nodo DESPUÉS de visitar sus hijos |
| `Macro.expand/2` | Expande macros en el AST dado un `__ENV__` |
| `Macro.to_string/1` | Convierte AST a código Elixir legible |
| `Code.eval_quoted/3` | Evalúa AST en runtime con bindings opcionales |

### Hygiene de macros

Por defecto, las macros son **higiénicas**: las variables definidas dentro no
"escapan" al contexto del llamador. `var!(:name, context)` rompe la hygiene
intencionalmente. Úsalo sólo cuando el objetivo explícito es mutar variables
del llamador (e.g., DSLs que acumulan estado).

---

## Ejercicio 1 — AST Inspector

Escribe una macro `ast_inspect/1` que, en **compile time**, imprima el AST de
la expresión que recibe, luego la evalúe normalmente y retorne su valor.

### Requisitos

- La macro debe imprimir el AST completo usando `IO.inspect/2` con un label descriptivo
- También debe imprimir la representación en código legible con `Macro.to_string/1`
- El valor de la expresión original debe retornarse sin modificación
- El output debe ocurrir durante la compilación, no en runtime

### Uso esperado

```elixir
defmodule MyModule do
  require ASTInspector
  import ASTInspector

  def example do
    result = ast_inspect(1 + 2 * 3)
    # Durante compilación imprime:
    # [AST] {:+, [...], [1, {:*, [...], [2, 3]}]}
    # [Code] 1 + 2 * 3
    result  # => 7 en runtime
  end
end
```

### Hints

<details>
<summary>Hint 1 — Estructura básica</summary>

```elixir
defmacro ast_inspect(expr) do
  # `expr` ya ES el AST — no necesitas quote dentro
  ast_str = Macro.to_string(expr)
  
  quote do
    IO.inspect(unquote(Macro.escape(expr)), label: "[AST]")
    IO.puts("[Code] #{unquote(ast_str)}")
    unquote(expr)
  end
end
```

El truco: `Macro.escape/1` convierte el AST (que es una estructura de datos Elixir)
en un AST que produce esa misma estructura de datos cuando se evalúa.
</details>

<details>
<summary>Hint 2 — ¿Por qué Macro.escape?</summary>

Sin `Macro.escape`, hacer `unquote(expr)` dentro de `quote do` insertaría el AST
como código a ejecutar. Con `Macro.escape`, lo convierte para que sea tratado como
dato literal. Compara:

```elixir
expr = quote do: 1 + 2
# Sin escape: unquote(expr) dentro de quote → evalúa 1 + 2 → resultado 3
# Con escape: unquote(Macro.escape(expr)) → produce {:+, [], [1, 2]} como valor
```
</details>

---

## Ejercicio 2 — Variable Tracker

Escribe una macro `track_vars/1` que analice un bloque de código y retorne
una lista de todos los nombres de variables referenciadas en él, sin ejecutar
el bloque.

### Requisitos

- Usar `Macro.postwalk/3` con un acumulador para recolectar variables
- Una variable en el AST tiene la forma `{nombre, meta, contexto}` donde
  `contexto` es un átomo (o `nil` para variables especiales)
- Filtrar `_` y variables que empiecen con `_` (ignoradas por convención)
- Retornar la lista como valor en **compile time** (no en runtime)
- Deduplicar y ordenar el resultado

### Uso esperado

```elixir
vars = VariableTracker.track_vars do
  x = y + z
  result = foo(x, bar)
  _ignored = 42
end

IO.inspect(vars)  # => [:bar, :foo, :result, :x, :y, :z]
# Nota: foo y bar pueden aparecer como llamadas a función, no variables
# El ejercicio debe distinguir ambos casos
```

### Hints

<details>
<summary>Hint 1 — ¿Cómo identificar variables en el AST?</summary>

En el AST de Elixir:
- **Variable**: `{:nombre, meta, nil}` o `{:nombre, meta, ContextModule}` donde el nombre es un átomo
- **Llamada a función**: `{:nombre, meta, lista_de_args}` donde el tercer elemento es una lista

```elixir
# Variable `x`:
{:x, [line: 1], nil}

# Llamada a función `foo()`:
{:foo, [line: 1], []}

# Llamada a función `foo(bar)`:
{:foo, [line: 1], [{:bar, [line: 1], nil}]}
```

La clave: el tercer elemento es `nil` o un módulo (átomo) para variables,
y una lista para llamadas de función.
</details>

<details>
<summary>Hint 2 — Estructura con postwalk</summary>

```elixir
defmacro track_vars(do: block) do
  {_ast, vars} =
    Macro.postwalk(block, [], fn
      {name, _meta, context} = node, acc
      when is_atom(name) and (is_atom(context) or is_nil(context))
      and not is_list(context) ->
        # Es una variable
        {node, [name | acc]}
      node, acc ->
        {node, acc}
    end)

  vars
  |> Enum.reject(&String.starts_with?(to_string(&1), "_"))
  |> Enum.uniq()
  |> Enum.sort()
  |> Macro.escape()  # retornar como valor, no como código
end
```
</details>

---

## Ejercicio 3 — Optimizer Macro

Escribe una macro `optimize/1` que transforme el AST de un bloque antes de compilarlo,
aplicando la siguiente optimización algebraica:

**Regla**: `x + x` → `2 * x`  (para cualquier expresión `x`)

### Requisitos

- Usar `Macro.prewalk/2` para transformar el AST
- La transformación debe ser recursiva: si `x` mismo contiene `y + y`, también
  se optimiza
- Sólo aplicar cuando ambos lados de `+` son **estructuralmente idénticos**
- El módulo debe exponer también una función `count_optimizations/1` que retorne
  cuántas transformaciones se aplicaron (usando un acumulador)

### Uso esperado

```elixir
result = Optimizer.optimize do
  a + a          # → 2 * a
  b + b + b      # → (2 * b) + b  (sólo el primer par)
  (x + y) + (x + y)  # → 2 * (x + y)
  c + d          # sin cambio
end

{result, count} = Optimizer.optimize_counted do
  a + a
  b + b
end
# count => 2
```

### Hints

<details>
<summary>Hint 1 — Matching en el AST</summary>

El AST de `a + a` es:
```elixir
{:+, meta, [left, right]}
```

En `prewalk`, cada nodo es visitado antes que sus hijos. Para detectar `x + x`:

```elixir
Macro.prewalk(ast, fn
  {:+, meta, [left, right]} when left == right ->
    {:*, meta, [2, left]}
  node ->
    node
end)
```
</details>

<details>
<summary>Hint 2 — Comparación estructural de AST</summary>

`left == right` funciona para comparación estructural de AST porque el AST
son tuplas/listas/átomos normales de Elixir. Sin embargo, la metadata (`meta`)
puede diferir entre dos usos de la misma variable (números de línea distintos).

Para ignorar metadata, necesitas un helper:

```elixir
defp ast_equal?(a, b) do
  strip_meta(a) == strip_meta(b)
end

defp strip_meta({name, _meta, args}) when is_list(args) do
  {name, [], Enum.map(args, &strip_meta/1)}
end
defp strip_meta({name, _meta, ctx}) do
  {name, [], ctx}
end
defp strip_meta(other), do: other
```
</details>

<details>
<summary>Hint 3 — Acumulador con traverse</summary>

Para contar transformaciones, usa `Macro.traverse/4` que acepta un acumulador
tanto en pre como en post walk:

```elixir
{new_ast, count} =
  Macro.traverse(
    ast,
    0,          # acumulador inicial
    fn          # pre_fun
      {:+, meta, [l, r]}, acc when ast_equal?(l, r) ->
        {{:*, meta, [2, l]}, acc + 1}
      node, acc ->
        {node, acc}
    end,
    fn node, acc -> {node, acc} end  # post_fun (identidad)
  )
```
</details>

---

## Trade-offs a considerar

### `prewalk` vs `postwalk`

- **`prewalk`**: transforma el nodo antes de visitar sus hijos. Útil para
  optimizaciones que eliminen ramas completas (evita trabajo innecesario).
  Riesgo: no ves el resultado de transformar los hijos.

- **`postwalk`**: transforma después de visitar hijos. Los hijos ya están
  transformados cuando llegas al padre. Más costoso si eliminas ramas,
  pero más seguro para composición.

### Hygiene vs pragmatismo

Las macros higiénicas son más seguras pero menos expresivas para DSLs.
`var!/2` es necesario cuando querás que una macro interactúe con variables
del contexto del llamador. Es una herramienta, no un anti-patrón — pero
debe ser explícito y documentado.

### `Code.eval_quoted/3` — ¿cuándo usarlo?

Evaluar AST en runtime (`Code.eval_quoted`) es poderoso pero tiene costos:
- No hay optimizaciones del compilador
- Difícil de debuggear
- Rompe algunas garantías del tipo system

Preferir expansión en compile-time siempre que sea posible.

---

## One possible solution

<details>
<summary>Ver solución (spoiler)</summary>

```elixir
defmodule ASTInspector do
  defmacro ast_inspect(expr) do
    ast_str = Macro.to_string(expr)
    escaped = Macro.escape(expr)

    quote do
      IO.inspect(unquote(escaped), label: "[AST]")
      IO.puts("[Code] #{unquote(ast_str)}")
      unquote(expr)
    end
  end
end

defmodule VariableTracker do
  defmacro track_vars(do: block) do
    {_ast, vars} =
      Macro.postwalk(block, [], fn
        {name, _meta, ctx} = node, acc
        when is_atom(name) and not is_list(ctx) ->
          {node, [name | acc]}
        node, acc ->
          {node, acc}
      end)

    vars
    |> Enum.reject(&(&1 == :_ or String.starts_with?(to_string(&1), "_")))
    |> Enum.uniq()
    |> Enum.sort()
    |> Macro.escape()
  end
end

defmodule Optimizer do
  defp strip_meta({name, _meta, args}) when is_list(args),
    do: {name, [], Enum.map(args, &strip_meta/1)}
  defp strip_meta({name, _meta, ctx}), do: {name, [], ctx}
  defp strip_meta(other), do: other

  defp ast_equal?(a, b), do: strip_meta(a) == strip_meta(b)

  defmacro optimize(do: block) do
    Macro.prewalk(block, fn
      {:+, meta, [l, r]} = node ->
        if ast_equal?(l, r), do: {:*, meta, [2, l]}, else: node
      node -> node
    end)
  end

  defmacro optimize_counted(do: block) do
    {new_ast, count} =
      Macro.traverse(block, 0,
        fn {:+, meta, [l, r]}, acc ->
          if ast_equal?(l, r), do: {{:*, meta, [2, l]}, acc + 1}, else: {{:+, meta, [l, r]}, acc}
        fn node, acc -> {node, acc}
        end,
        fn node, acc -> {node, acc} end
      )

    quote do
      {unquote(new_ast), unquote(count)}
    end
  end
end
```

</details>
