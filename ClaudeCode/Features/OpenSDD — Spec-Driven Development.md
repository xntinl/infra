# OpenSDD — Spec-Driven Development

> Metodología donde la especificación es el artefacto primario. El código es un output derivado, no el destino.

---

## SDD vs TDD vs BDD (en una línea cada uno)

| | Foco | Artefacto primario | Driver |
|---|---|---|---|
| **TDD** | Corrección de código | Test unitario | Ciclo red-green-refactor |
| **BDD** | Comportamiento del sistema | Escenario Given/When/Then | Lenguaje ubicuo con negocio |
| **SDD** | Intención del cambio | Spec + Design + Tasks | AI ejecuta desde contexto estructurado |

SDD no reemplaza TDD/BDD — los contiene. La spec usa Given/When/Then. Las tasks incluyen tests. SDD es la capa de orquestación por encima.

---

## Pipeline Core

```
explore → propose → spec → design → tasks → apply → verify → archive
```

Las fases tienen dependencias duras: no podés implementar sin design, no podés archivar con verify fallando.

## Artefactos — qué va en cada uno

**`proposal.md`** — El "por qué"
- Problema que resuelve, alcance del cambio, enfoque elegido
- Una página máxima. Si no entra, el cambio es demasiado grande

**`spec/`** — El "qué"
- Requisitos funcionales + escenarios Given/When/Then
- Separar happy path de edge cases (optimización de tokens para IA)
- Lenguaje de dominio, no de implementación — sin mencionar clases ni tablas

**`design.md`** — El "cómo"
- Decisiones de arquitectura con justificación explícita (ADRs inline)
- Interfaces/contratos entre capas, diagramas si aplica
- Restricciones técnicas que el agente debe respetar al generar código

**`tasks.md`** — El "en qué orden"
- Checklist atómico, cada tarea ejecutable por un agente en una sesión
- Dependencias entre tareas marcadas explícitamente
- Criterio de done por tarea (qué test o assertion la cierra)

## OpenSpec — el framework open source

- **Repo**: [github.com/Fission-AI/OpenSpec](https://github.com/Fission-AI/OpenSpec)
- **Site**: [openspec.pro](https://openspec.pro)
- Carpeta por cambio: `openspec/changes/{change-name}/` con proposal + specs + design + tasks
- Comandos clave:
  ```
  /opsx:new     — crea la carpeta del cambio
  /opsx:ff      — fast-forward: genera todos los artefactos de planificación
  /opsx:apply   — implementa las tasks siguiendo spec y design
  /opsx:archive — cierra el cambio y sincroniza specs a la spec principal
  ```
- Compatible con 20+ AI assistants (Claude Code, Copilot, Gemini CLI, Cursor, etc.)
- Sin dependencias Python — setup en 5 minutos vs 30 de spec-kit

---

## Integración con IA y agentes paralelos

- La spec + design actúan como **contexto estructurado** para el agente — reduce alucinaciones drásticamente
- Múltiples agentes pueden trabajar en tasks paralelas si no tienen dependencias entre sí
- El orchestrator mantiene estado (engram / openspec), los sub-agentes reciben contexto pre-resuelto
- Patrón probado: un agente por task, cada uno lee spec+design+su task, sin acceso al historial completo
- `AGENTS.md` o `CLAUDE.md` en el repo definen las reglas globales que todo agente hereda

---

## Cuándo usarlo

- Cambios con alcance definible upfront (features nuevas, migraciones, refactors estructurales)
- Equipos coordinando múltiples agentes IA en paralelo
- Proyectos donde la trazabilidad importa (auditoría, compliance, onboarding de devs nuevos)
- Cuando "vibe coding" ya generó deuda o inconsistencia arquitectural

## Cuándo NO usarlo

- Exploración pura donde los requisitos van a cambiar 3 veces antes de estabilizarse
- Hotfixes urgentes — el overhead no se justifica
- Prototipos desechables o spikes de investigación
- ThoughtWorks Radar lo advierte: riesgo de "heavy up-front specification" si se aplica a todo sin criterio

---

## Fuentes

- [OpenSpec — GitHub (Fission-AI)](https://github.com/Fission-AI/OpenSpec)
- [OpenSpec.pro](https://openspec.pro)
- [Thoughtworks: SDD unpacking 2025](https://www.thoughtworks.com/en-us/insights/blog/agile-engineering-practices/spec-driven-development-unpacking-2025-new-engineering-practices)
- [Martin Fowler: SDD tools — Kiro, spec-kit, Tessl](https://martinfowler.com/articles/exploring-gen-ai/sdd-3-tools.html)
- [GitHub Spec Kit blog post](https://github.blog/ai-and-ml/generative-ai/spec-driven-development-with-ai-get-started-with-a-new-open-source-toolkit/)
- [SDD vs TDD — DEV.to](https://dev.to/planu/sdd-vs-tdd-why-spec-driven-development-changes-the-game-for-ai-assisted-coding-5gba)
- [SDD comparison: BMAD vs spec-kit vs OpenSpec vs PromptX](https://redreamality.com/blog/-sddbmad-vs-spec-kit-vs-openspec-vs-promptx/)
