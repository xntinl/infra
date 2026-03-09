# D4. Justfile de Productividad Personal

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Personal justfile, `set fallback`, daily tasks, project scaffolding |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **`set fallback`** — Configuracion que hace que just busque un justfile en directorios padre si no encuentra uno en el directorio actual. Permite tener un justfile global que funciona desde cualquier subdirectorio.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Personal justfile pattern (`~/.user.justfile`)** — Convencion de tener un justfile personal en el home directory con recetas de uso diario. Se invoca con `just --justfile ~/.user.justfile` o mediante un alias de shell.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **`uuid()`** — Funcion built-in que genera un UUID v4 aleatorio directamente en just, sin depender de herramientas externas. Util para generar identificadores unicos en scripts y scaffolding.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_34.html)

- **`datetime()`** — Funcion built-in que devuelve la fecha y hora actual formateada segun un patron strftime. `datetime_utc()` devuelve la hora en UTC. Ideal para logs, nombres de archivo y timestamps.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_34.html)

- **Project scaffolding recipes** — Recetas que generan la estructura inicial de un proyecto nuevo, incluyendo directorios, archivos de configuracion y un justfile basico, automatizando el setup repetitivo.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Vas a crear un justfile personal de productividad que centraliza tareas cotidianas de desarrollo. Incluye workflows de git (crear branches, sincronizar, limpiar branches mergeadas), mantenimiento del sistema (limpiar Docker, temporales, ver disco), scaffolding de proyectos nuevos (Rust, genericos con justfile), y utilidades rapidas (generar UUID, obtener timestamp, calcular hash). Diseñado para usarse desde cualquier directorio con un alias de shell, convirtiendo just en tu navaja suiza de terminal.

## Codigo

```justfile
# ~/.user.justfile
# Usar con: just --justfile ~/.user.justfile RECIPE
# O configurar alias: alias j='just --justfile ~/.user.justfile'

set shell := ["bash", "-euo", "pipefail", "-c"]

default:
    @just --justfile {{justfile()}} --list

# ── Git ───────────────────────────────────────────────

[group('git')]
[doc("Crear feature branch desde main")]
feature name:
    git checkout main
    git pull --rebase
    git checkout -b "feature/{{name}}"

[group('git')]
[doc("Crear bugfix branch desde main")]
bugfix name:
    git checkout main
    git pull --rebase
    git checkout -b "bugfix/{{name}}"

[group('git')]
[doc("Sync con main via rebase")]
sync:
    git fetch origin
    git rebase origin/main

[group('git')]
[doc("Push y crear PR")]
pr:
    git push -u origin HEAD
    gh pr create --fill

[group('git')]
[doc("Limpiar branches mergeadas")]
clean-branches:
    git branch --merged main | grep -v '^\*\|main' | xargs -r git branch -d
    @echo "Branches mergeadas eliminadas"

# ── Sistema ───────────────────────────────────────────

[group('sistema')]
[doc("Limpiar Docker: containers, images, volumes sin usar")]
docker-clean:
    docker system prune -f
    docker volume prune -f
    @echo "Docker limpio"

[group('sistema')]
[doc("Limpiar archivos temporales")]
tmp-clean:
    rm -rf /tmp/test-* /tmp/kata-* 2>/dev/null || true
    @echo "Temporales limpiados"

[group('sistema')]
[doc("Ver uso de disco")]
disk:
    df -h / | tail -1
    du -sh ~/Documents ~/Downloads 2>/dev/null || true

# ── Scaffolding ───────────────────────────────────────

[group('scaffold')]
[doc("Crear nuevo proyecto Rust")]
new-rust name:
    cargo init {{name}}
    @echo "Proyecto Rust creado: {{name}}"

[group('scaffold')]
[doc("Crear nuevo proyecto con justfile basico")]
new-project name:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p "{{name}}"
    cat > "{{name}}/justfile" << 'JUST'
    default:
        @just --list

    build:
        @echo "Building..."

    test:
        @echo "Testing..."
    JUST
    echo "Proyecto {{name}} creado con justfile"

# ── Utils ─────────────────────────────────────────────

[group('utils')]
[doc("Generar UUID")]
uuid:
    @echo "{{uuid()}}"

[group('utils')]
[doc("Fecha y hora UTC")]
now:
    @echo "{{datetime_utc("%Y-%m-%dT%H:%M:%SZ")}}"

[group('utils')]
[doc("Hash SHA256 de un archivo")]
hash file:
    @echo "{{sha256_file(file)}}"
```

## Verificacion

1. Ejecuta `just --justfile ~/.user.justfile` y verifica que lista todas las recetas agrupadas por git, sistema, scaffold y utils.
2. Ejecuta `just --justfile ~/.user.justfile feature login` y confirma que crea la branch `feature/login` desde main.
3. Ejecuta `just --justfile ~/.user.justfile uuid` y verifica que genera un UUID usando la funcion built-in.
4. Ejecuta `just --justfile ~/.user.justfile new-rust my-crate` y confirma que crea un proyecto Rust.
5. Ejecuta `just --justfile ~/.user.justfile docker-clean` y verifica que limpia Docker.

## Solucion y Aprendizaje

- [Jeff Triplett - Justfiles Collection](https://micro.webology.dev/categories/justfiles/) — Coleccion de articulos sobre justfiles personales y patrones de productividad con just.
- [Annie Cherkaev - Workflow Automation](https://anniecherkaev.com/workflow-automation) — Articulo sobre automatizacion de flujos de trabajo diarios, incluyendo el patron de justfile personal.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Guia completa que cubre funciones built-in como `uuid()`, `datetime()` y `sha256_file()`.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
