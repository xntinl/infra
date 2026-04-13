# ARCH-VALIDATOR — Executive Summary

**Generated**: 2026-04-13T14:38:40.836894Z
**Total files validated**: 553
**Fully passing (20/20)**: 123 (22.2%)
**>= 95% pass**: 284 (51.4%)
**>= 80% pass**: 491 (88.8%)

## Per-directory

| Directory | Total | Fully passing | %  | Avg score |
|-----------|-------|---------------|----|-----------|
| 01-basico | 79 | 12 | 15.2% | 18.66/20 |
| 02-intermedio | 139 | 97 | 69.8% | 19.63/20 |
| 03-avanzado | 280 | 1 | 0.4% | 17.08/20 |
| 04-insane | 55 | 13 | 23.6% | 19.13/20 |

## Failure distribution (most common first)

| Item | Failures | % of files |
|------|----------|-----------|
| `10_has_test_file_subsection_with_exunit` | 225 | 40.7% |
| `05_has_project_structure_with_tree` | 191 | 34.5% |
| `09_has_lib_app_ex_subsection_with_moduledoc` | 153 | 27.7% |
| `08_has_mix_exs_subsection_with_defmodule` | 146 | 26.4% |
| `18_no_orphan_defp_deps` | 112 | 20.3% |
| `11_has_script_main_exs_subsection` | 73 | 13.2% |
| `14_no_wrong_dependencies_heading` | 62 | 11.2% |
| `15_no_wrong_tests_heading` | 30 | 5.4% |
| `13_no_solution_exs_copy_hint` | 23 | 4.2% |
| `19_moduledoc_is_specific` | 2 | 0.4% |
| `02_has_project_field` | 1 | 0.2% |
| `03_has_why_section` | 1 | 0.2% |
| `04_has_business_problem_section` | 1 | 0.2% |
| `06_has_design_decisions_section` | 1 | 0.2% |
| `07_has_implementation_section` | 1 | 0.2% |
| `12_has_key_concepts_or_equivalent` | 1 | 0.2% |

## Files below 80% pass (HOT-FIX candidates)

**62 files**

- **9/20** `01-basico/fundamentals/01-setup-and-mix/json_validator/README.md` — missing: 02_has_project_field, 03_has_why_section, 04_has_business_problem_section, 05_has_project_structure_with_tree, 06_has_design_decisions_section, 07_has_implementation_section (+5 more)
- **14/20** `03-avanzado/metaprogramming/140-ast-walker/140-ast-walker.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps, 19_moduledoc_is_specific
- **14/20** `03-avanzado/otp-advanced/384-gen-statem-state-functions-vs-handle-event/384-gen-statem-state-functions-vs-handle-event.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/otp-advanced/385-proc-lib-sys-handrolled-behaviour/385-proc-lib-sys-handrolled-behaviour.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/otp-advanced/386-hot-code-upgrades-release-handler/386-hot-code-upgrades-release-handler.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/otp-advanced/387-genserver-hibernation-idle-memory/387-genserver-hibernation-idle-memory.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/305-retry-exponential-backoff-jitter/305-retry-exponential-backoff-jitter.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/309-load-shedding-token-bucket/309-load-shedding-token-bucket.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/311-deadline-propagation-processes/311-deadline-propagation-processes.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/312-chaos-engineering-suspend-kill/312-chaos-engineering-suspend-kill.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/313-idempotency-keys-ets-ttl/313-idempotency-keys-ets-ttl.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/314-dead-letter-queue/314-dead-letter-queue.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/316-hedged-requests/316-hedged-requests.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **14/20** `03-avanzado/resilience-patterns/317-adaptive-concurrency-limits/317-adaptive-concurrency-limits.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/ets-dets-mnesia/116-ets-sharding/116-ets-sharding.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 11_has_script_main_exs_subsection, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/ets-dets-mnesia/19-cache-patterns-ets/19-cache-patterns-ets.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 11_has_script_main_exs_subsection, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/ets-dets-mnesia/20-ets-counter-and-atomics/20-ets-counter-and-atomics.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 11_has_script_main_exs_subsection, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/ets-dets-mnesia/374-ets-match-specs/374-ets-match-specs.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading
- **15/20** `03-avanzado/ets-dets-mnesia/375-ordered-set-range-queries/375-ordered-set-range-queries.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading
- **15/20** `03-avanzado/ets-dets-mnesia/376-dets-persistence/376-dets-persistence.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading
- **15/20** `03-avanzado/ets-dets-mnesia/377-mnesia-transactions-indices/377-mnesia-transactions-indices.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading
- **15/20** `03-avanzado/ets-dets-mnesia/378-mnesia-distributed-replicas/378-mnesia-distributed-replicas.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading
- **15/20** `03-avanzado/ets-dets-mnesia/379-ets-shard-pattern/379-ets-shard-pattern.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading
- **15/20** `03-avanzado/interop-and-native/31-ports-external-processes/31-ports-external-processes.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 11_has_script_main_exs_subsection, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/319-dirty-nif-cpu-bound/319-dirty-nif-cpu-bound.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/320-port-driver-streaming/320-port-driver-streaming.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/321-port-open-subprocess/321-port-open-subprocess.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/322-erlexec-resource-limits/322-erlexec-resource-limits.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/324-cnode-integration/324-cnode-integration.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/325-nif-resource-env/325-nif-resource-env.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/327-erlport-python-bridge/327-erlport-python-bridge.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/328-port-nodejs-json-framing/328-port-nodejs-json-framing.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/interop-and-native/329-embed-binary-resources/329-embed-binary-resources.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/131-unquote-fragment/131-unquote-fragment.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/132-compile-tracer/132-compile-tracer.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/134-defdelegate-custom/134-defdelegate-custom.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/137-protocol-derive/137-protocol-derive.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/138-fallback-any/138-fallback-any.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/139-optional-callbacks/139-optional-callbacks.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/141-macro-expand-debug/141-macro-expand-debug.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/142-dsl-schema-builder/142-dsl-schema-builder.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/143-dsl-router-builder/143-dsl-router-builder.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/144-spec-gen/144-spec-gen.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 10_has_test_file_subsection_with_exunit, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/metaprogramming/388-compile-time-config-validation-on-load/388-compile-time-config-validation-on-load.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/otp-advanced/02-genserver-handle-continue-recovery/02-genserver-handle-continue-recovery.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/otp-advanced/05-genserver-hot-state-migration/05-genserver-hot-state-migration.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/otp-advanced/33-gen-statem-state-machine/33-gen-statem-state-machine.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/otp-advanced/78-sys-suspend-resume/78-sys-suspend-resume.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 09_has_lib_app_ex_subsection_with_moduledoc, 11_has_script_main_exs_subsection, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/phoenix-ecosystem/51-phoenix-liveview-real-time/51-phoenix-liveview-real-time.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 10_has_test_file_subsection_with_exunit, 11_has_script_main_exs_subsection, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/resilience-patterns/304-bulkhead-process-pools/304-bulkhead-process-pools.md` — missing: 05_has_project_structure_with_tree, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/resilience-patterns/306-timeout-hierarchies/306-timeout-hierarchies.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/resilience-patterns/307-fallback-chains-with/307-fallback-chains-with.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/resilience-patterns/308-bounded-queue-genstage-backpressure/308-bounded-queue-genstage-backpressure.md` — missing: 05_has_project_structure_with_tree, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/resilience-patterns/310-graceful-degradation-feature-flags/310-graceful-degradation-feature-flags.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/resilience-patterns/315-saga-compensating-actions/315-saga-compensating-actions.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/06-supervision-strategies-advanced/06-supervision-strategies-advanced.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/07-partition-supervisor/07-partition-supervisor.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/08-task-supervisor-dynamic/08-task-supervisor-dynamic.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/09-supervision-tree-design/09-supervision-tree-design.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/10-graceful-shutdown-drain/10-graceful-shutdown-drain.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 11_has_script_main_exs_subsection, 14_no_wrong_dependencies_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/390-rest-for-one-custom-restart-intensity/390-rest-for-one-custom-restart-intensity.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
- **15/20** `03-avanzado/supervision-advanced/391-partition-supervisor-lock-contention/391-partition-supervisor-lock-contention.md` — missing: 05_has_project_structure_with_tree, 08_has_mix_exs_subsection_with_defmodule, 14_no_wrong_dependencies_heading, 15_no_wrong_tests_heading, 18_no_orphan_defp_deps
