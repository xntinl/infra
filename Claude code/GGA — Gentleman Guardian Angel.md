Code review automático con IA en cada git commit.

## Qué hace

Antes de que el commit se grabe, GGA manda el diff a Claude y verifica las reglas de tu proyecto.

```
git commit → GGA lee AGENTS.md → Claude revisa → PASS o FAIL
```

Si falla → el commit se bloquea hasta que corrijas las violaciones.

## Qué detecta

Lo que vos definís en `AGENTS.md`. Ejemplos reales:

- Lógica de negocio en el handler
- Errores ignorados silenciosamente
- Constructores que retornan tipos concretos en vez de interfaces
- Violaciones de la arquitectura hexagonal

## Setup

```
gga init      # crea .gga con configuración
gga install   # instala el pre-commit hook
# + crear AGENTS.md con tus reglas
```

## El valor

Las reglas de arquitectura se cumplen en cada commit sin depender de la memoria del equipo ni del code
review humano.