# D3. Error Handling Robusto

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | `require()`, `trap` para rollback, prerequisite checks, webhook notification |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **`require()`** — Funcion built-in que verifica en tiempo de carga del justfile que un programa externo esta disponible en el PATH. Si no lo encuentra, just falla inmediatamente con un error claro, antes de intentar ejecutar cualquier receta.
  [Documentacion: Built-in Functions - require](https://just.systems/man/en/chapter_34.html)

- **`trap` para rollback on failure** — Mecanismo de bash que registra un comando a ejecutar cuando ocurre un error (signal ERR). Dentro de una receta shebang, permite implementar rollback automatico si el deploy falla a mitad de camino.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Graceful degradation** — Patron donde la receta detecta si una herramienta preferida esta disponible y, si no, utiliza una alternativa. Por ejemplo, usar `cargo-nextest` si existe, o caer a `cargo test` estandar.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Pre-flight checks pattern** — Receta dedicada a verificar todos los prerequisitos antes de una operacion critica: Docker corriendo, git limpio, credenciales configuradas. Se usa como dependencia de las recetas de deploy.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Vas a crear un justfile orientado a deploys seguros con manejo robusto de errores. Incluye verificacion de herramientas requeridas con `require()`, una receta de pre-flight checks que valida el estado del entorno, deploy con rollback automatico usando `trap`, notificaciones a webhooks, y tests con graceful degradation. El objetivo es que los fallos se manejen de forma predecible y que el sistema intente recuperarse o notificar automaticamente cuando algo sale mal.

## Codigo

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

# Verificar herramientas requeridas
_ := require("docker")
_ := require("git")

webhook_url := env("WEBHOOK_URL", "")

default:
    @just --list

# Verificar prerequisitos
[group('checks')]
preflight:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Verificando prerequisitos..."

    # Verificar Docker esta corriendo
    if ! docker info > /dev/null 2>&1; then
        echo "{{RED}}Error: Docker no esta corriendo{{NORMAL}}"
        exit 1
    fi
    echo "  Docker: OK"

    # Verificar git limpio
    if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
        echo "{{YELLOW}}Warning: Cambios sin commit{{NORMAL}}"
    else
        echo "  Git: limpio"
    fi

    echo "{{GREEN}}Prerequisitos verificados{{NORMAL}}"

# Notificar (simulado)
[private]
_notify status message:
    #!/usr/bin/env bash
    if [[ -n "{{webhook_url}}" ]]; then
        curl -sf -X POST "{{webhook_url}}" \
            -H "Content-Type: application/json" \
            -d '{"status": "{{status}}", "message": "{{message}}"}' || true
    fi
    echo "[{{status}}] {{message}}"

# Deploy con rollback automatico
[group('deploy')]
[confirm("Desplegar en produccion?")]
deploy: preflight
    #!/usr/bin/env bash
    set -euo pipefail

    # Guardar version actual para rollback
    CURRENT_VERSION=$(cat VERSION 2>/dev/null || echo "unknown")

    trap 'echo "{{RED}}Deploy fallo, haciendo rollback a $CURRENT_VERSION...{{NORMAL}}"; just _notify "FAILED" "Deploy fallo, rollback a $CURRENT_VERSION"' ERR

    echo "Desplegando..."
    just _notify "STARTED" "Deploy iniciado"

    # Simular deploy
    echo "Building..."
    sleep 1
    echo "Pushing..."
    sleep 1
    echo "Deploying..."

    just _notify "SUCCESS" "Deploy completado exitosamente"
    echo "{{GREEN}}Deploy completado!{{NORMAL}}"

# Test graceful degradation
[group('test')]
test:
    #!/usr/bin/env bash
    set -euo pipefail
    if command -v cargo-nextest &>/dev/null; then
        echo "Usando cargo-nextest"
        cargo nextest run 2>/dev/null || echo "(simulado)"
    else
        echo "cargo-nextest no encontrado, usando cargo test"
        cargo test 2>/dev/null || echo "(simulado)"
    fi

# Rollback manual
[group('deploy')]
[confirm("Hacer rollback?")]
rollback:
    @echo "Ejecutando rollback..."
    just _notify "ROLLBACK" "Rollback manual ejecutado"
```

## Verificacion

1. Ejecuta `just preflight` y verifica que comprueba que Docker esta corriendo y que git esta limpio.
2. Sin docker o git instalados, confirma que `require()` falla inmediatamente al cargar el justfile, antes de ejecutar cualquier receta.
3. Ejecuta `just deploy` y verifica que solicita confirmacion, luego ejecuta con trap configurado para rollback automatico en caso de fallo.
4. Ejecuta `just test` y observa que cae gracefully a `cargo test` si `cargo-nextest` no esta instalado.
5. Ejecuta `WEBHOOK_URL=https://httpbin.org/post just deploy` y confirma que envia notificaciones al webhook.

## Solucion y Aprendizaje

- [Just Manual - require](https://just.systems/man/en/chapter_34.html) — Documentacion de la funcion `require()` para verificar dependencias externas al cargar el justfile.
- [Annie Cherkaev - Workflow Automation](https://anniecherkaev.com/workflow-automation) — Articulo sobre automatizacion de flujos de trabajo con patrones de manejo de errores y recuperacion.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Guia completa de just que cubre settings de shell, recetas shebang y buenas practicas para scripts robustos.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
