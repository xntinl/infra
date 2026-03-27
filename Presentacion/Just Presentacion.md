---
title: "Just: El Command Runner Moderno"
tags:
  - just
  - devops
  - tooling
author: consulting
date: 2026-03-27
---

# Just — El Command Runner Moderno

---

## 1. El Problema

Todos los proyectos tienen comandos repetitivos:

```bash
npx tsc --build && node dist/index.js
npx vitest run --coverage && npx eslint src/
docker compose -f docker-compose.dev.yml up --build
npx prisma migrate deploy && npx prisma generate
```

- Nadie los recuerda
- Terminan perdidos en el historial del shell o en un README desactualizado
- Cada dev nuevo pregunta: *"¿cómo era el comando para levantar el proyecto?"*
- Los `npm scripts` se vuelven ilegibles con comandos encadenados

> [!danger] Esto no debería pasar
> Si tu onboarding depende de memoria humana, tenés un problema.

---

## 2. ¿Qué es Just?

Un **command runner** — ejecuta comandos de proyecto con un nombre corto.

| | |
| --- | --- |
| **Creador** | Casey Rodarmor (también creó Bitcoin Ordinals) |
| **Año** | 2016 |
| **Lenguaje** | Rust |
| **Filosofía** | Hacer UNA cosa bien: ejecutar comandos |

```bash
just dev          # en vez de "docker compose -f docker-compose.dev.yml up --build"
just test         # en vez de "npx vitest run --coverage"
just migrate      # en vez de "npx prisma migrate deploy && npx prisma generate"
just deploy prod  # en vez de copiar 5 comandos del README
```

---

## 3. Anatomía de un `justfile`

```just
set dotenv-load                            # carga .env automáticamente

project := "mi-api"
version := `git describe --tags --always`  # backtick = ejecutar comando
node_bin := "./node_modules/.bin"

# Muestra recetas disponibles
default:
    @just --list

# Instala dependencias
install:
    npm ci

# Servidor de desarrollo con hot reload
dev: install
    npx tsx watch src/index.ts

# Ejecuta los tests
test: install                              # dependencia: install primero
    npx vitest run

# Despliega al entorno indicado
deploy env="dev":                          # parámetro con default
    ./scripts/deploy.sh {{env}}

# Pipeline CI completo
ci: lint test build
    @echo "CI passed for {{project}} v{{version}}"
```

---

## 4. Encadenar Comandos (Dependencias)

> [!tip] La feature más poderosa de Just
> Encadená recetas para crear pipelines sin repetir lógica.

### 4.1 Dependencias previas (se ejecutan ANTES)

```just
# install se ejecuta antes que build
build: install
    npx tsc --build

# install y build se ejecutan antes que test
test: install build
    npx vitest run

# lint + test + build, todo en orden
ci: lint test build
    @echo "CI passed"
```

```bash
just ci   # ejecuta: lint → test → build → echo
```

### 4.2 Dependencias posteriores (se ejecutan DESPUÉS con `&&`)

```just
# Después de migrate, ejecuta generate
migrate: && generate
    npx prisma migrate deploy

generate:
    npx prisma generate
```

```bash
just migrate   # ejecuta: prisma migrate → prisma generate
```

### 4.3 Dependencias con argumentos

```just
build env:
    @echo "Building for {{env}}"

# Pasa "prod" como argumento a build
deploy: (build "prod")
    ./scripts/deploy.sh
```

### 4.4 Pipelines complejos

```just
# ── Pipeline completo de CI ────────────
# Orden: install → lint → typecheck → test → build → docker-build

install:
    npm ci

lint: install
    npx eslint src/ --max-warnings 0

typecheck: install
    npx tsc --noEmit

test: install
    npx vitest run --coverage

build: install
    npx tsc --build

docker-build: build
    docker build -t myapp:{{`git rev-parse --short HEAD`}} .

# Un solo comando ejecuta TODO el pipeline
ci: lint typecheck test docker-build
    @echo "Pipeline completo"
```

```bash
just ci   # ejecuta todo en el orden correcto, sin duplicar install
```

> [!info] Ejecución única
> Si `install` aparece como dependencia de `lint`, `test` y `build`, Just lo ejecuta **una sola vez**. No se duplica.

---

## 5. Features Principales

### 5.1 Parámetros nativos

```just
deploy env="dev" +flags="":
    ./scripts/deploy.sh {{env}} {{flags}}
```

```bash
just deploy prod --skip-migrations
```

### 5.2 Documentación automática

```just
# Instala dependencias
install:
    npm ci

# Servidor de desarrollo
dev:
    npx tsx watch src/index.ts
```

```bash
$ just --list
Available recipes:
    install  # Instala dependencias
    dev      # Servidor de desarrollo
```

### 5.3 Recetas en cualquier lenguaje (shebang)

```just
# Seed de la base de datos
seed:
    #!/usr/bin/env node
    const { PrismaClient } = require('@prisma/client');
    const prisma = new PrismaClient();
    async function main() {
        await prisma.user.create({ data: { email: 'admin@test.com', role: 'ADMIN' } });
        console.log('Seed completed');
    }
    main().finally(() => prisma.$disconnect());

# Verificar salud de servicios
health-check:
    #!/usr/bin/env bash
    set -euo pipefail
    for svc in api auth gateway; do
        status=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:3000/$svc/health")
        echo "$svc: $status"
    done
```

### 5.4 Carga de `.env`

```just
set dotenv-load

dev:
    echo "DB: $DATABASE_URL, Port: $PORT"
    npx tsx watch src/index.ts
```

### 5.5 Condicionales por OS

```just
[linux]
install-deps:
    sudo apt install -y build-essential

[macos]
install-deps:
    brew install gcc
```

### 5.6 Confirmación para acciones destructivas

```just
[confirm("¿Seguro? Esto borra TODA la base de datos")]
db-reset:
    npx prisma migrate reset --force
```

### 5.7 Grupos

```just
[group('database')]
migrate:
    npx prisma migrate deploy

[group('database')]
seed:
    npx prisma db seed

[group('database')]
studio:
    npx prisma studio

[group('testing')]
test:
    npx vitest run

[group('testing')]
test-watch:
    npx vitest watch

[group('testing')]
test-e2e:
    npx playwright test
```

### 5.8 Módulos (para monorepos y multi-proyecto)

```just
mod api 'apps/api/api.just'
mod web 'apps/web/web.just'
mod shared 'packages/shared/shared.just'

# Uso:
# just api dev       → levanta el backend
# just web dev       → levanta el frontend
# just shared build  → compila paquetes compartidos
```

---

## 6. Ejemplo Real: Backend Node/TS

```just
set dotenv-load
set shell := ["bash", "-uc"]

project := "my-api"
node_bin := "./node_modules/.bin"
tag := `git rev-parse --short HEAD`

# Muestra recetas disponibles
default:
    @just --list

# ── Setup ─────────────────────────────

# Instala dependencias
[group('setup')]
install:
    npm ci

# Setup completo: install + migrate + seed
[group('setup')]
setup: install migrate seed
    @echo "Proyecto listo"

# ── Desarrollo ────────────────────────

# Servidor con hot reload
[group('dev')]
dev: install
    npx tsx watch src/index.ts

# Abrir Prisma Studio (GUI de la DB)
[group('dev')]
studio:
    npx prisma studio

# ── Base de datos ─────────────────────

# Ejecutar migraciones pendientes
[group('database')]
migrate:
    npx prisma migrate deploy
    npx prisma generate

# Crear nueva migración
[group('database')]
migrate-new name:
    npx prisma migrate dev --name {{name}}

# Seed de datos iniciales
[group('database')]
seed:
    npx prisma db seed

# Reset completo de la DB
[group('database')]
[confirm("Esto borra TODA la base de datos. ¿Continuar?")]
db-reset:
    npx prisma migrate reset --force

# ── Calidad ───────────────────────────

# Lint
[group('quality')]
lint: install
    npx eslint src/ --max-warnings 0

# Type-checking sin compilar
[group('quality')]
typecheck: install
    npx tsc --noEmit

# Formatear código
[group('quality')]
fmt:
    npx prettier --write "src/**/*.ts"

# ── Testing ───────────────────────────

# Tests unitarios
[group('testing')]
test: install
    npx vitest run

# Tests con coverage
[group('testing')]
test-cov: install
    npx vitest run --coverage

# Tests en watch mode
[group('testing')]
test-watch:
    npx vitest watch

# Tests E2E
[group('testing')]
test-e2e: install
    npx playwright test

# ── Build & Deploy ────────────────────

# Compilar TypeScript
[group('build')]
build: install
    npx tsc --build

# Build de Docker
[group('build')]
docker-build: build
    docker build -t {{project}}:{{tag}} .

# Push a registry
[group('build')]
docker-push: docker-build
    docker push {{project}}:{{tag}}

# Deploy al entorno indicado
[group('deploy')]
deploy env="dev": docker-push
    ./scripts/deploy.sh {{env}} {{tag}}

# ── CI (encadena todo) ────────────────

# Pipeline completo: lint → typecheck → test → build
ci: lint typecheck test build
    @echo "CI passed for {{project}}:{{tag}}"
```

---

## 7. Ejemplo: Monorepo Multi-Proyecto

```
my-monorepo/
├── justfile              ← raíz: orquesta todo
├── apps/
│   ├── api/api.just      ← backend NestJS
│   └── web/web.just      ← frontend Next.js
└── packages/
    └── shared/shared.just ← paquetes compartidos
```

**justfile (raíz):**
```just
mod api 'apps/api/api.just'
mod web 'apps/web/web.just'
mod shared 'packages/shared/shared.just'

# Levantar todo el stack
dev:
    #!/usr/bin/env bash
    set -euo pipefail
    just shared build &
    just api dev &
    just web dev &
    wait

# Instalar todo
install:
    just shared install
    just api install
    just web install

# CI completo
ci: (shared "ci") (api "ci") (web "ci")
    @echo "Monorepo CI passed"
```

**apps/api/api.just:**
```just
set dotenv-load
set dotenv-path := "../../.env"

dev:
    npx tsx watch src/main.ts

test:
    npx vitest run

build:
    npx tsc --build

ci: lint test build
```

**apps/web/web.just:**
```just
dev:
    npx next dev --port 3001

build:
    npx next build

ci: lint test build
```

```bash
just api dev       # solo backend
just web dev       # solo frontend
just dev           # todo el stack
just api test      # tests del backend
just ci            # CI de todo el monorepo
```

---

## 8. Instalación (30 segundos)

```bash
# macOS
brew install just

# Linux (Debian/Ubuntu 24.04+)
apt install just

# Via npm (para proyectos Node)
npm install -g rust-just

# Completions para zsh
just --completions zsh > ~/.zfunc/_just
```

---

## 9. Comandos CLI Esenciales

| Comando | Qué hace |
| :------ | :------- |
| `just` | Ejecuta la receta por defecto |
| `just receta` | Ejecuta una receta |
| `just receta arg1 arg2` | Con argumentos |
| `just --list` | Lista recetas con descripciones |
| `just --show receta` | Muestra el código de una receta |
| `just --evaluate` | Muestra todas las variables |
| `just --dry-run receta` | Simula sin ejecutar |
| `just --fmt` | Formatea el justfile |
| `just --choose` | Selector interactivo (fzf) |
| `just --summary` | Lista compacta de nombres |

---

## 10. Errores Comunes

> [!warning] Cada línea = shell separado
> ```just
> # ESTO NO FUNCIONA
> build:
>     cd apps/api
>     npm run build    # se ejecuta en el directorio ORIGINAL
> ```
> **Solución:** `cd apps/api && npm run build` o usar receta shebang.

> [!warning] No es un build system
> Just NO rastrea cambios en archivos. Siempre ejecuta todo.
> Si querés cache de builds, usá `turbo` o `nx` para el build y `just` para orquestar.

> [!warning] Shell por defecto es `sh`, no `bash`
> Si usás `[[ ]]` o arrays, agregá: `set shell := ["bash", "-uc"]`

---

## Cierre

> [!quote] Filosofía
> *"Un `justfile` es el lugar donde vive el conocimiento colectivo de un proyecto — los comandos para testear, compilar, deployar, y todas esas invocaciones arcanas que de otro modo se pierden en el historial del shell."*
> — Casey Rodarmor, creador de just

```bash
# Empezá hoy
brew install just
touch justfile
just --list
```

---

> [!info] Recursos
> - Repo: [github.com/casey/just](https://github.com/casey/just)
> - Manual: [just.systems/man](https://just.systems/man/en/)
> - VS Code: extensión `mkhl.just`
> - JetBrains: plugin "Just"
