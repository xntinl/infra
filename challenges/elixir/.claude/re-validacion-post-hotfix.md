# Re-Validación Post HOT-FIX MASTER

**Total archivos (01-04)**: 553
**20/20 pass**: 123 (22.2%)
**>=95% (19+/20)**: 284 (51.4%)
**>=90% (18+/20)**: 415 (75.0%)
**Score promedio global**: 18.15/20

## Comparativa
- ANTES: 1/553 (0.2%) archivo de oro
- AHORA: 123/553 con 20/20 (22.2%)
- Delta: +122 archivos pasaron a oro

## Por directorio

| Dir | Total | 20/20 | % | Avg |
|---|---|---|---|---|
| 01-basico | 79 | 12 | 15.2% | 18.66/20 |
| 02-intermedio | 139 | 97 | 69.8% | 19.63/20 |
| 03-avanzado | 280 | 1 | 0.4% | 17.08/20 |
| 04-insane | 55 | 13 | 23.6% | 19.13/20 |

## Fallos más comunes

| Item | # fallos | % |
|---|---|---|
| 10_has_test_file_subsection_with_exunit | 225 | 40.7% |
| 05_has_project_structure_with_tree | 191 | 34.5% |
| 09_has_lib_app_ex_subsection_with_moduledoc | 153 | 27.7% |
| 08_has_mix_exs_subsection_with_defmodule | 146 | 26.4% |
| 18_no_orphan_defp_deps | 112 | 20.3% |
| 11_has_script_main_exs_subsection | 73 | 13.2% |
| 14_no_wrong_dependencies_heading | 62 | 11.2% |
| 15_no_wrong_tests_heading | 30 | 5.4% |
| 13_no_solution_exs_copy_hint | 23 | 4.2% |
| 19_moduledoc_is_specific | 2 | 0.4% |

## Top 10 archivos más problemáticos

- **9/20** `01-basico/fundamentals/01-setup-and-mix/json_validator/README.md` — 02_has_project_field, 03_has_why_section, 04_has_business_problem_section, 05_has_project_structure_with_tree, 06_has_design_decisions_section (+6)
- **14/20** `03-avanzado/metaprogramming/140-ast-walker/140-ast-walker.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps (+1)
- **14/20** `03-avanzado/otp-advanced/384-gen-statem-state-functions-vs-handle-event/384-gen-statem-state-functions-vs-handle-event.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/otp-advanced/385-proc-lib-sys-handrolled-behaviour/385-proc-lib-sys-handrolled-behaviour.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/otp-advanced/386-hot-code-upgrades-release-handler/386-hot-code-upgrades-release-handler.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/otp-advanced/387-genserver-hibernation-idle-memory/387-genserver-hibernation-idle-memory.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/resilience-patterns/305-retry-exponential-backoff-jitter/305-retry-exponential-backoff-jitter.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/resilience-patterns/309-load-shedding-token-bucket/309-load-shedding-token-bucket.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/resilience-patterns/311-deadline-propagation-processes/311-deadline-propagation-processes.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
- **14/20** `03-avanzado/resilience-patterns/312-chaos-engineering-suspend-kill/312-chaos-engineering-suspend-kill.md` — 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading (+1)
