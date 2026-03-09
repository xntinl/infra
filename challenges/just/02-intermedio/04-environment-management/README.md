# 8. Environment Management

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Carga de dotenv, cambio de environments, validacion de variables |
| **Dificultad** | Intermedio |

## Que vas a aprender

- **`set dotenv-load`** — Este setting le indica a `just` que cargue automaticamente las variables definidas en el archivo `.env` del directorio actual, haciendolas disponibles como variables de entorno para todas las recipes.
  [Documentacion: dotenv Integration](https://just.systems/man/en/chapter_26.html)

- **`set dotenv-filename`** — Permite especificar un nombre de archivo diferente a `.env` para la carga automatica de variables de entorno, util cuando manejas multiples configuraciones.
  [Documentacion: Settings](https://just.systems/man/en/chapter_26.html)

- **`env()` con valores por defecto** — La funcion `env("VARIABLE", "default")` lee una variable de entorno y devuelve el valor por defecto si no esta definida, proporcionando configuracion segura sin errores.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_39.html)

- **Shebang recipes para logica multi-linea** — Las recipes que comienzan con `#!/usr/bin/env bash` (shebang) ejecutan todo el contenido como un solo script, permitiendo usar condicionales, bucles y logica compleja.
  [Documentacion: Shebang Recipes](https://just.systems/man/en/chapter_30.html)

- **`[script]` attribute** — El atributo `[script]` es una alternativa moderna a las shebang recipes que permite ejecutar bloques de codigo como scripts completos sin necesidad de la linea shebang.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

## Descripcion

En este ejercicio crearas un sistema de gestion de environments usando archivos `.env`. El justfile incluira recipes para cambiar entre environments (dev, staging), mostrar la configuracion actual, validar que todas las variables requeridas estan definidas, y listar los environments disponibles. Usaras shebang recipes para la logica de validacion.

## Codigo

Primero, crea el archivo `.env.dev`:

```
APP_ENV=development
DATABASE_URL=postgres://localhost:5432/myapp_dev
API_KEY=dev-key-123
PORT=3000
```

Luego, crea el archivo `.env.staging`:

```
APP_ENV=staging
DATABASE_URL=postgres://staging-db:5432/myapp_staging
API_KEY=staging-key-456
PORT=8080
```

Finalmente, crea el `justfile`:

```justfile
set dotenv-load

default:
    @just --list

# Mostrar configuracion actual
[group('env')]
env-show:
    @echo "Environment: {{env('APP_ENV', 'no definido')}}"
    @echo "Database:    {{env('DATABASE_URL', 'no definido')}}"
    @echo "API Key:     {{env('API_KEY', 'no definido')}}"
    @echo "Port:        {{env('PORT', 'no definido')}}"

# Cambiar a un environment
[group('env')]
env-switch target:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -f ".env.{{target}}" ]]; then
        cp ".env.{{target}}" .env
        echo "Cambiado a environment: {{target}}"
    else
        echo "Error: no existe .env.{{target}}"
        echo "Disponibles:"
        ls -1 .env.* 2>/dev/null | sed 's/.env./  /'
        exit 1
    fi

# Validar variables requeridas
[group('env')]
env-check:
    #!/usr/bin/env bash
    set -euo pipefail
    REQUIRED=("APP_ENV" "DATABASE_URL" "API_KEY")
    MISSING=()
    for var in "${REQUIRED[@]}"; do
        if [[ -z "${!var:-}" ]]; then
            MISSING+=("$var")
        fi
    done
    if [[ ${#MISSING[@]} -gt 0 ]]; then
        echo "Variables faltantes:"
        printf '  - %s\n' "${MISSING[@]}"
        exit 1
    fi
    echo "Todas las variables requeridas estan definidas"

# Listar environments disponibles
[group('env')]
env-list:
    @ls -1 .env.* 2>/dev/null | sed 's/.env.//' || echo "No hay environments"
```

## Verificacion

1. Ejecuta `just env-switch dev`. Debe copiar `.env.dev` a `.env` y confirmar el cambio:
   ```
   just env-switch dev
   ```

2. Ejecuta `just env-show`. Debe mostrar los valores del environment actual cargados desde `.env`:
   ```
   just env-show
   ```

3. Ejecuta `just env-check`. Debe validar que todas las variables requeridas estan definidas:
   ```
   just env-check
   ```

4. Ejecuta `just env-list`. Debe listar los environments disponibles (dev, staging):
   ```
   just env-list
   ```

5. Ejecuta `just env-switch nonexistent`. Debe fallar con un mensaje de error y mostrar los environments disponibles:
   ```
   just env-switch nonexistent
   ```

## Solucion y Aprendizaje

- [Just Manual - dotenv Integration](https://just.systems/man/en/chapter_26.html) — Documentacion sobre la carga automatica de archivos `.env` y los settings relacionados.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just` incluyendo integracion con dotenv.
- [Just Manual - Shebang Recipes](https://just.systems/man/en/chapter_30.html) — Como escribir recipes multi-linea con logica compleja usando shebang.

## Recursos

- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
