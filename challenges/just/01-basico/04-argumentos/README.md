# 4. Argumentos

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Argumentos requeridos, valores por defecto, variadic (+/*), env args ($), validacion |
| **Dificultad** | Principiante-Intermedio |

## Que vas a aprender

- **Argumentos requeridos** — Un argumento sin valor por defecto es obligatorio. Si no se proporciona al invocar la recipe, `just` muestra un error indicando el parametro faltante.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

- **Valores por defecto** — Los argumentos pueden tener valores por defecto usando `=`. Si el usuario no proporciona un valor, se usa el valor por defecto definido.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

- **Variadic `+` (uno o mas)** — El prefijo `+` antes de un argumento indica que acepta uno o mas valores. Falla si no se proporciona al menos uno.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

- **Variadic `*` (cero o mas)** — El prefijo `*` antes de un argumento indica que acepta cero o mas valores. No falla si no se proporciona ningun valor.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

- **`$` env args** — El prefijo `$` antes de un argumento lo exporta como variable de entorno al shell que ejecuta la recipe, permitiendo usarlo directamente como `$VAR`.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

## Descripcion

En este ejercicio exploraras todos los patrones de argumentos disponibles en `just`: argumentos requeridos, valores por defecto, argumentos variadicos (uno o mas, cero o mas), argumentos exportados como variables de entorno, y validacion de valores usando shebang recipes con logica condicional.

## Codigo

Crea el `justfile` con el siguiente contenido:

```justfile
default:
    @just --list

# Saludo con default
greet name greeting="Hola":
    @echo "{{greeting}}, {{name}}!"

# Deploy con validacion de entorno
deploy env:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{env}}" != "dev" && "{{env}}" != "staging" && "{{env}}" != "prod" ]]; then
        echo "Error: env debe ser dev, staging o prod"
        exit 1
    fi
    echo "Desplegando en {{env}}..."

# Backup: al menos un archivo requerido
backup +files:
    @echo "Respaldando: {{files}}"

# Commit: mensaje requerido, flags opcionales
commit message *flags:
    @echo "git commit -m \"{{message}}\" {{flags}}"

# Puerto como variable de entorno
serve $PORT="8080":
    @echo "Sirviendo en puerto $PORT"
```

## Verificacion

1. Ejecuta `just greet Juan`. Debe imprimir "Hola, Juan!" usando el saludo por defecto:
   ```
   just greet Juan
   ```

2. Ejecuta `just greet Juan "Buenos dias"`. Debe imprimir "Buenos dias, Juan!":
   ```
   just greet Juan "Buenos dias"
   ```

3. Ejecuta `just deploy production`. Debe fallar con un mensaje de error porque "production" no es un valor valido:
   ```
   just deploy production
   ```

4. Ejecuta `just deploy dev`. Debe ejecutarse correctamente:
   ```
   just deploy dev
   ```

5. Ejecuta `just backup` sin argumentos. Debe fallar porque `+files` requiere al menos un valor:
   ```
   just backup
   ```

6. Ejecuta `just backup a.txt b.txt`. Debe imprimir ambos archivos:
   ```
   just backup a.txt b.txt
   ```

7. Ejecuta `just commit "fix bug"`. Debe funcionar sin flags adicionales:
   ```
   just commit "fix bug"
   ```

8. Ejecuta `just commit "fix bug" --no-verify`. Debe pasar el flag adicional:
   ```
   just commit "fix bug" --no-verify
   ```

9. Ejecuta `just serve`. Debe usar el puerto 8080 por defecto. Luego ejecuta `just serve 3000` para usar el puerto 3000:
   ```
   just serve
   just serve 3000
   ```

## Solucion y Aprendizaje

- [Just Manual - Parameters](https://just.systems/man/en/chapter_36.html) — Documentacion completa sobre todos los tipos de parametros: requeridos, por defecto, variadicos y de entorno.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just`, incluyendo el manejo de argumentos.
- [Just Manual - Recipes](https://just.systems/man/en/chapter_30.html) — Referencia sobre recipes, shebang recipes y ejecucion de comandos.

## Recursos

- [Just Manual - Parameters](https://just.systems/man/en/chapter_36.html)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
