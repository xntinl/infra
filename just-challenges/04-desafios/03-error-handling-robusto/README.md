# 18. Justfile con Error Handling Robusto

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Deploy recipe con verificacion de prerequisitos, backup, rollback y notificaciones |
| **Dificultad** | Desafio |

## Que vas a aprender

- **Shebang recipes para control de flujo** — Con shebang recipes (`#!/usr/bin/env bash`) puedes usar `trap`, `if/else`, y manejo de exit codes para implementar logica de error handling compleja.
  [Documentacion: Shebang Recipes](https://just.systems/man/en/chapter_44.html)
- **`command -v` para verificar prerequisitos** — Patron de verificar que herramientas necesarias estan instaladas antes de ejecutar la recipe, evitando fallos a mitad de ejecucion.
  [Documentacion: Just Manual](https://just.systems/man/en/)
- **Rollback con `trap`** — Usando `trap` en bash puedes registrar funciones de limpieza o rollback que se ejecutan automaticamente cuando un script falla.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Crea un justfile con deploy recipe que:
- Verifique prerequisitos (docker, kubectl instalados)
- Haga backup antes de deploy
- Implemente rollback automatico si falla
- Notifique por webhook al finalizar (exito o fallo)

## Codigo

```justfile
set shell := ["bash", "-euo", "pipefail", "-c"]

GREEN  := '\033[0;32m'
RED    := '\033[0;31m'
RESET  := '\033[0m'

default:
    @just --list

# Verificar que las herramientas necesarias estan instaladas
[group('checks')]
check-prereqs:
    #!/usr/bin/env bash
    set -euo pipefail
    missing=()
    for cmd in docker kubectl curl; do
        if ! command -v "$cmd" &>/dev/null; then
            missing+=("$cmd")
        fi
    done
    if [ ${#missing[@]} -gt 0 ]; then
        echo -e "{{RED}}Faltan herramientas: ${missing[*]}{{RESET}}"
        exit 1
    fi
    echo -e "{{GREEN}}Todos los prerequisitos instalados{{RESET}}"

# Backup antes de deploy
[group('deploy')]
backup:
    #!/usr/bin/env bash
    set -euo pipefail
    timestamp=$(date +%Y%m%d_%H%M%S)
    echo "Creando backup backup-${timestamp}.tar.gz..."
    echo "placeholder" > "backup-${timestamp}.tar.gz"
    echo -e "{{GREEN}}Backup creado: backup-${timestamp}.tar.gz{{RESET}}"

# Notificar resultado via webhook
[private]
_notify status message:
    #!/usr/bin/env bash
    echo "Webhook: [{{status}}] {{message}}"
    # curl -s -X POST "$WEBHOOK_URL" -d '{"status":"{{status}}","message":"{{message}}"}'

# Deploy con rollback automatico
[group('deploy')]
[confirm("Proceder con el deploy?")]
deploy env="dev": check-prereqs backup
    #!/usr/bin/env bash
    set -euo pipefail
    current_version="v1.0.0"

    rollback() {
        echo -e "{{RED}}Deploy fallo! Ejecutando rollback a $current_version...{{RESET}}"
        echo "Rollback completado"
        just _notify "FAILURE" "Deploy a {{env}} fallo, rollback a $current_version"
    }
    trap rollback ERR

    echo "Desplegando a {{env}}..."
    echo "Deploy exitoso!"
    just _notify "SUCCESS" "Deploy a {{env}} completado"
    echo -e "{{GREEN}}Deploy a {{env}} completado exitosamente{{RESET}}"
```

## Verificacion

1. `just check-prereqs` verifica que docker, kubectl y curl estan instalados.
2. `just backup` crea un archivo de backup con timestamp.
3. `just deploy` ejecuta la secuencia completa: prereqs, backup, deploy con rollback si falla.
4. Si el deploy falla, el rollback se ejecuta automaticamente via `trap`.
5. Se envia notificacion de exito o fallo al webhook.

## Solucion y Aprendizaje

- [Just Manual - Shebang Recipes](https://just.systems/man/en/chapter_44.html) — Como escribir scripts complejos con bash dentro de recipes.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las capacidades de just incluyendo manejo de errores.
- [Frank Wiles - Just Do It](https://frankwiles.com/posts/just-do-it/) — Articulo sobre el uso de just para workflows de deploy.
- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html) — Referencia de atributos como `[confirm]` y `[private]`.

## Recursos

- [Just Manual - Shebang Recipes](https://just.systems/man/en/chapter_44.html)
- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
