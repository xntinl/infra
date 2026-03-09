# 2. Variables y Backticks

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Variables, backtick expressions, `env()`, string interpolation |
| **Dificultad** | Principiante |

## Que vas a aprender

- **Variables `:=`** — Las variables en un justfile se definen con `:=` y pueden contener valores estaticos o dinamicos. Se referencian usando `{{variable}}` dentro de las recipes.
  [Documentacion: Variables](https://just.systems/man/en/chapter_37.html)

- **Backtick expressions (`` ` ``)** — Los backticks permiten capturar la salida de un comando del shell y asignarla a una variable. Se evaluan cuando el justfile se carga.
  [Documentacion: Backtick Expressions](https://just.systems/man/en/chapter_37.html)

- **`env()` con valor por defecto** — La funcion `env()` lee variables de entorno del sistema. El segundo argumento es un valor por defecto que se usa cuando la variable no esta definida.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_39.html)

- **String interpolation `{{var}}`** — Dentro de las recipes, `{{variable}}` se reemplaza por el valor de la variable. Esto funciona tanto con variables definidas en el justfile como con funciones built-in.
  [Documentacion: String Interpolation](https://just.systems/man/en/chapter_37.html)

- **`read()` para leer archivos** — La funcion `read()` lee el contenido de un archivo y lo devuelve como string, util para obtener valores de configuracion desde archivos externos.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_39.html)

## Descripcion

En este ejercicio crearas un justfile que lee la version del proyecto desde un archivo `VERSION`, usa backticks para obtener el hash de git y la hora de compilacion, y muestra toda esa informacion en una recipe de build. Tambien aprenderas a usar `env()` para leer variables de entorno del sistema con valores por defecto.

## Codigo

Primero, crea un archivo llamado `VERSION` con el siguiente contenido:

```
1.0.0
```

Luego, crea el `justfile`:

```justfile
# Variables
version := read("VERSION")
git_hash := `git rev-parse --short HEAD 2>/dev/null || echo "no-git"`
build_time := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
ci := env("CI", "false")

# Mostrar recetas
default:
    @just --list

# Build con info de version
build:
    @echo "Building v{{version}} ({{git_hash}})"
    @echo "Time: {{build_time}}"
    @echo "CI: {{ci}}"

# Mostrar todas las variables
vars:
    @just --evaluate
```

## Verificacion

1. Ejecuta `just build`. Debe mostrar la version leida del archivo `VERSION` y el hash de git:
   ```
   just build
   ```

2. Ejecuta `just vars`. Debe mostrar todas las variables y sus valores evaluados:
   ```
   just vars
   ```

3. Ejecuta `just build` con la variable de entorno `CI` definida. Debe mostrar "CI: true":
   ```
   CI=true just build
   ```

## Solucion y Aprendizaje

- [Just Manual - Variables](https://just.systems/man/en/chapter_37.html) — Documentacion completa sobre variables, asignaciones y backtick expressions.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just`, incluyendo variables y expresiones.
- [Just Manual - Built-in Functions](https://just.systems/man/en/chapter_39.html) — Referencia de todas las funciones built-in disponibles como `env()`, `read()`, y mas.

## Recursos

- [Just Manual - Variables](https://just.systems/man/en/chapter_37.html)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
