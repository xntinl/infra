# D1. Self-Documenting Justfile

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | `[doc]`, `[group]`, `just --list` como documentacion |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **`[doc()]`** — Atributo que define la descripcion de una receta que se muestra en `just --list`. A diferencia de los comentarios (`#`), `[doc()]` es la forma explicita y recomendada de documentar recetas, y tiene prioridad sobre los comentarios.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **`[group()]`** — Atributo que asigna una receta a una categoria nombrada. Las recetas con `[group]` aparecen organizadas por seccion en la salida de `just --list`, facilitando la navegacion en justfiles grandes.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Formato de `just --list`** — La salida de `just --list` se convierte en la documentacion viva del proyecto. Combinar `[doc]`, `[group]` y `[private]` produce un listado claro, categorizado y sin ruido.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Comentarios de receta vs `[doc]`** — Los comentarios con `#` encima de una receta tambien aparecen en `just --list`, pero `[doc()]` los sobreescribe. Entender esta prioridad es clave para documentar intencionalmente.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Vas a crear un justfile donde cada receta esta documentada con `[doc()]` y organizada en grupos con `[group()]`, produciendo una salida de `just --list` perfectamente estructurada que sirve como documentacion del proyecto. Las recetas internas se marcan como `[private]` para que no aparezcan en el listado. El objetivo es que cualquier desarrollador nuevo pueda ejecutar `just` y entender inmediatamente que comandos estan disponibles y para que sirve cada uno.

## Codigo

```justfile
set shell := ["bash", "-uc"]

default:
    @just --list

# ── Build ─────────────────────────────────────────────

[doc("Compilar el proyecto en modo debug")]
[group('build')]
build:
    @echo "Building debug..."

[doc("Compilar optimizado para produccion")]
[group('build')]
build-release:
    @echo "Building release..."

[doc("Compilar para Linux x86_64")]
[group('build')]
build-linux:
    @echo "Building for linux..."

# ── Test ──────────────────────────────────────────────

[doc("Ejecutar todos los tests")]
[group('test')]
test:
    @echo "Testing..."

[doc("Tests con output verbose")]
[group('test')]
test-verbose:
    @echo "Testing verbose..."

[doc("Tests de un modulo especifico")]
[group('test')]
test-filter pattern:
    @echo "Testing {{pattern}}..."

# ── Quality ───────────────────────────────────────────

[doc("Ejecutar linter con auto-fix")]
[group('quality')]
lint:
    @echo "Linting..."

[doc("Verificar formato del codigo")]
[group('quality')]
fmt-check:
    @echo "Checking format..."

[doc("Pipeline CI completo")]
[group('quality')]
ci: fmt-check lint test build
    @echo "CI passed!"

# ── Deploy ────────────────────────────────────────────

[doc("Desplegar al entorno especificado")]
[group('deploy')]
[confirm("Desplegar en {{env}}?")]
deploy env:
    @echo "Deploying to {{env}}..."

# ── Helpers (ocultos) ─────────────────────────────────

[private]
_setup:
    @echo "Internal setup..."
```

## Verificacion

1. Ejecuta `just --list` y verifica que las recetas aparecen organizadas por grupo, con las descripciones definidas en `[doc()]`.
2. Confirma que `_setup` NO aparece en el listado gracias al atributo `[private]`.
3. Verifica que los grupos aparecen en orden alfabetico: build, deploy, quality, test.
4. Confirma que cada receta muestra la descripcion de su `[doc]`, no el comentario `#` que pueda tener encima.

## Solucion y Aprendizaje

- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html) — Referencia completa de atributos incluyendo `[doc()]`, `[group()]` y `[private]`.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las funcionalidades de just que incluye ejemplos de documentacion y organizacion de recetas.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
