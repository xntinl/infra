
Memoria persistente para Claude Code. Guarda decisiones, contexto y artefactos entre sesiones.

## Instalar

```
npx @engramhq/engram install
```

## Configurar en Claude Code

Agregá el MCP server en `~/.claude/claude_desktop_config.json` (o via `claude mcp add`):

```
claude mcp add engram
```

## Cómo funciona

```
Sesión 1 → Claude aprende contexto → guarda en engram
Sesión 2 → Claude carga contexto → continúa desde donde quedó
```

## Qué persiste

- Decisiones de arquitectura
- Convenciones del proyecto
- Artefactos SDD (specs, designs, tasks)
- Preferencias de trabajo

## Verificar que funciona

```
# En una sesión nueva:
buscá en memoria: sdd/hexagonal-endpoint
```

Si responde con contexto → funciona.

Sin engram: cada sesión empieza de cero.
Con engram: Claude recuerda todo.