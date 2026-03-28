# Dart & Flutter — From Zero to Insane

A hands-on curriculum that takes you from writing your first Dart variable to building
production-grade Flutter applications with custom render objects and shader effects.
Twenty sections, four difficulty levels per section, no hand-waving.

---

## Who This Is For

Anyone who wants to learn Dart and Flutter seriously. Whether you have never touched
Dart before or you are a senior engineer looking for a structured way to fill gaps,
there is a path through this material for you. The only assumption is that you have
*some* programming experience — you know what a variable is, you have written a loop
before, you understand the general idea of functions and objects.

## Pedagogical Approach

This curriculum is built on a deliberate combination of learning science frameworks,
not because buzzwords are fun, but because each one solves a specific problem:

- **Diataxis** — Each section separates tutorials (learning-oriented), how-to guides
  (task-oriented), explanations (understanding-oriented), and reference material.
  You always know *what kind* of content you are reading.
- **Bloom's Taxonomy** — Exercises progress from remembering and understanding (Basic)
  through applying and analyzing (Intermediate/Advanced) to evaluating and creating
  (Insane). The difficulty labels map directly to cognitive levels.
- **Dreyfus Model** — The four levels also track skill acquisition stages: novice,
  competent, proficient, expert. Each level changes not just difficulty but the type
  of scaffolding provided.
- **Cognitive Load Theory** — New concepts are introduced one at a time. Worked examples
  come before practice problems. Complexity is added only after fundamentals are solid.
- **Zone of Proximal Development (ZPD)** — Each level is designed to be just beyond
  what you can do comfortably at the previous level. Hints and progressive solutions
  bridge the gap.
- **Active Recall** — Every section requires you to write code, not just read it. The
  solution guides use progressive hints so you can try before you look.

---

## Prerequisites

- A computer with Dart SDK 3.x installed (Flutter SDK includes Dart)
- An editor with Dart/Flutter support (VS Code with Dart extension, or IntelliJ/Android Studio)
- For Flutter sections (09-20): Flutter SDK installed and at least one target platform configured (Chrome, iOS Simulator, or Android emulator)
- Basic programming literacy in any language
- Comfort with the command line

---

## How to Use This Curriculum

### The Four Difficulty Levels

Every section contains exercises at four levels. They are not just "harder versions of
the same thing" — each level changes what is expected of you:

| Level | What It Means | Time Estimate per Section | Scaffolding |
|-------|---------------|--------------------------|-------------|
| **Basic** | Core concepts, guided exercises, fill-in-the-blank style. You follow along and build understanding. | 1-2 hours | High — step-by-step instructions, starter code provided |
| **Intermediate** | Apply concepts to realistic problems. Less hand-holding, more decisions to make. | 2-4 hours | Medium — problem statement and constraints, some hints |
| **Advanced** | Combine multiple concepts, handle edge cases, think about trade-offs and design. | 4-8 hours | Low — problem statement only, you choose the approach |
| **Insane** | Production-level challenges, performance constraints, API design, internals deep-dives. | 8-16 hours | Minimal — open-ended problems, you define the scope |

Start at the level that feels *slightly* uncomfortable. If Basic is trivial, jump to
Intermediate. If Advanced feels impossible, you skipped something — go back.

### The Scaffolding Approach

Each section gradually removes support as you level up. At Basic, you get starter code
and clear instructions. By Insane, you get a problem statement and maybe a link to the
relevant Dart or Flutter source code. This is intentional — real engineering does not
come with a tutorial attached.

---

## Curriculum Map

| # | Section | Topics | Difficulty Range |
|---|---------|--------|-----------------|
| 01 | [Dart: Variables, Types & Operators](01-dart-variables-types/) | var/final/const, type system, records, pattern matching, null-aware operators | Basic to Insane |
| 02 | [Dart: Functions & Closures](02-dart-functions-closures/) | first-class functions, closures, higher-order functions, generators, tear-offs | Basic to Insane |
| 03 | [Dart: Control Flow & Collections](03-dart-control-flow-collections/) | switch expressions, pattern matching, List/Set/Map, iterables, collection operators | Basic to Insane |
| 04 | [Dart: Object-Oriented Programming](04-dart-oop/) | classes, mixins, sealed classes, extension types, class modifiers | Basic to Insane |
| 05 | [Dart: Async Programming](05-dart-async-programming/) | Future, Stream, Isolates, Zones, concurrency patterns | Basic to Insane |
| 06 | [Dart: Generics & Type System](06-dart-generics-type-system/) | generic classes/functions, bounded types, extension types, type reification | Basic to Insane |
| 07 | [Dart: Error Handling & Null Safety](07-dart-error-handling-null-safety/) | exceptions, Result pattern, sound null safety, zones | Basic to Insane |
| 08 | [Dart: Advanced](08-dart-advanced/) | code generation, FFI, zones, macros, package development | Basic to Insane |
| 09 | [Flutter: Setup & Widget Fundamentals](09-flutter-setup-widgets/) | widget tree, StatelessWidget, StatefulWidget, lifecycle, Keys, BuildContext | Basic to Insane |
| 10 | [Flutter: Layouts](10-flutter-layouts/) | constraints model, Row/Column, Flex, Stack, responsive design, LayoutBuilder | Basic to Insane |
| 11 | [Flutter: Navigation & Routing](11-flutter-navigation-routing/) | Navigator, GoRouter, deep linking, route guards, nested navigation | Basic to Insane |
| 12 | [Flutter: State Management Basics](12-flutter-state-basics/) | setState, InheritedWidget, Provider, ValueNotifier | Basic to Insane |
| 13 | [Flutter: Forms & User Input](13-flutter-forms-input/) | Form, validation, focus, gestures, drag-and-drop, accessibility | Basic to Insane |
| 14 | [Flutter: Networking & Data](14-flutter-networking-data/) | HTTP, Dio, JSON serialization, offline-first, WebSockets, repository pattern | Basic to Insane |
| 15 | [Flutter: State Management Advanced](15-flutter-state-advanced/) | Riverpod, Bloc/Cubit, state modeling, persistence, testing | Basic to Insane |
| 16 | [Flutter: Animations](16-flutter-animations/) | implicit, explicit, staggered, Hero, physics-based, CustomPainter particles | Basic to Insane |
| 17 | [Flutter: Testing](17-flutter-testing/) | unit, widget, integration, golden tests, mocking, coverage | Basic to Insane |
| 18 | [Flutter: Architecture](18-flutter-architecture/) | Clean Architecture, MVVM, repository pattern, DI, modularization | Basic to Insane |
| 19 | [Flutter: Performance](19-flutter-performance/) | DevTools, profiling, build optimization, memory, shader jank | Basic to Insane |
| 20 | [Flutter: Advanced UI](20-flutter-advanced-ui/) | CustomPainter, Slivers, Platform Channels, plugins, shaders, RenderObject | Basic to Insane |

---

## Suggested Learning Paths

Not everyone needs to do everything. Pick the path that matches where you are and where
you want to go.

### Complete Beginner
Sections 01 through 09, Basic exercises only. This gives you a solid foundation in Dart
and gets you building your first Flutter widgets. Come back for Intermediate once you
have built something on your own.

### Mobile Developer
All 20 sections, Basic and Intermediate exercises. This covers everything you need to
build and ship real applications. Skip Advanced/Insane unless a specific topic grabs you.

### Senior Engineer
All 20 sections, all four levels. You probably already know some of this — skim the
Basic level to calibrate, then push into Advanced and Insane where the real depth lives.

### Dart-Only
Sections 01 through 08. Covers the full Dart language without any Flutter dependency.
Good for backend Dart, CLI tools, or just understanding the language before touching
the framework.

### Flutter-Focused (Already Knows Dart)
Sections 09 through 20. Jumps straight into Flutter assuming you are comfortable with
Dart fundamentals. If you hit a gap, the corresponding Dart section (01-08) is there
as a reference.

---

## What Each Section Contains

Every section folder includes two files:

- **README.md** — The main learning material. Explains concepts with examples, then
  presents exercises organized by difficulty level (Basic, Intermediate, Advanced,
  Insane). Each exercise has clear objectives, constraints, and acceptance criteria.

- **SOLUTION.md** — The solution companion. Organized to match the exercises, with:
  - Progressive hints (try these before reading the full solution)
  - Complete, working solutions with commentary
  - Common mistakes and why they happen
  - Deep-dive explanations for the curious
  - Links to relevant Dart/Flutter source code and documentation

The idea is simple: try the exercise first, use hints if stuck, check the solution to
learn what you missed, read the deep-dive to understand why.

---

## References

The pedagogical design of this curriculum draws from:

- **Diataxis** — Procida, D. *Diataxis: A systematic approach to technical documentation authoring.* [diataxis.fr](https://diataxis.fr)
- **Bloom's Taxonomy** — Anderson, L.W. & Krathwohl, D.R. (2001). *A Taxonomy for Learning, Teaching, and Assessing.* Longman.
- **Dreyfus Model of Skill Acquisition** — Dreyfus, S.E. & Dreyfus, H.L. (1980). *A Five-Stage Model of the Mental Activities Involved in Directed Skill Acquisition.* UC Berkeley.
- **Cognitive Load Theory** — Sweller, J. (1988). *Cognitive Load During Problem Solving: Effects on Learning.* Cognitive Science, 12(2).
- **Zone of Proximal Development** — Vygotsky, L.S. (1978). *Mind in Society: The Development of Higher Psychological Processes.* Harvard University Press.
- **Active Recall / Testing Effect** — Roediger, H.L. & Butler, A.C. (2011). *The Critical Role of Retrieval Practice in Long-Term Retention.* Trends in Cognitive Sciences, 15(1).
