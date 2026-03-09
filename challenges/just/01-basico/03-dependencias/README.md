# 3. Dependencias

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Dependencias entre recipes, before/after, paso de argumentos a dependencias |
| **Dificultad** | Principiante |

## Que vas a aprender

- **Prior dependencies** — Las dependencias previas se listan despues del nombre de la recipe y los dos puntos. Se ejecutan antes de la recipe actual, garantizando el orden correcto de ejecucion.
  [Documentacion: Dependencies](https://just.systems/man/en/chapter_33.html)

- **Posterior dependencies (`&&`)** — Las dependencias posteriores se declaran con `&&` y se ejecutan despues de que la recipe actual termina exitosamente.
  [Documentacion: Dependencies](https://just.systems/man/en/chapter_33.html)

- **Paso de argumentos a dependencias** — Puedes pasar argumentos a una dependencia envolviendola en parentesis, por ejemplo `(build "release")`.
  [Documentacion: Dependencies](https://just.systems/man/en/chapter_33.html)

- **Deduplicacion** — Si una dependencia aparece multiples veces en la cadena de ejecucion, `just` la ejecuta solo una vez, evitando trabajo duplicado.
  [Documentacion: Dependencies](https://just.systems/man/en/chapter_33.html)

- **Recipes privadas con `[private]` o prefijo `_`** — Las recipes que comienzan con `_` o tienen el atributo `[private]` no aparecen en `just --list`, pero pueden ser invocadas directamente o usadas como dependencias.
  [Documentacion: Private Recipes](https://just.systems/man/en/chapter_32.html)

## Descripcion

En este ejercicio crearas un justfile con una cadena de dependencias que simula un flujo de trabajo de desarrollo: limpiar artefactos, compilar, ejecutar tests, y crear un release. Aprenderas a encadenar recipes con dependencias previas y posteriores, a usar recipes privadas, y a pasar argumentos a dependencias.

## Codigo

Crea el `justfile` con el siguiente contenido:

```justfile
default:
    @just --list

# Limpiar artifacts
clean:
    @echo "Limpiando..."
    rm -rf build/

# Build del proyecto
build: clean
    @echo "Construyendo..."
    mkdir -p build
    @echo "binary" > build/app

# Ejecutar tests (depende de build)
test: build
    @echo "Ejecutando tests..."
    @echo "Tests pasaron!"

# Notificar (simulado)
[private]
_notify:
    @echo "Notificando al equipo..."

# Release completo (test primero, luego notify)
release: test && _notify
    @echo "Creando release..."
    @echo "Release completado!"

# Ejemplo de pasar argumentos a dependencias
push target: (build)
    @echo "Pushing {{target}}..."
```

## Verificacion

1. Ejecuta `just test`. Debe ejecutar `clean`, luego `build`, y finalmente `test` en ese orden:
   ```
   just test
   ```

2. Ejecuta `just release`. Debe ejecutar la cadena completa: clean -> build -> test -> release -> _notify:
   ```
   just release
   ```

3. Ejecuta `just --list`. La recipe `_notify` NO debe aparecer en la lista porque es privada:
   ```
   just --list
   ```

4. Ejecuta `just push staging`. Debe ejecutar `build` primero y luego `push`:
   ```
   just push staging
   ```

## Solucion y Aprendizaje

- [Just Manual - Dependencies](https://just.systems/man/en/chapter_33.html) — Documentacion completa sobre dependencias previas, posteriores y paso de argumentos.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las principales caracteristicas, incluyendo el sistema de dependencias.
- [Just Manual - Private Recipes](https://just.systems/man/en/chapter_32.html) — Como ocultar recipes auxiliares de la lista publica.

## Recursos

- [Just Manual - Dependencies](https://just.systems/man/en/chapter_33.html)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
