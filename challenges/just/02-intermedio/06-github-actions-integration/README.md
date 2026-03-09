# 10. GitHub Actions Integration

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Integracion de just con GitHub Actions, CI pipeline, formato automatico |
| **Dificultad** | Intermedio |

## Que vas a aprender

- **`extractions/setup-just`** — GitHub Action oficial para instalar `just` en runners de CI. Soporta especificar version y se cachea automaticamente.
  [Documentacion: extractions/setup-just](https://github.com/extractions/setup-just)
- **`just --fmt --check --unstable`** — Comando que verifica que el justfile este correctamente formateado sin modificarlo. Util como check de CI para mantener consistencia.
  [Documentacion: Just Manual - Formatting](https://just.systems/man/en/)
- **Recipes de CI** — Patron de crear una recipe `ci` que ejecuta todos los checks (lint, test, build) en secuencia, sirviendo como unico punto de entrada para pipelines.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Crea un workflow de GitHub Actions que:
1. Instale just con `extractions/setup-just`
2. Ejecute `just ci` para lint + test + build
3. En merge a main, ejecute `just deploy`
4. Verifique que `just --fmt --check --unstable` pase

## Codigo

### justfile

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

default:
    @just --list

# Lint del codigo
[group('ci')]
lint:
    echo "Running linter..."

# Tests
[group('ci')]
test:
    echo "Running tests..."

# Build
[group('ci')]
build:
    echo "Building project..."

# Todos los checks de CI
[group('ci')]
ci: lint test build
    @echo "All CI checks passed!"

# Deploy (solo desde main)
[group('deploy')]
deploy:
    @echo "Deploying..."
```

### .github/workflows/ci.yml

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:

jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: extractions/setup-just@v2
      - name: Check formatting
        run: just --fmt --check --unstable
      - name: Run CI
        run: just ci

  deploy:
    needs: ci
    if: github.ref == 'refs/heads/main' && github.event_name == 'push'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: extractions/setup-just@v2
      - name: Deploy
        run: just deploy
```

## Verificacion

1. `just ci` ejecuta lint, test y build en secuencia.
2. `just --fmt --check --unstable` pasa sin errores.
3. El workflow YAML es valido y define dos jobs: ci y deploy.
4. El job `deploy` solo se ejecuta en push a `main`.

## Solucion y Aprendizaje

- [extractions/setup-just](https://github.com/extractions/setup-just) — GitHub Action oficial para instalar just en workflows de CI/CD.
- [Just Manual - GitHub Actions](https://just.systems/man/en/github-actions.html) — Documentacion oficial sobre integracion de just con GitHub Actions.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido completo por las capacidades de just incluyendo formateo y CI.
- [Loopwerk - One Command to Run Them All](https://www.loopwerk.io/articles/2025/just-command-runner/) — Articulo sobre el uso de just como command runner unificado en proyectos.

## Recursos

- [extractions/setup-just](https://github.com/extractions/setup-just)
- [Just Manual - GitHub Actions](https://just.systems/man/en/github-actions.html)
- [Just Manual](https://just.systems/man/en/)

## Notas

_Espacio para tus notas personales._
