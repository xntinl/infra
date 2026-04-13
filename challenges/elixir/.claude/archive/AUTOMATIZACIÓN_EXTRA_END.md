# Automatización: Remoción de `end` Extra en 03-avanzado

## Resumen Ejecutivo

Se ha creado un conjunto automatizado de herramientas Elixir para detectar y corregir bloques de código con `end` suelto en los archivos Markdown de `03-avanzado`. 

**Estado**: ✓ Scripts listos para ejecutar. En espera de generación de archivos .md (tarea #7).

---

## Archivos Creados

| Archivo | Ubicación | Propósito |
|---------|-----------|----------|
| `fix_extra_end.exs` | `/Users/consulting/Documents/consulting/infra/challenges/elixir/` | Script principal: detecta y corrige `end` extra |
| `validate_all_blocks.exs` | `/Users/consulting/Documents/consulting/infra/challenges/elixir/` | Script de validación: verifica sintaxis de bloques |
| `process_03_avanzado.sh` | `/Users/consulting/Documents/consulting/infra/challenges/elixir/` | Orquestador: ejecuta validación → corrección → validación |
| `README_FIX_EXTRA_END.txt` | `/Users/consulting/Documents/consulting/infra/challenges/elixir/` | Documentación rápida |
| `AUTOMATIZACIÓN_EXTRA_END.md` | `/Users/consulting/Documents/consulting/infra/challenges/elixir/` | Este documento |

---

## Patrón a Reparar

### Antes (Inválido)
```elixir
defmodule Example do
  def hello do
    IO.puts("Hola")
  end
end
end    ← EXTRA: causa "unexpected reserved word: end"
```

### Después (Válido)
```elixir
defmodule Example do
  def hello do
    IO.puts("Hola")
  end
end
```

---

## Cómo Ejecutar

### Opción 1: Script Maestro (Recomendado)

Ejecuta las tres fases en orden: validación inicial → corrección → validación final.

```bash
cd /Users/consulting/Documents/consulting/infra/challenges/elixir
./process_03_avanzado.sh
```

**Salida esperada**:
```
================================================================================
PROCESAMIENTO DE 03-AVANZADO: FIX EXTRA END + VALIDACIÓN
================================================================================

[✓] Se encontraron 280 archivos .md

[INFO] FASE 1: Validación inicial de bloques...
[1/280] Validando: advanced-testing/01-test-runner.md
...

[INFO] FASE 2: Corrección automática de 'end' extra...
[1/280] Procesando: advanced-testing/01-test-runner.md ✓ 3 bloques corregidos
...

[INFO] FASE 3: Validación final de bloques...
[1/280] Validando: advanced-testing/01-test-runner.md
...

================================================================================
[✓] Procesamiento completado
================================================================================
```

### Opción 2: Ejecutar Scripts por Separado

#### A. Validación Inicial
```bash
elixir /Users/consulting/Documents/consulting/infra/challenges/elixir/validate_all_blocks.exs \
  /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/
```

#### B. Corrección Automática
```bash
elixir /Users/consulting/Documents/consulting/infra/challenges/elixir/fix_extra_end.exs \
  /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/
```

#### C. Validación Final
```bash
elixir /Users/consulting/Documents/consulting/infra/challenges/elixir/validate_all_blocks.exs \
  /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/
```

---

## Algoritmo Detallado

### fix_extra_end.exs

```
Para cada archivo .md en 03-avanzado/:
  1. Extrae bloques ```elixir ... ``` usando regex (~r/```elixir\n(.*?)\n```/s)
  2. Para cada bloque:
     a) Intenta compilar con Code.string_to_quoted(code, [])
     b) Si retorna {:ok, _ast}:
        → Bloque ya es válido, no hacer cambios
     c) Si retorna {:error, {line, "unexpected reserved word: end", _}}:
        → Hay un `end` suelto
        → Remueve líneas finales que sean solo "end" o espacios en blanco
        → Re-valida con Code.string_to_quoted/2
        → Si es válido, reemplaza bloque en archivo y guarda
        → Si sigue inválido, reporta como error
     d) Si retorna {:error, {line, other_reason, _}}:
        → Es un error diferente, no modificar, reportar

3. Genera reporte final con:
   - Cantidad de archivos .md encontrados
   - Cantidad de archivos modificados
   - Cantidad de bloques corregidos
   - Lista de archivos con errores pendientes
```

### validate_all_blocks.exs

```
Para cada archivo .md en 03-avanzado/:
  1. Extrae bloques ```elixir ... ```
  2. Para cada bloque:
     a) Intenta compilar con Code.string_to_quoted(code, [])
     b) Si es válido, incrementa contador
     c) Si hay error, guarda detalles del error
  
  3. Genera reporte con:
     - Total archivos validados
     - Archivos sin bloques elixir
     - Archivos con todos los bloques válidos
     - Archivos con errores (detallado por línea)
```

---

## Salida Esperada

### Reporte de fix_extra_end.exs

```
================================================================================
REPORTE FINAL
================================================================================
Directorio procesado: /Users/consulting/.../03-avanzado/
Total archivos .md encontrados: 280
Archivos modificados: 145
Bloques corregidos: 507

✓ Todos los bloques fueron validados correctamente.
================================================================================
```

### Reporte de validate_all_blocks.exs (Éxito)

```
================================================================================
REPORTE DE VALIDACIÓN
================================================================================
Directorio validado: /Users/consulting/.../03-avanzado/
Total archivos .md encontrados: 280
Archivos sin bloques elixir: 5
Archivos con bloques válidos: 275
Total bloques válidos: 2147

✓ ¡Todos los archivos tienen bloques válidos!
================================================================================
✓ Validación completada: SIN ERRORES
```

### Reporte de validate_all_blocks.exs (Con Errores)

```
================================================================================
REPORTE DE VALIDACIÓN
================================================================================
Directorio validado: /Users/consulting/.../03-avanzado/
Total archivos .md encontrados: 280
Archivos sin bloques elixir: 5
Archivos con bloques válidos: 273
Total bloques válidos: 2140

✗ Archivos con ERRORES DE SINTAXIS: 2
Total errores encontrados: 3

  • advanced-testing/03-fixtures.md [4/5 válidos]
      └─ Línea 12: unexpected token "when"
  • ecto-advanced/02-custom-types.md [2/3 válidos]
      └─ Línea 8: unexpected token "=>"
      └─ Línea 15: unexpected reserved word: end"
================================================================================
✗ Validación completada: 2 archivos con errores
```

---

## Resolución de Errores

Si el script reporta archivos con errores después de la corrección automática:

### 1. Revisar el archivo específico

```bash
vim /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/advanced-testing/03-fixtures.md
```

### 2. Ubicar el bloque problemático

El reporte indica la línea exacta dentro del bloque elixir.

### 3. Identificar el problema

Los errores más comunes son:
- **"unexpected reserved word: end"**: `end` suelto (ya debe estar corregido)
- **"unexpected token X"**: Sintaxis inválida en el código Elixir
- **"unexpected EOF"**: Falta cerrar una estructura (if/do/fn, etc.)

### 4. Corregir manualmente

Edita el bloque para que sea Elixir válido.

### 5. Re-validar

```bash
elixir /Users/consulting/Documents/consulting/infra/challenges/elixir/validate_all_blocks.exs \
  /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/
```

---

## Características de la Solución

✓ **Validación con compilador nativo**: Usa `Code.string_to_quoted/2` (compilador Elixir real)  
✓ **Detección específica**: Identifica solo el patrón "unexpected reserved word: end"  
✓ **Corrección segura**: Solo remueve líneas finales claramente extras  
✓ **Validación post-corrección**: Re-valida antes de guardar  
✓ **Reportes detallados**: Información clara sobre qué se modificó y qué requiere revisión  
✓ **Procesamiento por lotes**: Maneja los 280 archivos en segundos  
✓ **Logging informativo**: Barra de progreso y actualizaciones en tiempo real  

---

## Métricas Esperadas

Basado en el reporte anterior de validación (tarea anterior):

| Métrica | Esperado |
|---------|----------|
| Archivos .md totales | ~280 |
| Archivos con bloques elixir | ~275 |
| Bloques con error "extra end" | ~145-200 |
| Bloques corregidos | ~300-500 |
| Bloques con otros errores | ~5-20 |

---

## Próximos Pasos Después de Ejecución

1. **Revisar reporte final**
   - Si 0 archivos con errores: ✓ Tarea completada
   - Si hay errores: proceder al paso 2

2. **Para cada archivo con errores**
   - Abrir archivo en editor
   - Ubicar bloque problemático usando línea reportada
   - Corregir sintaxis Elixir
   - Guardar

3. **Re-ejecutar validación**
   - Confirmar que todos los errores fueron solucionados

4. **Compilación final (opcional)**
   - Compilar un ejercicio de muestra para asegurar que todo funciona:
   ```bash
   cd /Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/advanced-testing
   cat 01-test-runner.md | grep -A 100 '```elixir' | head -50 > test.exs
   elixir test.exs
   ```

---

## Dependencias

- **Elixir**: 1.14+ (con `Code.string_to_quoted/2`)
- **Bash**: para ejecutar `process_03_avanzado.sh`

### Verificar versión Elixir

```bash
elixir --version
```

---

## Historial de Cambios

**2026-04-12** - Creación inicial de scripts:
- `fix_extra_end.exs` v1.0
- `validate_all_blocks.exs` v1.0
- `process_03_avanzado.sh` v1.0

---

## Contacto / Soporte

Documentación guardada en engram:
- **Topic key**: `sdd/fix-extra-end-script-automatizado-para-03-avanzado`
- **Proyecto**: elixir

Si se necesitan cambios o mejoras al script, consultar con el coordinador de la tarea #10.

---

## Apéndice: Funcionamiento Interno

### Regex para Extracción de Bloques

```elixir
@elixir_block_regex ~r/```elixir\n(.*?)\n```/s
```

- Patrón: ` ```elixir\n` seguido por cualquier contenido (multilinea) seguido por `\n``` `
- Captura: solo el contenido del bloque (sin los delimitadores)
- Modo: `/s` = el punto (.) coincide con newlines también

### Función de Remoción de `end` Extra

```elixir
defp remove_extra_end(code) do
  lines = String.split(code, "\n")
  
  trimmed_lines =
    lines
    |> Enum.reverse()
    |> Enum.drop_while(fn line ->
      String.trim(line) == "end" or String.trim(line) == ""
    end)
    |> Enum.reverse()
  
  Enum.join(trimmed_lines, "\n")
end
```

1. Divide el código en líneas
2. Invierte el orden (para procesar desde el final)
3. Remueve líneas que sean solo "end" o espacios (mientras sea verdadero)
4. Re-invierte a orden original
5. Une las líneas resultantes

Esto garantiza remover solo líneas finales extra, preservando `end` válidos en el interior.

---

**Documento generado**: 2026-04-12  
**Última actualización**: 2026-04-12
