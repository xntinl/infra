# 16. Self-Documenting Justfile

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Justfile auto-documentado con atributos `[doc]` y `[group]` |
| **Dificultad** | Desafio |

## Que vas a aprender

- **Atributo `[doc("...")]`** — Agrega documentacion descriptiva a cada recipe que se muestra en `just --list`. Reemplaza el uso de comentarios `#` encima de la recipe.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)
- **Atributo `[group("...")]`** — Organiza las recipes en categorias logicas. `just --list` agrupa las recipes bajo sus respectivos headers.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)
- **`just --list`** — Comando que muestra todas las recipes disponibles con su documentacion y agrupacion, creando una interfaz auto-documentada.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Crea un justfile donde cada receta tenga un `[doc]` y `[group]` apropiado, de forma que `just --list` produzca una salida perfectamente organizada y documentada.

## Codigo

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

default:
    @just --list

# -- Build ----------------------------------------------------

[doc("Compilar el proyecto en modo debug")]
[group("build")]
build:
    echo "Building debug..."

[doc("Compilar el proyecto en modo release optimizado")]
[group("build")]
build-release:
    echo "Building release..."

[doc("Limpiar artefactos de compilacion")]
[group("build")]
clean:
    echo "Cleaning..."

# -- Test -----------------------------------------------------

[doc("Ejecutar todos los tests")]
[group("test")]
test:
    echo "Running tests..."

[doc("Ejecutar tests con coverage report")]
[group("test")]
test-coverage:
    echo "Running tests with coverage..."

[doc("Ejecutar tests en modo watch")]
[group("test")]
test-watch:
    echo "Watching tests..."

# -- Lint -----------------------------------------------------

[doc("Ejecutar linter sobre todo el codigo")]
[group("lint")]
lint:
    echo "Linting..."

[doc("Verificar formato del codigo")]
[group("lint")]
fmt-check:
    echo "Checking format..."

[doc("Formatear el codigo automaticamente")]
[group("lint")]
fmt:
    echo "Formatting..."

# -- Deploy ---------------------------------------------------

[doc("Deploy a staging")]
[group("deploy")]
deploy-staging:
    echo "Deploying to staging..."

[doc("Deploy a produccion (requiere confirmacion)")]
[group("deploy")]
[confirm("Estas seguro de hacer deploy a produccion?")]
deploy-prod:
    echo "Deploying to prod..."
```

## Verificacion

1. `just --list` muestra las recipes agrupadas bajo "build", "test", "lint" y "deploy".
2. Cada recipe muestra su descripcion del atributo `[doc]`.
3. La salida esta limpia, organizada y sirve como documentacion del proyecto.
4. `just deploy-prod` solicita confirmacion antes de ejecutar.

## Solucion y Aprendizaje

- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html) — Documentacion completa de atributos como `[doc]`, `[group]`, `[confirm]` y `[private]`.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de just incluyendo self-documenting patterns.
- [Loopwerk - One Command to Run Them All](https://www.loopwerk.io/articles/2025/just-command-runner/) — Articulo sobre la organizacion de justfiles auto-documentados.

## Recursos

- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html)
- [Just Manual](https://just.systems/man/en/)

## Notas

_Espacio para tus notas personales._
