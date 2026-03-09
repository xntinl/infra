# 11. Monorepo con Modulos

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | `mod` keyword, namespaced recipes, `import`, cross-service orchestration |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **`mod`** — Permite dividir un justfile en modulos con namespace propio. Cada modulo se invoca con `just modulo::receta`, manteniendo la organizacion en proyectos grandes.
  [Documentacion: Modules](https://just.systems/man/en/modules1190.html)

- **`import`** — Incluye recetas de otro archivo directamente en el namespace actual, sin prefijo. Ideal para herramientas compartidas que se usan frecuentemente.
  [Documentacion: Imports](https://just.systems/man/en/chapter_52.html)

- **Convenciones de archivos de modulo** — Un modulo `foo` se resuelve buscando `foo.just` o `foo/mod.just`, lo que permite estructuras simples o con subdirectorios segun la complejidad.
  [Documentacion: Modules](https://just.systems/man/en/modules1190.html)

- **Orquestacion cross-module** — Las recetas del justfile raiz pueden invocar recetas de cualquier modulo usando `just modulo::receta`, permitiendo pipelines que coordinan multiples servicios en orden.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Vas a construir la estructura de un monorepo con tres servicios: una API en Rust, un frontend web con pnpm, y una libreria compartida. El justfile raiz orquesta todos los servicios mediante modulos (`mod`), mientras que las herramientas comunes de Docker se importan directamente con `import` para acceso sin namespace. El resultado es un sistema donde `just build` compila todo en orden, `just api::dev` levanta solo la API, y `just docker-up` gestiona los contenedores.

## Codigo

**justfile** (raiz):

```justfile
set dotenv-load

mod api
mod web
mod shared

import 'tools/docker.just'

default:
    @just --list

# Setup completo
setup:
    just api::install
    just web::install
    just shared::install

# Build todo
build:
    just shared::build
    just api::build
    just web::build

# Test todo
test:
    just shared::test
    just api::test
    just web::test

# CI pipeline
ci: build test
    @echo "CI passed!"
```

**api.just**:

```justfile
# Instalar dependencias
install:
    cd api && cargo fetch

# Build
build:
    cd api && cargo build

# Tests
test:
    cd api && cargo test

# Dev con hot reload
dev:
    cd api && cargo watch -x run

# Lint
lint:
    cd api && cargo clippy -- -D warnings
```

**web.just**:

```justfile
install:
    cd web && pnpm install

build:
    cd web && pnpm build

test:
    cd web && pnpm test

dev:
    cd web && pnpm dev

lint:
    cd web && pnpm lint
```

**shared.just**:

```justfile
install:
    cd shared && cargo fetch

build:
    cd shared && cargo build

test:
    cd shared && cargo test
```

**tools/docker.just**:

```justfile
# Levantar servicios Docker
docker-up:
    docker compose up -d

# Bajar servicios Docker
docker-down:
    docker compose down

# Ver logs
docker-logs *args:
    docker compose logs -f {{args}}
```

## Verificacion

1. Ejecuta `just` y verifica que lista todas las recetas, incluyendo los namespaces de cada modulo (`api::`, `web::`, `shared::`).
2. Ejecuta `just api::build` y confirma que se ejecuta el build de la API.
3. Ejecuta `just web::test` y confirma que se ejecutan los tests del frontend.
4. Ejecuta `just docker-up` y verifica que funciona sin namespace (fue importado con `import`, no `mod`).
5. Ejecuta `just build` y observa que la orquestacion compila shared, luego api, luego web, en ese orden.
6. Ejecuta `just --list api` y verifica que solo muestra las recetas del modulo api.

## Solucion y Aprendizaje

- [Just Manual - Modules](https://just.systems/man/en/modules1190.html) — Referencia completa sobre como declarar y usar modulos con la palabra clave `mod`.
- [Just Manual - Imports](https://just.systems/man/en/chapter_52.html) — Documentacion de `import` para incluir recetas en el namespace actual.
- [WordPress/openverse justfile](https://github.com/WordPress/openverse/blob/main/justfile) — Ejemplo real de un monorepo grande que usa just con modulos para orquestar multiples servicios.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
