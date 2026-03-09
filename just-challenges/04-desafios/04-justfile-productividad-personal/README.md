# 19. Justfile de Productividad Personal

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Justfile global con recetas para tareas diarias de desarrollo |
| **Dificultad** | Desafio |

## Que vas a aprender

- **`set fallback`** — Configuracion que busca un justfile en directorios padre cuando no encuentra uno en el directorio actual. Permite tener un justfile global que se aplica en cualquier subdirectorio.
  [Documentacion: Settings](https://just.systems/man/en/chapter_26.html)
- **Justfile global con `--justfile`** — Puedes invocar un justfile en una ruta especifica con `just --justfile ~/.user.justfile`, o crear un alias de shell para tenerlo siempre disponible.
  [Documentacion: Just Manual](https://just.systems/man/en/)
- **Scaffolding con `mkdir` y `cat`** — Patron de crear recipes que generan la estructura de directorios y archivos base para nuevos proyectos, automatizando el setup inicial.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Crea un justfile personal (`~/.user.justfile`) con recetas para tus tareas diarias:
- Git workflows (feature branch, PR, release)
- Cleanup de sistema (docker prune, tmp files)
- Project scaffolding (new-go, new-rust, new-ts)
- Development shortcuts
- Usa `set fallback` para buscar justfiles en directorios padre

## Codigo

```justfile
# ~/.user.justfile - Recetas de productividad personal
set shell := ["bash", "-euo", "pipefail", "-c"]
set fallback

default:
    @just --list

# -- Git Workflows --------------------------------------------

# Crear feature branch desde main
[group('git')]
feature name:
    git checkout main
    git pull origin main
    git checkout -b feature/{{name}}

# Crear PR (requiere gh CLI)
[group('git')]
pr title:
    git push -u origin $(git branch --show-current)
    gh pr create --title "{{title}}" --fill

# Quick commit
[group('git')]
save *message="WIP":
    git add -A
    git commit -m "{{message}}"

# Sync con main
[group('git')]
sync:
    git fetch origin main
    git rebase origin/main

# -- Cleanup --------------------------------------------------

# Limpiar Docker (containers, images, volumes no usados)
[group('cleanup')]
[confirm("Limpiar recursos Docker no utilizados?")]
docker-prune:
    docker system prune -af --volumes

# Limpiar archivos temporales
[group('cleanup')]
clean-tmp:
    #!/usr/bin/env bash
    echo "Limpiando archivos temporales..."
    find /tmp -user $(whoami) -mtime +7 -delete 2>/dev/null || true
    echo "Limpieza completada"

# -- Scaffolding ----------------------------------------------

# Crear nuevo proyecto Rust
[group('new')]
new-rust name:
    cargo init {{name}}
    @echo "Proyecto Rust '{{name}}' creado"

# Crear nuevo proyecto Go
[group('new')]
new-go name:
    mkdir -p {{name}}
    cd {{name}} && go mod init {{name}}
    @echo "Proyecto Go '{{name}}' creado"

# -- Dev Shortcuts --------------------------------------------

# Buscar texto en el proyecto
[group('dev')]
search term:
    grep -rn "{{term}}" --include="*.rs" --include="*.go" --include="*.ts" .

# Puertos en uso
[group('dev')]
ports:
    lsof -iTCP -sTCP:LISTEN -P -n | grep -v "^$"

# Tamano de directorios
[group('dev')]
sizes:
    du -sh */ 2>/dev/null | sort -hr | head -20
```

## Verificacion

1. `just --justfile ~/.user.justfile` lista todas las recipes disponibles agrupadas.
2. `just feature my-feature` crea una nueva branch `feature/my-feature` desde main.
3. `just save "mi mensaje"` hace add y commit con el mensaje proporcionado.
4. `just docker-prune` solicita confirmacion antes de limpiar Docker.
5. `just new-rust myapp` crea un nuevo proyecto Rust con cargo init.
6. `just ports` muestra los puertos TCP en uso.

## Solucion y Aprendizaje

- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html) — Documentacion de settings como `fallback` y `shell`.
- [Jeff Triplett - Justfiles Collection](https://micro.webology.dev/categories/justfiles/) — Coleccion de justfiles para productividad personal y proyectos.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido completo por las funcionalidades de just.
- [Duy NG - Justfile My Favorite Task Runner](https://tduyng.com/blog/justfile-my-favorite-task-runner/) — Articulo sobre justfiles como herramienta de productividad diaria.

## Recursos

- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html)
- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
