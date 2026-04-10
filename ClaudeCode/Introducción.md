# Claude Code — Taller

## 1.- Modelos

- Familia actual: Opus 4.5 · Sonnet 4.5 · Haiku 4.5
- **Default en Claude Code: Sonnet** — mejor balance costo/capacidad
- Opus = máxima capacidad; para tareas complejas o razonamiento profundo
- Haiku = velocidad; para tareas simples, completions rápidas
- `/model` para cambiar modelo en sesión
- `/fast` activa output más veloz con el mismo modelo

## 2.- Instalación y CLAUDE.md

- `npm install -g @anthropic-ai/claude-code` → luego `claude` en terminal
- **CLAUDE.md = sistema prompt del proyecto**: instrucciones, convenciones, contexto permanente
- Contenido útil: arquitectura, convenciones de código, restricciones, comandos del proyecto

## 3.- Permisos
- Default: Claude pide permiso antes de ejecutar acciones destructivas

## 4.- MCP (figma, playwright, notion)

- **MCP = Model Context Protocol**: estándar para conectar Claude con herramientas externas
- Claude Code incluye MCPs built-in: filesystem, bash, editor
- MCPs externos se configuran en `~/.claude/settings.json` → clave `mcpServers`
- `claude mcp add <nombre>` para agregar un servidor
- **Figma MCP**: Claude lee y escribe diseños directamente desde código
- **Playwright MCP**: Claude navega browsers, llena formularios, toma screenshots
- **Notion MCP**: Claude lee y escribe docs de Notion como contexto
- Cada MCP expone tools que Claude usa igual que cualquier herramienta nativa

## 5.- Skills (Agents)

- **Skills = instrucciones prefabricadas** que se activan con `/nombre-skill`
- Definen comportamiento específico: cómo crear PRs, cómo testear, cómo diseñar
- Global: `~/.claude/skills/` · Por proyecto: `.agent/skills/`
- Cada skill es un archivo `SKILL.md` con el prompt completo del agente
- Claude Code puede lanzar **sub-agentes** vía el tool `Agent` para trabajo paralelo
- N agentes en paralelo = tareas independientes que corren simultáneamente
- Skills compuestos: un skill puede orquestar otros skills como fases


## 6.- Flujos y modos

- `/plan` — Claude presenta el plan antes de actuar; revisás antes de aprobar
- `/fast` — output más veloz; útil para ediciones simples
- `Shift+Enter` — nueva línea sin enviar el mensaje
- `!comando` — ejecuta shell en la sesión y pega el output en el contexto
- **Agentes paralelos**: N sub-agentes simultáneos para trabajo independiente
- **Engram** — memoria persistente entre sesiones; Claude recuerda decisiones y contexto
- **GGA** — code review automático con IA en cada `git commit` (pre-commit hook)
- **SDD** — pipeline estructurado: `explore → spec → design → tasks → apply → verify → archive`

## 7.- Demo

- Flujo real end-to-end: crear un feature simple con Claude Code en vivo
- Sugerencia: iniciar con `/sdd-init`, explorar con `/sdd-explore`, implementar con `/sdd-apply`
- Mostrar: edición de código, ejecución de tests, permisos en acción, un MCP activo
