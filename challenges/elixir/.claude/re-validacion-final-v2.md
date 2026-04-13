# Re-Validación Final v2
**Fecha**: 2026-04-13
**Archivos analizados**: 552

## Resumen
- Gold (20/20): **286/552** (51.81%)
- >=95% (19+/20): 462
- >=90% (18+/20): 545
- >=80% (16+/20): 552
- Score promedio global: **96.71%**

## Comparación histórica
- Baseline inicial: 1/553 gold (0.2%)
- Post HOT-FIX MASTER: 123/553 gold (22.2%)
- **Ahora (v2)**: 286/552 gold (51.81%)

## Score por directorio

| Dir | Archivos | Gold | Promedio |
|---|---|---|---|
| 01-basico | 78 | 50 | 98.01% |
| 02-intermedio | 139 | 97 | 98.17% |
| 03-avanzado | 280 | 92 | 95.12% |
| 04-insane | 55 | 47 | 99.27% |

## Items que más fallan

| Item | Descripción | Fallos |
|---|---|---|
| 20 | @doc específico | 165 |
| 10 | ### test + ExUnit/async/doctest/describe | 110 |
| 9 | ### `lib/<app>.ex` + specific @moduledoc | 47 |
| 13 | NO 'Copy the code below...' | 23 |
| 8 | ### `mix.exs` + defmodule MixProject | 5 |
| 19 | @moduledoc específico (>30c) | 4 |
| 5 | ## Project structure (tree lib/script/test/mix.exs) | 4 |
| 18 | NO 'defp deps do' suelto | 3 |
| 11 | ### `script/main.exs` + Main.main() | 2 |

## Top 5 archivos problemáticos

- `02-intermedio/applications-and-releases/69-release-runtime-overlays/69-release-runtime-overlays.md` — 17/20 — falla items: [9, 19, 20]
- `02-intermedio/applications-and-releases/73-release-systemd-packaging/73-release-systemd-packaging.md` — 17/20 — falla items: [9, 19, 20]
- `02-intermedio/integrations-basics/129-ecto-migrations/129-ecto-migrations.md` — 17/20 — falla items: [9, 19, 20]
- `03-avanzado/interop-and-native/161-rustler-binary/161-rustler-binary.md` — 17/20 — falla items: [8, 11, 20]
- `03-avanzado/interop-and-native/162-rustler-dirty/162-rustler-dirty.md` — 17/20 — falla items: [8, 11, 20]
