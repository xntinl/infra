# 7. Migracion de Makefile

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Conversion de Makefile a justfile, diferencias de sintaxis, mejoras |
| **Dificultad** | Intermedio |

## Que vas a aprender

- **Diferencias de sintaxis Makefile vs justfile** — Las principales diferencias incluyen: `.PHONY` no es necesario (todas las recipes son "phony" por defecto), las variables usan `{{}}` en vez de `$()`, y la indentacion puede ser con espacios o tabs.
  [Documentacion: Comparison to Make](https://just.systems/man/en/chapter_22.html)

- **Eliminacion de `.PHONY`** — En `make`, los targets que no corresponden a archivos necesitan `.PHONY` para ejecutarse siempre. En `just`, todas las recipes siempre se ejecutan, eliminando esta necesidad.
  [Documentacion: Comparison to Make](https://just.systems/man/en/chapter_22.html)

- **`$(VAR)` a `{{VAR}}`** — La interpolacion de variables cambia de la sintaxis `$(VARIABLE)` de Make a `{{variable}}` en `just`. Las variables se definen con `:=` en ambos casos, pero `just` usa minusculas por convencion.
  [Documentacion: Variables](https://just.systems/man/en/chapter_37.html)

- **Paso de argumentos** — En `make`, pasar argumentos a targets es complicado y requiere workarounds. En `just`, las recipes pueden declarar parametros directamente con valores por defecto, haciendo el paso de argumentos natural e intuitivo.
  [Documentacion: Parameters](https://just.systems/man/en/chapter_36.html)

- **Atributo `[confirm]`** — El atributo `[confirm("mensaje")]` solicita confirmacion del usuario antes de ejecutar una recipe, util para operaciones destructivas o de deploy.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

## Descripcion

En este ejercicio tomaras un Makefile tipico de un proyecto Go y lo convertiras a un justfile equivalente pero mejorado. Aprenderas las diferencias clave de sintaxis entre Make y `just`, y como aprovechar las caracteristicas adicionales de `just` como argumentos con defaults, confirmacion interactiva, grupos y documentacion automatica.

## Codigo

### Makefile original

Este es el Makefile que vamos a migrar:

```makefile
.PHONY: build test lint clean deploy

BINARY_NAME=myapp
VERSION=$(shell git describe --tags --always)

build:
	go build -ldflags "-X main.version=$(VERSION)" -o bin/$(BINARY_NAME) .

test: build
	go test -v ./...

lint:
	golangci-lint run

clean:
	rm -rf bin/

deploy: test
	scp bin/$(BINARY_NAME) server:/opt/app/
```

### Justfile convertido

Crea el `justfile` con el siguiente contenido:

```justfile
binary_name := "myapp"
version := `git describe --tags --always 2>/dev/null || echo "dev"`

default:
    @just --list

# Build la aplicacion
[group('build')]
build:
    go build -ldflags "-X main.version={{version}}" -o bin/{{binary_name}} .

# Ejecutar tests
[group('test')]
test: build
    go test -v ./...

# Ejecutar linter
[group('quality')]
lint:
    golangci-lint run

# Limpiar artifacts
[group('dev')]
clean:
    rm -rf bin/

# Desplegar al servidor
[group('deploy')]
[confirm("Desplegar {{binary_name}} v{{version}} al servidor?")]
deploy server="server": test
    scp bin/{{binary_name}} {{server}}:/opt/app/
```

### Tabla de diferencias

| Concepto | Makefile | justfile |
|----------|----------|----------|
| Variables | `$(VARIABLE)` | `{{variable}}` |
| Shell command | `$(shell cmd)` | `` `cmd` `` |
| Phony targets | `.PHONY: target` | No necesario |
| Indentacion | Solo tabs | Tabs o espacios |
| Argumentos | Complicado | `recipe arg="default":` |
| Documentacion | Manual | Comentarios `#` auto-listados |
| Confirmacion | No existe | `[confirm("msg")]` |
| Grupos | No existe | `[group('name')]` |

## Verificacion

1. Verifica que no se necesita `.PHONY`. Todas las recipes se ejecutan siempre:
   ```
   just build
   ```

2. Verifica que las variables usan `{{}}` en vez de `$()`:
   ```
   just --evaluate
   ```

3. Verifica que `deploy` ahora acepta `server` como argumento con valor por defecto:
   ```
   just deploy staging-server
   ```

4. Verifica que `deploy` tiene `[confirm]` y pide confirmacion antes de ejecutar:
   ```
   just deploy
   ```

5. Ejecuta `just --list`. Debe mostrar recipes documentadas y agrupadas:
   ```
   just --list
   ```

6. Verifica que la indentacion con espacios funciona correctamente (a diferencia de Make donde solo tabs son validos).

## Solucion y Aprendizaje

- [Applied Go - Just Make a Task](https://appliedgo.net/spotlight/just-make-a-task/) — Articulo detallado comparando `make` con `just` y mostrando como migrar.
- [Charm - Make vs Just](https://discourse.charmhub.io/t/make-vs-just-a-detailed-comparison/16097) — Comparacion detallada entre Make y Just con ejemplos practicos.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just` que mejoran la experiencia respecto a Make.
- [Just Manual - Comparison to Make](https://just.systems/man/en/chapter_22.html) — Comparacion oficial entre `just` y `make` del manual.

## Recursos

- [Just GitHub](https://github.com/casey/just)
- [Applied Go - Just Make a Task](https://appliedgo.net/spotlight/just-make-a-task/)
- [Just Manual - Comparison to Make](https://just.systems/man/en/chapter_22.html)

## Notas

_Espacio para tus notas personales._
