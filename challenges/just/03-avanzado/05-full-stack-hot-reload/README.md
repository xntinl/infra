# 15. Full-Stack con Hot Reload

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Backend + Frontend + DB con hot reload, Docker Compose, bootstrap |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **Desarrollo paralelo** — Patron donde el justfile coordina multiples servicios (backend, frontend, base de datos) que se ejecutan simultaneamente, cada uno en su propia terminal con hot reload independiente.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **`[confirm]` para operaciones de DB** — Uso del atributo `[confirm]` para proteger operaciones destructivas de base de datos como reset o migraciones irreversibles, evitando perdida accidental de datos.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Docker Compose integration** — Recetas que envuelven comandos de Docker Compose para levantar, detener y ver logs de servicios, con soporte para argumentos variables mediante `*args`.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Bootstrap pattern** — Receta que configura todo el entorno de desarrollo desde cero: copia archivos de ejemplo, instala dependencias, levanta servicios, ejecuta migraciones y muestra instrucciones al desarrollador nuevo.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **`alias`** — Permite definir nombres cortos para recetas frecuentes. Por ejemplo, `alias b := backend` permite usar `just b` en lugar de `just backend`, agilizando el flujo de trabajo diario.
  [Documentacion: Aliases](https://just.systems/man/en/chapter_38.html)

## Descripcion

Vas a crear un justfile para un proyecto full-stack compuesto por un backend en Rust con cargo-watch para hot reload, un frontend con pnpm, y una base de datos PostgreSQL gestionada con Docker Compose y migraciones sqlx. El justfile incluye un comando `bootstrap` que prepara todo el entorno para un desarrollador nuevo, aliases para acceso rapido, y recetas agrupadas por categoria. Las operaciones destructivas de base de datos requieren confirmacion explicita.

## Codigo

```justfile
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]

export DATABASE_URL := env("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/myapp")

alias f := frontend
alias b := backend
alias d := down

default:
    @just --list

# ── Docker Compose ────────────────────────────────────

[group('docker')]
up *args:
    docker compose up -d {{args}}

[group('docker')]
down *args:
    docker compose down {{args}}

[group('docker')]
logs service="" lines="100":
    docker compose logs -f --tail={{lines}} {{service}}

# ── Backend ───────────────────────────────────────────

[group('backend')]
backend:
    cd backend && cargo watch -x run

[group('backend')]
backend-build:
    cd backend && cargo build

[group('backend')]
backend-test:
    cd backend && cargo test

[group('backend')]
backend-lint:
    cd backend && cargo clippy -- -D warnings

# ── Frontend ──────────────────────────────────────────

[group('frontend')]
frontend:
    cd frontend && pnpm dev

[group('frontend')]
frontend-build:
    cd frontend && pnpm build

[group('frontend')]
frontend-test:
    cd frontend && pnpm test

[group('frontend')]
frontend-lint:
    cd frontend && pnpm lint

# ── Database ──────────────────────────────────────────

[group('database')]
db-migrate:
    sqlx migrate run

[group('database')]
db-create-migration name:
    sqlx migrate add -r {{name}}

[group('database')]
[confirm("Resetear la DB? Se perderan todos los datos.")]
db-reset:
    sqlx database reset -y

# ── Quality ───────────────────────────────────────────

[group('quality')]
lint: backend-lint frontend-lint

[group('quality')]
test: backend-test frontend-test

[group('quality')]
ci: lint test backend-build frontend-build
    @echo "CI pipeline passed!"

# ── Setup ─────────────────────────────────────────────

[group('setup')]
bootstrap:
    cp -n .env.example .env || true
    cd frontend && pnpm install
    just up db
    @echo "Esperando a que la DB este lista..."
    sleep 3
    just db-migrate
    @echo "Entorno listo!"
    @echo "  Backend:  just backend"
    @echo "  Frontend: just frontend"
    @echo "  Ambos:    just dev"

# Dev: levantar todo
[group('dev')]
dev:
    just up db
    @echo "DB levantada. Inicia backend y frontend en terminales separadas:"
    @echo "  Terminal 1: just backend"
    @echo "  Terminal 2: just frontend"
```

## Verificacion

1. Ejecuta `just` y verifica que las recetas aparecen agrupadas por docker, backend, frontend, database, quality, setup y dev.
2. Ejecuta `just bootstrap` y confirma que realiza la configuracion completa para un desarrollador nuevo.
3. Ejecuta `just b` y verifica que es un alias para `just backend`.
4. Ejecuta `just f` y verifica que es un alias para `just frontend`.
5. Ejecuta `just d` y verifica que es un alias para `just down`.
6. Ejecuta `just ci` y confirma que ejecuta lint, tests y builds de ambos servicios.
7. Ejecuta `just db-reset` y verifica que solicita confirmacion antes de resetear la base de datos.

## Solucion y Aprendizaje

- [Just Manual - Aliases](https://just.systems/man/en/chapter_38.html) — Documentacion sobre como definir aliases para acceso rapido a recetas frecuentes.
- [Loopwerk - One Command to Run Them All](https://www.loopwerk.io/articles/2025/just-command-runner/) — Articulo sobre como usar just para unificar el flujo de trabajo en proyectos full-stack.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido general que cubre grupos, aliases, confirmacion y otras funcionalidades usadas en este ejercicio.

## Recursos

- [Just GitHub](https://github.com/casey/just)
- [Just Manual](https://just.systems/man/en/)

## Notas

_Espacio para tus notas personales._
