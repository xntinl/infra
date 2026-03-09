# 9. Docker Compose Workflow

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Gestion de Docker Compose, tags con git SHA, backup/restore, confirm |
| **Dificultad** | Intermedio |

## Que vas a aprender

- **Atributo `[confirm]`** — El atributo `[confirm("mensaje")]` muestra un mensaje de confirmacion al usuario antes de ejecutar la recipe. Si el usuario no confirma, la recipe se cancela. Es esencial para operaciones destructivas.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Organizacion con `[group]`** — El atributo `[group]` categoriza las recipes en secciones logicas (docker, database, setup) que se muestran agrupadas en `just --list`, facilitando la navegacion en justfiles grandes.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Backtick para git SHA** — Usando backtick expressions con `git rev-parse --short HEAD`, puedes capturar el hash corto del commit actual para usarlo como tag de imagenes Docker, asegurando trazabilidad entre codigo y artefactos.
  [Documentacion: Backtick Expressions](https://just.systems/man/en/chapter_37.html)

- **`set shell` para manejo de errores** — Configurar `set shell := ["bash", "-euo", "pipefail", "-c"]` asegura que las recipes fallen inmediatamente ante cualquier error, variables no definidas, o fallos en pipelines.
  [Documentacion: Settings](https://just.systems/man/en/chapter_26.html)

- **Patrones de argumentos para servicios** — Usando argumentos con valores por defecto y variadic `*args`, puedes crear recipes flexibles que trabajan con servicios Docker Compose especificos o con todos a la vez.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

## Descripcion

En este ejercicio crearas un justfile completo para gestionar un stack de Docker Compose. Incluye recipes para levantar y bajar servicios, ver logs, acceder a shells interactivos, hacer build con tags de git SHA, backup y restore de base de datos, y un bootstrap para nuevos desarrolladores. Las operaciones destructivas estan protegidas con confirmacion interactiva.

## Codigo

Crea el `justfile` con el siguiente contenido:

```justfile
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]

compose_file := "docker-compose.yml"
project := env("PROJECT", "myapp")
git_sha := `git rev-parse --short HEAD 2>/dev/null || echo "latest"`

default:
    @just --list

# Levantar servicios
[group('docker')]
up *args:
    docker compose -f {{compose_file}} up -d {{args}}

# Bajar servicios
[group('docker')]
down *args:
    docker compose -f {{compose_file}} down {{args}}

# Ver logs de un servicio
[group('docker')]
logs service="" *flags="--follow --tail=100":
    docker compose -f {{compose_file}} logs {{flags}} {{service}}

# Shell en un servicio
[group('docker')]
shell service="api" *cmd="bash":
    docker compose -f {{compose_file}} exec {{service}} {{cmd}}

# Build con git SHA como tag
[group('docker')]
build service="api":
    docker compose -f {{compose_file}} build {{service}}
    @echo "Built {{service}} ({{git_sha}})"

# Reiniciar un servicio
[group('docker')]
restart service:
    docker compose -f {{compose_file}} up -d --build {{service}}

# Backup de base de datos
[group('database')]
db-backup file=`date +%Y%m%d_%H%M%S`.dump:
    docker compose exec db pg_dump -U postgres {{project}} --format=custom > {{file}}
    @echo "Backup guardado en {{file}}"

# Restaurar base de datos
[group('database')]
[confirm("Restaurar DB desde {{file}}? Sobreescribira datos actuales.")]
db-restore file:
    docker compose exec -T db pg_restore -U postgres --clean --dbname {{project}} < {{file}}

# Eliminar todo (containers, volumes, images)
[group('docker')]
[confirm("Esto eliminara TODOS los containers, volumes e imagenes.")]
nuke:
    docker compose -f {{compose_file}} down -v --rmi all
    docker system prune -f

# Setup completo para nuevos desarrolladores
[group('setup')]
bootstrap:
    cp -n .env.example .env || true
    just build
    just up
    @echo "Esperando a que la DB este lista..."
    sleep 5
    @echo "Entorno listo! Ejecuta 'just logs' para ver los logs."
```

## Verificacion

1. Ejecuta `just` sin argumentos. Debe mostrar las recipes organizadas por grupo (docker, database, setup):
   ```
   just
   ```

2. Ejecuta `just up`. Debe levantar los servicios de Docker Compose en modo detached:
   ```
   just up
   ```

3. Ejecuta `just logs api`. Debe seguir los logs del servicio `api` con las ultimas 100 lineas:
   ```
   just logs api
   ```

4. Ejecuta `just nuke`. Debe pedir confirmacion antes de eliminar todos los containers, volumes e imagenes:
   ```
   just nuke
   ```

5. Ejecuta `just db-backup`. Debe crear un archivo dump con timestamp en el nombre:
   ```
   just db-backup
   ```

6. Ejecuta `just bootstrap`. Debe realizar el setup completo: copiar `.env.example`, hacer build, levantar servicios y esperar por la base de datos:
   ```
   just bootstrap
   ```

## Solucion y Aprendizaje

- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html) — Documentacion completa de atributos como `[confirm]`, `[group]` y `[private]`.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just` con ejemplos de uso real.
- [Duy NG - Justfile My Favorite Task Runner](https://tduyng.com/blog/justfile-my-favorite-task-runner/) — Articulo practico sobre el uso de justfiles en proyectos con Docker.
- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html) — Referencia de settings como `shell` y `dotenv-load` para configurar el comportamiento global.

## Recursos

- [Just GitHub](https://github.com/casey/just)
- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html)
- [Just Manual](https://just.systems/man/en/)

## Notas

_Espacio para tus notas personales._
