---
title: "Just: 10 Ejemplos Ilustrativos"
tags:
  - just
  - node
  - typescript
  - backend
date: 2026-03-27
---

# Just 

## 1. Variables y Backticks

> [!info] Concepto
> Variables estáticas con `:=`, dinámicas con backticks, y del entorno con `env()`.

```just
version := "1.0.0"
git_hash := `git rev-parse --short HEAD`
node_env := env("NODE_ENV", "development")

build:
    @echo "Construyendo v{{version}} ({{git_hash}}) en modo {{node_env}}"
    npx tsc --outDir dist
```

```bash
just build
# → Construyendo v1.0.0 (a3f2c1b) en modo development
```

---

## 2. Encadenar Dependencias

> [!tip] Concepto
> `:` = se ejecuta ANTES. `&&` = se ejecuta DESPUÉS. Múltiples deps = pipeline.

```just
install:
    @echo "1. Instalando dependencias"

build: install
    @echo "2. Compilando TypeScript"

lint:
    @echo "3. Ejecutando linter"

test:
    @echo "4. Ejecutando tests"

# Después de migrate, ejecuta seed automáticamente
migrate: && seed
    @echo "5. Ejecutando migraciones"

seed:
    @echo "6. Poblando base de datos"

# Encadena todo en orden
ci: lint test build
    @echo "Pipeline CI completado"
```

```bash
just ci
# → 3. Ejecutando linter
# → 4. Ejecutando tests
# → 1. Instalando dependencias  (build depende de install)
# → 2. Compilando TypeScript
# → Pipeline CI completado

just migrate
# → 5. Ejecutando migraciones
# → 6. Poblando base de datos   (se ejecuta DESPUÉS por &&)
```

> [!success] Si `install` aparece como dependencia de `lint`, `test` y `build`, Just lo ejecuta **UNA SOLA VEZ**.

---

## 3. Parámetros

> [!info] Concepto
> Requerido, con default, y variádico (`+`).

```just
# Parámetro requerido
deploy env:
    npx tsx scripts/deploy.ts --env={{env}}

# Parámetro con valor por defecto
test suite="unit":
    npx vitest run --project={{suite}}

# Variádico: acepta N argumentos extra
generate entity +flags:
    npx tsx scripts/codegen.ts {{entity}} {{flags}}
```

```bash
just deploy prod              # env = "prod"
just test                     # suite = "unit" (default)
just test e2e                 # suite = "e2e"
just generate user --dry-run  # entity = "user", flags = "--dry-run"
```

---

## 4. Shebang (recetas en cualquier lenguaje)

> [!info] Concepto
> Con `#!/usr/bin/env` la receta entera se ejecuta como un script. El estado persiste entre líneas.

```just
# Receta en Node.js
info:
    #!/usr/bin/env node
    const pkg = require('./package.json')
    console.log(`${pkg.name}@${pkg.version}`)

# Receta en Bash con modo estricto
salud:
    #!/usr/bin/env bash
    set -euo pipefail
    servicios=("http://localhost:3000/health" "http://localhost:5432")
    for url in "${servicios[@]}"; do
        if curl -sf --max-time 3 "$url" > /dev/null 2>&1; then
            echo "OK: $url"
        else
            echo "FALLO: $url" >&2
            exit 1
        fi
    done
```

```bash
just info   # → mi-api@1.0.0
just salud  # → OK: http://localhost:3000/health
            #   FALLO: http://localhost:5432
```

---

## 5. Dotenv y Confirm

> [!info] Concepto
> `set dotenv-load` carga `.env` automáticamente. `[confirm]` pide confirmación antes de ejecutar.

```just
set dotenv-load

# Usa $DATABASE_URL del .env
migrate:
    npx prisma migrate deploy --url "$DATABASE_URL"

# Pide confirmación antes de destruir datos
[confirm("¿Seguro que querés resetear la base de datos?")]
reset-db:
    npx prisma migrate reset --force
```

```bash
just migrate   # → ejecuta directo
just reset-db  # → "¿Seguro que querés resetear la base de datos? (y/N)"
```

---

## 6. Grupos y Recetas Privadas

> [!info] Concepto
> `[group()]` organiza el `--list`. `[private]` o `_prefijo` oculta recetas internas.

```just
[group('database')]
migrate:
    npx prisma migrate dev

[group('database')]
seed: _check-env
    npx prisma db seed

[group('testing')]
test:
    npx vitest run

[group('testing')]
test-watch:
    npx vitest watch

# No aparece en just --list, pero se puede invocar
[private]
_check-env:
    @test -f .env || (echo "Falta .env" && exit 1)
```

```bash
$ just --list
Available recipes:
    [database]
    migrate
    seed

    [testing]
    test
    test-watch
```

> [!tip] `_check-env` no aparece pero `seed` la usa como dependencia.

---

## 7. Módulos (Monorepo)

> [!info] Concepto
> `mod` crea namespaces. Cada proyecto tiene su propio `.just` file.

**`justfile`** (raíz):
```just
mod api
mod web
```

**`api.just`**:
```just
dev:
    cd apps/api && npm run dev

test:
    cd apps/api && npm test

build:
    cd apps/api && npm run build
```

**`web.just`**:
```just
dev:
    cd apps/web && npm run dev

test:
    cd apps/web && npm test

build:
    cd apps/web && npm run build
```

```bash
just api dev     # levanta el backend
just web dev     # levanta el frontend
just api test    # tests solo del backend
just web build   # build solo del frontend
```

---

## 8. Condicionales por OS

> [!info] Concepto
> `[linux]`/`[macos]` ejecuta según el OS. `if os() ==` para variables.

```just
pkg_manager := if os() == "macos" { "brew" } else { "apt" }

[linux]
install:
    sudo apt update && sudo apt install -y curl git

[macos]
install:
    brew install curl git

info:
    @echo "Usando: {{pkg_manager}}"
```

```bash
just install  # ejecuta la versión correcta según tu OS
just info     # → "Usando: brew" (en macOS)
```

---

## 9. Prefijos: `@` silenciar, `-` ignorar errores

> [!info] Concepto
> `@` no muestra el comando. `-` continúa aunque falle. `[no-exit-message]` oculta el error de just.

```just
# @ suprime la impresión del comando
saludar:
    @echo "Hola desde just"

# - ignora errores (útil para limpieza)
limpiar:
    -rm -rf dist
    -rm -rf node_modules

# No muestra "error: Recipe failed..." si npm falla
[no-exit-message]
dev:
    npm run dev
```

```bash
just saludar
# → Hola desde just          (sin mostrar "echo Hola desde just")

just limpiar
# → continúa aunque dist/ no exista
```

---

## 10. Todo Junto: Backend Node/TS Completo

> [!success] Este ejemplo combina todos los conceptos anteriores.

```just
set dotenv-load
set shell := ["bash", "-uc"]

project := "my-api"
tag := `git rev-parse --short HEAD`

default:
    @just --list

# ── Setup ──────────────────────────────

[group('setup')]
install:
    npm ci

[group('setup')]
setup: install migrate seed
    @echo "Proyecto listo"

# ── Dev ────────────────────────────────

[group('dev')]
dev: install
    npx tsx watch src/index.ts

[group('dev')]
studio:
    npx prisma studio

# ── Database ───────────────────────────

[group('database')]
migrate: && generate
    npx prisma migrate deploy

[group('database')]
generate:
    npx prisma generate

[group('database')]
seed:
    npx prisma db seed

[group('database')]
[confirm("¿Borrar TODA la base de datos?")]
db-reset:
    npx prisma migrate reset --force

# ── Quality ────────────────────────────

[group('quality')]
lint: install
    npx eslint src/ --max-warnings 0

[group('quality')]
typecheck: install
    npx tsc --noEmit

[group('quality')]
fmt:
    npx prettier --write "src/**/*.ts"

# ── Testing ────────────────────────────

[group('testing')]
test: install
    npx vitest run

[group('testing')]
test-cov: install
    npx vitest run --coverage

# ── Build & Deploy ─────────────────────

[group('build')]
build: install
    npx tsc --build

[group('build')]
docker-build: build
    docker build -t {{project}}:{{tag}} .

[group('deploy')]
deploy env="dev": docker-build
    ./scripts/deploy.sh {{env}} {{tag}}

# ── CI (encadena TODO) ─────────────────

ci: lint typecheck test build
    @echo "CI passed for {{project}}:{{tag}}"
```

```bash
just            # → muestra todas las recetas agrupadas
just dev        # → install → tsx watch
just ci         # → lint → typecheck → test → install → build
just deploy prod  # → install → build → docker-build → deploy
just migrate    # → prisma migrate → prisma generate (por &&)
just db-reset   # → "¿Borrar TODA la base de datos? (y/N)"
```

---

> [!quote] Resumen
> 10 conceptos, 10 ejemplos. Cada uno se puede copiar y usar tal cual.
> Combinados, tenés un justfile profesional para cualquier proyecto Node/TS.
