# 6. Justfile para Proyecto Rust

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Justfile completo para proyecto Rust con clippy, nextest, coverage, feature flags |
| **Dificultad** | Intermedio |

## Que vas a aprender

- **`cargo metadata` para valores dinamicos** — Usando backtick expressions con `cargo metadata` y `jq`, puedes extraer automaticamente el nombre y la version del proyecto desde `Cargo.toml`, evitando duplicacion de informacion.
  [Documentacion: Backtick Expressions](https://just.systems/man/en/chapter_37.html)

- **`--workspace`, `--all-targets`, `--all-features`** — Flags de cargo que aseguran que los comandos se aplican a todos los crates del workspace, todos los targets (bins, libs, tests, benches) y todas las features, garantizando una verificacion completa.
  [Documentacion: Recipes](https://just.systems/man/en/chapter_30.html)

- **Organizacion con `[group]`** — El atributo `[group]` permite categorizar recipes en secciones logicas (build, test, quality, docs, dev) que se muestran agrupadas en la salida de `just --list`.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Constantes de color (`BOLD`, `GREEN`, `NORMAL`)** — `just` proporciona constantes built-in para formatear la salida del terminal con colores y estilos, mejorando la legibilidad de los mensajes.
  [Documentacion: Constants](https://just.systems/man/en/chapter_39.html)

- **`export` para variables de entorno** — Usando `export VARIABLE := "valor"`, las variables del justfile se exportan automaticamente como variables de entorno disponibles para todos los comandos ejecutados por las recipes.
  [Documentacion: Settings](https://just.systems/man/en/chapter_26.html)

## Descripcion

En este ejercicio crearas un justfile completo para un workspace de Rust. Incluye recipes para compilar en modo debug y release, ejecutar tests con y sin output detallado, verificar la calidad del codigo con clippy y fmt, generar documentacion, y recipes de desarrollo. El proyecto usa `cargo metadata` para extraer informacion automaticamente y constantes de color para mensajes destacados.

## Codigo

Crea el `justfile` con el siguiente contenido:

```justfile
set dotenv-load
set export
set shell := ["bash", "-uc"]

project_name := `cargo metadata --format-version 1 --no-deps | jq -r '.packages[0].name' 2>/dev/null || echo "myapp"`
version := `cargo metadata --format-version 1 --no-deps | jq -r '.packages[0].version' 2>/dev/null || echo "0.0.0"`

export RUST_BACKTRACE := "1"
export RUST_LOG := env("RUST_LOG", "info")

default:
    @just --list --unsorted

[group('build')]
build:
    cargo build

[group('build')]
build-release:
    cargo build --release

[group('test')]
test:
    cargo test --workspace

[group('test')]
test-verbose:
    cargo test --workspace -- --nocapture

[group('quality')]
clippy:
    cargo clippy --workspace --all-targets --all-features -- -D warnings

[group('quality')]
fmt:
    cargo fmt --all

[group('quality')]
fmt-check:
    cargo fmt --all -- --check

# CI: todos los checks
[group('quality')]
ci: fmt-check clippy test
    @echo "{{GREEN}}Todos los checks pasaron!{{NORMAL}}"

[group('docs')]
docs:
    cargo doc --workspace --no-deps --all-features --open

[group('dev')]
run *args:
    cargo run -- {{args}}

[group('dev')]
watch:
    cargo watch -x check -x 'test -- --nocapture' -x 'clippy -- -D warnings'

[group('dev')]
clean:
    cargo clean

[group('dev')]
info:
    @echo "{{BOLD}}Proyecto:{{NORMAL}} {{project_name}}"
    @echo "{{BOLD}}Version:{{NORMAL}}  {{version}}"
    @echo "{{BOLD}}Rust:{{NORMAL}}     $(rustc --version)"
```

## Verificacion

1. Ejecuta `just` sin argumentos. Debe mostrar todas las recipes organizadas por grupo:
   ```
   just
   ```

2. Ejecuta `just ci`. Debe ejecutar `fmt-check`, `clippy` y `test` secuencialmente, mostrando un mensaje verde al finalizar:
   ```
   just ci
   ```

3. Ejecuta `just info`. Debe mostrar la informacion del proyecto con formato en negrita:
   ```
   just info
   ```

4. Ejecuta `just build-release`. Debe compilar el proyecto en modo release optimizado:
   ```
   just build-release
   ```

## Solucion y Aprendizaje

- [Just Manual](https://just.systems/man/en/) — Documentacion completa de `just`, incluyendo settings, atributos y constantes.
- [miguno/rust-template justfile](https://github.com/miguno/rust-template/blob/main/justfile) — Ejemplo real de un justfile completo para un proyecto Rust con mejores practicas.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just` con ejemplos practicos.

## Recursos

- [Just GitHub](https://github.com/casey/just)
- [miguno/rust-template](https://github.com/miguno/rust-template)
- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html)

## Notas

_Espacio para tus notas personales._
