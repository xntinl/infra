# Go -- Challenges & Exercises

> 497 hands-on exercises for Go, organized across 47 sections from basic syntax to systems programming.
> Each exercise follows a progressive learning path using Bloom's Taxonomy and Dreyfus Model scaffolding.

**Requirements**: Go 1.22+ installed (`go version`), terminal, text editor or IDE with Go support.

**Convention**: Each exercise is a self-contained Go project. Run `go run .` to execute, `go test` to verify.

---

### 01 - Environment and Tooling

| # | Exercise | Difficulty |
|---|----------|------------|
| 1 | [Your First Go Program](01-environment-and-tooling/01-your-first-go-program/01-your-first-go-program.md) | Basic |
| 2 | [Go Modules and Dependencies](01-environment-and-tooling/02-go-modules-and-dependencies/02-go-modules-and-dependencies.md) | Basic |
| 3 | [Go Workspace and Project Layout](01-environment-and-tooling/03-go-workspace-and-project-layout/03-go-workspace-and-project-layout.md) | Basic |
| 4 | [Go Tool Commands: build, run, vet, fmt](01-environment-and-tooling/04-go-tool-commands/04-go-tool-commands.md) | Basic |
| 5 | [Go Install and Third-Party Packages](01-environment-and-tooling/05-go-install-and-third-party-packages/05-go-install-and-third-party-packages.md) | Basic |
| 6 | [Linting with golangci-lint](01-environment-and-tooling/06-linting-with-golangci-lint/06-linting-with-golangci-lint.md) | Basic |
| 7 | [Debugging with Delve](01-environment-and-tooling/07-debugging-with-delve/07-debugging-with-delve.md) | Basic |
| 8 | [Cross-Compilation and Build Tags](01-environment-and-tooling/08-cross-compilation-and-build-tags/08-cross-compilation-and-build-tags.md) | Intermediate |

### 02 - Variables, Types, and Constants

| # | Exercise | Difficulty |
|---|----------|------------|
| 9 | [Variable Declaration and Short Assignment](02-variables-types-and-constants/01-variable-declaration-and-short-assignment/01-variable-declaration-and-short-assignment.md) | Basic |
| 10 | [Zero Values and Default Initialization](02-variables-types-and-constants/02-zero-values-and-default-initialization/02-zero-values-and-default-initialization.md) | Basic |
| 11 | [Basic Types: int, float, bool, string](02-variables-types-and-constants/03-basic-types/03-basic-types.md) | Basic |
| 12 | [Constants and iota](02-variables-types-and-constants/04-constants-and-iota/04-constants-and-iota.md) | Basic |
| 13 | [Type Conversions and Type Assertions](02-variables-types-and-constants/05-type-conversions-and-type-assertions/05-type-conversions-and-type-assertions.md) | Basic |
| 14 | [Type Aliases vs Type Definitions](02-variables-types-and-constants/06-type-aliases-vs-type-definitions/06-type-aliases-vs-type-definitions.md) | Basic |
| 15 | [Numeric Precision and Overflow](02-variables-types-and-constants/07-numeric-precision-and-overflow/07-numeric-precision-and-overflow.md) | Intermediate |
| 16 | [Untyped Constants and Constant Expressions](02-variables-types-and-constants/08-untyped-constants-and-constant-expressions/08-untyped-constants-and-constant-expressions.md) | Intermediate |
| 17 | [Blank Identifier and Shadowing](02-variables-types-and-constants/09-blank-identifier-and-shadowing/09-blank-identifier-and-shadowing.md) | Basic |
| 18 | [Type Inference Deep Dive](02-variables-types-and-constants/10-type-inference-deep-dive/10-type-inference-deep-dive.md) | Intermediate |

### 03 - Control Flow

| # | Exercise | Difficulty |
|---|----------|------------|
| 19 | [If, Else, and Init Statements](03-control-flow/01-if-else-and-init-statements/01-if-else-and-init-statements.md) | Basic |
| 20 | [For Loops: Classic, While, Infinite](03-control-flow/02-for-loops/02-for-loops.md) | Basic |
| 21 | [Switch Statements and Expressionless Switch](03-control-flow/03-switch-statements/03-switch-statements.md) | Basic |
| 22 | [Type Switch](03-control-flow/04-type-switch/04-type-switch.md) | Basic |
| 23 | [Range Over Collections](03-control-flow/05-range-over-collections/05-range-over-collections.md) | Basic |
| 24 | [Labels, Break, Continue, and Goto](03-control-flow/06-labels-break-continue-goto/06-labels-break-continue-goto.md) | Intermediate |
| 25 | [Defer: Semantics and Ordering](03-control-flow/07-defer-semantics-and-ordering/07-defer-semantics-and-ordering.md) | Intermediate |
| 26 | [Panic and Recover](03-control-flow/08-panic-and-recover/08-panic-and-recover.md) | Intermediate |
| 27 | [Range Over Integers and Functions (Go 1.22+)](03-control-flow/09-range-over-integers-and-functions/09-range-over-integers-and-functions.md) | Intermediate |
| 28 | [Control Flow Debugging Challenge](03-control-flow/10-control-flow-debugging-challenge/10-control-flow-debugging-challenge.md) | Advanced |

### 04 - Functions

| # | Exercise | Difficulty |
|---|----------|------------|
| 29 | [Function Declaration and Multiple Return Values](04-functions/01-function-declaration-and-multiple-return-values/01-function-declaration-and-multiple-return-values.md) | Basic |
| 30 | [Named Return Values](04-functions/02-named-return-values/02-named-return-values.md) | Basic |
| 31 | [Variadic Functions](04-functions/03-variadic-functions/03-variadic-functions.md) | Basic |
| 32 | [First-Class Functions and Closures](04-functions/04-first-class-functions-and-closures/04-first-class-functions-and-closures.md) | Basic |
| 33 | [Anonymous Functions and Immediately Invoked](04-functions/05-anonymous-functions/05-anonymous-functions.md) | Basic |
| 34 | [Function Types and Callbacks](04-functions/06-function-types-and-callbacks/06-function-types-and-callbacks.md) | Intermediate |
| 35 | [Recursive Functions and Stack Depth](04-functions/07-recursive-functions-and-stack-depth/07-recursive-functions-and-stack-depth.md) | Intermediate |
| 36 | [Init Functions and Package Initialization](04-functions/08-init-functions-and-package-initialization/08-init-functions-and-package-initialization.md) | Intermediate |
| 37 | [Closure Gotchas: Loop Variable Capture](04-functions/09-closure-gotchas-loop-variable-capture/09-closure-gotchas-loop-variable-capture.md) | Intermediate |
| 38 | [Higher-Order Functions: Map, Filter, Reduce](04-functions/10-higher-order-functions/10-higher-order-functions.md) | Intermediate |
| 39 | [Defer Stacking and Resource Cleanup Patterns](04-functions/11-defer-stacking-and-resource-cleanup/11-defer-stacking-and-resource-cleanup.md) | Intermediate |
| 40 | [Functional Options Pattern](04-functions/12-functional-options-pattern/12-functional-options-pattern.md) | Advanced |

### 05 - Strings, Runes, and Unicode

| # | Exercise | Difficulty |
|---|----------|------------|
| 41 | [String Basics: Immutability and UTF-8](05-strings-runes-and-unicode/01-string-basics/01-string-basics.md) | Basic |
| 42 | [Byte Slices vs Strings](05-strings-runes-and-unicode/02-byte-slices-vs-strings/02-byte-slices-vs-strings.md) | Basic |
| 43 | [Runes and Unicode Code Points](05-strings-runes-and-unicode/03-runes-and-unicode-code-points/03-runes-and-unicode-code-points.md) | Basic |
| 44 | [String Iteration: Bytes vs Runes](05-strings-runes-and-unicode/04-string-iteration-bytes-vs-runes/04-string-iteration-bytes-vs-runes.md) | Basic |
| 45 | [Strings Package: Builder, Split, Join, Replace](05-strings-runes-and-unicode/05-strings-package/05-strings-package.md) | Intermediate |
| 46 | [String Formatting with fmt](05-strings-runes-and-unicode/06-string-formatting-with-fmt/06-string-formatting-with-fmt.md) | Intermediate |
| 47 | [Regular Expressions with regexp](05-strings-runes-and-unicode/07-regular-expressions/07-regular-expressions.md) | Intermediate |
| 48 | [Unicode Normalization and Collation](05-strings-runes-and-unicode/08-unicode-normalization-and-collation/08-unicode-normalization-and-collation.md) | Advanced |
| 49 | [strings.Builder Performance vs Concatenation](05-strings-runes-and-unicode/09-strings-builder-performance/09-strings-builder-performance.md) | Advanced |
| 50 | [Building a Text Processing Pipeline](05-strings-runes-and-unicode/10-building-a-text-processing-pipeline/10-building-a-text-processing-pipeline.md) | Advanced |

### 06 - Collections: Arrays, Slices, and Maps

| # | Exercise | Difficulty |
|---|----------|------------|
| 51 | [Arrays: Fixed Size and Value Semantics](06-collections-arrays-slices-and-maps/01-arrays/01-arrays.md) | Basic |
| 52 | [Slices: Creation, Append, and Capacity](06-collections-arrays-slices-and-maps/02-slices-creation-append-capacity/02-slices-creation-append-capacity.md) | Basic |
| 53 | [Slice Expressions and Sub-Slicing](06-collections-arrays-slices-and-maps/03-slice-expressions-and-sub-slicing/03-slice-expressions-and-sub-slicing.md) | Basic |
| 54 | [Maps: Creation, Access, and Iteration](06-collections-arrays-slices-and-maps/04-maps-creation-access-iteration/04-maps-creation-access-iteration.md) | Basic |
| 55 | [Nil Slices vs Empty Slices vs Nil Maps](06-collections-arrays-slices-and-maps/05-nil-slices-vs-empty-slices/05-nil-slices-vs-empty-slices.md) | Basic |
| 56 | [Copy and the Full Slice Expression](06-collections-arrays-slices-and-maps/06-copy-and-full-slice-expression/06-copy-and-full-slice-expression.md) | Intermediate |
| 57 | [Slice Internals: Header, Length, Capacity](06-collections-arrays-slices-and-maps/07-slice-internals/07-slice-internals.md) | Intermediate |
| 58 | [Map Internals and Iteration Order](06-collections-arrays-slices-and-maps/08-map-internals-and-iteration-order/08-map-internals-and-iteration-order.md) | Intermediate |
| 59 | [Slices Package (Go 1.21+)](06-collections-arrays-slices-and-maps/09-slices-package/09-slices-package.md) | Intermediate |
| 60 | [Maps Package (Go 1.21+)](06-collections-arrays-slices-and-maps/10-maps-package/10-maps-package.md) | Intermediate |
| 61 | [Slice Memory Leaks and Gotchas](06-collections-arrays-slices-and-maps/11-slice-memory-leaks-and-gotchas/11-slice-memory-leaks-and-gotchas.md) | Advanced |
| 62 | [Sorted Collections and Binary Search](06-collections-arrays-slices-and-maps/12-sorted-collections-and-binary-search/12-sorted-collections-and-binary-search.md) | Advanced |
| 63 | [Implementing a Ring Buffer](06-collections-arrays-slices-and-maps/13-implementing-a-ring-buffer/13-implementing-a-ring-buffer.md) | Advanced |
| 64 | [Custom Map-Based Data Structure](06-collections-arrays-slices-and-maps/14-custom-map-based-data-structure/14-custom-map-based-data-structure.md) | Insane |

### 07 - Structs and Methods

| # | Exercise | Difficulty |
|---|----------|------------|
| 65 | [Struct Declaration and Initialization](07-structs-and-methods/01-struct-declaration-and-initialization/01-struct-declaration-and-initialization.md) | Basic |
| 66 | [Struct Tags and JSON Encoding](07-structs-and-methods/02-struct-tags-and-json-encoding/02-struct-tags-and-json-encoding.md) | Basic |
| 67 | [Methods: Value Receivers vs Pointer Receivers](07-structs-and-methods/03-methods-value-vs-pointer-receivers/03-methods-value-vs-pointer-receivers.md) | Basic |
| 68 | [Anonymous Structs and Struct Embedding](07-structs-and-methods/04-anonymous-structs-and-embedding/04-anonymous-structs-and-embedding.md) | Basic |
| 69 | [Struct Comparison and Equality](07-structs-and-methods/05-struct-comparison-and-equality/05-struct-comparison-and-equality.md) | Intermediate |
| 70 | [Constructor Functions and Validation](07-structs-and-methods/06-constructor-functions-and-validation/06-constructor-functions-and-validation.md) | Intermediate |
| 71 | [Method Sets and Addressability](07-structs-and-methods/07-method-sets-and-addressability/07-method-sets-and-addressability.md) | Intermediate |
| 72 | [Embedding for Composition Over Inheritance](07-structs-and-methods/08-embedding-for-composition/08-embedding-for-composition.md) | Intermediate |
| 73 | [Struct Memory Layout and Padding](07-structs-and-methods/09-struct-memory-layout-and-padding/09-struct-memory-layout-and-padding.md) | Advanced |
| 74 | [Implementing Stringer and Standard Interfaces](07-structs-and-methods/10-implementing-stringer/10-implementing-stringer.md) | Intermediate |
| 75 | [Builder Pattern for Complex Structs](07-structs-and-methods/11-builder-pattern-for-complex-structs/11-builder-pattern-for-complex-structs.md) | Advanced |
| 76 | [Designing a Domain Model with Structs](07-structs-and-methods/12-designing-a-domain-model/12-designing-a-domain-model.md) | Advanced |

### 08 - Interfaces

| # | Exercise | Difficulty |
|---|----------|------------|
| 77 | [Implicit Interface Satisfaction](08-interfaces/01-implicit-interface-satisfaction/01-implicit-interface-satisfaction.md) | Basic |
| 78 | [The Empty Interface and any](08-interfaces/02-empty-interface-and-any/02-empty-interface-and-any.md) | Basic |
| 79 | [Type Assertions and Type Switches](08-interfaces/03-type-assertions-and-type-switches/03-type-assertions-and-type-switches.md) | Basic |
| 80 | [Common Standard Library Interfaces: io.Reader, io.Writer](08-interfaces/04-common-standard-library-interfaces/04-common-standard-library-interfaces.md) | Basic |
| 81 | [Interface Composition and Embedding](08-interfaces/05-interface-composition-and-embedding/05-interface-composition-and-embedding.md) | Intermediate |
| 82 | [Interface Segregation: Small Interfaces](08-interfaces/06-interface-segregation/06-interface-segregation.md) | Intermediate |
| 83 | [Nil Interface Values vs Nil Concrete Values](08-interfaces/07-nil-interface-values/07-nil-interface-values.md) | Intermediate |
| 84 | [Accept Interfaces, Return Structs](08-interfaces/08-accept-interfaces-return-structs/08-accept-interfaces-return-structs.md) | Intermediate |
| 85 | [Interface Internals: iface and eface](08-interfaces/09-interface-internals/09-interface-internals.md) | Advanced |
| 86 | [Dependency Injection with Interfaces](08-interfaces/10-dependency-injection-with-interfaces/10-dependency-injection-with-interfaces.md) | Advanced |
| 87 | [Mock Interfaces for Testing](08-interfaces/11-mock-interfaces-for-testing/11-mock-interfaces-for-testing.md) | Advanced |
| 88 | [Interface Pollution Anti-Patterns](08-interfaces/12-interface-pollution-anti-patterns/12-interface-pollution-anti-patterns.md) | Advanced |
| 89 | [Designing a Plugin System with Interfaces](08-interfaces/13-designing-a-plugin-system/13-designing-a-plugin-system.md) | Insane |
| 90 | [Interface-Based Middleware Chain](08-interfaces/14-interface-based-middleware-chain/14-interface-based-middleware-chain.md) | Insane |

### 09 - Pointers

| # | Exercise | Difficulty |
|---|----------|------------|
| 91 | [Pointer Basics: Address and Dereference](09-pointers/01-pointer-basics/01-pointer-basics.md) | Basic |
| 92 | [Pointers and Function Parameters](09-pointers/02-pointers-and-function-parameters/02-pointers-and-function-parameters.md) | Basic |
| 93 | [new() vs &T{} Allocation](09-pointers/03-new-vs-address-allocation/03-new-vs-address-allocation.md) | Basic |
| 94 | [Nil Pointers and Guard Checks](09-pointers/04-nil-pointers-and-guard-checks/04-nil-pointers-and-guard-checks.md) | Basic |
| 95 | [Pointers to Structs and Automatic Dereferencing](09-pointers/05-pointers-to-structs/05-pointers-to-structs.md) | Intermediate |
| 96 | [Pointer Receivers and Interface Satisfaction](09-pointers/06-pointer-receivers-and-interface-satisfaction/06-pointer-receivers-and-interface-satisfaction.md) | Intermediate |
| 97 | [Escape Analysis: Stack vs Heap](09-pointers/07-escape-analysis/07-escape-analysis.md) | Advanced |
| 98 | [Pointers in Slices and Maps](09-pointers/08-pointers-in-slices-and-maps/08-pointers-in-slices-and-maps.md) | Intermediate |
| 99 | [Pointer Aliasing and Data Races](09-pointers/09-pointer-aliasing-and-data-races/09-pointer-aliasing-and-data-races.md) | Advanced |
| 100 | [Designing Pointer-Safe APIs](09-pointers/10-designing-pointer-safe-apis/10-designing-pointer-safe-apis.md) | Advanced |

### 10 - Error Handling

| # | Exercise | Difficulty |
|---|----------|------------|
| 101 | [The error Interface and Basic Patterns](10-error-handling/01-error-interface-and-basic-patterns/01-error-interface-and-basic-patterns.md) | Basic |
| 102 | [fmt.Errorf and Error Wrapping with %w](10-error-handling/02-fmt-errorf-and-error-wrapping/02-fmt-errorf-and-error-wrapping.md) | Basic |
| 103 | [errors.Is and errors.As](10-error-handling/03-errors-is-and-errors-as/03-errors-is-and-errors-as.md) | Basic |
| 104 | [Custom Error Types](10-error-handling/04-custom-error-types/04-custom-error-types.md) | Basic |
| 105 | [Sentinel Errors and When to Use Them](10-error-handling/05-sentinel-errors/05-sentinel-errors.md) | Intermediate |
| 106 | [Error Wrapping Chains and Unwrap](10-error-handling/06-error-wrapping-chains/06-error-wrapping-chains.md) | Intermediate |
| 107 | [Multiple Error Returns: errors.Join (Go 1.20+)](10-error-handling/07-multiple-error-returns/07-multiple-error-returns.md) | Intermediate |
| 108 | [Panic vs Error: When to Use Each](10-error-handling/08-panic-vs-error/08-panic-vs-error.md) | Intermediate |
| 109 | [Error Handling in Goroutines](10-error-handling/09-error-handling-in-goroutines/09-error-handling-in-goroutines.md) | Advanced |
| 110 | [Error Handling Middleware for HTTP](10-error-handling/10-error-handling-middleware/10-error-handling-middleware.md) | Advanced |
| 111 | [Structured Error Types for APIs](10-error-handling/11-structured-error-types/11-structured-error-types.md) | Advanced |
| 112 | [Retry Patterns with Backoff](10-error-handling/12-retry-patterns-with-backoff/12-retry-patterns-with-backoff.md) | Advanced |
| 113 | [Designing an Error Hierarchy for a Library](10-error-handling/13-designing-an-error-hierarchy/13-designing-an-error-hierarchy.md) | Insane |
| 114 | [Error Observability: Logging, Metrics, Tracing](10-error-handling/14-error-observability/14-error-observability.md) | Insane |

### 11 - Packages and Modules

| # | Exercise | Difficulty |
|---|----------|------------|
| 115 | [Package Declaration and Imports](11-packages-and-modules/01-package-declaration-and-imports/01-package-declaration-and-imports.md) | Basic |
| 116 | [Exported vs Unexported Identifiers](11-packages-and-modules/02-exported-vs-unexported/02-exported-vs-unexported.md) | Basic |
| 117 | [Internal Packages](11-packages-and-modules/03-internal-packages/03-internal-packages.md) | Intermediate |
| 118 | [Go Module Versioning and go.sum](11-packages-and-modules/04-go-module-versioning/04-go-module-versioning.md) | Intermediate |
| 119 | [Multi-Module Workspaces (go.work)](11-packages-and-modules/05-multi-module-workspaces/05-multi-module-workspaces.md) | Intermediate |
| 120 | [Dependency Management: Upgrade, Downgrade, Replace](11-packages-and-modules/06-dependency-management/06-dependency-management.md) | Intermediate |
| 121 | [Module Proxies and GOPROXY](11-packages-and-modules/07-module-proxies-and-goproxy/07-module-proxies-and-goproxy.md) | Advanced |
| 122 | [Vendor Directory and Reproducible Builds](11-packages-and-modules/08-vendor-directory/08-vendor-directory.md) | Advanced |
| 123 | [Designing a Public Go Module](11-packages-and-modules/09-designing-a-public-go-module/09-designing-a-public-go-module.md) | Advanced |
| 124 | [Monorepo Module Strategy](11-packages-and-modules/10-monorepo-module-strategy/10-monorepo-module-strategy.md) | Insane |

### 12 - Testing Ecosystem

| # | Exercise | Difficulty |
|---|----------|------------|
| 125 | [Your First Test with testing.T](12-testing-ecosystem/01-your-first-test/01-your-first-test.md) | Basic |
| 126 | [Table-Driven Tests](12-testing-ecosystem/02-table-driven-tests/02-table-driven-tests.md) | Basic |
| 127 | [Test Helpers and t.Helper()](12-testing-ecosystem/03-test-helpers/03-test-helpers.md) | Intermediate |
| 128 | [Subtests and t.Run()](12-testing-ecosystem/04-subtests-and-t-run/04-subtests-and-t-run.md) | Intermediate |
| 129 | [Benchmarks with testing.B](12-testing-ecosystem/05-benchmarks/05-benchmarks.md) | Intermediate |
| 130 | [Fuzz Testing with testing.F](12-testing-ecosystem/06-fuzz-testing/06-fuzz-testing.md) | Intermediate |
| 131 | [Test Fixtures and testdata Directory](12-testing-ecosystem/07-test-fixtures-and-testdata/07-test-fixtures-and-testdata.md) | Intermediate |
| 132 | [Mocking with Interfaces](12-testing-ecosystem/08-mocking-with-interfaces/08-mocking-with-interfaces.md) | Intermediate |
| 133 | [httptest: Testing HTTP Handlers](12-testing-ecosystem/09-httptest/09-httptest.md) | Intermediate |
| 134 | [Testing Readers with iotest](12-testing-ecosystem/10-testing-readers-with-iotest/10-testing-readers-with-iotest.md) | Intermediate |
| 135 | [Testing Filesystems with fstest.MapFS](12-testing-ecosystem/11-testing-filesystems-with-fstest/11-testing-filesystems-with-fstest.md) | Intermediate |
| 136 | [t.Cleanup Patterns for Resource Teardown](12-testing-ecosystem/12-t-cleanup-patterns/12-t-cleanup-patterns.md) | Intermediate |
| 137 | [Build Tags for Test Separation (unit vs integration)](12-testing-ecosystem/13-build-tags-for-test-separation/13-build-tags-for-test-separation.md) | Intermediate |
| 138 | [Parallel Tests with t.Parallel](12-testing-ecosystem/14-parallel-tests/14-parallel-tests.md) | Intermediate |
| 139 | [Testable Examples (Example Functions)](12-testing-ecosystem/15-testable-examples/15-testable-examples.md) | Intermediate |
| 140 | [Testing Time-Dependent Code with Fake Clocks](12-testing-ecosystem/16-testing-time-dependent-code/16-testing-time-dependent-code.md) | Intermediate |
| 141 | [Testing with Environment Variables](12-testing-ecosystem/17-testing-with-environment-variables/17-testing-with-environment-variables.md) | Intermediate |
| 142 | [Integration Tests with Build Tags](12-testing-ecosystem/18-integration-tests-with-build-tags/18-integration-tests-with-build-tags.md) | Advanced |
| 143 | [Golden File Testing](12-testing-ecosystem/19-golden-file-testing/19-golden-file-testing.md) | Advanced |
| 144 | [Test Coverage Analysis and Improvement](12-testing-ecosystem/20-test-coverage-analysis/20-test-coverage-analysis.md) | Advanced |
| 145 | [Race Detector and Concurrent Test Safety](12-testing-ecosystem/21-race-detector/21-race-detector.md) | Advanced |
| 146 | [TestMain and Test Setup/Teardown](12-testing-ecosystem/22-testmain-setup-teardown/22-testmain-setup-teardown.md) | Advanced |
| 147 | [Snapshot/Approval Testing](12-testing-ecosystem/23-snapshot-approval-testing/23-snapshot-approval-testing.md) | Advanced |
| 148 | [Property-Based Testing with rapid](12-testing-ecosystem/24-property-based-testing/24-property-based-testing.md) | Insane |
| 149 | [Building a Test Suite for a Production Service](12-testing-ecosystem/25-building-a-test-suite/25-building-a-test-suite.md) | Insane |

### 13 - Goroutines and Channels

| # | Exercise | Difficulty |
|---|----------|------------|
| 150 | [Your First Goroutine](13-goroutines-and-channels/01-your-first-goroutine/01-your-first-goroutine.md) | Basic |
| 151 | [Channel Basics: Send and Receive](13-goroutines-and-channels/02-channel-basics/02-channel-basics.md) | Basic |
| 152 | [Buffered vs Unbuffered Channels](13-goroutines-and-channels/03-buffered-vs-unbuffered-channels/03-buffered-vs-unbuffered-channels.md) | Basic |
| 153 | [Channel Direction: Send-Only and Receive-Only](13-goroutines-and-channels/04-channel-direction/04-channel-direction.md) | Basic |
| 154 | [WaitGroup for Goroutine Synchronization](13-goroutines-and-channels/05-waitgroup/05-waitgroup.md) | Basic |
| 155 | [Ranging Over Channels](13-goroutines-and-channels/06-ranging-over-channels/06-ranging-over-channels.md) | Intermediate |
| 156 | [Done Channel Pattern](13-goroutines-and-channels/07-done-channel-pattern/07-done-channel-pattern.md) | Intermediate |
| 157 | [Goroutine Leak Detection](13-goroutines-and-channels/08-goroutine-leak-detection/08-goroutine-leak-detection.md) | Intermediate |
| 158 | [Channel of Channels](13-goroutines-and-channels/09-channel-of-channels/09-channel-of-channels.md) | Intermediate |
| 159 | [Signaling with Closed Channels](13-goroutines-and-channels/10-signaling-with-closed-channels/10-signaling-with-closed-channels.md) | Intermediate |
| 160 | [Goroutine Lifecycle Management](13-goroutines-and-channels/11-goroutine-lifecycle-management/11-goroutine-lifecycle-management.md) | Advanced |
| 161 | [Channel Patterns: Semaphore, Barrier, Latch](13-goroutines-and-channels/12-channel-patterns-semaphore-barrier/12-channel-patterns-semaphore-barrier.md) | Advanced |
| 162 | [Goroutine Pools: Fixed Size and Dynamic](13-goroutines-and-channels/13-goroutine-pools/13-goroutine-pools.md) | Advanced |
| 163 | [Deadlock Detection and Prevention](13-goroutines-and-channels/14-deadlock-detection-and-prevention/14-deadlock-detection-and-prevention.md) | Advanced |
| 164 | [Building a Concurrent Task Scheduler](13-goroutines-and-channels/15-building-a-concurrent-task-scheduler/15-building-a-concurrent-task-scheduler.md) | Insane |
| 165 | [Goroutine Debugging Under Load](13-goroutines-and-channels/16-goroutine-debugging-under-load/16-goroutine-debugging-under-load.md) | Insane |

### 14 - Select and Context

| # | Exercise | Difficulty |
|---|----------|------------|
| 166 | [Select Statement Basics](14-select-and-context/01-select-statement-basics/01-select-statement-basics.md) | Basic |
| 167 | [Select with Default: Non-Blocking Operations](14-select-and-context/02-select-with-default/02-select-with-default.md) | Basic |
| 168 | [Timeout with select and time.After](14-select-and-context/03-timeout-with-select/03-timeout-with-select.md) | Intermediate |
| 169 | [Context.WithCancel for Cancellation](14-select-and-context/04-context-withcancel/04-context-withcancel.md) | Intermediate |
| 170 | [Context.WithTimeout and Context.WithDeadline](14-select-and-context/05-context-withtimeout-and-withdeadline/05-context-withtimeout-and-withdeadline.md) | Intermediate |
| 171 | [Context.WithValue and Request-Scoped Data](14-select-and-context/06-context-withvalue/06-context-withvalue.md) | Intermediate |
| 172 | [Context Propagation Through Call Stacks](14-select-and-context/07-context-propagation/07-context-propagation.md) | Intermediate |
| 173 | [Select Priority and Starvation](14-select-and-context/08-select-priority-and-starvation/08-select-priority-and-starvation.md) | Advanced |
| 174 | [Context in HTTP Servers and Clients](14-select-and-context/09-context-in-http/09-context-in-http.md) | Advanced |
| 175 | [Context-Aware Database Queries](14-select-and-context/10-context-aware-database-queries/10-context-aware-database-queries.md) | Advanced |
| 176 | [Graceful Shutdown with Context](14-select-and-context/11-graceful-shutdown-with-context/11-graceful-shutdown-with-context.md) | Advanced |
| 177 | [Multi-Stage Pipeline Cancellation](14-select-and-context/12-multi-stage-pipeline-cancellation/12-multi-stage-pipeline-cancellation.md) | Advanced |
| 178 | [Context Leak Detection and Prevention](14-select-and-context/13-context-leak-detection/13-context-leak-detection.md) | Insane |
| 179 | [Building a Context-Aware Service Framework](14-select-and-context/14-building-a-context-aware-service-framework/14-building-a-context-aware-service-framework.md) | Insane |

### 15 - Sync Primitives

| # | Exercise | Difficulty |
|---|----------|------------|
| 180 | [sync.Mutex and Critical Sections](15-sync-primitives/01-sync-mutex/01-sync-mutex.md) | Basic |
| 181 | [sync.RWMutex for Read-Heavy Workloads](15-sync-primitives/02-sync-rwmutex/02-sync-rwmutex.md) | Intermediate |
| 182 | [sync.Once for Lazy Initialization](15-sync-primitives/03-sync-once/03-sync-once.md) | Intermediate |
| 183 | [sync.Map for Concurrent Map Access](15-sync-primitives/04-sync-map/04-sync-map.md) | Intermediate |
| 184 | [sync.Pool for Object Reuse](15-sync-primitives/05-sync-pool/05-sync-pool.md) | Intermediate |
| 185 | [sync.Cond for Conditional Waiting](15-sync-primitives/06-sync-cond/06-sync-cond.md) | Advanced |
| 186 | [atomic Package: Load, Store, CompareAndSwap](15-sync-primitives/07-atomic-package/07-atomic-package.md) | Advanced |
| 187 | [atomic.Value for Config Hot-Reload](15-sync-primitives/08-atomic-value-config-hot-reload/08-atomic-value-config-hot-reload.md) | Advanced |
| 188 | [Lock Ordering and Deadlock Prevention](15-sync-primitives/09-lock-ordering-deadlock-prevention/09-lock-ordering-deadlock-prevention.md) | Advanced |
| 189 | [Mutex vs Channel: Decision Framework](15-sync-primitives/10-mutex-vs-channel/10-mutex-vs-channel.md) | Advanced |
| 190 | [Lock-Free Data Structures with CAS](15-sync-primitives/11-lock-free-data-structures/11-lock-free-data-structures.md) | Insane |
| 191 | [sync.OnceValue and sync.OnceFunc (Go 1.21+)](15-sync-primitives/12-sync-oncevalue-oncefunc/12-sync-oncevalue-oncefunc.md) | Intermediate |
| 192 | [Building a Thread-Safe Cache](15-sync-primitives/13-building-a-thread-safe-cache/13-building-a-thread-safe-cache.md) | Insane |
| 193 | [Contention Profiling and Lock Analysis](15-sync-primitives/14-contention-profiling/14-contention-profiling.md) | Insane |

### 16 - Concurrency Patterns

| # | Exercise | Difficulty |
|---|----------|------------|
| 194 | [Pipeline Pattern](16-concurrency-patterns/01-pipeline-pattern/01-pipeline-pattern.md) | Intermediate |
| 195 | [Fan-Out Pattern](16-concurrency-patterns/02-fan-out-pattern/02-fan-out-pattern.md) | Intermediate |
| 196 | [Fan-In Pattern](16-concurrency-patterns/03-fan-in-pattern/03-fan-in-pattern.md) | Intermediate |
| 197 | [Worker Pool Pattern](16-concurrency-patterns/04-worker-pool-pattern/04-worker-pool-pattern.md) | Intermediate |
| 198 | [Generator Pattern](16-concurrency-patterns/05-generator-pattern/05-generator-pattern.md) | Intermediate |
| 199 | [errgroup Basic Usage](16-concurrency-patterns/06-errgroup-basic-usage/06-errgroup-basic-usage.md) | Intermediate |
| 200 | [errgroup with Context Cancellation](16-concurrency-patterns/07-errgroup-with-context/07-errgroup-with-context.md) | Intermediate |
| 201 | [time.Ticker and Periodic Goroutines](16-concurrency-patterns/08-time-ticker-periodic-goroutines/08-time-ticker-periodic-goroutines.md) | Intermediate |
| 202 | [Or-Channel Pattern](16-concurrency-patterns/09-or-channel-pattern/09-or-channel-pattern.md) | Advanced |
| 203 | [Or-Done Channel Pattern](16-concurrency-patterns/10-or-done-channel-pattern/10-or-done-channel-pattern.md) | Advanced |
| 204 | [Tee Channel Pattern](16-concurrency-patterns/11-tee-channel-pattern/11-tee-channel-pattern.md) | Advanced |
| 205 | [Bridge Channel Pattern](16-concurrency-patterns/12-bridge-channel-pattern/12-bridge-channel-pattern.md) | Advanced |
| 206 | [Rate Limiter with Token Bucket](16-concurrency-patterns/13-rate-limiter-token-bucket/13-rate-limiter-token-bucket.md) | Advanced |
| 207 | [Circuit Breaker Pattern](16-concurrency-patterns/14-circuit-breaker-pattern/14-circuit-breaker-pattern.md) | Advanced |
| 208 | [Bounded Parallelism](16-concurrency-patterns/15-bounded-parallelism/15-bounded-parallelism.md) | Advanced |
| 209 | [Pub/Sub with Channels](16-concurrency-patterns/16-pub-sub-with-channels/16-pub-sub-with-channels.md) | Advanced |
| 210 | [Error Group and Parallel Error Handling](16-concurrency-patterns/17-error-group-parallel-error-handling/17-error-group-parallel-error-handling.md) | Advanced |
| 211 | [Bounded Worker Pool with Adaptive Sizing](16-concurrency-patterns/18-bounded-worker-pool-adaptive-sizing/18-bounded-worker-pool-adaptive-sizing.md) | Advanced |
| 212 | [Pipeline with Per-Stage Metrics](16-concurrency-patterns/19-pipeline-with-per-stage-metrics/19-pipeline-with-per-stage-metrics.md) | Advanced |
| 213 | [Batch Processing with Partial Failure](16-concurrency-patterns/20-batch-processing-partial-failure/20-batch-processing-partial-failure.md) | Advanced |
| 214 | [Graceful Goroutine Draining on Shutdown](16-concurrency-patterns/21-graceful-goroutine-draining/21-graceful-goroutine-draining.md) | Advanced |
| 215 | [Channel-Based State Machine](16-concurrency-patterns/22-channel-based-state-machine/22-channel-based-state-machine.md) | Advanced |
| 216 | [Request Coalescing with singleflight](16-concurrency-patterns/23-request-coalescing-singleflight/23-request-coalescing-singleflight.md) | Advanced |
| 217 | [Streaming Pipeline with Backpressure](16-concurrency-patterns/24-streaming-pipeline-backpressure/24-streaming-pipeline-backpressure.md) | Insane |
| 218 | [Actor Model in Go](16-concurrency-patterns/25-actor-model-in-go/25-actor-model-in-go.md) | Insane |
| 219 | [CSP vs Actor: Comparative Design](16-concurrency-patterns/26-csp-vs-actor/26-csp-vs-actor.md) | Insane |
| 220 | [Building a Concurrent Web Crawler](16-concurrency-patterns/27-building-a-concurrent-web-crawler/27-building-a-concurrent-web-crawler.md) | Insane |
| 221 | [Fan-Out with Priority Queues](16-concurrency-patterns/28-fan-out-with-priority-queues/28-fan-out-with-priority-queues.md) | Insane |

### 17 - HTTP Programming

| # | Exercise | Difficulty |
|---|----------|------------|
| 222 | [HTTP Server with net/http](17-http-programming/01-http-server-with-net-http/01-http-server-with-net-http.md) | Basic |
| 223 | [HTTP Client: GET, POST, and Headers](17-http-programming/02-http-client/02-http-client.md) | Basic |
| 224 | [ServeMux Routing and Patterns (Go 1.22+)](17-http-programming/03-servemux-routing-and-patterns/03-servemux-routing-and-patterns.md) | Intermediate |
| 225 | [Middleware Chains: Logging, Auth, Recovery](17-http-programming/04-middleware-chains/04-middleware-chains.md) | Intermediate |
| 226 | [Request Body Parsing and Validation](17-http-programming/05-request-body-parsing-and-validation/05-request-body-parsing-and-validation.md) | Intermediate |
| 227 | [HTTP Client Timeouts and Connection Pooling](17-http-programming/06-http-client-timeouts/06-http-client-timeouts.md) | Intermediate |
| 228 | [Cookie and Session Management](17-http-programming/07-cookie-and-session-management/07-cookie-and-session-management.md) | Intermediate |
| 229 | [File Upload and Multipart Forms](17-http-programming/08-file-upload-and-multipart-forms/08-file-upload-and-multipart-forms.md) | Intermediate |
| 230 | [Server-Sent Events (SSE)](17-http-programming/09-server-sent-events/09-server-sent-events.md) | Advanced |
| 231 | [WebSocket Server with gorilla/websocket](17-http-programming/10-websocket-server/10-websocket-server.md) | Advanced |
| 232 | [HTTP/2 Server Push and Configuration](17-http-programming/11-http2-server-push/11-http2-server-push.md) | Advanced |
| 233 | [Reverse Proxy and Load Balancer](17-http-programming/12-reverse-proxy-and-load-balancer/12-reverse-proxy-and-load-balancer.md) | Advanced |
| 234 | [Building a REST API with Full Middleware Stack](17-http-programming/13-building-a-rest-api/13-building-a-rest-api.md) | Insane |
| 235 | [HTTP Client with Retry, Circuit Breaker, Tracing](17-http-programming/14-http-client-retry-circuit-breaker-tracing/14-http-client-retry-circuit-breaker-tracing.md) | Insane |

### 18 - Encoding: JSON, XML, and Protocol Buffers

| # | Exercise | Difficulty |
|---|----------|------------|
| 236 | [JSON Marshal and Unmarshal](18-encoding-json-xml-protobuf/01-json-marshal-and-unmarshal/01-json-marshal-and-unmarshal.md) | Basic |
| 237 | [Struct Tags for JSON: omitempty, rename, ignore](18-encoding-json-xml-protobuf/02-struct-tags-for-json/02-struct-tags-for-json.md) | Basic |
| 238 | [Custom JSON Marshaler and Unmarshaler](18-encoding-json-xml-protobuf/03-custom-json-marshaler/03-custom-json-marshaler.md) | Intermediate |
| 239 | [Streaming JSON with Encoder and Decoder](18-encoding-json-xml-protobuf/04-streaming-json/04-streaming-json.md) | Intermediate |
| 240 | [Handling Unknown JSON Fields](18-encoding-json-xml-protobuf/05-handling-unknown-json-fields/05-handling-unknown-json-fields.md) | Intermediate |
| 241 | [JSON Patch and Merge Patterns](18-encoding-json-xml-protobuf/06-json-patch-and-merge/06-json-patch-and-merge.md) | Intermediate |
| 242 | [XML Encoding and Decoding](18-encoding-json-xml-protobuf/07-xml-encoding-and-decoding/07-xml-encoding-and-decoding.md) | Intermediate |
| 243 | [Protocol Buffers with protoc-gen-go](18-encoding-json-xml-protobuf/08-protocol-buffers/08-protocol-buffers.md) | Advanced |
| 244 | [gRPC Service Definition and Implementation](18-encoding-json-xml-protobuf/09-grpc-service/09-grpc-service.md) | Advanced |
| 245 | [Binary Encoding with encoding/binary](18-encoding-json-xml-protobuf/10-binary-encoding/10-binary-encoding.md) | Advanced |
| 246 | [Custom Encoding Format Design](18-encoding-json-xml-protobuf/11-custom-encoding-format/11-custom-encoding-format.md) | Insane |
| 247 | [Performance: JSON vs Protobuf vs MessagePack](18-encoding-json-xml-protobuf/12-performance-json-vs-protobuf-vs-msgpack/12-performance-json-vs-protobuf-vs-msgpack.md) | Insane |

### 19 - I/O and Filesystem

| # | Exercise | Difficulty |
|---|----------|------------|
| 248 | [Reading and Writing Files](19-io-and-filesystem/01-reading-and-writing-files/01-reading-and-writing-files.md) | Basic |
| 249 | [io.Reader and io.Writer Composition](19-io-and-filesystem/02-io-reader-and-io-writer/02-io-reader-and-io-writer.md) | Basic |
| 250 | [Buffered I/O with bufio](19-io-and-filesystem/03-buffered-io-with-bufio/03-buffered-io-with-bufio.md) | Intermediate |
| 251 | [io.Copy, io.TeeReader, io.MultiWriter](19-io-and-filesystem/04-io-copy-teereader-multiwriter/04-io-copy-teereader-multiwriter.md) | Intermediate |
| 252 | [Walking Directory Trees with fs.WalkDir](19-io-and-filesystem/05-walking-directory-trees/05-walking-directory-trees.md) | Intermediate |
| 253 | [Embed Directive: Embedding Files in Binaries](19-io-and-filesystem/06-embed-directive/06-embed-directive.md) | Intermediate |
| 254 | [Temporary Files and Directories](19-io-and-filesystem/07-temporary-files-and-directories/07-temporary-files-and-directories.md) | Intermediate |
| 255 | [CSV Reading and Writing](19-io-and-filesystem/08-csv-reading-and-writing/08-csv-reading-and-writing.md) | Intermediate |
| 256 | [YAML Parsing with External Libraries](19-io-and-filesystem/09-yaml-parsing/09-yaml-parsing.md) | Intermediate |
| 257 | [TOML Configuration Files](19-io-and-filesystem/10-toml-configuration-files/10-toml-configuration-files.md) | Intermediate |
| 258 | [stdin/stdout Piping Patterns](19-io-and-filesystem/11-stdin-stdout-piping/11-stdin-stdout-piping.md) | Intermediate |
| 259 | [Archive Creation and Extraction (tar, zip)](19-io-and-filesystem/12-archive-creation-and-extraction/12-archive-creation-and-extraction.md) | Intermediate |
| 260 | [io/fs: Virtual Filesystems and Testing](19-io-and-filesystem/13-io-fs-virtual-filesystems/13-io-fs-virtual-filesystems.md) | Advanced |
| 261 | [Pipe-Based I/O: io.Pipe for Producer-Consumer](19-io-and-filesystem/14-pipe-based-io/14-pipe-based-io.md) | Advanced |
| 262 | [Memory-Mapped Files with mmap](19-io-and-filesystem/15-memory-mapped-files/15-memory-mapped-files.md) | Advanced |
| 263 | [Implementing a Custom io.Reader](19-io-and-filesystem/16-implementing-a-custom-io-reader/16-implementing-a-custom-io-reader.md) | Advanced |
| 264 | [Structured Logging to Files with Rotation](19-io-and-filesystem/17-structured-logging-to-files/17-structured-logging-to-files.md) | Advanced |
| 265 | [Building a File Watcher](19-io-and-filesystem/18-building-a-file-watcher/18-building-a-file-watcher.md) | Insane |

### 20 - Generics

| # | Exercise | Difficulty |
|---|----------|------------|
| 266 | [Type Parameters and Constraints](20-generics/01-type-parameters-and-constraints/01-type-parameters-and-constraints.md) | Basic |
| 267 | [Generic Functions: Min, Max, Contains](20-generics/02-generic-functions/02-generic-functions.md) | Basic |
| 268 | [Comparable and Ordered Constraints](20-generics/03-comparable-and-ordered/03-comparable-and-ordered.md) | Intermediate |
| 269 | [Generic Data Structures: Stack and Queue](20-generics/04-generic-data-structures/04-generic-data-structures.md) | Intermediate |
| 270 | [Interface Constraints with Methods](20-generics/05-interface-constraints-with-methods/05-interface-constraints-with-methods.md) | Intermediate |
| 271 | [Union Type Constraints](20-generics/06-union-type-constraints/06-union-type-constraints.md) | Intermediate |
| 272 | [Type Inference and Constraint Inference](20-generics/07-type-inference-and-constraint-inference/07-type-inference-and-constraint-inference.md) | Intermediate |
| 273 | [Generic Tree Structures](20-generics/08-generic-tree-structures/08-generic-tree-structures.md) | Advanced |
| 274 | [Generic Iterator Patterns](20-generics/09-generic-iterator-patterns/09-generic-iterator-patterns.md) | Advanced |
| 275 | [Generic Repository Pattern for Data Access](20-generics/10-generic-repository-pattern/10-generic-repository-pattern.md) | Advanced |
| 276 | [Generics vs Interfaces: Decision Framework](20-generics/11-generics-vs-interfaces/11-generics-vs-interfaces.md) | Advanced |
| 277 | [Type Constraint Composition](20-generics/12-type-constraint-composition/12-type-constraint-composition.md) | Advanced |
| 278 | [Generic Middleware and Decorator Patterns](20-generics/13-generic-middleware-and-decorator/13-generic-middleware-and-decorator.md) | Insane |
| 279 | [Building a Type-Safe Event Bus with Generics](20-generics/14-building-a-type-safe-event-bus/14-building-a-type-safe-event-bus.md) | Insane |

### 21 - Structured Logging with slog

| # | Exercise | Difficulty |
|---|----------|------------|
| 280 | [slog Basics: Info, Warn, Error with Attributes](21-structured-logging-with-slog/01-slog-basics/01-slog-basics.md) | Basic |
| 281 | [Log Levels and Filtering](21-structured-logging-with-slog/02-log-levels-and-filtering/02-log-levels-and-filtering.md) | Basic |
| 282 | [JSON Handler vs Text Handler](21-structured-logging-with-slog/03-json-handler-vs-text-handler/03-json-handler-vs-text-handler.md) | Intermediate |
| 283 | [Groups and Nested Attributes](21-structured-logging-with-slog/04-groups-and-nested-attributes/04-groups-and-nested-attributes.md) | Intermediate |
| 284 | [slog.With for Logger Enrichment](21-structured-logging-with-slog/05-slog-with-for-logger-enrichment/05-slog-with-for-logger-enrichment.md) | Intermediate |
| 285 | [Custom slog.Handler Implementation](21-structured-logging-with-slog/06-custom-slog-handler/06-custom-slog-handler.md) | Advanced |
| 286 | [Context-Aware Logging (Trace IDs)](21-structured-logging-with-slog/07-context-aware-logging/07-context-aware-logging.md) | Advanced |
| 287 | [Log Sampling for High-Throughput Services](21-structured-logging-with-slog/08-log-sampling/08-log-sampling.md) | Advanced |
| 288 | [Replacing Global Logger Patterns](21-structured-logging-with-slog/09-replacing-global-logger-patterns/09-replacing-global-logger-patterns.md) | Advanced |

### 22 - Database Patterns

| # | Exercise | Difficulty |
|---|----------|------------|
| 289 | [database/sql Basics: Open, Query, Exec](22-database-patterns/01-database-sql-basics/01-database-sql-basics.md) | Intermediate |
| 290 | [Row Scanning and Struct Mapping](22-database-patterns/02-row-scanning-and-struct-mapping/02-row-scanning-and-struct-mapping.md) | Intermediate |
| 291 | [Connection Pool Configuration](22-database-patterns/03-connection-pool-configuration/03-connection-pool-configuration.md) | Intermediate |
| 292 | [Prepared Statements](22-database-patterns/04-prepared-statements/04-prepared-statements.md) | Intermediate |
| 293 | [Transactions: Begin, Commit, Rollback](22-database-patterns/05-transactions/05-transactions.md) | Intermediate |
| 294 | [Null Handling (sql.NullString, etc.)](22-database-patterns/06-null-handling/06-null-handling.md) | Intermediate |
| 295 | [Migration Patterns](22-database-patterns/07-migration-patterns/07-migration-patterns.md) | Advanced |
| 296 | [sqlc: Type-Safe SQL Generation](22-database-patterns/08-sqlc-type-safe-sql/08-sqlc-type-safe-sql.md) | Advanced |
| 297 | [Context-Aware Queries](22-database-patterns/09-context-aware-queries/09-context-aware-queries.md) | Advanced |
| 298 | [Testing with In-Memory SQLite](22-database-patterns/10-testing-with-in-memory-sqlite/10-testing-with-in-memory-sqlite.md) | Advanced |

### 23 - CLI Applications

| # | Exercise | Difficulty |
|---|----------|------------|
| 299 | [flag Package Basics](23-cli-applications/01-flag-package-basics/01-flag-package-basics.md) | Intermediate |
| 300 | [Custom Flag Types](23-cli-applications/02-custom-flag-types/02-custom-flag-types.md) | Intermediate |
| 301 | [Subcommands with flag.FlagSet](23-cli-applications/03-subcommands-with-flagset/03-subcommands-with-flagset.md) | Intermediate |
| 302 | [cobra: Commands, Flags, and Args](23-cli-applications/04-cobra-commands-flags-args/04-cobra-commands-flags-args.md) | Intermediate |
| 303 | [Interactive Prompts with survey/huh](23-cli-applications/05-interactive-prompts/05-interactive-prompts.md) | Intermediate |
| 304 | [Progress Bars and Spinners](23-cli-applications/06-progress-bars-and-spinners/06-progress-bars-and-spinners.md) | Intermediate |
| 305 | [Output Formatting (Table, JSON, YAML)](23-cli-applications/07-output-formatting/07-output-formatting.md) | Intermediate |
| 306 | [Config Loading (env + file + flags precedence)](23-cli-applications/08-config-loading/08-config-loading.md) | Advanced |
| 307 | [Shell Completion Generation](23-cli-applications/09-shell-completion-generation/09-shell-completion-generation.md) | Advanced |
| 308 | [Building a Complete CLI Tool](23-cli-applications/10-building-a-complete-cli-tool/10-building-a-complete-cli-tool.md) | Insane |

### 24 - Design Patterns in Go

| # | Exercise | Difficulty |
|---|----------|------------|
| 309 | [Functional Options Pattern Deep Dive](24-design-patterns-in-go/01-functional-options-deep-dive/01-functional-options-deep-dive.md) | Intermediate |
| 310 | [Builder Pattern for Complex Configuration](24-design-patterns-in-go/02-builder-pattern/02-builder-pattern.md) | Intermediate |
| 311 | [Strategy Pattern via Interfaces](24-design-patterns-in-go/03-strategy-pattern-via-interfaces/03-strategy-pattern-via-interfaces.md) | Intermediate |
| 312 | [Dependency Injection (Constructor, No Frameworks)](24-design-patterns-in-go/04-dependency-injection/04-dependency-injection.md) | Intermediate |
| 313 | [Repository Pattern for Data Access](24-design-patterns-in-go/05-repository-pattern/05-repository-pattern.md) | Intermediate |
| 314 | [Service Layer Pattern](24-design-patterns-in-go/06-service-layer-pattern/06-service-layer-pattern.md) | Advanced |
| 315 | [Adapter Pattern for External Dependencies](24-design-patterns-in-go/07-adapter-pattern/07-adapter-pattern.md) | Advanced |
| 316 | [Middleware/Decorator Pattern](24-design-patterns-in-go/08-middleware-decorator-pattern/08-middleware-decorator-pattern.md) | Advanced |
| 317 | [Observer Pattern with Channels](24-design-patterns-in-go/09-observer-pattern-with-channels/09-observer-pattern-with-channels.md) | Advanced |

### 25 - Iterators and Modern Go (Go 1.22-1.23+)

| # | Exercise | Difficulty |
|---|----------|------------|
| 318 | [Range Over Integers (Go 1.22)](25-iterators-and-modern-go/01-range-over-integers/01-range-over-integers.md) | Basic |
| 319 | [Loopvar Semantic Change (Go 1.22)](25-iterators-and-modern-go/02-loopvar-semantic-change/02-loopvar-semantic-change.md) | Intermediate |
| 320 | [Range Over Func: Push Iterators (Go 1.23)](25-iterators-and-modern-go/03-range-over-func-push-iterators/03-range-over-func-push-iterators.md) | Intermediate |
| 321 | [Range Over Func: Pull Iterators (Go 1.23)](25-iterators-and-modern-go/04-range-over-func-pull-iterators/04-range-over-func-pull-iterators.md) | Intermediate |
| 322 | [Designing Iterator APIs for Custom Collections](25-iterators-and-modern-go/05-designing-iterator-apis/05-designing-iterator-apis.md) | Advanced |
| 323 | [Composing Iterators: Filter, Map, Take](25-iterators-and-modern-go/06-composing-iterators/06-composing-iterators.md) | Advanced |
| 324 | [iter Package Usage](25-iterators-and-modern-go/07-iter-package-usage/07-iter-package-usage.md) | Advanced |
| 325 | [Standard Library Iterators: slices.All, maps.Keys](25-iterators-and-modern-go/08-standard-library-iterators/08-standard-library-iterators.md) | Intermediate |

### 26 - Memory Model and Optimization

| # | Exercise | Difficulty |
|---|----------|------------|
| 326 | [Happens-Before Relationships](26-memory-model-and-optimization/01-happens-before-relationships/01-happens-before-relationships.md) | Intermediate |
| 327 | [CPU Profiling with pprof](26-memory-model-and-optimization/02-cpu-profiling-with-pprof/02-cpu-profiling-with-pprof.md) | Intermediate |
| 328 | [Memory Profiling and Allocation Analysis](26-memory-model-and-optimization/03-memory-profiling/03-memory-profiling.md) | Intermediate |
| 329 | [Benchmarking Methodology and b.ResetTimer](26-memory-model-and-optimization/04-benchmarking-methodology/04-benchmarking-methodology.md) | Advanced |
| 330 | [Escape Analysis and Allocation Reduction](26-memory-model-and-optimization/05-escape-analysis/05-escape-analysis.md) | Advanced |
| 331 | [Struct Field Ordering for Cache Lines](26-memory-model-and-optimization/06-struct-field-ordering-cache-lines/06-struct-field-ordering-cache-lines.md) | Advanced |
| 332 | [String Interning and Reducing Allocations](26-memory-model-and-optimization/07-string-interning/07-string-interning.md) | Advanced |
| 333 | [sync.Pool Tuning for Hot Paths](26-memory-model-and-optimization/08-sync-pool-tuning/08-sync-pool-tuning.md) | Advanced |
| 334 | [Trace Tool for Goroutine Scheduling](26-memory-model-and-optimization/09-trace-tool-goroutine-scheduling/09-trace-tool-goroutine-scheduling.md) | Advanced |
| 335 | [Memory Ballast and GOGC Tuning](26-memory-model-and-optimization/10-memory-ballast-gogc-tuning/10-memory-ballast-gogc-tuning.md) | Advanced |
| 336 | [False Sharing and Cache Contention](26-memory-model-and-optimization/11-false-sharing-cache-contention/11-false-sharing-cache-contention.md) | Insane |
| 337 | [Zero-Allocation Patterns](26-memory-model-and-optimization/12-zero-allocation-patterns/12-zero-allocation-patterns.md) | Insane |
| 338 | [Performance Regression Testing with benchstat](26-memory-model-and-optimization/13-performance-regression-testing/13-performance-regression-testing.md) | Insane |
| 339 | [Optimizing a Real-World Hot Path](26-memory-model-and-optimization/14-optimizing-a-real-world-hot-path/14-optimizing-a-real-world-hot-path.md) | Insane |

### 27 - Reflection

| # | Exercise | Difficulty |
|---|----------|------------|
| 340 | [reflect.TypeOf and reflect.ValueOf](27-reflection/01-reflect-typeof-valueof/01-reflect-typeof-valueof.md) | Intermediate |
| 341 | [Inspecting Struct Fields and Tags](27-reflection/02-inspecting-struct-fields-tags/02-inspecting-struct-fields-tags.md) | Intermediate |
| 342 | [Dynamic Method Invocation](27-reflection/03-dynamic-method-invocation/03-dynamic-method-invocation.md) | Advanced |
| 343 | [Setting Values with reflect.Value.Set](27-reflection/04-setting-values-with-reflect/04-setting-values-with-reflect.md) | Advanced |
| 344 | [Building a Struct Validator with Tags](27-reflection/05-building-a-struct-validator/05-building-a-struct-validator.md) | Advanced |
| 345 | [DeepEqual and Custom Comparison](27-reflection/06-deepequal-and-custom-comparison/06-deepequal-and-custom-comparison.md) | Advanced |
| 346 | [Reflection Performance Costs](27-reflection/07-reflection-performance-costs/07-reflection-performance-costs.md) | Advanced |
| 347 | [Building a Simple ORM with Reflection](27-reflection/08-building-a-simple-orm/08-building-a-simple-orm.md) | Insane |
| 348 | [Code Generation vs Reflection Trade-offs](27-reflection/09-code-generation-vs-reflection/09-code-generation-vs-reflection.md) | Insane |
| 349 | [Building a Configuration Loader](27-reflection/10-building-a-configuration-loader/10-building-a-configuration-loader.md) | Insane |

### 28 - unsafe and cgo

| # | Exercise | Difficulty |
|---|----------|------------|
| 350 | [unsafe.Pointer and uintptr Basics](28-unsafe-and-cgo/01-unsafe-pointer-and-uintptr/01-unsafe-pointer-and-uintptr.md) | Advanced |
| 351 | [unsafe.Sizeof, Alignof, Offsetof](28-unsafe-and-cgo/02-unsafe-sizeof-alignof-offsetof/02-unsafe-sizeof-alignof-offsetof.md) | Advanced |
| 352 | [Type Punning with unsafe.Pointer](28-unsafe-and-cgo/03-type-punning/03-type-punning.md) | Advanced |
| 353 | [cgo Basics: Calling C from Go](28-unsafe-and-cgo/04-cgo-basics/04-cgo-basics.md) | Advanced |
| 354 | [Passing Data Between Go and C](28-unsafe-and-cgo/05-passing-data-go-and-c/05-passing-data-go-and-c.md) | Advanced |
| 355 | [cgo Performance Overhead and Measurement](28-unsafe-and-cgo/06-cgo-performance-overhead/06-cgo-performance-overhead.md) | Advanced |
| 356 | [unsafe.Slice and unsafe.String (Go 1.17+)](28-unsafe-and-cgo/07-unsafe-slice-and-string/07-unsafe-slice-and-string.md) | Advanced |
| 357 | [Wrapping a C Library with cgo](28-unsafe-and-cgo/08-wrapping-a-c-library/08-wrapping-a-c-library.md) | Insane |
| 358 | [Zero-Copy Deserialization with unsafe](28-unsafe-and-cgo/09-zero-copy-deserialization/09-zero-copy-deserialization.md) | Insane |
| 359 | [Memory-Mapped Data Store with unsafe](28-unsafe-and-cgo/10-memory-mapped-data-store/10-memory-mapped-data-store.md) | Insane |

### 29 - Code Generation and Build System

| # | Exercise | Difficulty |
|---|----------|------------|
| 360 | [go generate and Code Generation Basics](29-code-generation-and-build-system/01-go-generate-basics/01-go-generate-basics.md) | Intermediate |
| 361 | [Stringer: Generating String Methods for Enums](29-code-generation-and-build-system/02-stringer/02-stringer.md) | Intermediate |
| 362 | [Writing a Custom Code Generator](29-code-generation-and-build-system/03-writing-a-custom-code-generator/03-writing-a-custom-code-generator.md) | Advanced |
| 363 | [AST Parsing with go/ast](29-code-generation-and-build-system/04-ast-parsing/04-ast-parsing.md) | Advanced |
| 364 | [Template-Based Code Generation](29-code-generation-and-build-system/05-template-based-code-generation/05-template-based-code-generation.md) | Advanced |
| 365 | [Build Constraints and File Suffixes](29-code-generation-and-build-system/06-build-constraints-and-file-suffixes/06-build-constraints-and-file-suffixes.md) | Intermediate |
| 366 | [Link-Time Variable Injection with ldflags](29-code-generation-and-build-system/07-link-time-variable-injection/07-link-time-variable-injection.md) | Intermediate |
| 367 | [Plugin System with plugin Package](29-code-generation-and-build-system/08-plugin-system/08-plugin-system.md) | Advanced |
| 368 | [Building a CLI Code Generator](29-code-generation-and-build-system/09-building-a-cli-code-generator/09-building-a-cli-code-generator.md) | Insane |
| 369 | [AST Rewriting Tool for Code Transformation](29-code-generation-and-build-system/10-ast-rewriting-tool/10-ast-rewriting-tool.md) | Insane |

### 30 - Production Patterns

| # | Exercise | Difficulty |
|---|----------|------------|
| 370 | [Graceful Shutdown: SIGTERM, Drain, Context Cascade](30-production-patterns/01-graceful-shutdown/01-graceful-shutdown.md) | Advanced |
| 371 | [Configuration: Layered (defaults, file, env, flags)](30-production-patterns/02-configuration-layered/02-configuration-layered.md) | Advanced |
| 372 | [Feature Flags Without External Services](30-production-patterns/03-feature-flags/03-feature-flags.md) | Advanced |
| 373 | [Health Endpoints: Liveness vs Readiness vs Startup](30-production-patterns/04-health-endpoints/04-health-endpoints.md) | Advanced |
| 374 | [Request ID Propagation Through Middleware](30-production-patterns/05-request-id-propagation/05-request-id-propagation.md) | Advanced |
| 375 | [Structured Error Responses for REST APIs](30-production-patterns/06-structured-error-responses/06-structured-error-responses.md) | Advanced |
| 376 | [OpenTelemetry Instrumentation Basics](30-production-patterns/07-opentelemetry-instrumentation/07-opentelemetry-instrumentation.md) | Advanced |
| 377 | [Distributed Tracing Context Propagation](30-production-patterns/08-distributed-tracing-context/08-distributed-tracing-context.md) | Advanced |
| 378 | [Circuit Breaker with Half-Open State](30-production-patterns/09-circuit-breaker-half-open/09-circuit-breaker-half-open.md) | Advanced |
| 379 | [Retry with Exponential Backoff and Jitter](30-production-patterns/10-retry-exponential-backoff-jitter/10-retry-exponential-backoff-jitter.md) | Advanced |
| 380 | [Timeout Budgets Across Service Calls](30-production-patterns/11-timeout-budgets/11-timeout-budgets.md) | Advanced |
| 381 | [Connection Pool Health Monitoring](30-production-patterns/12-connection-pool-health-monitoring/12-connection-pool-health-monitoring.md) | Advanced |
| 382 | [Panic Recovery in Production](30-production-patterns/13-panic-recovery-in-production/13-panic-recovery-in-production.md) | Advanced |
| 383 | [Blue-Green Deployment Patterns in Go Services](30-production-patterns/14-blue-green-deployment-patterns/14-blue-green-deployment-patterns.md) | Advanced |

### 31 - Cloud-Native Go

| # | Exercise | Difficulty |
|---|----------|------------|
| 384 | [Lambda Handler Patterns in Go](31-cloud-native-go/01-lambda-handler-patterns/01-lambda-handler-patterns.md) | Advanced |
| 385 | [Lambda Cold Start Measurement and Optimization](31-cloud-native-go/02-lambda-cold-start-optimization/02-lambda-cold-start-optimization.md) | Advanced |
| 386 | [SQS Message Handler with Visibility Timeout](31-cloud-native-go/03-sqs-message-handler/03-sqs-message-handler.md) | Advanced |
| 387 | [EventBridge Event Routing and Handling](31-cloud-native-go/04-eventbridge-event-routing/04-eventbridge-event-routing.md) | Advanced |
| 388 | [S3 Event Processing Pipeline](31-cloud-native-go/05-s3-event-processing/05-s3-event-processing.md) | Advanced |
| 389 | [Kubernetes client-go: List, Watch, Informers](31-cloud-native-go/06-kubernetes-client-go/06-kubernetes-client-go.md) | Advanced |
| 390 | [Kubernetes Controller: Reconciliation Loop](31-cloud-native-go/07-kubernetes-controller/07-kubernetes-controller.md) | Advanced |
| 391 | [Terraform Provider Skeleton: Resource CRUD](31-cloud-native-go/08-terraform-provider-skeleton/08-terraform-provider-skeleton.md) | Insane |
| 392 | [Container Health Checks from Go](31-cloud-native-go/09-container-health-checks/09-container-health-checks.md) | Advanced |
| 393 | [Prometheus Metrics Exposition](31-cloud-native-go/10-prometheus-metrics-exposition/10-prometheus-metrics-exposition.md) | Advanced |
| 394 | [OpenTelemetry Collector Integration](31-cloud-native-go/11-opentelemetry-collector-integration/11-opentelemetry-collector-integration.md) | Advanced |

### 32 - Concurrency Debugging and Testing

| # | Exercise | Difficulty |
|---|----------|------------|
| 395 | [Race Condition Reproduction and Fixing](32-concurrency-debugging-and-testing/01-race-condition-reproduction/01-race-condition-reproduction.md) | Advanced |
| 396 | [Goroutine Leak Detection with goleak](32-concurrency-debugging-and-testing/02-goroutine-leak-detection-goleak/02-goroutine-leak-detection-goleak.md) | Advanced |
| 397 | [Testing Concurrent Code Deterministically](32-concurrency-debugging-and-testing/03-testing-concurrent-code/03-testing-concurrent-code.md) | Advanced |
| 398 | [Deadlock Detection Strategies](32-concurrency-debugging-and-testing/04-deadlock-detection-strategies/04-deadlock-detection-strategies.md) | Advanced |
| 399 | [Contention Analysis with Mutex Profiling](32-concurrency-debugging-and-testing/05-contention-analysis/05-contention-analysis.md) | Advanced |
| 400 | [Goroutine Dump Analysis (SIGQUIT / runtime.Stack)](32-concurrency-debugging-and-testing/06-goroutine-dump-analysis/06-goroutine-dump-analysis.md) | Advanced |
| 401 | [Concurrent Test Isolation Patterns](32-concurrency-debugging-and-testing/07-concurrent-test-isolation/07-concurrent-test-isolation.md) | Advanced |
| 402 | [Chaos Testing Concurrent Code](32-concurrency-debugging-and-testing/08-chaos-testing-concurrent-code/08-chaos-testing-concurrent-code.md) | Advanced |

### 33 - TCP/UDP and Networking

| # | Exercise | Difficulty |
|---|----------|------------|
| 403 | [TCP Server and Client with net.Dial](33-tcp-udp-and-networking/01-tcp-server-and-client/01-tcp-server-and-client.md) | Intermediate |
| 404 | [UDP Server and Client](33-tcp-udp-and-networking/02-udp-server-and-client/02-udp-server-and-client.md) | Intermediate |
| 405 | [Concurrent TCP Server with Goroutines](33-tcp-udp-and-networking/03-concurrent-tcp-server/03-concurrent-tcp-server.md) | Intermediate |
| 406 | [Connection Timeouts and Deadlines](33-tcp-udp-and-networking/04-connection-timeouts-and-deadlines/04-connection-timeouts-and-deadlines.md) | Advanced |
| 407 | [TCP Keep-Alive and Connection Health](33-tcp-udp-and-networking/05-tcp-keep-alive/05-tcp-keep-alive.md) | Advanced |
| 408 | [Building a Line-Based Protocol (Redis RESP)](33-tcp-udp-and-networking/06-building-a-line-based-protocol/06-building-a-line-based-protocol.md) | Advanced |
| 409 | [Connection Pooling Implementation](33-tcp-udp-and-networking/07-connection-pooling-implementation/07-connection-pooling-implementation.md) | Advanced |
| 410 | [TLS Server and Client with crypto/tls](33-tcp-udp-and-networking/08-tls-server-and-client/08-tls-server-and-client.md) | Advanced |
| 411 | [Mutual TLS Authentication](33-tcp-udp-and-networking/09-mutual-tls-authentication/09-mutual-tls-authentication.md) | Advanced |
| 412 | [DNS Resolver and Custom Dialer](33-tcp-udp-and-networking/10-dns-resolver-and-custom-dialer/10-dns-resolver-and-custom-dialer.md) | Advanced |
| 413 | [HTTP/1.1 Keep-Alive and Connection Reuse Analysis](33-tcp-udp-and-networking/11-http-keep-alive-connection-reuse/11-http-keep-alive-connection-reuse.md) | Advanced |
| 414 | [HTTP Client Instrumentation (DNS, TLS, Connect)](33-tcp-udp-and-networking/12-http-client-instrumentation/12-http-client-instrumentation.md) | Advanced |
| 415 | [gRPC Streaming: Server, Client, Bidirectional](33-tcp-udp-and-networking/13-grpc-streaming/13-grpc-streaming.md) | Advanced |
| 416 | [gRPC Interceptors for Auth, Logging, Metrics](33-tcp-udp-and-networking/14-grpc-interceptors/14-grpc-interceptors.md) | Advanced |
| 417 | [Custom HTTP Transport with Pool Metrics](33-tcp-udp-and-networking/15-custom-http-transport/15-custom-http-transport.md) | Advanced |
| 418 | [Reverse Proxy with Header Manipulation](33-tcp-udp-and-networking/16-reverse-proxy-header-manipulation/16-reverse-proxy-header-manipulation.md) | Advanced |
| 419 | [WebSocket Binary Frames and Ping/Pong](33-tcp-udp-and-networking/17-websocket-binary-frames/17-websocket-binary-frames.md) | Advanced |
| 420 | [Connection Draining During Rolling Deploys](33-tcp-udp-and-networking/18-connection-draining/18-connection-draining.md) | Advanced |
| 421 | [Building a SOCKS5 Proxy](33-tcp-udp-and-networking/19-building-a-socks5-proxy/19-building-a-socks5-proxy.md) | Insane |
| 422 | [Implementing a Custom Wire Protocol](33-tcp-udp-and-networking/20-implementing-a-custom-wire-protocol/20-implementing-a-custom-wire-protocol.md) | Insane |
| 423 | [TCP Load Balancer with Health Checks](33-tcp-udp-and-networking/21-tcp-load-balancer/21-tcp-load-balancer.md) | Insane |
| 424 | [Building a Port Scanner](33-tcp-udp-and-networking/22-building-a-port-scanner/22-building-a-port-scanner.md) | Insane |
| 425 | [DNS Recursive Resolver with Caching](33-tcp-udp-and-networking/23-dns-recursive-resolver/23-dns-recursive-resolver.md) | Insane |
| 426 | [QUIC Transport Protocol Basics](33-tcp-udp-and-networking/24-quic-transport-protocol/24-quic-transport-protocol.md) | Insane |
| 427 | [HTTP/3 over QUIC](33-tcp-udp-and-networking/25-http3-over-quic/25-http3-over-quic.md) | Insane |
| 428 | [VPN Tunnel Implementation (TUN + Encryption)](33-tcp-udp-and-networking/26-vpn-tunnel-implementation/26-vpn-tunnel-implementation.md) | Insane |
| 429 | [NAT Traversal with STUN/TURN](33-tcp-udp-and-networking/27-nat-traversal-stun-turn/27-nat-traversal-stun-turn.md) | Insane |
| 430 | [Packet Sniffer with BPF Filters](33-tcp-udp-and-networking/28-packet-sniffer-bpf-filters/28-packet-sniffer-bpf-filters.md) | Insane |

### 34 - Runtime: Scheduler

| # | Exercise | Difficulty |
|---|----------|------------|
| 431 | [GMP Model: Goroutines, OS Threads, Processors](34-runtime-scheduler/01-gmp-model/01-gmp-model.md) | Advanced |
| 432 | [GOMAXPROCS and Processor Binding](34-runtime-scheduler/02-gomaxprocs-processor-binding/02-gomaxprocs-processor-binding.md) | Advanced |
| 433 | [Work Stealing and Load Balancing](34-runtime-scheduler/03-work-stealing/03-work-stealing.md) | Advanced |
| 434 | [Cooperative vs Preemptive Scheduling](34-runtime-scheduler/04-cooperative-vs-preemptive/04-cooperative-vs-preemptive.md) | Advanced |
| 435 | [runtime.Gosched and Yielding](34-runtime-scheduler/05-runtime-gosched/05-runtime-gosched.md) | Advanced |
| 436 | [Goroutine Stack Growth and Shrinking](34-runtime-scheduler/06-goroutine-stack-growth/06-goroutine-stack-growth.md) | Advanced |
| 437 | [Observing Scheduler with GODEBUG](34-runtime-scheduler/07-observing-scheduler-godebug/07-observing-scheduler-godebug.md) | Insane |
| 438 | [Scheduler Latency Analysis with Trace](34-runtime-scheduler/08-scheduler-latency-analysis/08-scheduler-latency-analysis.md) | Insane |
| 439 | [CPU Pinning and NUMA-Aware Scheduling](34-runtime-scheduler/09-cpu-pinning-numa/09-cpu-pinning-numa.md) | Insane |
| 440 | [Designing Scheduler-Friendly Algorithms](34-runtime-scheduler/10-designing-scheduler-friendly-algorithms/10-designing-scheduler-friendly-algorithms.md) | Insane |

### 35 - Runtime: Garbage Collector

| # | Exercise | Difficulty |
|---|----------|------------|
| 441 | [Tri-Color Mark and Sweep Algorithm](35-runtime-garbage-collector/01-tri-color-mark-and-sweep/01-tri-color-mark-and-sweep.md) | Advanced |
| 442 | [GC Phases: Mark, Sweep, Off](35-runtime-garbage-collector/02-gc-phases/02-gc-phases.md) | Advanced |
| 443 | [GOGC and GOMEMLIMIT Tuning](35-runtime-garbage-collector/03-gogc-and-gomemlimit/03-gogc-and-gomemlimit.md) | Advanced |
| 444 | [Write Barriers and GC Invariants](35-runtime-garbage-collector/04-write-barriers/04-write-barriers.md) | Advanced |
| 445 | [Observing GC with GODEBUG=gctrace=1](35-runtime-garbage-collector/05-observing-gc-godebug/05-observing-gc-godebug.md) | Advanced |
| 446 | [GC Pacer and Target Heap Size](35-runtime-garbage-collector/06-gc-pacer/06-gc-pacer.md) | Insane |
| 447 | [Soft Memory Limit (Go 1.19+)](35-runtime-garbage-collector/07-soft-memory-limit/07-soft-memory-limit.md) | Insane |
| 448 | [GC Impact on Tail Latency](35-runtime-garbage-collector/08-gc-impact-tail-latency/08-gc-impact-tail-latency.md) | Insane |
| 449 | [Reducing GC Pressure in Hot Paths](35-runtime-garbage-collector/09-reducing-gc-pressure/09-reducing-gc-pressure.md) | Insane |
| 450 | [Arena Allocation Patterns](35-runtime-garbage-collector/10-arena-allocation-patterns/10-arena-allocation-patterns.md) | Insane |

### 36 - Runtime: Compiler and Assembly

| # | Exercise | Difficulty |
|---|----------|------------|
| 451 | [Reading SSA Output with GOSSAFUNC](36-runtime-compiler-and-assembly/01-reading-ssa-output/01-reading-ssa-output.md) | Advanced |
| 452 | [Compiler Optimization Passes](36-runtime-compiler-and-assembly/02-compiler-optimization-passes/02-compiler-optimization-passes.md) | Advanced |
| 453 | [Inlining: Heuristics and go:noinline](36-runtime-compiler-and-assembly/03-inlining-heuristics/03-inlining-heuristics.md) | Advanced |
| 454 | [Bounds Check Elimination](36-runtime-compiler-and-assembly/04-bounds-check-elimination/04-bounds-check-elimination.md) | Advanced |
| 455 | [PGO: Profile-Guided Optimization](36-runtime-compiler-and-assembly/05-pgo-profile-guided-optimization/05-pgo-profile-guided-optimization.md) | Advanced |
| 456 | [Compiler Devirtualization](36-runtime-compiler-and-assembly/06-compiler-devirtualization/06-compiler-devirtualization.md) | Advanced |
| 457 | [Dead Code Elimination Analysis](36-runtime-compiler-and-assembly/07-dead-code-elimination/07-dead-code-elimination.md) | Advanced |
| 458 | [Runtime.SetFinalizer Patterns and Pitfalls](36-runtime-compiler-and-assembly/08-runtime-setfinalizer/08-runtime-setfinalizer.md) | Advanced |
| 459 | [Go Assembly Basics: Plan 9 Syntax](36-runtime-compiler-and-assembly/09-go-assembly-basics/09-go-assembly-basics.md) | Insane |
| 460 | [Writing Assembly Functions](36-runtime-compiler-and-assembly/10-writing-assembly-functions/10-writing-assembly-functions.md) | Insane |
| 461 | [SIMD with Assembly](36-runtime-compiler-and-assembly/11-simd-with-assembly/11-simd-with-assembly.md) | Insane |
| 462 | [Analyzing Compiler Output for Perf Bugs](36-runtime-compiler-and-assembly/12-analyzing-compiler-output/12-analyzing-compiler-output.md) | Insane |
| 463 | [Implementing a Custom Memory Allocator (Slab)](36-runtime-compiler-and-assembly/13-implementing-a-custom-memory-allocator/13-implementing-a-custom-memory-allocator.md) | Insane |
| 464 | [Writing a Goroutine-Aware Profiler](36-runtime-compiler-and-assembly/14-writing-a-goroutine-aware-profiler/14-writing-a-goroutine-aware-profiler.md) | Insane |
| 465 | [Implementing a Green Thread Scheduler](36-runtime-compiler-and-assembly/15-implementing-a-green-thread-scheduler/15-implementing-a-green-thread-scheduler.md) | Insane |

### 37 - Distributed Systems Fundamentals

| # | Exercise | Difficulty |
|---|----------|------------|
| 466 | [Consistent Hashing Ring](37-distributed-systems-fundamentals/01-consistent-hashing-ring/01-consistent-hashing-ring.md) | Advanced |
| 467 | [Implementing a Gossip Protocol](37-distributed-systems-fundamentals/02-implementing-a-gossip-protocol/02-implementing-a-gossip-protocol.md) | Advanced |
| 468 | [Leader Election with Bully Algorithm](37-distributed-systems-fundamentals/03-leader-election-bully-algorithm/03-leader-election-bully-algorithm.md) | Advanced |
| 469 | [Distributed Locking with Lease Mechanism](37-distributed-systems-fundamentals/04-distributed-locking/04-distributed-locking.md) | Advanced |
| 470 | [Vector Clocks and Causality Tracking](37-distributed-systems-fundamentals/05-vector-clocks/05-vector-clocks.md) | Advanced |
| 471 | [Raft Consensus: Leader Election](37-distributed-systems-fundamentals/06-raft-leader-election/06-raft-leader-election.md) | Insane |
| 472 | [Raft Consensus: Log Replication](37-distributed-systems-fundamentals/07-raft-log-replication/07-raft-log-replication.md) | Insane |
| 473 | [Raft Consensus: Snapshots](37-distributed-systems-fundamentals/08-raft-snapshots/08-raft-snapshots.md) | Insane |
| 474 | [CRDTs: Conflict-Free Replicated Data Types](37-distributed-systems-fundamentals/09-crdts/09-crdts.md) | Insane |
| 475 | [Merkle Tree for Data Verification](37-distributed-systems-fundamentals/10-merkle-tree/10-merkle-tree.md) | Insane |
| 476 | [Service Discovery with Health Checking](37-distributed-systems-fundamentals/11-service-discovery/11-service-discovery.md) | Insane |
| 477 | [Distributed Rate Limiter](37-distributed-systems-fundamentals/12-distributed-rate-limiter/12-distributed-rate-limiter.md) | Insane |
| 478 | [Sharded Key-Value Store](37-distributed-systems-fundamentals/13-sharded-key-value-store/13-sharded-key-value-store.md) | Insane |
| 479 | [Chaos Testing Framework](37-distributed-systems-fundamentals/14-chaos-testing-framework/14-chaos-testing-framework.md) | Insane |
| 480 | [Paxos Consensus Implementation](37-distributed-systems-fundamentals/15-paxos-consensus/15-paxos-consensus.md) | Insane |
| 481 | [Two-Phase Commit Protocol](37-distributed-systems-fundamentals/16-two-phase-commit/16-two-phase-commit.md) | Insane |
| 482 | [Saga Orchestrator with Compensation](37-distributed-systems-fundamentals/17-saga-orchestrator/17-saga-orchestrator.md) | Insane |
| 483 | [Event Sourcing Engine with Snapshotting](37-distributed-systems-fundamentals/18-event-sourcing-engine/18-event-sourcing-engine.md) | Insane |
| 484 | [CQRS with Eventual Consistency](37-distributed-systems-fundamentals/19-cqrs-eventual-consistency/19-cqrs-eventual-consistency.md) | Insane |
| 485 | [Distributed Transaction Coordinator](37-distributed-systems-fundamentals/20-distributed-transaction-coordinator/20-distributed-transaction-coordinator.md) | Insane |
| 486 | [Anti-Entropy Protocol for Replica Repair](37-distributed-systems-fundamentals/21-anti-entropy-protocol/21-anti-entropy-protocol.md) | Insane |
| 487 | [Failure Detector (Phi Accrual)](37-distributed-systems-fundamentals/22-failure-detector-phi-accrual/22-failure-detector-phi-accrual.md) | Insane |
| 488 | [Quorum-Based Replication (NWR Model)](37-distributed-systems-fundamentals/23-quorum-based-replication/23-quorum-based-replication.md) | Insane |
| 489 | [Consistent Prefix Reads](37-distributed-systems-fundamentals/24-consistent-prefix-reads/24-consistent-prefix-reads.md) | Insane |

### 38 - Capstone: Container Runtime

| # | Exercise | Difficulty |
|---|----------|------------|
| 490 | [Linux Namespaces from Go: UTS and PID](38-capstone-container-runtime/01-linux-namespaces-uts-pid/01-linux-namespaces-uts-pid.md) | Insane |
| 491 | [Mount Namespace and Root Filesystem](38-capstone-container-runtime/02-mount-namespace-root-filesystem/02-mount-namespace-root-filesystem.md) | Insane |
| 492 | [Network Namespace and Virtual Ethernet](38-capstone-container-runtime/03-network-namespace-veth/03-network-namespace-veth.md) | Insane |
| 493 | [Cgroups v2: CPU and Memory Limits](38-capstone-container-runtime/04-cgroups-v2-cpu-memory/04-cgroups-v2-cpu-memory.md) | Insane |
| 494 | [Overlay Filesystem for Container Layers](38-capstone-container-runtime/05-overlay-filesystem/05-overlay-filesystem.md) | Insane |
| 495 | [OCI Image Pulling and Unpacking](38-capstone-container-runtime/06-oci-image-pulling/06-oci-image-pulling.md) | Insane |
| 496 | [Container Lifecycle: Create, Start, Stop, Delete](38-capstone-container-runtime/07-container-lifecycle/07-container-lifecycle.md) | Insane |
| 497 | [Exec into Running Container](38-capstone-container-runtime/08-exec-into-running-container/08-exec-into-running-container.md) | Insane |
| 498 | [Container Networking: Bridge and NAT](38-capstone-container-runtime/09-container-networking-bridge-nat/09-container-networking-bridge-nat.md) | Insane |
| 499 | [Full OCI-Compliant Container Runtime](38-capstone-container-runtime/10-full-oci-container-runtime/10-full-oci-container-runtime.md) | Insane |

### 39 - Capstone: Database Engine

| # | Exercise | Difficulty |
|---|----------|------------|
| 500 | [Write-Ahead Log (WAL) Implementation](39-capstone-database-engine/01-write-ahead-log/01-write-ahead-log.md) | Insane |
| 501 | [B-Tree Index Implementation](39-capstone-database-engine/02-b-tree-index/02-b-tree-index.md) | Insane |
| 502 | [Buffer Pool Manager with LRU Eviction](39-capstone-database-engine/03-buffer-pool-manager/03-buffer-pool-manager.md) | Insane |
| 503 | [SQL Lexer and Tokenizer](39-capstone-database-engine/04-sql-lexer-and-tokenizer/04-sql-lexer-and-tokenizer.md) | Insane |
| 504 | [SQL Parser: SELECT, INSERT, CREATE TABLE](39-capstone-database-engine/05-sql-parser/05-sql-parser.md) | Insane |
| 505 | [Query Planner and Table Scan](39-capstone-database-engine/06-query-planner/06-query-planner.md) | Insane |
| 506 | [MVCC: Multi-Version Concurrency Control](39-capstone-database-engine/07-mvcc/07-mvcc.md) | Insane |
| 507 | [Transaction Manager: BEGIN, COMMIT, ROLLBACK](39-capstone-database-engine/08-transaction-manager/08-transaction-manager.md) | Insane |
| 508 | [Network Protocol: Wire Format and Client](39-capstone-database-engine/09-network-protocol-wire-format/09-network-protocol-wire-format.md) | Insane |
| 509 | [Full Embedded Database with SQL Interface](39-capstone-database-engine/10-full-embedded-database/10-full-embedded-database.md) | Insane |

### 40 - Capstone: Language Interpreter

| # | Exercise | Difficulty |
|---|----------|------------|
| 510 | [Lexer: Tokenizing a Simple Language](40-capstone-language-interpreter/01-lexer-tokenizing/01-lexer-tokenizing.md) | Insane |
| 511 | [Parser: Pratt Parsing for Expressions](40-capstone-language-interpreter/02-parser-pratt-parsing/02-parser-pratt-parsing.md) | Insane |
| 512 | [AST: Abstract Syntax Tree Representation](40-capstone-language-interpreter/03-ast-representation/03-ast-representation.md) | Insane |
| 513 | [Evaluator: Tree-Walking Interpreter](40-capstone-language-interpreter/04-evaluator-tree-walking/04-evaluator-tree-walking.md) | Insane |
| 514 | [Built-in Functions and Standard Library](40-capstone-language-interpreter/05-built-in-functions/05-built-in-functions.md) | Insane |
| 515 | [Closures and First-Class Functions](40-capstone-language-interpreter/06-closures-and-first-class-functions/06-closures-and-first-class-functions.md) | Insane |
| 516 | [REPL with Line Editing](40-capstone-language-interpreter/07-repl-with-line-editing/07-repl-with-line-editing.md) | Insane |
| 517 | [Full Interpreter: Monkey Language](40-capstone-language-interpreter/08-full-interpreter-monkey-language/08-full-interpreter-monkey-language.md) | Insane |

### 41 - Capstone: Message Queue

| # | Exercise | Difficulty |
|---|----------|------------|
| 518 | [In-Memory Topic and Subscription](41-capstone-message-queue/01-in-memory-topic-and-subscription/01-in-memory-topic-and-subscription.md) | Insane |
| 519 | [Persistent Message Storage with WAL](41-capstone-message-queue/02-persistent-message-storage/02-persistent-message-storage.md) | Insane |
| 520 | [Consumer Groups and Offset Tracking](41-capstone-message-queue/03-consumer-groups-offset-tracking/03-consumer-groups-offset-tracking.md) | Insane |
| 521 | [Producer API: Batching and Acknowledgments](41-capstone-message-queue/04-producer-api-batching/04-producer-api-batching.md) | Insane |
| 522 | [Consumer API: Pull-Based with Backpressure](41-capstone-message-queue/05-consumer-api-pull-based/05-consumer-api-pull-based.md) | Insane |
| 523 | [Message Retention and Compaction](41-capstone-message-queue/06-message-retention-and-compaction/06-message-retention-and-compaction.md) | Insane |
| 524 | [TCP Protocol for Client Connections](41-capstone-message-queue/07-tcp-protocol-client-connections/07-tcp-protocol-client-connections.md) | Insane |
| 525 | [Full Message Queue with Partitioning](41-capstone-message-queue/08-full-message-queue-partitioning/08-full-message-queue-partitioning.md) | Insane |

### 42 - Capstone: Service Mesh Data Plane

| # | Exercise | Difficulty |
|---|----------|------------|
| 526 | [L4 TCP Proxy with Connection Tracking](42-capstone-service-mesh-data-plane/01-l4-tcp-proxy/01-l4-tcp-proxy.md) | Insane |
| 527 | [L7 HTTP Proxy with Header-Based Routing](42-capstone-service-mesh-data-plane/02-l7-http-proxy/02-l7-http-proxy.md) | Insane |
| 528 | [mTLS Termination and Origination](42-capstone-service-mesh-data-plane/03-mtls-termination/03-mtls-termination.md) | Insane |
| 529 | [Load Balancing: Round-Robin, Least-Conn, Consistent Hash](42-capstone-service-mesh-data-plane/04-load-balancing/04-load-balancing.md) | Insane |
| 530 | [Health Checking: Active and Passive (Outlier Detection)](42-capstone-service-mesh-data-plane/05-health-checking/05-health-checking.md) | Insane |
| 531 | [Traffic Management: Retries, Timeouts, Circuit Breaking](42-capstone-service-mesh-data-plane/06-traffic-management/06-traffic-management.md) | Insane |
| 532 | [Rate Limiting Per Client/Route](42-capstone-service-mesh-data-plane/07-rate-limiting/07-rate-limiting.md) | Insane |
| 533 | [Observability: Metrics, Latency Histograms, Error Rates](42-capstone-service-mesh-data-plane/08-observability/08-observability.md) | Insane |
| 534 | [Control Plane Integration: gRPC Streaming Config](42-capstone-service-mesh-data-plane/09-control-plane-integration/09-control-plane-integration.md) | Insane |
| 535 | [Full Data Plane with All Features](42-capstone-service-mesh-data-plane/10-full-data-plane/10-full-data-plane.md) | Insane |

### 43 - Capstone: Stream Processing Engine

| # | Exercise | Difficulty |
|---|----------|------------|
| 536 | [Source Connectors: File, TCP, HTTP](43-capstone-stream-processing-engine/01-source-connectors/01-source-connectors.md) | Insane |
| 537 | [Operators: Map, Filter, FlatMap](43-capstone-stream-processing-engine/02-operators-map-filter-flatmap/02-operators-map-filter-flatmap.md) | Insane |
| 538 | [Windowing: Tumbling, Sliding, Session](43-capstone-stream-processing-engine/03-windowing/03-windowing.md) | Insane |
| 539 | [Watermarks and Late Data Handling](43-capstone-stream-processing-engine/04-watermarks-and-late-data/04-watermarks-and-late-data.md) | Insane |
| 540 | [Checkpointing for Exactly-Once Semantics](43-capstone-stream-processing-engine/05-checkpointing/05-checkpointing.md) | Insane |
| 541 | [Parallel Execution with Data Partitioning](43-capstone-stream-processing-engine/06-parallel-execution/06-parallel-execution.md) | Insane |
| 542 | [Sink Connectors: stdout, File, TCP](43-capstone-stream-processing-engine/07-sink-connectors/07-sink-connectors.md) | Insane |
| 543 | [Full Engine with Windowed Aggregation](43-capstone-stream-processing-engine/08-full-engine-windowed-aggregation/08-full-engine-windowed-aggregation.md) | Insane |

### 44 - Capstone: HTTP/2 Implementation

| # | Exercise | Difficulty |
|---|----------|------------|
| 544 | [Frame Parsing and Serialization](44-capstone-http2-implementation/01-frame-parsing-and-serialization/01-frame-parsing-and-serialization.md) | Insane |
| 545 | [HPACK Header Compression](44-capstone-http2-implementation/02-hpack-header-compression/02-hpack-header-compression.md) | Insane |
| 546 | [Stream Multiplexing and Flow Control](44-capstone-http2-implementation/03-stream-multiplexing-flow-control/03-stream-multiplexing-flow-control.md) | Insane |
| 547 | [Server Push](44-capstone-http2-implementation/04-server-push/04-server-push.md) | Insane |
| 548 | [Connection and Stream Error Handling](44-capstone-http2-implementation/05-connection-and-stream-error-handling/05-connection-and-stream-error-handling.md) | Insane |
| 549 | [Full HTTP/2 Server Serving Real Traffic](44-capstone-http2-implementation/06-full-http2-server/06-full-http2-server.md) | Insane |

### 45 - Capstone: Distributed Key-Value Store

| # | Exercise | Difficulty |
|---|----------|------------|
| 550 | [Partitioned Storage with Consistent Hashing](45-capstone-distributed-key-value-store/01-partitioned-storage/01-partitioned-storage.md) | Insane |
| 551 | [Replication with Configurable Consistency (ONE/QUORUM/ALL)](45-capstone-distributed-key-value-store/02-replication-configurable-consistency/02-replication-configurable-consistency.md) | Insane |
| 552 | [Anti-Entropy with Merkle Trees](45-capstone-distributed-key-value-store/03-anti-entropy-merkle-trees/03-anti-entropy-merkle-trees.md) | Insane |
| 553 | [Hinted Handoff for Temporary Failures](45-capstone-distributed-key-value-store/04-hinted-handoff/04-hinted-handoff.md) | Insane |
| 554 | [Read Repair on Divergence Detection](45-capstone-distributed-key-value-store/05-read-repair/05-read-repair.md) | Insane |
| 555 | [Membership Protocol with Failure Detection](45-capstone-distributed-key-value-store/06-membership-protocol/06-membership-protocol.md) | Insane |
| 556 | [Client Protocol: GET, PUT, DELETE with Consistency Levels](45-capstone-distributed-key-value-store/07-client-protocol/07-client-protocol.md) | Insane |
| 557 | [Full Distributed KV Store (Dynamo-like)](45-capstone-distributed-key-value-store/08-full-distributed-kv-store/08-full-distributed-kv-store.md) | Insane |

### 46 - Capstone: Concurrency Deep Dive

| # | Exercise | Difficulty |
|---|----------|------------|
| 558 | [Lock-Free MPMC Queue (CAS-based)](46-capstone-concurrency-deep-dive/01-lock-free-mpmc-queue/01-lock-free-mpmc-queue.md) | Insane |
| 559 | [Concurrent Skip List](46-capstone-concurrency-deep-dive/02-concurrent-skip-list/02-concurrent-skip-list.md) | Insane |
| 560 | [Hazard Pointer Memory Reclamation](46-capstone-concurrency-deep-dive/03-hazard-pointer-memory-reclamation/03-hazard-pointer-memory-reclamation.md) | Insane |
| 561 | [Epoch-Based Reclamation](46-capstone-concurrency-deep-dive/04-epoch-based-reclamation/04-epoch-based-reclamation.md) | Insane |
| 562 | [Work-Stealing Deque](46-capstone-concurrency-deep-dive/05-work-stealing-deque/05-work-stealing-deque.md) | Insane |
| 563 | [Software Transactional Memory (STM)](46-capstone-concurrency-deep-dive/06-software-transactional-memory/06-software-transactional-memory.md) | Insane |
| 564 | [Concurrent B-Tree with Lock Coupling](46-capstone-concurrency-deep-dive/07-concurrent-b-tree/07-concurrent-b-tree.md) | Insane |
| 565 | [Async/Await on Top of Channels](46-capstone-concurrency-deep-dive/08-async-await-on-channels/08-async-await-on-channels.md) | Insane |
| 566 | [Coroutine Library Using Goroutines](46-capstone-concurrency-deep-dive/09-coroutine-library/09-coroutine-library.md) | Insane |
| 567 | [Wait-Free Stack Implementation](46-capstone-concurrency-deep-dive/10-wait-free-stack/10-wait-free-stack.md) | Insane |
| 568 | [Double-Buffering for Producers/Consumers](46-capstone-concurrency-deep-dive/11-double-buffering/11-double-buffering.md) | Insane |
| 569 | [Ring Buffer with Lock-Free Reads](46-capstone-concurrency-deep-dive/12-ring-buffer-lock-free-reads/12-ring-buffer-lock-free-reads.md) | Insane |
| 570 | [Lock-Free Hash Map](46-capstone-concurrency-deep-dive/13-lock-free-hash-map/13-lock-free-hash-map.md) | Insane |

### 47 - Capstone: Systems and Kernel

| # | Exercise | Difficulty |
|---|----------|------------|
| 571 | [Direct Syscalls Without libc](47-capstone-systems-and-kernel/01-direct-syscalls/01-direct-syscalls.md) | Insane |
| 572 | [eBPF Tracing Program in Go](47-capstone-systems-and-kernel/02-ebpf-tracing/02-ebpf-tracing.md) | Insane |
| 573 | [Netlink Socket for Network Interface Mgmt](47-capstone-systems-and-kernel/03-netlink-socket/03-netlink-socket.md) | Insane |
| 574 | [FUSE Filesystem Implementation](47-capstone-systems-and-kernel/04-fuse-filesystem/04-fuse-filesystem.md) | Insane |
| 575 | [io_uring Integration for Async I/O](47-capstone-systems-and-kernel/05-io-uring-integration/05-io-uring-integration.md) | Insane |
| 576 | [seccomp Filter Program](47-capstone-systems-and-kernel/06-seccomp-filter/06-seccomp-filter.md) | Insane |
| 577 | [ptrace-Based Syscall Tracer](47-capstone-systems-and-kernel/07-ptrace-syscall-tracer/07-ptrace-syscall-tracer.md) | Insane |
| 578 | [Raw Socket Packet Capture](47-capstone-systems-and-kernel/08-raw-socket-packet-capture/08-raw-socket-packet-capture.md) | Insane |
| 579 | [Custom Network Protocol Stack (ARP + ICMP)](47-capstone-systems-and-kernel/09-custom-network-protocol-stack/09-custom-network-protocol-stack.md) | Insane |
| 580 | [Building a Go Language Server (LSP)](47-capstone-systems-and-kernel/10-building-a-go-language-server/10-building-a-go-language-server.md) | Insane |
| 581 | [Dead Code Elimination Tool using go/ssa](47-capstone-systems-and-kernel/11-dead-code-elimination-tool/11-dead-code-elimination-tool.md) | Insane |
| 582 | [Go-to-WebAssembly Compiler Frontend](47-capstone-systems-and-kernel/12-go-to-webassembly-compiler/12-go-to-webassembly-compiler.md) | Insane |
| 583 | [Interactive Go Debugger with ptrace](47-capstone-systems-and-kernel/13-interactive-go-debugger/13-interactive-go-debugger.md) | Insane |

---

## Statistics

| Level | Count | Percentage |
|-------|-------|------------|
| Basic | 68 | 14% |
| Intermediate | 128 | 26% |
| Advanced | 156 | 31% |
| Insane | 145 | 29% |
| **Total** | **497** | **100%** |

| Group | Sections | Exercises |
|-------|----------|-----------|
| Fundamentals (01-11) | 11 | 124 |
| Core (12-25) | 14 | 173 |
| Systems (26-30) | 5 | 91 |
| Cloud-Native and Advanced (31-33) | 3 | 47 |
| Runtime Internals (34-36) | 3 | 35 |
| Distributed Systems (37) | 1 | 24 |
| Capstones (38-47) | 10 | 94 |
| **Total** | **47** | **497** |
