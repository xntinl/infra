# 9. Control Flow: if, case, cond

**Difficulty**: Basico

## Prerequisites

- Haber completado los ejercicios 01–08
- Conocimiento de pattern matching (ejercicio 05), maps (ejercicio 07) y funciones (ejercicio 08)
- IEx disponible en tu terminal

## Learning Objectives

- Entender qué valores son falsy en Elixir (solo `false` y `nil`)
- Usar `if` y `unless` para condiciones simples
- Usar `case` para branching basado en pattern matching
- Usar `cond` como alternativa a "else if" encadenado
- Entender que todas estas estructuras son **expresiones** que retornan valores
- Introducir `with` para encadenar matches exitosos

## Concepts

### Truthiness en Elixir: solo `false` y `nil` son falsy

A diferencia de Python o JavaScript, **en Elixir solo `false` y `nil` son falsy**. Todo lo demás es truthy:

```elixir
# Falsy: SOLO estos dos valores
if false, do: "yes", else: "no"   # "no"
if nil,   do: "yes", else: "no"   # "no"

# Truthy: todo lo demás — incluyendo 0, "", [], {}
if 0,    do: "truthy", else: "falsy"   # "truthy"  — ¡diferente a Python/JS!
if "",   do: "truthy", else: "falsy"   # "truthy"
if [],   do: "truthy", else: "falsy"   # "truthy"
if 0.0,  do: "truthy", else: "falsy"  # "truthy"
```

### if y unless

`if` es una expresión — retorna el valor de la rama ejecutada:

```elixir
# Forma compacta (una sola expresión)
result = if 10 > 5, do: "yes", else: "no"
result  # "yes"

# Forma con bloque
result = if age >= 18 do
  "adult"
else
  "minor"
end

# Sin else: retorna nil si la condición es false
if false, do: "executed"  # nil

# unless es el inverso de if
unless user == nil, do: "logged in"
unless is_nil(user), do: user.name, else: "anonymous"
```

### case: branching por pattern matching

`case` evalúa una expresión y la compara contra múltiples patrones:

```elixir
status = :ok

result = case status do
  :ok      -> "Success"
  :error   -> "Something went wrong"
  :pending -> "Still processing"
  other    -> "Unknown: #{inspect(other)}"  # catch-all
end
# "Success"
```

`case` también aplica a estructuras complejas:

```elixir
# Pattern matching con tuplas
case File.read("config.txt") do
  {:ok, content}    -> "File content: #{content}"
  {:error, :enoent} -> "File not found"
  {:error, reason}  -> "Error: #{inspect(reason)}"
end

# Pattern matching con maps
case user do
  %{role: :admin, name: name} -> "Admin: #{name}"
  %{role: :user,  name: name} -> "User: #{name}"
  _                           -> "Unknown user"
end
```

### cond: condiciones booleanas encadenadas

`cond` evalúa condiciones en orden y ejecuta la primera que sea truthy. Es el equivalente idiomático de `else if` encadenado:

```elixir
score = 75

grade = cond do
  score >= 90 -> "A"
  score >= 80 -> "B"
  score >= 70 -> "C"
  score >= 60 -> "D"
  true        -> "F"   # catch-all obligatorio — evita CondClauseError
end
# "C"
```

### Todo es una expresión

`if`, `case`, y `cond` retornan el valor de la rama ejecutada. Puedes asignarlos directamente:

```elixir
label = if n > 0, do: "positive", else: "non-positive"

status = case code do
  200 -> :ok
  404 -> :not_found
  _   -> :error
end

category = cond do
  n < 0    -> :negative
  n == 0   -> :zero
  n < 100  -> :small
  true     -> :large
end
```

### with: encadenamiento de matches exitosos (intro básica)

`with` ejecuta una secuencia de matches. Si todos hacen match, ejecuta el bloque `do`. Si alguno falla, retorna el primer valor que no hizo match:

```elixir
# Sin with: if anidados difíciles de leer
result = with {:ok, user}  <- fetch_user(id),
              {:ok, token} <- generate_token(user),
              {:ok, _sent} <- send_email(user, token) do
  {:ok, "Email sent to #{user.email}"}
end
# Si cualquier paso retorna {:error, ...}, with lo propaga directamente
```

## Exercises

### Exercise 1: if/else — forma compacta y con bloque

```elixir
age = 20

# Forma compacta
result = if age >= 18, do: "adult", else: "minor"
result  # "adult"

# Forma con bloque
label = if age >= 18 do
  "You can vote"
else
  "Too young to vote"
end
label  # "You can vote"

# if sin else retorna nil cuando la condición es false
maybe = if age < 18, do: "minor"
maybe  # nil — age no es < 18, y no hay else

# Usar if como parte de una expresión más grande
greeting = "Hello, " <> if(age >= 18, do: "adult", else: "young person") <> "!"
greeting  # "Hello, adult!"
```

**Expected output:**

```
iex> age = 20
20
iex> if age >= 18, do: "adult", else: "minor"
"adult"
iex> if age < 18, do: "minor"
nil
iex> "Hello, " <> if(age >= 18, do: "adult", else: "young person") <> "!"
"Hello, adult!"
```

---

### Exercise 2: unless

```elixir
user = %{name: "Alice", logged_in: true}
guest = nil

# unless es el inverso de if
unless guest == nil, do: "logged in", else: "anonymous"
# "anonymous"

unless user == nil, do: "logged in", else: "anonymous"
# "logged in"

# Más idiomático con is_nil/1
unless is_nil(user), do: user.name, else: "guest"
# "Alice"

# unless con bloque
message = unless user[:logged_in] == false do
  "Welcome back, #{user.name}!"
else
  "Please log in"
end
message  # "Welcome back, Alice!"
```

**Expected output:**

```
iex> guest = nil
nil
iex> unless guest == nil, do: "logged in", else: "anonymous"
"anonymous"
iex> unless is_nil(user), do: user.name, else: "guest"
"Alice"
```

---

### Exercise 3: Truthiness — 0, "", [] son truthy

```elixir
# En Elixir: SOLO false y nil son falsy

# 0 es truthy (diferente a Python/JavaScript)
if 0, do: "truthy", else: "falsy"    # "truthy"

# "" es truthy
if "", do: "truthy", else: "falsy"   # "truthy"

# [] es truthy
if [], do: "truthy", else: "falsy"   # "truthy"

# Solo false es falsy
if false, do: "truthy", else: "falsy"  # "falsy"

# Solo nil es falsy
if nil, do: "truthy", else: "falsy"    # "falsy"

# Para verificar vacío de lista, usar == o Enum.empty?
list = []
if list == [], do: "empty list", else: "has elements"    # "empty list"
if Enum.empty?(list), do: "empty", else: "not empty"     # "empty"
```

**Expected output:**

```
iex> if 0, do: "truthy", else: "falsy"
"truthy"
iex> if "", do: "truthy", else: "falsy"
"truthy"
iex> if [], do: "truthy", else: "falsy"
"truthy"
iex> if false, do: "truthy", else: "falsy"
"falsy"
iex> if nil, do: "truthy", else: "falsy"
"falsy"
```

---

### Exercise 4: case con pattern matching

```elixir
# case con átomos
status = :error

case status do
  :ok      -> "Operation succeeded"
  :error   -> "Operation failed"
  :pending -> "Still waiting"
  other    -> "Unexpected: #{inspect(other)}"
end
# "Operation failed"

# case con tuplas {:ok, value} / {:error, reason}
response = {:ok, 42}

result = case response do
  {:ok, value}        -> "Got value: #{value}"
  {:error, :timeout}  -> "Request timed out"
  {:error, reason}    -> "Error: #{inspect(reason)}"
end
# "Got value: 42"

# case con guards
n = 15
case n do
  x when x < 0   -> "negative"
  0               -> "zero"
  x when x < 10  -> "small positive"
  x when x < 100 -> "medium positive"
  _               -> "large"
end
# "medium positive"
```

**Expected output:**

```
iex> status = :error
:error
iex> case status do :ok -> "succeeded"; :error -> "failed"; _ -> "other" end
"failed"
iex> case {:ok, 42} do {:ok, value} -> "Got: #{value}"; {:error, r} -> "Error: #{r}" end
"Got: 42"
iex> case 15 do x when x < 0 -> "neg"; 0 -> "zero"; x when x < 10 -> "small"; x when x < 100 -> "medium"; _ -> "large" end
"medium"
```

---

### Exercise 5: cond — clasificar con condiciones booleanas

```elixir
# Clasificar temperatura
temp = 35

description = cond do
  temp < 0   -> "freezing"
  temp < 10  -> "cold"
  temp < 20  -> "cool"
  temp < 30  -> "warm"
  temp < 40  -> "hot"
  true       -> "extreme heat"  # catch-all OBLIGATORIO
end
# "hot"

# Clasificar número con múltiples condiciones
n = -7

category = cond do
  n < 0 and rem(n, 2) == 0 -> "negative even"
  n < 0 and rem(n, 2) != 0 -> "negative odd"
  n == 0                   -> "zero"
  n > 0 and rem(n, 2) == 0 -> "positive even"
  true                     -> "positive odd"
end
# "negative odd"

# Sin catch-all: CondClauseError si ninguna condición es truthy
# cond do
#   false -> "never"
# end
# ** (CondClauseError)
```

**Expected output:**

```
iex> temp = 35
35
iex> cond do temp < 0 -> "freezing"; temp < 10 -> "cold"; temp < 20 -> "cool"; temp < 30 -> "warm"; temp < 40 -> "hot"; true -> "extreme" end
"hot"
iex> n = -7
-7
iex> cond do n < 0 and rem(n, 2) == 0 -> "neg even"; n < 0 -> "neg odd"; n == 0 -> "zero"; true -> "positive" end
"neg odd"
```

---

### Exercise 6: case anidado y con maps

```elixir
# Parsear una respuesta de API anidada
api_response = {:ok, %{status: :active, user: %{name: "Alice", role: :admin}}}

result = case api_response do
  {:ok, %{status: :active, user: %{name: name, role: :admin}}} ->
    "Active admin: #{name}"

  {:ok, %{status: :active, user: %{name: name}}} ->
    "Active user: #{name}"

  {:ok, %{status: :inactive}} ->
    "Account inactive"

  {:error, reason} ->
    "API error: #{inspect(reason)}"
end
# "Active admin: Alice"

# case dentro de una función
defmodule ResponseHandler do
  def handle({:ok, %{status: :active} = data}), do: process(data)
  def handle({:ok, %{status: :inactive}}),       do: {:error, :account_inactive}
  def handle({:error, reason}),                  do: {:error, reason}

  defp process(%{user: %{name: name}}), do: {:ok, "Processing for #{name}"}
  defp process(_),                      do: {:error, :invalid_data}
end

ResponseHandler.handle({:ok, %{status: :active, user: %{name: "Bob"}}})
# {:ok, "Processing for Bob"}
```

**Expected output:**

```
iex> api_response = {:ok, %{status: :active, user: %{name: "Alice", role: :admin}}}
{:ok, %{status: :active, user: %{name: "Alice", role: :admin}}}
iex> case api_response do {:ok, %{status: :active, user: %{name: name, role: :admin}}} -> "Active admin: #{name}"; _ -> "other" end
"Active admin: Alice"
```

## Common Mistakes

### Error 1: Asumir que 0, "", [] son falsy (como en Python/JS)

```elixir
# WRONG: en Python/JS, 0 sería falsy. En Elixir NO.
count = 0

# Esto SIEMPRE ejecuta el bloque do — count = 0 es truthy
if count, do: IO.puts("has elements")   # se ejecuta aunque count sea 0

# FIX: ser explícito con la condición
if count > 0, do: IO.puts("has elements")
if count == 0, do: IO.puts("is zero")

# Para listas vacías
list = []
if list != [], do: IO.puts("not empty")      # correcto
if not Enum.empty?(list), do: IO.puts("not empty")  # correcto
```

### Error 2: cond sin catch-all `true ->`

```elixir
# WRONG: si ninguna condición es truthy, lanza CondClauseError
n = 50

# cond do
#   n < 0  -> "negative"
#   n == 0 -> "zero"
# end
# ** (CondClauseError) no cond clause evaluated to a truthy value

# FIX: siempre incluir true -> al final
cond do
  n < 0  -> "negative"
  n == 0 -> "zero"
  true   -> "positive"  # catch-all
end
```

### Error 3: Usar cond cuando case es más adecuado (y viceversa)

```elixir
# WRONG: usar cond para match en valores concretos es verboso
status = :ok

# Con cond (funciona pero es innecesariamente verboso):
cond do
  status == :ok    -> "success"
  status == :error -> "failure"
  true             -> "unknown"
end

# FIX: usar case cuando comparas patrones o valores concretos
case status do
  :ok    -> "success"
  :error -> "failure"
  _      -> "unknown"
end

# cond es ideal cuando las condiciones son booleanas independientes:
cond do
  String.length(name) < 2        -> "name too short"
  not String.match?(name, ~r/^\w+$/) -> "invalid characters"
  true                           -> "valid"
end
```

### Error 4: Olvidar que if sin else retorna nil

```elixir
# WRONG: asumir que si no se ejecuta el do, el resultado es ""
label = if user.active, do: "Active"
# label es nil si user.active es false — no es ""

# FIX: siempre incluir else cuando el resultado importa
label = if user.active, do: "Active", else: "Inactive"

# O verificar nil explícitamente
label = if user.active, do: "Active"
display = label || "Status unknown"
```

## Verification

```bash
elixir -e '
n = 75

grade = cond do
  n >= 90 -> "A"
  n >= 80 -> "B"
  n >= 70 -> "C"
  n >= 60 -> "D"
  true    -> "F"
end

IO.puts("Grade: #{grade}")

result = case {n > 70, rem(n, 2) == 0} do
  {true, true}  -> "high and even"
  {true, false} -> "high and odd"
  {false, _}    -> "low"
end

IO.puts("Result: #{result}")
IO.puts("0 is truthy: #{if 0, do: "yes", else: "no"}")
IO.puts("nil is falsy: #{if nil, do: "yes", else: "no"}")
'
```

**Expected output:**

```
Grade: C
Result: high and odd
0 is truthy: yes
nil is falsy: no
```

## Summary

- **Solo `false` y `nil` son falsy** en Elixir — `0`, `""`, `[]` son truthy
- `if`/`unless` son para condiciones simples; retornan `nil` si no hay `else` y la condición falla
- `case` usa **pattern matching** — ideal para branching sobre formas distintas de datos
- `cond` usa **condiciones booleanas** — ideal como "else if" encadenado; requiere `true ->` al final
- Todas son **expresiones**: retornan un valor que puedes asignar directamente

## What's Next

- **10-recursion-and-tail-call-optimization**: Usar `case` y pattern matching en funciones recursivas
- **Módulos más avanzados**: `with` para pipelines de operaciones que pueden fallar

## Resources

- [Elixir Getting Started — case, cond, and if](https://elixir-lang.org/getting-started/case-cond-and-if.html)
- [Elixir School — Control Structures](https://elixirschool.com/en/lessons/basics/control_structures)
- [Elixir Docs — Kernel.if/2](https://hexdocs.pm/elixir/Kernel.html#if/2)
- [Elixir Docs — Special Forms — case](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#case/2)
