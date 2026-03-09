# D2. Scripts Multi-Lenguaje

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Shebang recipes, `[script]` con diferentes interpretes |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **Shebang recipes (`#!/usr/bin/env`)** — Recetas que comienzan con `#!` se ejecutan como un script completo en un archivo temporal, no linea por linea. Esto permite usar cualquier lenguaje que tenga un interprete disponible en el sistema.
  [Documentacion: Shebang Recipes](https://just.systems/man/en/chapter_42.html)

- **`[script("interpreter")]`** — Atributo que especifica el interprete de forma explicita sin necesidad de linea shebang. Mas limpio y declarativo, ideal cuando el script no necesita opciones especiales del interprete.
  [Documentacion: script attribute](https://just.systems/man/en/chapter_32.html)

- **Multi-language en un solo justfile** — Capacidad de combinar scripts en bash, Python, Node.js, Ruby y otros lenguajes en un mismo justfile, eligiendo el mejor lenguaje para cada tarea especifica.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Script vs regular recipes** — Las recetas regulares ejecutan cada linea como un comando separado del shell. Las recetas shebang o `[script]` ejecutan todo el cuerpo como un unico archivo, permitiendo variables locales, funciones y logica compleja.
  [Documentacion: Shebang Recipes](https://just.systems/man/en/chapter_42.html)

## Descripcion

Vas a crear un justfile que demuestra la capacidad de just para ejecutar scripts en multiples lenguajes de programacion. Cada receta usa un lenguaje diferente -- bash para tareas de sistema, Python para procesamiento de datos, Node.js para generacion de UUIDs -- tanto con la sintaxis shebang tradicional como con el atributo `[script]`. Esto muestra como just puede reemplazar scripts sueltos en diferentes lenguajes con un unico punto de entrada organizado.

## Codigo

```justfile
default:
    @just --list

# Script en Bash
[group('scripts')]
bash-info:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "=== Bash Script ==="
    echo "Shell: $BASH_VERSION"
    echo "User: $(whoami)"
    echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Script en Python
[group('scripts')]
python-calc:
    #!/usr/bin/env python3
    import json
    import sys
    data = {
        "fibonacci": [1, 1, 2, 3, 5, 8, 13, 21],
        "sum": sum([1, 1, 2, 3, 5, 8, 13, 21]),
        "python_version": sys.version.split()[0]
    }
    print(json.dumps(data, indent=2))

# Script en Node.js
[group('scripts')]
node-uuid:
    #!/usr/bin/env node
    const crypto = require('crypto');
    const uuid = crypto.randomUUID();
    console.log(`Generated UUID: ${uuid}`);
    console.log(`Node version: ${process.version}`);

# Script con interprete explicito
[group('scripts')]
[script("python3")]
python-generate-config:
    import json
    config = {
        "debug": True,
        "port": 8080,
        "features": ["auth", "logging"]
    }
    print(json.dumps(config, indent=2))

# Bash con logica compleja
[group('scripts')]
health-check url="http://localhost:8080":
    #!/usr/bin/env bash
    set -euo pipefail
    echo "Checking {{url}}..."
    if curl -sf "{{url}}/health" > /dev/null 2>&1; then
        echo "OK: Service is healthy"
    else
        echo "FAIL: Service is not responding"
        exit 1
    fi
```

## Verificacion

1. Ejecuta `just bash-info` y verifica que muestra la version de bash e informacion del sistema.
2. Ejecuta `just python-calc` y confirma que imprime JSON con los datos de fibonacci.
3. Ejecuta `just node-uuid` y verifica que genera un UUID usando Node.js.
4. Ejecuta `just python-generate-config` y confirma que usa la sintaxis `[script("python3")]` sin linea shebang.
5. Ejecuta `just health-check` y verifica que prueba una URL con curl.
6. Observa que cada script se ejecuta como un unico archivo (no linea por linea), lo que permite variables locales e imports.

## Solucion y Aprendizaje

- [Just Manual - Shebang Recipes](https://just.systems/man/en/chapter_42.html) — Documentacion detallada sobre como funcionan las recetas con shebang y sus diferencias con las recetas regulares.
- [Just Manual - script attribute](https://just.systems/man/en/chapter_32.html) — Referencia del atributo `[script]` para especificar el interprete de forma declarativa.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido por just que incluye ejemplos de scripts multi-lenguaje y sus casos de uso.

## Recursos

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Notas

_Espacio para tus notas personales._
