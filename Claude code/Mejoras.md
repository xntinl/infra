
```
SDD — Spec-Driven Development

Pipeline estructurado para implementar cambios con trazabilidad completa.

---
Comandos
SDD — Comandos

Pipeline estructurado para implementar cambios con trazabilidad completa.
---
Exploración

/sdd-init — detecta el stack y configura el contexto base en
memoria
/sdd-explore <tema> — investiga el problema, compara enfoques,
recomienda uno

Planificación

/sdd-spec <cambio> — define qué debe hacer el sistema
/sdd-design <cambio> — define cómo implementarlo
/sdd-tasks <cambio> — divide el trabajo en tareas atómicas

Ejecución

/sdd-apply <cambio> — escribe el código siguiendo spec y design
/sdd-verify <cambio> — ejecuta tests y cruza cada escenario contra
resultados reales
/sdd-archive <cambio> — cierra el ciclo y guarda trazabilidad
completa

---
El ciclo
explore → spec → design → tasks → apply → verify → archive
---
El valor

- Cada artefacto depende del anterior → no se puede implementar sin diseño, no
se puede archivar con tests fallando
- Todo queda en memoria persistente (engram) → contexto disponible en sesiones
futuras
- El verify no acepta "el código existe" como evidencia — requiere tests que
pasen
```


```
Engram

Memoria persistente para Claude Code. Guarda decisiones, contexto y artefactos
entre sesiones.

---
Instalar

npx @engramhq/engram install

---
Configurar en Claude Code

Agregá el MCP server en ~/.claude/claude_desktop_config.json (o via claude mcp
add):

claude mcp add engram

---
Cómo funciona

Sesión 1 → Claude aprende contexto → guarda en engram
Sesión 2 → Claude carga contexto → continúa desde donde quedó

---
Qué persiste

- Decisiones de arquitectura
- Convenciones del proyecto
- Artefactos SDD (specs, designs, tasks)
- Preferencias de trabajo

---
Verificar que funciona

# En una sesión nueva:
buscá en memoria: sdd/hexagonal-endpoint

Si responde con contexto → funciona.

---

Sin engram: cada sesión empieza de cero.
Con engram: Claude recuerda todo.
```


```
GGA — Gentleman Guardian Angel

Code review automático con IA en cada git commit.

---
Qué hace

Antes de que el commit se grabe, GGA manda el diff a Claude y
verifica las reglas de tu proyecto.

git commit → GGA lee AGENTS.md → Claude revisa → PASS o FAIL 

Si falla → el commit se bloquea hasta que corrijas las violaciones.

---
Qué detecta

Lo que vos definís en AGENTS.md. Ejemplos reales de hoy:

- Lógica de negocio en el handler
- Errores ignorados silenciosamente
- Constructores que retornan tipos concretos en vez de interfaces
- Violaciones de la arquitectura hexagonal

---
Setup

gga init      # crea .gga con configuración
gga install   # instala el pre-commit hook
# + crear AGENTS.md con tus reglas

---
El valor
Las reglas de arquitectura se cumplen en cada commit sin depender de la memoria del equipo.de en el code review humano.
```