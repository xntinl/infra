# 17. Justfile con Scripts Multi-Lenguaje

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Shebang recipes para ejecutar scripts en multiples lenguajes |
| **Dificultad** | Desafio |

## Que vas a aprender

- **Shebang recipes** — Recipes que comienzan con `#!` (shebang) se ejecutan como scripts independientes en lugar de linea por linea. El cuerpo completo se guarda en un archivo temporal y se ejecuta con el interprete indicado.
  [Documentacion: Shebang Recipes](https://just.systems/man/en/chapter_44.html)
- **Multi-lenguaje en un solo archivo** — Con shebang recipes puedes usar Python, Node.js, Ruby, Perl o cualquier interprete disponible en el sistema, todo desde un unico justfile.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Crea un justfile que use shebang recipes para ejecutar scripts en al menos 4 lenguajes diferentes (bash, python, node, ruby) dentro del mismo justfile.

## Codigo

```justfile
default:
    @just --list

# Script en Bash
[group('scripts')]
bash-script:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Bash Script ==="
    files=$(find . -maxdepth 1 -type f | wc -l)
    echo "Archivos en directorio actual: $files"
    echo "Shell: $SHELL"
    echo "PID: $$"

# Script en Python
[group('scripts')]
python-script:
    #!/usr/bin/env python3
    import sys
    import os
    import json
    print("=== Python Script ===")
    info = {
        "version": sys.version.split()[0],
        "platform": sys.platform,
        "cwd": os.getcwd(),
        "python_path": sys.executable
    }
    print(json.dumps(info, indent=2))

# Script en Node.js
[group('scripts')]
node-script:
    #!/usr/bin/env node
    const os = require('os');
    console.log("=== Node.js Script ===");
    console.log(JSON.stringify({
        nodeVersion: process.version,
        platform: os.platform(),
        arch: os.arch(),
        cpus: os.cpus().length,
        memory: `${Math.round(os.totalmem() / 1024 / 1024)}MB`
    }, null, 2));

# Script en Ruby
[group('scripts')]
ruby-script:
    #!/usr/bin/env ruby
    puts "=== Ruby Script ==="
    puts "Ruby version: #{RUBY_VERSION}"
    puts "Platform: #{RUBY_PLATFORM}"
    puts "Current dir: #{Dir.pwd}"
    puts "Files: #{Dir.glob('*').join(', ')}"

# Ejecutar todos los scripts
[group('scripts')]
all: bash-script python-script node-script ruby-script
    @echo "Todos los scripts ejecutados!"
```

## Verificacion

1. `just bash-script` ejecuta el script con bash e imprime informacion del directorio.
2. `just python-script` ejecuta Python y muestra info del sistema en JSON.
3. `just node-script` ejecuta Node.js y muestra info del host.
4. `just ruby-script` ejecuta Ruby y lista archivos del directorio.
5. `just all` ejecuta los 4 scripts en secuencia.

## Solucion y Aprendizaje

- [Just Manual - Shebang Recipes](https://just.systems/man/en/chapter_44.html) — Documentacion sobre como escribir recipes que se ejecutan como scripts completos.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por just incluyendo ejemplos de shebang recipes.
- [Annie Cherkaev - Workflow Automation](https://anniecherkaev.com/workflow-automation) — Articulo sobre automatizacion de workflows usando multiples lenguajes.

## Recursos

- [Just Manual - Shebang Recipes](https://just.systems/man/en/chapter_44.html)
- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
