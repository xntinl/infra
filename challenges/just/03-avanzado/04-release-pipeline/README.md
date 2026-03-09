# 14. Release Pipeline

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Complete release workflow con checks, git tags, publishing, colors |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **`require()`** — Funcion built-in que verifica que una herramienta externa esta disponible en el PATH al momento de cargar el justfile. Si no la encuentra, falla inmediatamente con un mensaje claro antes de ejecutar cualquier receta.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_34.html)

- **Constantes de color (`RED`, `GREEN`, `BOLD`, `NORMAL`)** — Constantes predefinidas en just que permiten dar formato con colores a la salida del terminal. Se usan dentro de `{{RED}}texto{{NORMAL}}` para resaltar errores, exitos y mensajes importantes.
  [Documentacion: Constants](https://just.systems/man/en/chapter_43.html)

- **`[confirm]` con mensaje personalizado** — El atributo `[confirm("mensaje")]` muestra un prompt especifico antes de ejecutar la receta. Esencial para operaciones irreversibles como publicar un release o crear un tag en git.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Git tag workflow** — Patron donde las recetas coordinan la creacion y publicacion de tags de git como parte de un pipeline de release, incluyendo validaciones previas como verificar que el working directory esta limpio y que el tag no existe.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Pre-release validation chain** — Uso de dependencias entre recetas para crear una cadena de validacion: verificar git limpio, verificar tag disponible, lint, test y build, todo antes de permitir el release.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Vas a construir un pipeline de release completo que valida, compila, etiqueta y publica versiones de un proyecto. El justfile lee la version desde un archivo `VERSION`, verifica que el working directory de git este limpio, ejecuta lint y tests, compila el artefacto, y finalmente crea un tag de git con confirmacion del usuario. Los mensajes usan colores para indicar claramente errores en rojo, exitos en verde y texto destacado en negrita. Incluye un modo dry-run para simular el release sin efectos secundarios.

## Codigo

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

project := "myapp"
version := `cat VERSION 2>/dev/null || echo "0.0.0"`
git_clean := `git diff --quiet 2>/dev/null && echo "true" || echo "false"`

_ := require("git")

default:
    @just --list

# Verificar que git esta limpio
[private]
_verify-clean:
    #!/usr/bin/env bash
    if [[ "{{git_clean}}" != "true" ]]; then
        echo "{{RED}}Error: Working directory no esta limpio{{NORMAL}}"
        echo "Haz commit o stash de tus cambios primero."
        exit 1
    fi
    echo "{{GREEN}}Working directory limpio{{NORMAL}}"

# Verificar que el tag no existe
[private]
_verify-no-tag:
    #!/usr/bin/env bash
    if git tag -l "v{{version}}" | grep -q .; then
        echo "{{RED}}Error: Tag v{{version}} ya existe{{NORMAL}}"
        exit 1
    fi
    echo "{{GREEN}}Tag v{{version}} disponible{{NORMAL}}"

# Lint
[group('quality')]
lint:
    @echo "Linting..."

# Tests
[group('quality')]
test:
    @echo "Running tests..."

# Build release
[group('build')]
build-release:
    @echo "{{BOLD}}Building v{{version}}...{{NORMAL}}"
    mkdir -p dist
    @echo "binary-v{{version}}" > dist/{{project}}

# Pre-release: todos los checks
[group('release')]
pre-release: _verify-clean _verify-no-tag lint test build-release
    @echo "{{GREEN}}{{BOLD}}Pre-release checks passed!{{NORMAL}}"

# Publicar release
[group('release')]
[confirm("Publicar {{project}} v{{version}}?")]
release: pre-release
    git tag -a "v{{version}}" -m "Release v{{version}}"
    git push origin "v{{version}}"
    @echo "{{GREEN}}{{BOLD}}Release v{{version}} publicado!{{NORMAL}}"

# Dry run de release
[group('release')]
release-dry: pre-release
    @echo "{{YELLOW}}Dry run: se crearia tag v{{version}}{{NORMAL}}"

# Mostrar info de release
[group('release')]
release-info:
    @echo "{{BOLD}}Release Info{{NORMAL}}"
    @echo "  Proyecto: {{project}}"
    @echo "  Version:  {{version}}"
    @echo "  Git limpio: {{git_clean}}"
    @echo "  Tags existentes:"
    @git tag -l "v*" | tail -5 || echo "    (ninguno)"
```

## Verificacion

1. Ejecuta `just release-info` y verifica que muestra la informacion del proyecto con formato en negrita.
2. Ejecuta `just release-dry` y confirma que corre todos los checks sin crear ningun tag de git.
3. Ejecuta `just release` y verifica que solicita confirmacion, valida todo, y crea el tag de git.
4. Con cambios sin commit en el working directory, verifica que `_verify-clean` falla mostrando el mensaje en rojo.
5. Con un tag que ya existe, verifica que `_verify-no-tag` falla mostrando el error en rojo.
6. Confirma que los colores (RED, GREEN, BOLD) se muestran correctamente en tu terminal.

## Solucion y Aprendizaje

- [jstrong.dev - Publishing Crates: a Justfile Workflow](https://jstrong.dev/posts/2023/publishing-crates-a-justfile-workflow/) — Ejemplo real de un workflow de publicacion de crates en Rust usando just con validaciones y tags de git.
- [Just Manual - Constants](https://just.systems/man/en/chapter_43.html) — Referencia de las constantes predefinidas como RED, GREEN, BOLD, NORMAL y YELLOW para formatear la salida.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido completo por las funcionalidades de just, incluyendo `require()`, atributos y cadenas de dependencias.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
