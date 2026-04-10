
  Pipeline estructurado para implementar cambios con trazabilidad completa.
  ---

## Comandos

### Exploración

  ```
  /sdd-init — detecta el stack y configura el contexto base en memoria
  /sdd-explore <tema> — investiga el problema, compara enfoques, recomienda uno
  ```

### Planificación

  ```
  /sdd-spec <cambio> — define qué debe hacer el sistema
  /sdd-design <cambio> — define cómo implementarlo
  /sdd-tasks <cambio> — divide el trabajo en tareas atómicas
  ```

### Ejecución
  ```
  /sdd-apply <cambio> — escribe el código siguiendo spec y design
  /sdd-verify <cambio> — ejecuta tests y cruza cada escenario contra resultados reales
  /sdd-archive <cambio> — cierra el ciclo y guarda trazabilidad completa
  ```

## El ciclo

  ```
  explore → spec → design → tasks → apply → verify → archive
  ```

  ---

## El valor

- Cada artefacto depende del anterior → no se puede implementar sin diseño, no se puede archivar con tests fallando
- Todo queda en memoria persistente (engram) → contexto disponible en sesiones futuras
- El verify no acepta "el código existe" como evidencia — requiere tests que pasen