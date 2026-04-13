================================================================================
SCRIPT: fix_extra_end.exs
================================================================================

PROPÓSITO:
Automatizar la remoción de `end` extra en bloques de código Elixir dentro de
archivos Markdown en la carpeta 03-avanzado.

PATRÓN A ARREGLAR:
```elixir
defmodule Example do
  # ... código ...
end
end    ← ESTE EXTRA se elimina
```

UBICACIÓN:
/Users/consulting/Documents/consulting/infra/challenges/elixir/fix_extra_end.exs

================================================================================
CÓMO EJECUTAR:
================================================================================

1. Opción 1 - Ejecutar directamente (requiere Elixir instalado):
   $ cd /Users/consulting/Documents/consulting/infra/challenges/elixir
   $ elixir fix_extra_end.exs /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/

2. Opción 2 - Hacer ejecutable:
   $ chmod +x fix_extra_end.exs
   $ ./fix_extra_end.exs /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/

================================================================================
ALGORITMO IMPLEMENTADO:
================================================================================

1. Busca todos los archivos .md en 03-avanzado recursivamente
2. Para cada archivo .md:
   - Extrae bloques ```elixir ... ``` usando regex
   - Para cada bloque:
     a) Intenta compilar con Code.string_to_quoted/2
     b) Si falla con "unexpected reserved word: end":
        - Remueve líneas finales que sean solo "end" o espacios
        - Re-valida el código
        - Si es válido, reemplaza el bloque en el archivo
     c) Si hay otros errores, los reporta

3. Genera reporte final con:
   - Cantidad de archivos .md procesados
   - Cantidad de archivos modificados
   - Cantidad de bloques corregidos
   - Lista de archivos con errores pendientes

================================================================================
SALIDA ESPERADA:
================================================================================

[1/280] Procesando: advanced-testing/01-test-runner.md ✓ 3 bloques corregidos
[2/280] Procesando: advanced-testing/02-mocking.md ✓ 1 bloque corregido
...

================================================================================
REPORTE FINAL
================================================================================
Directorio procesado: /Users/consulting/.../03-avanzado/
Total archivos .md encontrados: 280
Archivos modificados: 145
Bloques corregidos: 507

✓ Proceso completado sin errores.
================================================================================

================================================================================
EN CASO DE ERRORES:
================================================================================

Si un bloque no puede ser corregido automáticamente:
- El script lo reporta como "error"
- Se lista el archivo y la línea problemática
- Requiere revisión manual

Ejemplo de salida de error:
  ⚠ Archivos con errores (requieren revisión manual):
  • advanced-testing/01-test-runner.md
    - Línea 5: unexpected token "when"

================================================================================
NOTAS TÉCNICAS:
================================================================================

- Usa Code.string_to_quoted/2 para validar (compilador Elixir nativo)
- Regex: ~r/```elixir\n(.*?)\n```/s (captura multilinea)
- Remueve solo líneas finales que sean exclusivamente "end" o espacios
- No modifica archivos sin bloques ```elixir
- Guarda archivos directamente si hay cambios
- Genera logs informativos durante ejecución

================================================================================
PRÓXIMOS PASOS DESPUÉS DE LA EJECUCIÓN:
================================================================================

1. Revisar archivos reportados con errores
2. Ejecutar validación adicional:
   $ elixir -e "Enum.each(Path.wildcard('/Users/.../03-avanzado/**/*.md'), fn file -> IO.inspect({file, File.read!(file)}) end)"
3. Si hay archivos con errores, revisar manualmente y corregir
4. Compilar todos los ejercicios con el compilador Elixir para asegurar validez

================================================================================
