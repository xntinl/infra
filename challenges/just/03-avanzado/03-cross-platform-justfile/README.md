# 13. Cross-Platform Justfile

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Atributos condicionales por OS, funciones os()/arch(), shell multiplataforma |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **Atributos `[linux]`, `[macos]`, `[windows]`** — Atributos condicionales que restringen la ejecucion de una recipe a un sistema operativo especifico. Solo la variante del OS actual se ejecuta.
  [Documentacion: Attributes](https://just.systems/man/en/chapter_32.html)
- **Funciones `os()` y `arch()`** — Funciones built-in que devuelven el sistema operativo y la arquitectura del host. Permiten construir rutas y nombres de artefactos dinamicos sin depender de comandos shell.
  [Documentacion: Functions](https://just.systems/man/en/chapter_31.html)
- **`set windows-shell`** — Setting que configura PowerShell como shell en Windows en lugar de `cmd.exe`, permitiendo sintaxis moderna y consistente.
  [Documentacion: Settings](https://just.systems/man/en/chapter_26.html)

## Descripcion

Crea un justfile que funcione en Linux, macOS y Windows:
- Usa `[linux]`, `[macos]`, `[windows]` para install-deps
- Usa `os()` y `arch()` para rutas dinamicas
- Usa `set windows-shell` para PowerShell
- Usa funciones built-in en lugar de comandos shell

## Codigo

```justfile
set windows-shell := ["powershell.exe", "-NoLogo", "-Command"]

project := "myapp"
target := os() + "-" + arch()

default:
    @just --list

# Instalar dependencias (Linux)
[linux]
install-deps:
    sudo apt-get update && sudo apt-get install -y build-essential

# Instalar dependencias (macOS)
[macos]
install-deps:
    brew install cmake openssl

# Instalar dependencias (Windows)
[windows]
install-deps:
    choco install cmake openssl

# Build con nombre de artefacto segun plataforma
build:
    @echo "Building for {{target}}..."
    @echo "Output: dist/{{project}}-{{target}}"
    mkdir -p dist
    echo "binary placeholder" > dist/{{project}}-{{target}}

# Mostrar info de plataforma
info:
    @echo "OS:   {{os()}}"
    @echo "Arch: {{arch()}}"
    @echo "Family: {{os_family()}}"

# Abrir directorio (cross-platform)
[linux]
open-dir dir=".":
    xdg-open {{dir}}

[macos]
open-dir dir=".":
    open {{dir}}

[windows]
open-dir dir=".":
    explorer {{dir}}
```

## Verificacion

1. `just info` muestra el OS, arquitectura y familia correctos.
2. `just install-deps` ejecuta solo la variante del OS actual.
3. `just build` genera un artefacto con el nombre correcto segun la plataforma.
4. `just open-dir` usa el comando correcto segun el OS.

## Solucion y Aprendizaje

- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html) — Documentacion de atributos condicionales por OS.
- [Just Manual - Functions](https://just.systems/man/en/chapter_31.html) — Referencia de funciones built-in como os(), arch(), os_family().
- [Stuart Ellis - Just Task Runner](https://www.stuartellis.name/articles/just-task-runner/) — Guia practica sobre shared tooling con just para equipos con sistemas diversos.
- [Just Manual - Settings](https://just.systems/man/en/chapter_26.html) — Configuracion de shells por plataforma.

## Recursos

- [Just Manual - Functions](https://just.systems/man/en/chapter_31.html)
- [Just Manual - Attributes](https://just.systems/man/en/chapter_32.html)
- [Just Manual](https://just.systems/man/en/)

## Notas

_Espacio para tus notas personales._
