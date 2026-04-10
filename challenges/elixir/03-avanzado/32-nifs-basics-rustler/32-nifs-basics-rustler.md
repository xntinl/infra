# 32 — NIFs Básicos con Rustler

**Nivel**: Avanzado  
**Tema**: Native Implemented Functions con Rustler para código nativo en Rust

---

## Contexto

Una **NIF** (Native Implemented Function) es una función implementada en código nativo
(C o Rust) que se llama desde Elixir como si fuera una función Elixir normal. La VM
no distingue entre una función definida en BEAM bytecode y una NIF.

La diferencia crítica: **un crash en una NIF es un crash de toda la VM**. No hay
try/catch. No hay supervisores que puedan rescatarte. El proceso que llamó la NIF
no recibe un error — la VM entera termina.

### Cuándo un NIF tiene sentido

```
¿Tienes un cuello de botella probado por benchmarks?
   ├─ NO  →  Usa Elixir puro. No premature optimize.
   └─ SÍ  →  ¿Es CPU-bound?
               ├─ NO  →  Probablemente es I/O → usa Tasks/async
               └─ SÍ  →  ¿Ya intentaste pure Elixir optimizado?
                           ├─ NO  →  Intenta primero
                           └─ SÍ  →  ¿Necesitas < 1ms sin context switch?
                                       ├─ NO  →  Port puede ser suficiente
                                       └─ SÍ  →  Considera NIF con Rustler
```

### Rustler — NIFs seguros en Rust

[Rustler](https://github.com/rusterlium/rustler) es una biblioteca que hace NIFs
en Rust mucho más seguros que en C:

- El ownership system de Rust previene use-after-free y buffer overflows
- `rustler::NifResult<T>` para manejo explícito de errores
- Terms tipados: `rustler::Binary`, `rustler::Term`, `rustler::Atom`
- Panics en Rust se convierten en errores BEAM (no crash de VM en las versiones modernas)

### Estructura de un proyecto con Rustler

```
my_app/
├── lib/
│   └── my_nif.ex          # wrapper Elixir
├── native/
│   └── my_nif/
│       ├── Cargo.toml
│       └── src/
│           └── lib.rs
└── mix.exs                # :rustler en deps
```

### Setup en mix.exs

```elixir
defp deps do
  [
    {:rustler, "~> 0.34"}
  ]
end
```

### Módulo Elixir que carga el NIF

```elixir
defmodule MyApp.MyNif do
  use Rustler,
    otp_app: :my_app,
    crate: "my_nif"      # nombre del crate en native/

  # Placeholder — reemplazado por la implementación Rust al cargar
  def my_function(_arg), do: :erlang.nif_error(:nif_not_loaded)
end
```

### Rust con Rustler

```rust
// native/my_nif/src/lib.rs
use rustler::{Encoder, Env, NifResult, Term};

#[rustler::nif]
fn add(a: i64, b: i64) -> i64 {
    a + b
}

rustler::init!("Elixir.MyApp.MyNif", [add]);
```

### Dirty NIFs — para trabajo largo

Un NIF que tarda más de 1ms bloquea el scheduler de BEAM, degradando todo el sistema.
Los **dirty NIFs** corren en threads separados que no bloquean los schedulers normales.

```rust
// CPU intensivo: dirty_cpu
#[rustler::nif(schedule = "DirtyCpu")]
fn heavy_computation(data: Binary) -> Vec<u8> {
    // puede tardar mucho sin afectar schedulers BEAM
    compute_hash(data.as_slice())
}

// I/O intensivo: dirty_io
#[rustler::nif(schedule = "DirtyIo")]
fn read_file(path: String) -> NifResult<Vec<u8>> {
    std::fs::read(path).map_err(|e| rustler::Error::Term(Box::new(e.to_string())))
}
```

### Tipos Rust ↔ Elixir en Rustler

| Elixir | Rust |
|---|---|
| `integer` | `i64`, `u64`, `i32`, etc. |
| `float` | `f64` |
| `binary` | `rustler::Binary<'a>` o `Vec<u8>` |
| `string` (binary UTF-8) | `String` |
| `list` | `Vec<T>` |
| `tuple` | `(A, B, C)` |
| `atom` | `rustler::Atom` |
| `:ok` / `:error` | `Ok(T)` / `Err(E)` via `NifResult<T>` |

---

## Ejercicio 1 — Fibonacci NIF en Rust

Implementa `fib(n)` como NIF en Rust usando Rustler y compara el rendimiento
con la implementación Elixir pura. El objetivo es entender el overhead de llamar
un NIF y cuándo realmente vale la pena.

### Requisitos

- Módulo Elixir `FibNif` con `fib_nif/1` (implementado en Rust)
- Módulo Elixir `FibElixir` con `fib/1` (implementado en Elixir puro, iterativo)
- Benchmark con `:timer.tc/1` para n = 30, 35, 40
- El NIF debe manejar el caso `n < 0` retornando `{:error, :negative_input}`
- El NIF es regular (no dirty) — fib(40) en Rust es rápido

### Uso esperado

```elixir
FibNif.fib_nif(10)   #=> {:ok, 55}
FibNif.fib_nif(-1)   #=> {:error, :negative_input}
FibNif.fib_nif(40)   #=> {:ok, 102334155}

# Benchmark:
{time_nif, _}   = :timer.tc(fn -> FibNif.fib_nif(40) end)
{time_elixir, _} = :timer.tc(fn -> FibElixir.fib(40) end)
IO.puts("NIF: #{time_nif}μs, Elixir: #{time_elixir}μs")
# Para n=40, NIF típicamente 10x-100x más rápido
```

### Hints

<details>
<summary>Hint 1 — Cargo.toml del crate</summary>

```toml
[package]
name = "fib_nif"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
rustler = "0.34"
```

El tipo `cdylib` es necesario para generar una biblioteca dinámica que la BEAM pueda cargar.
</details>

<details>
<summary>Hint 2 — Implementación Rust con NifResult</summary>

```rust
use rustler::NifResult;

#[rustler::nif]
fn fib_nif(n: i64) -> NifResult<u64> {
    if n < 0 {
        return Err(rustler::Error::Atom("negative_input"));
    }
    Ok(fib_impl(n as u64))
}

fn fib_impl(n: u64) -> u64 {
    match n {
        0 => 0,
        1 => 1,
        _ => {
            let mut a = 0u64;
            let mut b = 1u64;
            for _ in 2..=n {
                let c = a + b;
                a = b;
                b = c;
            }
            b
        }
    }
}

rustler::init!("Elixir.FibNif", [fib_nif]);
```

`NifResult<T>` mapea automáticamente: `Ok(v)` → `{:ok, v}`, `Err(atom)` → `{:error, atom}`.
</details>

<details>
<summary>Hint 3 — Elixir iterativo para comparar</summary>

```elixir
defmodule FibElixir do
  def fib(n) when n < 0, do: {:error, :negative_input}
  def fib(n), do: {:ok, fib_iter(n, 0, 1)}

  defp fib_iter(0, acc, _), do: acc
  defp fib_iter(n, acc, next), do: fib_iter(n - 1, next, acc + next)
end
```

Elixir tail-recursive es muy rápido. El NIF brilla más en algoritmos con
overhead de boxing/unboxing bajo y computation alta (procesado de binarios, matrices).
</details>

---

## Ejercicio 2 — Dirty NIF: SHA-256 de Binario Grande

Implementa `sha256/1` como dirty_cpu NIF en Rust. El objetivo es entender cuándo
un NIF *debe* ser dirty y las consecuencias de no serlo.

### Requisitos

- `Sha256Nif.sha256(binary)` → `{:ok, hex_string}` donde hex_string es el hash en hex
- El NIF usa `schedule = "DirtyCpu"` porque procesa binarios potencialmente grandes
- Demuestra el problema: implementa también `sha256_blocking/1` como NIF regular
  y mide el impacto en la latencia de otros procesos durante la llamada
- Usa la crate `sha2` de Rust (no `:crypto` de Erlang — el ejercicio es hacer el NIF)

### Uso esperado

```elixir
data = :crypto.strong_rand_bytes(10 * 1024 * 1024)  # 10MB

Sha256Nif.sha256(data)
#=> {:ok, "a665a45920422f9d417e4867efdc4fb8a04a1f3fff1fa07e998e86f7f7a27ae3"}

# Con dirty: otros procesos no se ven afectados durante el cálculo
# Sin dirty (blocking): el scheduler BEAM se bloquea durante el cálculo
```

### El problema del NIF bloqueante — demostración

```elixir
# Proceso A: llama al NIF bloqueante con datos grandes
Task.async(fn -> Sha256Nif.sha256_blocking(big_binary) end)

# Proceso B: debería responder rápido, pero...
Task.async(fn ->
  start = System.monotonic_time(:millisecond)
  Sha256Nif.sha256(<<1>>)  # operación trivial
  elapsed = System.monotonic_time(:millisecond) - start
  IO.puts("Tardó: #{elapsed}ms")  # puede tardar segundos si scheduler bloqueado
end)
```

### Hints

<details>
<summary>Hint 1 — Cargo.toml con sha2</summary>

```toml
[dependencies]
rustler = "0.34"
sha2 = "0.10"
hex = "0.4"
```
</details>

<details>
<summary>Hint 2 — Dirty NIF con Binary de Rustler</summary>

```rust
use rustler::Binary;
use sha2::{Digest, Sha256};

#[rustler::nif(schedule = "DirtyCpu")]
fn sha256(data: Binary) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data.as_slice());
    let result = hasher.finalize();
    format!("{:x}", result)
}

// NIF bloqueante (sin dirty) para comparación — NO hagas esto en producción
#[rustler::nif]
fn sha256_blocking(data: Binary) -> String {
    let mut hasher = Sha256::new();
    hasher.update(data.as_slice());
    let result = hasher.finalize();
    format!("{:x}", result)
}

rustler::init!("Elixir.Sha256Nif", [sha256, sha256_blocking]);
```
</details>

<details>
<summary>Hint 3 — Medir impacto en scheduler</summary>

```elixir
defmodule SchedulerImpact do
  def demo(big_binary) do
    # Lanzar tarea bloqueante
    Task.start(fn ->
      IO.puts("Iniciando sha256 bloqueante...")
      Sha256Nif.sha256_blocking(big_binary)
      IO.puts("sha256 bloqueante terminado")
    end)

    # Medir latencia de operaciones simples durante el cálculo
    for i <- 1..5 do
      {t, _} = :timer.tc(fn -> Sha256Nif.sha256(<<i>>) end)
      IO.puts("Operación #{i}: #{t}μs")
    end
  end
end
```

Con el scheduler bloqueado, las operaciones simples pueden tardar cientos de ms
en vez de microsegundos. Con `DirtyCpu`, el impacto es mínimo.
</details>

---

## Ejercicio 3 — Safety: Manejo de Errores y Estrategias de Mitigación

Explora qué ocurre cuando un NIF tiene bugs y cómo diseñar defensivamente.
Este ejercicio es principalmente conceptual con código de demostración.

### Escenario A — Panic en Rust (recoverable con Rustler moderno)

```rust
// Un NIF que puede hacer panic
#[rustler::nif]
fn risky_nif(n: i64) -> i64 {
    if n == 0 {
        panic!("division by zero demonstration");
    }
    100 / n
}
```

En Rustler >= 0.31, los panics de Rust **se capturan** y se convierten en
excepciones BEAM — el proceso que llamó el NIF muere, pero la VM sobrevive.
Esto es diferente al comportamiento clásico de NIFs en C.

### Escenario B — Long-running NIF sin dirty (problema real)

```rust
#[rustler::nif]  // ¡SIN dirty! Bug de diseño
fn infinite_loop(_: i64) -> i64 {
    loop {
        // Este NIF bloquea un scheduler BEAM para siempre
        std::hint::black_box(1 + 1);
    }
}
```

No hay forma de interrumpir este NIF desde Elixir. La VM tiene timeslice-based
preemption para código BEAM pero NO para NIFs. El scheduler queda bloqueado.

### Requisitos del ejercicio

1. Implementar `SafeNif.divide/2` que maneje división por cero retornando `{:error, :division_by_zero}`
2. Implementar `SafeNif.process_chunks/2` — procesa un binario en chunks de N bytes,
   llamando al NIF repetidamente en vez de un solo NIF largo (reducción voluntaria de trabajo)
3. Documentar con comentarios la diferencia entre panic-safe (Rustler moderno) y memory-unsafe (C NIFs)
4. Escribir un test ExUnit que verifique que el NIF retorna error limpiamente sin crashear la VM

### El patrón de chunks — NIFs que ceden el scheduler

```elixir
# En vez de un NIF que procesa 100MB de una vez:
def process_large(data) do
  data
  |> chunk_binary(1024 * 1024)   # chunks de 1MB
  |> Enum.reduce(initial_state(), fn chunk, acc ->
    SafeNif.process_chunk(chunk, acc)  # NIF rápido por chunk
  end)
end
```

### Hints

<details>
<summary>Hint 1 — Error handling en Rustler sin panic</summary>

```rust
use rustler::{NifResult, Error};

#[rustler::nif]
fn divide(a: i64, b: i64) -> NifResult<i64> {
    if b == 0 {
        return Err(Error::Atom("division_by_zero"));
    }
    Ok(a / b)
}
```

Siempre prefiere `NifResult<T>` y retornar `Err(...)` sobre hacer `panic!`.
Los errores son parte del flujo normal — los panics son bugs.
</details>

<details>
<summary>Hint 2 — Chunked processing pattern</summary>

```rust
// NIF que procesa un chunk y retorna estado parcial
#[rustler::nif(schedule = "DirtyCpu")]
fn process_chunk(chunk: Binary, state: Vec<u8>) -> NifResult<Vec<u8>> {
    // Procesar chunk, actualizar estado
    let mut new_state = state;
    new_state.extend_from_slice(chunk.as_slice());
    Ok(new_state)
}
```

```elixir
defp chunk_binary(binary, chunk_size) do
  for <<chunk::binary-size(chunk_size) <- binary>>, do: chunk
end
```
</details>

<details>
<summary>Hint 3 — Test ExUnit para comportamiento del NIF</summary>

```elixir
defmodule SafeNifTest do
  use ExUnit.Case

  test "divide retorna error en división por cero sin crashear VM" do
    assert {:error, :division_by_zero} = SafeNif.divide(10, 0)
    # Verificar que la VM sigue viva
    assert {:ok, 5} = SafeNif.divide(10, 2)
  end

  test "panic en NIF no mata el proceso padre" do
    # El proceso que llama el NIF panicky debe morir
    # pero el test process (padre) sobrevive
    result = Task.async(fn -> SafeNif.risky_nif(0) end) |> Task.await()
    # Con Rustler moderno, el Task muere pero nosotros no
    # Esto es intentional — documentar el comportamiento
  end
end
```
</details>

---

## Trade-offs a considerar

### ¿NIF o no NIF? La checklist real

Antes de escribir un NIF, responde:

1. **¿Has medido?** Nunca optimizes sin profiling. `benchee` primero.
2. **¿Es CPU-bound?** Si es I/O, los Ports son más seguros.
3. **¿Cuánto dura?** < 1ms → NIF regular. > 1ms → dirty NIF obligatorio.
4. **¿Qué pasa si falla?** Con C NIFs, crashea la VM. Con Rustler, es más seguro.
5. **¿Vale el costo de complejidad?** Un proyecto Rust aumenta significativamente
   la complejidad del build y del onboarding del equipo.

### Rustler vs C NIFs

| | C NIF | Rustler (Rust) |
|---|---|---|
| Seguridad de memoria | Manual, unsafe | Garantizada por borrow checker |
| Panics | VM crash | Capturado por Rustler (>= 0.31) |
| Complejidad de build | Moderada | Mayor (requiere Rust toolchain) |
| Performance | Máxima | Equivalente (sin overhead) |
| Ecosistema de libs | C/C++ (vastísimo) | Rust (crates.io, creciendo) |

### El regla del 1ms

La BEAM tiene un scheduler preemptivo para código BEAM: cada proceso tiene
~1ms antes de ser preemptado (yield). Los NIFs **no son preemptibles**. Un NIF
de 10ms bloquea un thread de scheduler por 10ms, afectando a todos los procesos
en ese scheduler. Por eso los dirty schedulers existen: corren en threads aparte
sin afectar los schedulers normales de BEAM.

---

## One possible solution

<details>
<summary>Ver solución (spoiler)</summary>

```elixir
# lib/fib_nif.ex
defmodule FibNif do
  use Rustler, otp_app: :my_app, crate: "fib_nif"

  def fib_nif(_n), do: :erlang.nif_error(:nif_not_loaded)
end

# native/fib_nif/src/lib.rs
use rustler::NifResult;

#[rustler::nif]
fn fib_nif(n: i64) -> NifResult<u64> {
    if n < 0 {
        return Err(rustler::Error::Atom("negative_input"));
    }
    let mut a = 0u64;
    let mut b = 1u64;
    for _ in 0..n {
        let c = a.wrapping_add(b);
        a = b;
        b = c;
    }
    Ok(a)
}

rustler::init!("Elixir.FibNif", [fib_nif]);

# ---

# lib/sha256_nif.ex
defmodule Sha256Nif do
  use Rustler, otp_app: :my_app, crate: "sha256_nif"

  def sha256(_data),          do: :erlang.nif_error(:nif_not_loaded)
  def sha256_blocking(_data), do: :erlang.nif_error(:nif_not_loaded)
end

# native/sha256_nif/src/lib.rs
use rustler::Binary;
use sha2::{Digest, Sha256};

#[rustler::nif(schedule = "DirtyCpu")]
fn sha256(data: Binary) -> String {
    format!("{:x}", Sha256::digest(data.as_slice()))
}

#[rustler::nif]
fn sha256_blocking(data: Binary) -> String {
    format!("{:x}", Sha256::digest(data.as_slice()))
}

rustler::init!("Elixir.Sha256Nif", [sha256, sha256_blocking]);

# ---

# lib/safe_nif.ex
defmodule SafeNif do
  use Rustler, otp_app: :my_app, crate: "safe_nif"

  def divide(_a, _b),          do: :erlang.nif_error(:nif_not_loaded)
  def process_chunk(_chunk, _state), do: :erlang.nif_error(:nif_not_loaded)

  def process_large(data, chunk_size \\ 1024 * 1024) do
    data
    |> chunk_binary(chunk_size)
    |> Enum.reduce(<<>>, fn chunk, acc ->
      {:ok, new_acc} = process_chunk(chunk, acc)
      new_acc
    end)
  end

  defp chunk_binary(binary, size) do
    for <<chunk::binary-size(size) <- binary>>, do: chunk
  end
end
```

</details>
