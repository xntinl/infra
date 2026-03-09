# 5. Justfile para Proyecto Go

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Justfile completo para proyecto Go con ldflags, testing, linting, Docker |
| **Dificultad** | Intermedio |

## Que vas a aprender

- **`set shell`, `set dotenv-load`, `set export`** — Los settings del justfile permiten configurar el comportamiento global: el shell por defecto, la carga automatica de archivos `.env`, y la exportacion automatica de variables como variables de entorno.
  [Documentacion: Settings](https://just.systems/man/en/chapter_26.html)

- **ldflags con version embedding** — Usando la concatenacion de strings en variables de `just`, puedes construir ldflags complejos que inyectan informacion de version, commit y tiempo de compilacion directamente en el binario de Go.
  [Documentacion: Variables](https://just.systems/man/en/chapter_37.html)

- **Atributo `[group]`** — El atributo `[group('nombre')]` organiza las recipes en grupos logicos que se muestran juntos al ejecutar `just --list`, mejorando la legibilidad en justfiles grandes.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)

- **Cross-compilation** — Usando variables de entorno como `CGO_ENABLED=0` dentro de las recipes, puedes configurar compilacion estatica y cross-compilation para diferentes plataformas.
  [Documentacion: Recipes](https://just.systems/man/en/chapter_30.html)

- **Concatenacion de strings** — Las variables en `just` soportan concatenacion con el operador `+`, permitiendo construir strings complejos como flags de compilacion de forma legible.
  [Documentacion: Variables](https://just.systems/man/en/chapter_37.html)

## Descripcion

En este ejercicio crearas un justfile completo para un proyecto Go. Incluye recipes para compilar con ldflags que inyectan informacion de version, ejecutar tests con race detector y coverage, lint con golangci-lint, build de imagenes Docker, y recipes de desarrollo. Las recipes estan organizadas en grupos para una mejor navegacion.

## Codigo

Crea el `justfile` con el siguiente contenido:

```justfile
set dotenv-load
set export
set shell := ["bash", "-uc"]

project_name := "myapp"
version := env("VERSION", `git describe --tags --always --dirty 2>/dev/null || echo "dev"`)
commit := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`
build_time := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
ldflags := "-s -w -X main.version=" + version + " -X main.commit=" + commit + " -X main.buildTime=" + build_time

default:
    @just --list --unsorted

# Build la aplicacion
[group('build')]
build:
    go build -trimpath -ldflags '{{ldflags}}' -o bin/{{project_name}} ./cmd/{{project_name}}

# Build release optimizado
[group('build')]
build-release:
    CGO_ENABLED=0 go build -trimpath -ldflags '{{ldflags}}' -o bin/{{project_name}} ./cmd/{{project_name}}

# Ejecutar todos los tests
[group('test')]
test:
    go test -race -count=1 ./...

# Tests con coverage
[group('test')]
test-coverage:
    go test -race -coverprofile=coverage.out -covermode=atomic ./...
    go tool cover -html=coverage.out -o coverage.html
    @echo "Reporte: coverage.html"

# Linter
[group('quality')]
lint:
    golangci-lint run ./...

# Formatear codigo
[group('quality')]
fmt:
    gofmt -s -w .

# Todos los checks
[group('quality')]
check: fmt lint test

# Docker build
[group('docker')]
docker-build:
    docker build -t {{project_name}}:{{version}} -t {{project_name}}:latest .

# Ejecutar la app
[group('dev')]
run *args:
    go run ./cmd/{{project_name}} {{args}}

# Limpiar artifacts
[group('dev')]
clean:
    rm -rf bin/ coverage.out coverage.html

# Info del proyecto
[group('dev')]
info:
    @echo "Proyecto:  {{project_name}}"
    @echo "Version:   {{version}}"
    @echo "Commit:    {{commit}}"
    @echo "Go:        $(go version)"
```

## Verificacion

1. Ejecuta `just` sin argumentos. Debe mostrar todas las recipes organizadas por grupo (build, test, quality, docker, dev):
   ```
   just
   ```

2. Ejecuta `just build`. Debe crear el binario en el directorio `bin/` con ldflags inyectados:
   ```
   just build
   ```

3. Ejecuta `just test`. Debe ejecutar los tests con el race detector habilitado:
   ```
   just test
   ```

4. Ejecuta `just check`. Debe ejecutar `fmt`, `lint` y `test` en orden:
   ```
   just check
   ```

5. Ejecuta `just info`. Debe mostrar la metadata del proyecto incluyendo version, commit y version de Go:
   ```
   just info
   ```

6. Ejecuta `just docker-build`. Debe construir la imagen Docker con el tag de version y el tag `latest`:
   ```
   just docker-build
   ```

## Solucion y Aprendizaje

- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html) — Documentacion de todos los settings disponibles como `dotenv-load`, `export` y `shell`.
- [binbandit/ultimate-gojust](https://github.com/binbandit/ultimate-gojust) — Ejemplo completo de un justfile para proyectos Go con mejores practicas.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por las caracteristicas de `just` aplicadas a proyectos reales.
- [miguno/golang-docker-build-tutorial](https://github.com/miguno/golang-docker-build-tutorial) — Tutorial de Docker build para Go que incluye ejemplo de justfile.

## Recursos

- [Just GitHub](https://github.com/casey/just)
- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html)
- [miguno/golang-docker-build-tutorial](https://github.com/miguno/golang-docker-build-tutorial)

## Notas

_Espacio para tus notas personales._
