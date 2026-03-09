# 1. Tu Primer Justfile

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Estructura basica de un justfile, recipes, funciones built-in |
| **Dificultad** | Principiante |

## Que vas a aprender

- **Recipes basicas** — Las recipes son los comandos que defines en un justfile. Cada recipe tiene un nombre y uno o mas pasos que se ejecutan en orden.
  [Documentacion: Recipes](https://just.systems/man/en/chapter_30.html)

- **Prefijo `@` (silent)** — Al agregar `@` antes de un comando, `just` no imprime el comando antes de ejecutarlo, mostrando solo la salida.
  [Documentacion: Quiet Recipes](https://just.systems/man/en/chapter_30.html)

- **`os()` y `arch()`** — Funciones built-in que devuelven el sistema operativo y la arquitectura del sistema donde se ejecuta el justfile.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_39.html)

- **Argumentos con valores por defecto** — Las recipes pueden recibir argumentos. Si defines un valor por defecto con `=`, el argumento se vuelve opcional.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

- **`justfile_directory()` y `num_cpus()`** — Funciones built-in adicionales que proporcionan informacion del entorno de ejecucion, como el directorio del justfile y el numero de CPUs disponibles.
  [Documentacion: Built-in Functions](https://just.systems/man/en/chapter_39.html)

## Descripcion

En este ejercicio crearas tu primer justfile con tres recipes: una recipe `default` que lista todas las recipes disponibles, una recipe `hello` que saluda a alguien usando un argumento con valor por defecto, y una recipe `info` que muestra informacion del sistema utilizando funciones built-in de `just`.

## Codigo

Crea un archivo llamado `justfile` con el siguiente contenido:

```justfile
# Listar recetas disponibles
default:
    @just --list --unsorted

# Saludar a alguien
hello name="World":
    @echo "Hola, {{name}}!"

# Mostrar info del sistema
info:
    @echo "OS:     {{os()}}"
    @echo "Arch:   {{arch()}}"
    @echo "Dir:    {{justfile_directory()}}"
    @echo "CPUs:   {{num_cpus()}}"
```

## Verificacion

1. Ejecuta `just` sin argumentos. Debe listar todas las recipes disponibles:
   ```
   just
   ```

2. Ejecuta `just hello` sin argumentos. Debe imprimir "Hola, World!":
   ```
   just hello
   ```

3. Ejecuta `just hello Juan`. Debe imprimir "Hola, Juan!":
   ```
   just hello Juan
   ```

4. Ejecuta `just info`. Debe mostrar el sistema operativo, la arquitectura, el directorio del justfile y el numero de CPUs:
   ```
   just info
   ```

## Solucion y Aprendizaje

- [Just Manual - Getting Started](https://just.systems/man/en/) — Guia oficial para comenzar a usar `just` desde cero.
- [Casey Rodarmor - Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido del autor de `just` por las principales caracteristicas de la herramienta.
- [Just Manual - Recipes](https://just.systems/man/en/chapter_30.html) — Documentacion completa sobre como definir y usar recipes.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
