# Tendencias Tecnológicas 2020–2026: Guía para el Desarrollador Senior

> Documento de referencia denso. Cada sección cubre origen, evolución año a año, por qué importa y estado actual en 2026.

---

## Índice

1. [Paradigmas de Programación](#1-paradigmas-de-programación)
2. [Lenguajes Emergentes](#2-lenguajes-emergentes)
3. [Sistemas de Tipos Avanzados](#3-sistemas-de-tipos-avanzados)
4. [Verificación Formal](#4-verificación-formal)
5. [WebAssembly: Evolución Año a Año](#5-webassembly-evolución-año-a-año)
6. [Modelos de Concurrencia](#6-modelos-de-concurrencia)
7. [Compiladores y Runtimes](#7-compiladores-y-runtimes)
8. [Hardware-Software Codesign](#8-hardware-software-codesign)
9. [Sistemas Distribuidos](#9-sistemas-distribuidos)
10. [Infraestructura AI/ML para Devs](#10-infraestructura-aiml-para-devs)
11. [Seguridad desde Fundamentos](#11-seguridad-desde-fundamentos)
12. [Observabilidad Avanzada](#12-observabilidad-avanzada)
13. [Bases de Datos: Nuevos Paradigmas](#13-bases-de-datos-nuevos-paradigmas)
14. [Edge Computing y Serverless](#14-edge-computing-y-serverless)

---

## 1. Paradigmas de Programación

### 1.1 Efectos Algebraicos (*Algebraic Effects*)

**Origen:** Teoría académica de los 2000s (Plotkin & Pretnar). El paper fundacional "Handlers of Algebraic Effects" (2009) establece la base formal.

**¿Qué son?** Un mecanismo para separar la *descripción* de un efecto computacional (lanzar una excepción, leer estado, hacer IO) de su *interpretación*. Es como monads pero composables sin transformers. El código declara qué efectos usa; el "handler" (manejador) define cómo se ejecutan.

```
// Pseudocódigo conceptual
effect Log:
  log(msg: String) -> Unit

effect State<S>:
  get() -> S
  put(s: S) -> Unit

// La función declara sus efectos en la firma
def compute(): Int uses [Log, State<Int>] =
  log("starting")
  n = get()
  put(n + 1)
  n + 1

// El handler decide cómo interpretarlos
run compute() with
  Log -> { log(msg) => println(msg); resume() }
  State -> { get() => resume(42); put(s) => resume() }
```

**Cronología:**

| Año | Hito |
|-----|------|
| Pre-2020 | Lenguajes de investigación: **Eff** (Matija Pretnar), **Frank**, **Effekt**. Koka iniciado por Daan Leijen (Microsoft Research). |
| 2020 | Koka 2.0 con inferencia de efectos. Algebraic effects como tema recurrente en conferencias ICFP/POPL. |
| 2021 | OCaml Multicore mergeado al trunk con effects *no tipados* (untyped effects como primitiva de bajo nivel). Debate sobre efectos tipados vs no tipados. |
| 2022 | **OCaml 5.0** lanzado en diciembre. Primera implementación *production-grade* de effect handlers. Industria presta atención real. |
| 2023 | Eio (IO library para OCaml 5) usa efectos para IO directo sin monads. Proliferación de librerías del ecosistema. |
| 2024 | Debate en el equipo de Scala sobre efectos tipados (Scala 3 Capabilities). Propuestas para Haskell con `!` notation. |
| 2025 | Wasm Effect Handlers propuesto como extensión del estándar. Effekt language madura con tipos de efectos polimórficos. |
| 2026 | Efectos algebraicos se han convertido en fundamento teórico de los nuevos lenguajes de sistemas (Roc, Unison). Koka considerado referencia de diseño. |

**Por qué importa:** Elimina el "efecto viral" de async/await y monads. En lugar de que `async` contamine toda la cadena de llamadas, un efecto se declara y el runtime lo maneja de forma transparent. React Hooks fue una implementación ad-hoc de una idea parecida.

**Estado 2026:** OCaml 5.x es la implementación production-ready más madura. Koka sigue siendo el lenguaje de referencia académica con mejor ergonomía. El concepto está infiltrando el diseño de Rust (colored functions debate), Swift Concurrency, y los nuevos lenguajes funcionales.

---

### 1.2 Programación Funcional Aplicada

**El giro de 2020–2026:** FP dejó de ser "académico" y pasó a ser *la arquitectura por defecto* en tooling, compiladores, y sistemas de datos. El vector no fue convencer a los desarrolladores de usar Haskell, sino que los lenguajes mainstream adoptaron las ideas.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Rust se adopta masivamente. Su ownership/borrowing es linear types aplicado. TypeScript 4.x con template literal types. |
| 2021 | Scala 3 lanzado con union types, intersection types, opaque types. Kotlin Coroutines maduro. |
| 2022 | OCaml 5.0. Python type system madura (PEP 673, PEP 675). Elm mantiene su nicho. |
| 2023 | Gleam 1.0 RC. Elixir adopta pattern matching más expresivo. F# 8 mejora performance. |
| 2024 | Gleam 1.0 estable. Roc avanza hacia producción. Haskell GHC 9.6+ con mejoras dramáticas de performance. |
| 2025 | FP como herramienta estándar en compilers (MLIR dialects son ADTs). LLM inference en FP es mainstream. |
| 2026 | El debate no es "FP vs OOP" sino qué nivel de pureza conviene por dominio. |

**Ideas FP que ganaron adopción mainstream:**
- **Inmutabilidad por defecto:** Rust, Kotlin, Swift, incluso Java Records (2021)
- **Pattern matching:** Python 3.10+ (2021), Java 21 (2023), C# 8+
- **Tipos suma / union types:** TypeScript (maduros), Python, Kotlin sealed classes
- **Composición sobre herencia:** Go, Rust traits, Kotlin delegation
- **Efectos controlados:** Rust borrow checker como sistema de efectos implícito

---

### 1.3 Programación Lógica: El Renacimiento Silencioso

**Prolog nunca desapareció**, pero su renacimiento viene de un vector inesperado: la inferencia probabilística y la IA neuro-simbólica.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Datalog como base de sistemas de análisis estático: Soufflé (usado en DOOP, Semmle/CodeQL de GitHub). |
| 2021 | CodeQL adquirido por GitHub. Millones de repos analizados con Datalog. |
| 2022 | Yedalog, XTDB (base de datos con Datalog como query language). Mercury y Ciao siguen activos. |
| 2023 | Minikanren y miniKanren influencia en LLM reasoning. LLMs como "buscadores de unificación aproximada". |
| 2024 | Datalog embebido en Datomic, XTDB 2.0. Julia tiene Metatheory.jl (e-graphs como reescritura de términos). |
| 2025 | ASP (Answer Set Programming) con Clingo usado en planning de agentes AI. Investigación neuro-simbólica. |
| 2026 | Lógica de Horn como lenguaje de reglas de negocio verificable. Datalog como IR para análisis de programas. |

**Por qué importa:** Si entiendes Datalog, entiendes cómo funciona CodeQL (análisis de vulnerabilidades en GitHub), cómo Datomic/XTDB modelan tiempo, y cómo los sistemas de reglas de negocio pueden ser verificables.

---

## 2. Lenguajes Emergentes

### 2.1 Rust

**Origen:** 2010 (Mozilla). 1.0 en 2015. El período 2020–2026 es su *consolidación industrial*.

**Cronología detallada:**

| Año | Hito |
|-----|------|
| 2020 | Rust Foundation creada. Primer año en top 5 del "Most Loved" de Stack Overflow (lo mantiene hasta 2026). Amazon, Microsoft, Google contribuyen activamente. |
| 2021 | **Rust 2021 Edition.** Mejoras en closures, resolver imports, disjoint captures. Tokio 1.0 estable. async/await maduro pero "colored functions" sigue siendo el punto de dolor. |
| 2022 | Linux kernel anuncia soporte oficial para Rust. Rust in Android. AWS reescribe partes de Firecracker. `rustc` puede compilarse a sí mismo. |
| 2023 | Primeros drivers Rust en Linux 6.1. Windows kernel drivers en Rust. NSA publica advisory recomendando Rust. Memory safety como imperativo de seguridad nacional (CISA). |
| 2024 | **Rust 2024 Edition** (Rust 1.85, Feb 2025). Async closures estabilizadas. `gen` keyword reservado para generators. `!` type fallback cambiado. Rust en AWS Lambda runtime oficial. |
| 2025 | Rust en Linux kernel declarado **no experimental** (diciembre 2025). Rust 1.8x con mejoras en ergonomics. async traits completamente estabilizadas. |
| 2026 | Rust es el lenguaje por defecto para nuevo código de sistemas en la mayoría de BigTech. TIOBE top 15, pero influencia desproporcionada en infra crítica. |

**Conceptos clave para entender Rust:**
- **Ownership + Borrow Checker:** Lineal types aplicados. Cada valor tiene un dueño único. Las referencias (&T, &mut T) son "préstamos" con lifetime acotado.
- **Lifetimes (`'a`):** Garantía estática de que referencias no sobreviven al dato que apuntan. Elimina use-after-free, dangling pointers sin GC.
- **Traits como interfaces:** Coherencia global (orphan rule). Trait objects (`dyn Trait`) como polimorfismo dinámico. Generics con monomorphization.
- **Zero-cost abstractions:** El compilador elimina abstracciones. Un iterador encadenado genera el mismo código que un loop manual.
- **async/await:** Stackless coroutines. El compilador transforma async fn en state machines. No hay runtime obligatorio; Tokio, async-std son opcionales.

**Estado 2026:** El ecosistema tiene 150k+ crates en crates.io. La pain area más real es compilación lenta (mejorada pero sigue siendo 3–5x más lenta que Go). La ergonomía de async sigue siendo compleja pero funcional.

---

### 2.2 Zig

**Origen:** 2016 (Andrew Kelley). Propuesta: hacer un C mejor, no un Rust.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Zig 0.6/0.7. `comptime` ya es la killer feature. Bun.sh empieza a usar Zig como herramienta de build. |
| 2021 | Zig 0.8. Cross-compilation trivial. `zig cc` como drop-in compiler para C/C++. |
| 2022 | Zig 0.9/0.10. TigerBeetle anuncia que están usando Zig en producción para su DB financiera. Bun 0.1 alpha en Zig. |
| 2023 | Zig 0.11. Async/await *removido* del lenguaje (decisión polémica; se moverá a librería). Zig Software Foundation. Bun 1.0 estable. |
| 2024 | Zig 0.12/0.13. Build system propio madura. Zig como herramienta de cross-compilation para Rust/C. |
| 2025 | Zig 0.14. 42k+ GitHub stars. Self-hosted compiler en producción. Ecosistema creciendo. |
| 2026 | Zig 0.15.x estable. Se espera 1.0 en 2027. Adoptado en infra de Cloudflare, varios startups de sistemas. |

**`comptime`: La idea central de Zig**

```zig
// comptime permite generics sin sintaxis especial
fn max(comptime T: type, a: T, b: T) T {
    return if (a > b) a else b;
}

// Ejecutado en tiempo de compilación
const x = max(i32, 10, 20); // type conocido en compile-time

// Reflection en compile-time
fn printFields(comptime T: type, value: T) void {
    comptime {
        const fields = std.meta.fields(T);
        inline for (fields) |field| {
            std.debug.print("{s}: {any}\n", .{field.name, @field(value, field.name)});
        }
    }
}
```

**Por qué importa:** `comptime` unifica templates, macros, generics, y metaprogramming en un único mecanismo. El código comptime se ejecuta con el mismo lenguaje, no un DSL especial. Esto elimina la complejidad de los sistemas de macros de C++/Rust.

**Diferencia filosófica con Rust:** Zig no previene todos los bugs en tiempo de compilación. Confía más en el programador pero provee herramientas excelentes (safety checks opcionales, undefined behavior detectado en debug mode). Es *pragmático* donde Rust es *prescriptivo*.

---

### 2.3 Gleam

**Origen:** 2019 (Louis Pilfold). Lenguaje funcional tipado que compila a Erlang VM (BEAM) y JavaScript.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020–2022 | Desarrollo activo. Sintaxis influenciada por Rust/Elm. Sistema de tipos sólido con inferencia. |
| 2023 | Gleam 0.x maduración. Comunidad crece en el espacio Elixir/Erlang. Stdlib completa. |
| 2024 | **Gleam 1.0** (marzo 2024). Primera versión estable. Garantías de estabilidad de API. |
| 2025 | Gleam 1.x series. Crecimiento en aplicaciones backend concurrentes. Target JS mejorado. |
| 2026 | Adoptado en fintech y telecom donde BEAM es estándar. Nicho claro: tipos estáticos sobre BEAM. |

**Por qué importa:** Aprovecha la confiabilidad legendaria de Erlang/BEAM (telecomunicaciones con 99.9999999% uptime) con un sistema de tipos moderno. Si necesitas concurrencia masiva y tolerancia a fallos, BEAM es imbatible. Gleam hace eso accesible sin la sintaxis de Erlang.

---

### 2.4 Koka

**Origen:** Daan Leijen (Microsoft Research), ~2012. Pero la versión moderna y madura es de 2020+.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Koka 2.0. Inferencia de efectos completa. Paper sobre "Perceus: Garbage-Free Reference Counting with Reuse" (PLDI 2021). |
| 2021 | Paper PLDI sobre reference counting eficiente sin GC usando linear types. Koka demuestra que FP puede ser tan rápido como C en ciertos casos. |
| 2022–2024 | Koka como lenguaje de referencia académica para efectos. Usado en investigación de PL. |
| 2025–2026 | No es lenguaje de producción, pero es el *laboratorio de ideas* más influyente para diseño de lenguajes. |

**La innovación de Perceus:** Koka usa reference counting con optimización de *reuse*. Si una función consume un dato y produce uno del mismo tamaño, reutiliza la memoria en lugar de allocar/free. Esto da GC-free FP con performance cercana a Rust en algunos benchmarks.

---

### 2.5 Unison

**Origen:** 2019 (Paul Chiusano, Arya Irani). Lenguaje funcional con idea radical: el código se almacena como AST con hash, no como texto.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020–2022 | Alpha/beta. Share.unison.cloud como plataforma. Distributed computing como ciudadano de primera clase. |
| 2023 | Unison Cloud en beta. El modelo de "abilities" (efectos algebraicos) como sistema de effectos. |
| 2024 | Unison Share como repositorio de código hash-addressed. |
| 2025–2026 | Nicho en sistemas distribuidos tipo actor con garantías fuertes. |

**La idea radical:** En Unison, el nombre de una función no importa; lo que importa es su *hash de contenido*. Renombrar una función no rompe nada. Los refactors son seguros por diseño. El versionado es inherente al sistema. Es como si Git estuviera integrado en el lenguaje.

---

### 2.6 Roc

**Origen:** Richard Feldman (autor de Elm in Action), ~2020. Goal: Elm-like pureza + performance de Go.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020–2023 | Desarrollo activo, diseño del compilador. |
| 2024 | Compilador funcional, benchmarks prometedores. 0.x releases. |
| 2025 | Plataformas (platforms) como concepto para manejar effects en los bordes del sistema. |
| 2026 | Pre-1.0 pero con adoptadores tempranos. Nicho: aplicaciones puras + performance. |

**La innovación:** "Platforms" en Roc separan la lógica pura (100% testeable, sin effects) de la plataforma que provee IO, red, etc. El código Roc no puede hacer IO directamente; la plataforma lo hace. Es effects algebraicos con una boundary arquitectónica hard.

---

### 2.7 Carbon

**Origen:** 2022 (Google, Chandler Carruth). Sucesor experimental de C++.

**Cronología:**

| Año | Hito |
|-----|------|
| 2022 | Anuncio público en CppNorth. 33k GitHub stars en días. Interoperabilidad bidireccional con C++ como goal #1. |
| 2023 | Compilador en desarrollo activo. Se aclara que es *experimental*, no production-ready. |
| 2024 | 5,100+ commits. Aproximándose a milestone 0.1. Foco en toolchain robusta. |
| 2025 | 0.1 milestone. Primer código Carbon en producción (interno Google). |
| 2026 | Lenguaje viable para migración gradual de código C++ legacy. No reemplaza Rust para código nuevo. |

**Por qué importa:** El problema de C++ es su deuda técnica de 40 años. Carbon permite migrar *un archivo a la vez* sin romper el resto del código C++. Es una estrategia de migración, no una revolución.

---

### 2.8 Mojo

**Origen:** 2023 (Modular, Chris Lattner). Python compatible, performance de C/C++ para ML.

**Cronología:**

| Año | Hito |
|-----|------|
| 2023 | Anuncio de Mojo. Benchmarks: 35,000x más rápido que Python nativo en algunos casos. Buzz masivo. |
| 2024 | Mojo SDK público. Open-source parcial. Sistema de tipos con MLIR backend. |
| 2025 | Mojo en camino a 1.0. Usado en Modular Inference Engine (MAX). |
| 2026 | Mojo como lenguaje de kernels para ML. Competencia con Triton de OpenAI. |

**Por qué importa:** Python es lento porque no tiene información de tipos en runtime. Mojo añade tipos, ownership a lo Rust, y SIMD/GPU como ciudadanos de primera clase, todo con sintaxis Python compatible. Permite escribir los *kernels* que PyTorch delega a CUDA, pero en Python-like.

---

## 3. Sistemas de Tipos Avanzados

Esta sección es densa en teoría pero con implicaciones prácticas directas en los lenguajes que usas hoy.

### 3.1 Linear Types (Tipos Lineales)

**Concepto:** Un valor de tipo lineal debe usarse *exactamente una vez*. No puede ser copiado ni descartado sin manejarlo.

**Origen formal:** Lógica lineal de Girard (1987). Aplicación práctica a PL en los 90s.

**Cronología en producción:**

| Año | Hito |
|-----|------|
| Pre-2020 | Rust usa ownership como implementación pragmática de linear types (con `Copy` como escape hatch). |
| 2020 | Linear Haskell (propuesta GHC) con `%1 ->` syntax. Paper en POPL 2018, implementación GHC 9.x. |
| 2021 | Linear Haskell en GHC 9.0. Permite expresar `IO` sin monad en ciertos contextos. |
| 2022 | Idris 2 usa quantitative type theory (linear types generalizados). |
| 2023 | Rust 2021 Edition consolida linear types como mainstream. |
| 2024–2026 | Linear types como fundamento para memory safety sin GC en nuevos lenguajes. |

**Aplicación práctica:**
```haskell
-- Linear Haskell: el archivo DEBE ser cerrado (exactamente una vez)
withFile :: FilePath -> (Handle %1 -> IO ()) %1 -> IO ()

-- El compilador rechaza esto porque handle se usa dos veces:
bad :: Handle %1 -> IO ()
bad h = do
  hPutStrLn h "hello"
  hPutStrLn h "world"  -- ERROR: h ya fue consumido
```

**Por qué importa:** Los linear types son el fundamento teórico de por qué Rust es memory-safe. Entenderlos te permite razonar sobre cualquier lenguaje con ownership, diseñar APIs que son imposibles de usar mal, y entender las limitaciones del sistema (cuándo usar Rc/Arc vs ownership).

---

### 3.2 Session Types (Tipos de Sesión)

**Concepto:** Tipan el *protocolo de comunicación* entre procesos. Si el tipo dice que después de enviar un entero debes recibir un booleano, el compilador lo verifica.

**Cronología:**

| Año | Hito |
|-----|------|
| Pre-2020 | Fundamentos teóricos en linear logic + pi-calculus. Implementaciones en Haskell, Rust experimental. |
| 2020 | Paper "Verified Linear Session-Typed Concurrent Programming" (ICFP 2020). |
| 2022 | "Polymorphic Typestate for Session Types" resuelve limitaciones anteriores con higher-order polymorphism. |
| 2023 | PPDP 2023: typestate polimórfico como solución unificada. |
| 2024–2026 | Investigación activa. No mainstream aún, pero Rust experimenta con session types vía macros. |

**Por qué importa:** Imagina que el *protocolo HTTP* estuviera en el tipo del canal. El compilador rechazaría enviar un cuerpo antes de los headers. Errores de protocolo como "leer de un socket cerrado" serían imposibles en tiempo de compilación.

---

### 3.3 Typestate

**Concepto:** El tipo de un objeto cambia según su estado. Un `File` puede estar en estado `Closed` o `Open`, y solo el `Open` tiene el método `read()`.

**Cronología:**

| Año | Hito |
|-----|------|
| Pre-2020 | Conceptos en Rust con el patrón builder y tipos de estado como parámetros genéricos. |
| 2020–2022 | Rust community desarrolla el "typestate pattern" extensamente. |
| 2023 | Papers sobre typestate polimórfico habilitando session types. |
| 2024–2026 | Rust sin soporte nativo de typestate, pero el pattern es estándar en APIs críticas. |

**Ejemplo en Rust:**
```rust
// Typestate pattern: el estado está en el tipo
struct Connection<State> { inner: TcpStream, _state: PhantomData<State> }

struct Disconnected;
struct Connected;
struct Authenticated;

impl Connection<Disconnected> {
    fn connect(addr: &str) -> Result<Connection<Connected>, Error> { ... }
}

impl Connection<Connected> {
    fn authenticate(self, creds: Credentials) -> Result<Connection<Authenticated>, Error> { ... }
}

impl Connection<Authenticated> {
    fn query(&mut self, sql: &str) -> Result<Rows, Error> { ... }
}

// El compilador rechaza llamar query() sin autenticar primero
```

---

### 3.4 Dependent Types (Tipos Dependientes)

**Concepto:** Los tipos pueden *depender de valores*. El tipo de un array puede ser `Array n Int` donde `n` es un valor natural conocido en compile-time.

**Cronología:**

| Año | Hito |
|-----|------|
| Pre-2020 | Agda, Coq, Idris 1 son los lenguajes de referencia. Demasiado complejos para producción. |
| 2020 | F* (Microsoft Research/Inria) con dependent types para verificación de código C. Usado en miTLS y HACL*. |
| 2021 | Idris 2 (Edwin Brady) lanzado. Usa Quantitative Type Theory (QTT) que unifica linear + dependent types. |
| 2022 | F* genera código C verificado para librerías crypto en proyectos como OpenSSH. |
| 2023 | Lean 4 como lenguaje de programación + prover (ver sección 4). |
| 2024 | Investigación sobre "Graded Dependent Types" combinando quantitative reasoning. |
| 2025–2026 | Dependent types en producción para crypto y sistemas críticos. No para uso general. |

**Por qué importa:** Con dependent types puedes expresar `sort : (n: Nat) -> Vec n Int -> Vec n Int` — una función de sort que garantiza que la salida tiene el mismo tamaño que la entrada. O `div : Int -> (d: Int) -> d ≠ 0 -> Int` — división que *requiere* en el tipo que el divisor no sea cero.

---

### 3.5 Graded Types (Tipos Graduados)

**Concepto:** Generalización de linear types. En lugar de "exactamente una vez", puedes decir "a lo sumo N veces", "exactamente N veces", "cualquier número de veces". Los grados forman una estructura semiring.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | "Graded Modal Dependent Type Theory" presentado en TyDe @ ICFP 2020. |
| 2021 | "A Graded Dependent Type System with Usage-Aware Semantics" en POPL 2021. |
| 2022 | Granule language (UK, Dominic Orchard) implementa graded types. |
| 2023–2026 | Investigación activa. Granule como lenguaje experimental. Ideas entrando en diseño de lenguajes nuevos. |

**Por qué importa:** Graded types unifican análisis de seguridad de información (una variable sensible no puede "fluir" a un canal público — el grado de privacidad está en el tipo), linear types, y reasoning sobre complejidad computacional. Es la generalización teórica de todo el espacio de "cuántas veces puedes usar algo".

---

## 4. Verificación Formal

### 4.1 TLA+ y Model Checking

**Origen:** Leslie Lamport (Microsoft Research), 1999. El período 2020–2026 es de adopción industrial acelerada.

**¿Qué es?** Un lenguaje de especificación para sistemas concurrentes y distribuidos. Describes el comportamiento del sistema matemáticamente, y el TLC model checker verifica que las propiedades (invariantes, liveness) se cumplen explorando exhaustivamente el espacio de estados.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | AWS publica resultados extensos de TLA+ en producción: S3, DynamoDB, EBS. Blog post de Chris Newcombe sobre su uso. |
| 2021 | TLA+ Toolbox 1.7. TLAPS (proof system) madurando. Adopción en MongoDB, Azure CosmosDB. |
| 2022 | PlusCal (alto nivel sobre TLA+) más accesible. Cursos universitarios lo adoptan. |
| 2023 | Investigación sobre LLMs generando TLA+. TLA+ para verificar protocolos de blockchain. |
| 2024 | Paper "Towards Language Model Guided TLA+ Proof Automation". GPT-4 capaz de generar especificaciones básicas. |
| 2025 | TLA+ como herramienta estándar en diseño de protocolos distribuidos en FAANG. |
| 2026 | AI asiste en escritura y verificación. TLA+ adoptado en regulación financiera (MiFID compliance). |

**Caso real (AWS):** Amazon encontró 7 bugs críticos en algoritmos que *ya estaban en producción* usando TLA+. Uno era un bug de correctness en S3 que habría causado corrupción de datos en condiciones de race muy específicas, imposible de encontrar con tests.

**Limitaciones:** TLA+ especifica el diseño, no el código. Hay un "gap de implementación" — el código puede no corresponder a la spec. El state-space explosion limita la escala del modelo verificable.

---

### 4.2 Dafny

**Origen:** Microsoft Research, 2009. Pero su adopción crece 2020–2026.

**¿Qué es?** Un lenguaje de programación imperativo donde puedes escribir *precondiciones, postcondiciones e invariantes* que el verificador (basado en Z3 SMT solver) verifica automáticamente.

```dafny
// La especificación es parte del código
method BinarySearch(a: array<int>, key: int) returns (index: int)
  requires forall i, j :: 0 <= i < j < a.Length ==> a[i] <= a[j]  // sorted
  ensures 0 <= index ==> index < a.Length && a[index] == key
  ensures index < 0 ==> forall i :: 0 <= i < a.Length ==> a[i] != key
{
  var lo, hi := 0, a.Length;
  while lo < hi
    invariant 0 <= lo <= hi <= a.Length
    invariant forall i :: (0 <= i < lo || hi <= i < a.Length) ==> a[i] != key
  {
    var mid := (lo + hi) / 2;
    if a[mid] < key      { lo := mid + 1; }
    else if key < a[mid] { hi := mid; }
    else                 { return mid; }
  }
  return -1;
}
```

**Cronología:**

| Año | Hito |
|-----|------|
| 2020–2021 | Dafny usado en cursos de PL en MIT, CMU. Research en crypto verification. |
| 2022 | Dafny open-sourced bajo MIT license. AWS Lambda Verified Execution (usa Dafny internamente). |
| 2023 | LLM + Dafny: GPT-4 genera código Dafny verificable. Mejora de LLM-generated verified code de 68% a 96%. |
| 2024 | Dafny 4.x con mejoras de ergonomía. Integración en VS Code. |
| 2025 | Veil: framework auto-activo de verificación en Lean que aprende de Dafny. |
| 2026 | Dafny como herramienta estándar en verificación de algoritmos críticos + LLM acceleration. |

---

### 4.3 Lean 4: La Revolución de la Matemática Formal

**Origen:** Leonardo de Moura (Microsoft Research → AWS), Lean 4 reescrito desde cero ~2018–2023.

**Lean 4 es único:** Es simultáneamente un *lenguaje de programación funcional* y un *proof assistant*. El mismo código puede ser un programa ejecutable y una demostración matemática.

**Cronología:**

| Año | Hito |
|-----|------|
| 2021 | Lean 4 release inicial. Reescrito completamente; Lean 4 es self-hosted. |
| 2022 | Mathlib4 (port de Mathlib3 a Lean 4). Comunidad masiva de matemáticos comienza migración. |
| 2023 | **Lean 4.0 estable** (8 septiembre 2023). Mathlib con 1M+ líneas, 100k+ definiciones. |
| 2024 | **AlphaProof** (Google DeepMind): AI que resuelve problemas de la Olimpiada Internacional de Matemáticas a nivel de medalla de plata, usando Lean. AWS usa Mathlib para verificar algoritmos de privacidad diferencial en Clean Rooms. |
| 2025 | Mathlib supera 210,000 teoremas formalizados. Integración con LLMs para asistencia de pruebas. |
| 2026 | **Leanstral** (Mistral AI, marzo 2026): primer agente AI open-source específicamente diseñado para Lean 4, 120B parámetros, 6B activos, Apache 2.0. Lean adoptado en verificación de contratos inteligentes (StarkWare Cairo verificado con Mathlib). |

**Por qué importa para un dev:** Lean 4 es el punto de convergencia entre matemáticas formales y software engineering. Las ideas que estaba madurando Lean influyen directamente en:
- Herramientas de análisis estático de próxima generación
- Verificación de código generado por LLMs
- Protocolos criptográficos verificados formalmente
- Compiladores que garantizan correctness de optimizaciones

---

### 4.4 Coq / Rocq

**Origen:** INRIA, 1989. Llamado **Rocq** desde 2024 (renaming oficial).

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | CompCert (compilador C verificado en Coq) es el estándar de oro para código crítico (aviación, nuclear). |
| 2021 | VST (Verified Software Toolchain): verificación de código C a nivel de máquina con Coq. |
| 2022 | FSCQ (file system verificado). Coq usado en verificación de chip RISC-V en hardware real. |
| 2023 | Iris framework (concurrent separation logic en Coq) madura. Usado para verificar Rust safe abstractions. |
| 2024 | **Rocq** como nuevo nombre oficial. Coq 8.19+. Mejor integración con LLMs. |
| 2025–2026 | Rocq + LLMs: asistencia automática en pruebas complejas. SerAPI para interaction programática. |

**Diferencia con Lean:** Coq/Rocq tiene décadas de madurez y librería de pruebas verificadas. Lean 4 es más moderno con mejor ergonomía y es simultaneously lenguaje de programación productivo. Para matemáticas nuevas, la comunidad se está moviendo a Lean.

---

## 5. WebAssembly: Evolución Año a Año

### Timeline Completo

**2019 — Fundamentos:**
- WebAssembly 1.0 se convierte en estándar W3C (diciembre 2019).
- WASI Preview 1: filesystem básico, args, variables de entorno. Solo para experimentos.
- Runtimes: Wasmtime, Wasmer, WasmEdge en alpha/beta.

**2020 — Más allá del browser:**
- Fastly lanza Compute@Edge con Wasm. Primera plataforma edge con Wasm en producción.
- Lucet (AOT Wasm compiler de Fastly) open-sourced.
- SIMD proposal finalizado: Wasm puede usar instrucciones SIMD del host.
- Multi-value returns: Wasm puede retornar múltiples valores (habilitador para GC proposal).

**2021 — El Component Model nace:**
- **Luke Wagner** (Mozilla/Fastly) comienza a definir el Component Model.
- Interface Types proposal: strings, records, variants entre módulos Wasm.
- Threads proposal estabilizado: SharedArrayBuffer + atomics para paralelismo real.
- Cloudflare Workers migra completamente a Wasm (V8 isolates + Wasm modules).
- Bytecode Alliance crece: Microsoft, Google, Fastly, Intel.

**2022 — Madurez de toolchain:**
- `wasm-tools` CLI de Bytecode Alliance como herramienta de referencia.
- WIT (WebAssembly Interface Types) language para describir interfaces de componentes.
- Wasmtime 1.0 estable.
- WASI Preview 2 en borrador con networking (WASI sockets).
- Docker + Wasm: OCI artifacts para módulos Wasm.
- Docker Desktop anuncia soporte experimental para Wasm en place of containers.

**2023 — Component Model en beta:**
- WASI Preview 2 beta con sockets HTTP, TCP/UDP.
- `wasm-pack`, `cargo-component` para Rust → Wasm workflows.
- Spin (Fermyon): framework serverless basado en Wasm components.
- wasmCloud: actor model sobre Wasm.
- "Worlds" en WIT: sets cohesivos de interfaces para dominios específicos (`wasi-cli`, `wasi-http`).
- Debate: ¿Wasm va a reemplazar Docker? (No, pero es complementario).

**2024 — WASI 0.2 Estable:**
- **WASI Preview 2 / WASI 0.2** lanzado (febrero 2024). Production-ready oficial.
- Component Model estabilizado. Composición de componentes sin overhead de FFI.
- Wasmtime con soporte completo de WASI 0.2.
- GC proposal finalizado: Wasm puede manejar garbage collection (habilitador para Java, C#, Python en Wasm).
- Tail calls proposal estabilizado.
- Extendend Const Expressions.
- **Kotlin/Wasm**, **Dart/Wasm** en beta aprovechando GC proposal.

**2025 — Async nativo y ecosistema:**
- WASI 0.3 RC con **async nativo**: streams, futures, cancellation como primitivas del Component Model.
- Threads en WASI: paralelismo real en servidor.
- Wasm en embedded: `wasm32-unknown-unknown` con `no_std` en Rust para microcontrollers.
- Java en Wasm vía TeaVM y GC proposal: serverless Java sin JVM startup overhead.
- Fermyon, Cosmonic con cloud nativo Wasm como servicio.

**2026 — Consolidación:**
- **WASI 0.3.0** estable con async completo.
- Wasm como "Docker alternativo" para microservicios ultra-livianos.
- Component Model como formato de distribución de plugins cross-language.
- Debate "Wasm vs Containers" resuelto pragmáticamente: Wasm para compute puro sin syscalls; containers para código existente.
- ~300ms startup para Java en Wasm vs ~3s para JVM. Gap cerrado para funciones serverless.

### La Arquitectura de Componentes Wasm (explicada)

```
┌─────────────────────────────────────────────────────────┐
│                   Wasm Component                         │
│  ┌──────────────┐  WIT interface   ┌──────────────────┐ │
│  │  Core Module │ ←──────────────→ │   Core Module    │ │
│  │  (Rust)      │                  │   (Go/TinyGo)    │ │
│  └──────────────┘                  └──────────────────┘ │
│         │                                  │            │
│         └──────────── Compositor ──────────┘            │
└─────────────────────────────────────────────────────────┘
         │
         ↓ WASI 0.2 interfaces
┌─────────────────────┐
│  Host Runtime       │  ← Wasmtime, WasmEdge, etc.
│  (OS / Edge node)   │
└─────────────────────┘
```

Los componentes se comunican via WIT (lenguaje de definición de interfaces) sin overhead de serialización. Un componente Rust puede llamar a uno Go directamente. El runtime garantiza aislamiento seguro entre ellos.

---

## 6. Modelos de Concurrencia

### 6.1 Structured Concurrency (Concurrencia Estructurada)

**Origen:** Martin Sústrik (nanomsg), luego Nathaniel Smith (Trio/Python), ~2016–2018. Popularizado masivamente 2020–2024.

**Principio:** Las tareas concurrentes tienen *scope estructurado*, como las llamadas a funciones. Una tarea padre no puede completar hasta que todas sus tareas hijas terminen. Esto elimina task leaks, hace el manejo de errores predecible y el cancelation composable.

**Cronología:**

| Año | Hito |
|-----|------|
| 2019–2020 | Python Trio como referencia. Paper de Roman Elizarov (JetBrains) sobre structured concurrency en Kotlin. |
| 2021 | **Kotlin Coroutines** con structured concurrency madura y se convierte en referencia. `CoroutineScope` como boundary. Swift introduce `async/await` + `TaskGroup`. |
| 2022 | **Swift Concurrency** (structured concurrency completo). Java Project Loom introduce `StructuredTaskScope` (incubating). Tokio en Rust no adopta structured concurrency por design. |
| 2023 | **Java 21 Virtual Threads GA** con `StructuredTaskScope` en preview. Un millón de threads en JVM. Go añade `errgroup` como structured pattern (no nativo). |
| 2024 | Java 23: `StructuredTaskScope` segunda preview. Swift 6 con strict concurrency checking (actor isolation). |
| 2025 | Java 25: Structured Concurrency posiblemente GA. Swift 6 widely adopted. Python 3.12+ con TaskGroup. |
| 2026 | Structured concurrency es el modelo dominante para nuevo código async. Runtimes no-structured (Tokio, Node.js raw) se usan con patrones de disciplina adicional. |

```kotlin
// Kotlin: structured concurrency con CoroutineScope
suspend fun fetchUserData(userId: Int): UserData = coroutineScope {
    // Ambas tareas se lanzan en paralelo dentro del scope
    val profile = async { api.getProfile(userId) }
    val orders = async { api.getOrders(userId) }
    
    // Si cualquiera falla, se cancela la otra y se propaga el error
    UserData(profile.await(), orders.await())
}
// Cuando fetchUserData retorna, GARANTIZADO que no hay tareas corriendo
```

---

### 6.2 Modelo de Actores

**Origen:** Carl Hewitt, 1973. Erlang lo popularizó en los 80s. El período 2020–2026 es de renacimiento.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Akka (Scala/Java) dominante en JVM. Orleans (Microsoft, C#) maduro. |
| 2021 | Akka cambia a licencia Business Source (BSL) — comunidad busca alternativas. |
| 2022 | **Pekko** fork de Akka bajo Apache 2.0. wasmCloud adopta actores Wasm. Orbit (EA) continúa. |
| 2023 | Gleam 1.0 RC: actores sobre BEAM como ciudadanos de primera clase. |
| 2024 | **Microsoft Orleans 8**: virtual actors para .NET. Elixir/Phoenix LiveView demuestra actores para UI real-time. |
| 2025 | Ray (Python) para distribuir actores de ML. Unison con modelo actor distribuido. |
| 2026 | Actores para agentes AI: cada agente LLM como actor con estado, mailbox, lifecycle. |

**Por qué los actores funcionan bien para AI agents:** Cada agente tiene su propio estado (conversación, memoria), se comunica por mensajes asíncronos, puede fallar y ser reiniciado (supervision trees de Erlang), y puede estar en cualquier nodo del cluster. Es el modelo natural para sistemas multi-agente.

---

### 6.3 STM (Software Transactional Memory)

**Origen:** Shavit & Touitou, 1995. Haskell lo popularizó con `STM monad`.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Haskell STM como referencia de diseño. Clojure STM con `ref`, `alter`, `dosync`. |
| 2021–2022 | Rust experimenta con STM (atomicell, swym crates). Go no adopta STM oficialmente. |
| 2023 | STM limitado a lenguajes con GC (Java Multiverse, Haskell). Rust prefiere channels/mutexes. |
| 2024–2026 | STM no ganó tracción mainstream. Su nicho es código con acceso concurrente complejo a múltiples variables compartidas. |

```haskell
-- STM en Haskell: transacciones composables sobre memoria compartida
transfer :: TVar Int -> TVar Int -> Int -> STM ()
transfer from to amount = do
  fromBalance <- readTVar from
  when (fromBalance < amount) retry  -- retry automáticamente cuando haya fondos
  modifyTVar from (subtract amount)
  modifyTVar to (+ amount)

-- Se ejecuta atómicamente. Si falla, se hace rollback y retry.
atomically $ transfer account1 account2 100
```

---

### 6.4 CRDTs (Conflict-free Replicated Data Types)

**Origen:** Marc Shapiro et al., 2011. El período 2020–2026 es de adopción mainstream.

**Concepto:** Estructuras de datos que pueden ser actualizadas concurrentemente en múltiples réplicas sin coordinación, y siempre convergen al mismo estado final.

**Tipos:**
- **State-based (CvRDT):** Se comparte el estado completo, se merge con una función join (semilattice).
- **Operation-based (CmRDT):** Se comparten las operaciones; commutativity garantiza convergencia.
- **Delta CRDTs:** Se comparte solo el delta del estado (más eficiente).

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | "CRDTs: The Hard Parts" — Martin Kleppmann. Talk seminal que expone los problemas reales (interleaving, intent capture, moving items en listas). |
| 2021 | **Yjs** (JavaScript CRDT) se convierte en el estándar de facto para colaboración en tiempo real. Tiptap, ProseMirror, Slate lo adoptan. |
| 2022 | **Microsoft Fluid Framework** open-source: CRDTs para Office 365 y collaboration. |
| 2023 | Automerge 2.0 (Rust core, JS wrapper): CRDT para documentos estructurados con historial. |
| 2024 | **El modelo Ink & Switch "local-first"** gana tracción. Refly, Linear, Notion exploran CRDTs para offline-first. |
| 2025 | CRDTs para bases de código: Unison usa content-addressed storage que es CRDT-compatible por naturaleza. |
| 2026 | CRDTs como primitiva standard en frameworks de colaboración. El problema de "intent" sigue siendo investigación activa. |

**El problema que señaló Kleppmann:** Los CRDTs garantizan *convergencia* pero no *corrección de intención*. Si dos personas mueven el mismo elemento en una lista simultáneamente, el CRDT converge a un resultado determinístico, pero puede no ser lo que ninguno quería. Esto requiere diseño cuidadoso de la semántica del CRDT.

---

## 7. Compiladores y Runtimes

### 7.1 MLIR: Multi-Level Intermediate Representation

**Origen:** Chris Lattner (Google, ahora Modular), 2019. Paper en CGO 2020.

**¿Qué es?** Una infraestructura de compilador que permite definir múltiples IRs (dialects) a diferentes niveles de abstracción, con transformaciones entre ellos. LLVM tiene un único IR; MLIR tiene muchos dialects que se pueden componer.

```
TensorFlow Graph → TF Dialect → MHLO Dialect → Linalg Dialect → LLVM Dialect → Machine Code
     Nivel 4           3              2                1                0
```

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Paper MLIR en CGO 2020. Integrado en LLVM project. TensorFlow adopta MLIR internamente. |
| 2021 | MLIR en producción en Google TPUs. Primera vez que dialects de hardware y ML comparten infraestructura. |
| 2022 | CIRCT: hardware design en MLIR (FIRRTL, SystemVerilog). IREE (ML inference engine). |
| 2023 | **Mojo** usa MLIR como backend: el código Mojo baja a MLIR dialects que generan CUDA, CPU, etc. OpenXLA adopta MLIR. |
| 2024 | MLIR en compiladores de chips: Qualcomm, Intel, AMD usan MLIR para sus toolchains. |
| 2025 | Wasm dialecto MLIR (WAMI): Wasm como ciudadano de primera clase en MLIR. |
| 2026 | MLIR como el IR universal para todo: ML, hardware, sistemas, Wasm. Cualquier nuevo lenguaje serio considera un MLIR dialect. |

**Por qué importa:** MLIR resuelve el problema de "hay N lenguajes y M targets, necesitas N×M compiladores". Con MLIR, cada lenguaje lowers a un dialect común, y los targets suben desde dialects comunes. Es N+M en lugar de N×M.

---

### 7.2 Cranelift

**Origen:** Mozilla, ~2016. Ahora Bytecode Alliance.

**¿Qué es?** Un compilador JIT y AOT escrito en Rust, diseñado para ser seguro, rápido de compilar (no de ejecutar), y correcto. Es el backend de Wasmtime y la opción principal para JIT en el ecosistema Wasm.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Cranelift como backend experimental de Rust (alternativa a LLVM para debug builds). |
| 2021 | Wasmtime adopta Cranelift como backend principal. 5x más rápido compilando que LLVM. |
| 2022 | Formal verification de Cranelift en progreso (VeriWasm). |
| 2023 | Cranelift con egraph-based optimizations (isle DSL). CLIF como IR estabilizado. |
| 2024 | Cranelift en Firefox para JavaScript JIT (reemplazando partes de SpiderMonkey). |
| 2025 | Cranelift usado en producción en Cloudflare Workers, Fastly Compute. |
| 2026 | Cranelift como el "second compiler" standard: LLVM para máxima performance, Cranelift para velocidad de compilación + seguridad. |

**El trade-off:** LLVM genera código ~20-30% más rápido pero compila 5-10x más lento. Cranelift es perfecto para JIT (donde compilar rápido importa) o para entornos donde la seguridad del compilador mismo es crítica (Wasm sandbox).

---

### 7.3 GraalVM: La JVM Poliglota

**Origen:** Oracle Labs, ~2012. Público desde 2018. El período 2020–2026 es de madurez.

**Componentes clave:**
- **Graal Compiler:** JIT compiler escrito en Java para la JVM.
- **Native Image:** AOT compilation de Java a binario nativo (sin JVM en runtime).
- **Truffle Framework:** Framework para implementar lenguajes (Ruby, Python, R, JS) sobre la JVM con JIT gratis.
- **Substrate VM:** El runtime mínimo para Native Image.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | GraalVM 20.x: Native Image en producción. Quarkus y Micronaut lo adoptan para serverless Java (startup en <100ms). |
| 2021 | GraalVM 21.x: mejoras en Native Image compatibility. Node.js sobre GraalVM. |
| 2022 | GraalVM Enterprise disponible gratis. GraalVM Community 22.x con mejoras en Truffle. |
| 2023 | Oracle GraalVM 23 (alineado a JDK versiones). Native Image con profile-guided optimization (PGO). Java 21 con Virtual Threads + Native Image. |
| 2024 | GraalVM para JDK 21/22/23. Truffle con mejor interop entre lenguajes. Spring Boot 3.x con Native Image support oficial. |
| 2025 | GraalVM para JDK 24. Micronaut, Quarkus con Native Image como deployment por defecto en cloud. |
| 2026 | GraalVM para JDK 24.0.2 (julio 2025). Native Image como opción estándar para Java serverless. El "problema de startup de Java" está técnicamente resuelto. |

**Por qué importa:** Native Image resuelve el problema histórico de Java en serverless y containers: startup de ~50ms vs ~3s, memory footprint 5-10x menor. Quarkus con Native Image hace Java competitivo con Go en cloud-native.

---

### 7.4 Zig's Comptime como Paradigma

**¿Por qué es una innovación de compilador?** `comptime` en Zig es básicamente un intérprete del propio lenguaje corriendo en el compilador. No es un sistema de macros separado ni templates con sintaxis especial; es Zig corriendo en compile-time.

**Implicaciones profundas:**
- **Generics sin sintaxis de generics:** `fn max(comptime T: type, a: T, b: T) T` — el tipo es un parámetro comptime.
- **Reflexión en compile-time:** Puedes iterar los campos de un struct y generar código para cada uno.
- **Computación garantizada en compile-time:** Cualquier función pura puede ser ejecutada en compile-time si sus inputs son conocidos.
- **Build system en Zig:** El `build.zig` es código Zig normal que corre en compile-time para definir el build graph.

Esta idea influye directamente en el diseño de Mojo y está siendo estudiada para futuros lenguajes de sistemas.

---

## 8. Hardware-Software Codesign

### 8.1 io_uring: Revolución de I/O en Linux

**Origen:** Jens Axboe (kernel storage maintainer), Linux 5.1 (mayo 2019). Madurez 2020–2026.

**¿Qué es?** Una interfaz de I/O asíncrona basada en ring buffers compartidos entre userspace y kernel. Elimina system calls para cada operación al acumularlas en el submission queue y leer resultados del completion queue.

**El problema que resuelve:** Las interfaces anteriores (read/write bloqueantes, AIO de Linux) tenían problemas: bloqueantes requieren threads, AIO era compleja y no funcionaba para todos los tipos de I/O (solo archivos). io_uring funciona para todo: archivos, sockets, timers, señales.

**Cronología:**

| Año | Hito |
|-----|------|
| 2019 | Linux 5.1: io_uring introducido. |
| 2020 | Linux 5.4–5.7: io_uring madura, soporte de más operaciones. `liburing` como API de alto nivel. LWN: "rapid growth of io_uring". |
| 2021 | Nginx experimental con io_uring. TigerBeetle elige io_uring como base de I/O. io_uring en fio (benchmark storage). |
| 2022 | Tokio (Rust) añade soporte de io_uring via `tokio-uring`. Eio (OCaml 5) usa io_uring como backend de alta performance. |
| 2023 | Facebook Thrift migra a io_uring para networking. Redis 7.x experimenta con io_uring. QEMU usa io_uring. |
| 2024 | Netty (Java) con io_uring nativo. io_uring como base de async en sistemas Linux de alta performance. |
| 2025 | Prácticamente todo servidor Linux de alta performance usa io_uring o planea hacerlo. |
| 2026 | io_uring como API de I/O estándar para nuevos sistemas en Linux. AIO deprecated en práctica. |

**Performance:** io_uring puede alcanzar ~2-4M ops/sec en comparación con ~300k de syscalls tradicionales para workloads de I/O pura. Elimina el costo de system calls (context switches kernel/userspace) acumulando operaciones en batch.

---

### 8.2 eBPF: Programabilidad del Kernel sin Riesgo

**Origen:** Berkeley Packet Filter (1993), extendido a "extended BPF" en Linux 3.18 (2014). Pero el auge es 2020–2026.

**¿Qué es?** Un subsistema de Linux que permite ejecutar programas verificados (verificados formalmente por el kernel verifier) en el kernel sin modificar el kernel. Los programas eBPF se ejecutan en respuesta a eventos (network packets, system calls, hardware events).

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Cilium (networking basado en eBPF) adoptado por CNCF. Cloudflare usa eBPF masivamente para DDoS mitigation. |
| 2021 | Meta open-source eBPF toolchain. Katran (Facebook load balancer) en eBPF. Linux Kernel Security (LSM hooks) via eBPF. |
| 2022 | **Tetragon** (Isovalent): runtime security con eBPF. BTF (BPF Type Format) permite CO-RE (Compile Once, Run Everywhere). |
| 2023 | eBPF para observabilidad: Pixie, Parca, Hubble. eBPF Windows (Microsoft). DataDog adopta eBPF para su agent. |
| 2024 | **OpenTelemetry eBPF Instrumentation (OBI):** traces automáticos sin modificar el código. Zero-instrumentation observability. |
| 2025 | eBPF en ARM64 y RISC-V. eBPF para AI/ML inference acceleration en networking path. |
| 2026 | eBPF como la plataforma de extensibilidad del kernel. OBI en camino a 1.0. Debate sobre eBPF para RISC-V madurez. |

**Por qué importa:** eBPF permite hacer cosas que antes requerían módulos de kernel (que son peligrosos, pueden crashear el sistema): network packet processing ultra-rápido, security policies, profiling, tracing. Todo sin reiniciar el sistema y con garantías de seguridad.

---

### 8.3 CXL (Compute Express Link)

**Origen:** Especificación CXL 1.0 en 2019. Intel, AMD, ARM, Google, Meta co-desarrollan. Madurez hardware 2023–2026.

**¿Qué es?** Un protocolo de interconexión sobre PCIe que permite coherencia de cache entre CPU y dispositivos (memorias, aceleradores). CXL permite que una GPU, FPGA, o tarjeta de memoria adicional comparta el espacio de memoria del CPU con coherencia de cache.

**¿Por qué importa?** La brecha memoria-CPU (Memory Wall) es el cuello de botella principal en sistemas modernos. CXL permite disaggregate memory: un servidor puede acceder a memoria de otro servidor sin copias, con latencia mucho menor que red.

**Cronología:**

| Año | Hito |
|-----|------|
| 2021 | CXL 2.0: fabric switching para más flexibilidad. |
| 2022 | CXL 3.0: peer-to-peer entre dispositivos CXL. Samsung, Micron anuncian memory expansion cards CXL. |
| 2023 | Primeros racks CXL en labs de hiperescaladores. CXLMemUring: propuesta para acceso asíncrono a memoria CXL via io_uring. |
| 2024 | CXL memory expansion en producción limitada. AWS, Google experimentan para AI inference (LLM KV-cache en CXL). |
| 2025 | CXL como solución para LLM memory wall: los KV-caches de atención ocupan 10s de GBs; CXL permite expandir sin mover datos. |
| 2026 | CXLAimPod: eBPF + CXL para gestión de memoria de LLMs. Mejoras de 71.6% en LLM text generation throughput. |

---

### 8.4 RISC-V: La ISA Abierta

**Origen:** UC Berkeley, 2010. Patrón de adopción: microcontrollers primero, servers después.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Espressif (ESP32-C3) migra todo a RISC-V. NVIDIA adopta RISC-V para microcontrollers internos de GPU. WCH Electronics con chips RISC-V ultra-baratos. |
| 2021 | SiFive HiFive Unmatched: primer SBC RISC-V con CPU de aplicación seria. Linux kernel con soporte robusto RISC-V. |
| 2022 | StarFive VisionFive: RISC-V board asequible. Milk-V, Pine64 Star64. Fedora y Ubuntu ports para RISC-V. |
| 2023 | Alibaba T-Head TH1520: RISC-V para laptops (Lichee Pi 4A). China invierte masivamente (independencia de ARM/x86). |
| 2024 | **RVA23 Profile** ratificado: estandariza extensiones (vector, bit manipulation, hypervisor). Ubuntu 25.10 adopta RVA23 como mínimo. |
| 2025 | RISE Project: CI/CD nativo RISC-V en GitHub. SiFive P670 con rendimiento competitivo con Cortex-A75. |
| 2026 | Canonical declara 2026 "año de RISC-V". Ubuntu 26.04 LTS con soporte RISC-V primera clase. RISC-V runners en GitHub Actions (RISE Project). |

**Por qué importa estratégicamente:** El dominio de ARM (móvil) y x86 (desktop/server) tiene décadas. RISC-V es la primera ISA abierta y libre de royalties que tiene tracción real en hardware de producción. Para embedded/IoT, ya es mainstream. Para servers, es el horizonte de los próximos 5 años.

---

## 9. Sistemas Distribuidos

### 9.1 Local-First Software

**Origen:** Paper "Local-First Software" de Ink & Switch (Martin Kleppmann et al.), 2019. El movimiento creció 2020–2026.

**Principio:** Los datos del usuario viven primariamente en su dispositivo, no en servidores cloud. La app funciona completamente offline. La sincronización es un servicio de conveniencia, no una dependencia.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | "CRDTs: The Hard Parts" — Kleppmann expone los problemas reales. Automerge 1.0. |
| 2021 | Yjs se convierte en la referencia de CRDT en JS. Liveblocks (SaaS de CRDTs) lanza. |
| 2022 | Linear (issue tracker) con offline support real. Obsidian (notes app) local-first con sync opcional. |
| 2023 | Automerge 2.0 con core en Rust. Notion experimenta con CRDTs para blocks. Ink & Switch publica Pushpin y Beehive. |
| 2024 | "A Programming Model for Verifiably Safe Local-First Software" (ACM 2024). ElectricSQL: sync layer para Postgres con local-first. |
| 2025 | Refly, Anytype con local-first. Frameworks de sync crecen. |
| 2026 | Local-first como expectativa del usuario. Apps SaaS con modo offline completo como diferenciador competitivo. |

---

### 9.2 Algoritmos de Consenso: Estado del Arte

**La jerarquía de consenso 2026:**

```
Paxos (1989) → Raft (2014) → EPaxos (2013) → CockroachDB's Raft → Parallel Raft
     ↓
Multi-Paxos / Flexible Paxos → Quorum variability
     ↓
Leaderless Consensus (BFT) → Tendermint, HotStuff, DiemBFT
     ↓
Deterministic Consensus (sin líder) → Investigación activa 2023-2026
```

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Raft como consenso "entendible" es el estándar. etcd, Consul, CockroachDB lo usan. |
| 2021 | **HotStuff** (Facebook/Meta): BFT consensus lineal (no cuadrático). Base de DiemBFT/Diem blockchain. |
| 2022 | **CockroachDB** con Parallel Raft: múltiples grupos Raft por nodo, reduciendo serialización. |
| 2023 | TiKV (TiDB) con Multi-Raft. FoundationDB con Paxos en producción masiva (Apple, Snowflake). |
| 2024 | Investigación sobre "leaderless consensus" para reducir latencia. |
| 2025 | **TigerBeetle** con protocolo propio (VSR - Viewstamped Replication) optimizado para transacciones financieras. |
| 2026 | El problema del consenso está "resuelto" para producción. El foco está en performance y reducción de latencia de leader election. |

---

### 9.3 Raft y su Diseminación Masiva

**Por qué Raft ganó:** La motivación de Raft era ser "más comprensible que Paxos". El paper (Ongaro & Ousterhout, 2014) incluye una comparación empírica de comprensión. El resultado es que Raft se puede implementar correctamente en semanas; Paxos tarda meses y casi siempre tiene bugs sutiles.

**Implementaciones relevantes 2020–2026:**
- **etcd:** Registro de configuración de Kubernetes. Todo cluster K8s depende de etcd con Raft.
- **CockroachDB:** Distributed SQL con Raft por range de datos.
- **TiDB/TiKV:** Database distribuida china con Multi-Raft.
- **InfluxDB 3.0:** Time-series con Raft para replicación.

---

## 10. Infraestructura AI/ML para Devs

Esta sección cubre lo que un dev necesita entender *debajo* de los LLMs y frameworks de ML, no solo cómo usarlos.

### 10.1 La Pila de Training

**El problema:** Entrenar un LLM de 70B parámetros en una GPU es imposible. Se necesita paralelismo masivo. Hay tres tipos de paralelismo:

```
Data Parallelism:    mismo modelo, diferentes batches
Tensor Parallelism:  un tensor (ej. matriz 8192×8192) partido entre 8 GPUs
Pipeline Parallelism: diferentes capas en diferentes GPUs
3D Parallelism:      los tres combinados (Megatron-LM)
```

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | **Megatron-LM** (NVIDIA): 3D parallelism para entrenar GPT-3 (175B). Paper fundacional. |
| 2021 | **ZeRO** (Microsoft DeepSpeed): Zero Redundancy Optimizer. Optimizer states, gradients, parameters particionados entre GPUs. Permite entrenar modelos 10x más grandes con la misma memoria. |
| 2022 | **FlashAttention** (Tri Dao, Stanford): reescribe el mecanismo de atención para ser I/O-aware. Lee/escribe HBM eficientemente, usa SRAM de la GPU. 2-4x speedup, memoria O(n) en lugar de O(n²). |
| 2023 | **FlashAttention-2:** mejor paralelización, reduce FLOPs por 2x. **FlashAttention-3** (H100-específico). FSDP (Fully Sharded Data Parallel) en PyTorch como alternativa a DeepSpeed. |
| 2024 | **vLLM:** PagedAttention para servir LLMs eficientemente. Trata la KV-cache como virtual memory con paginación. 24x throughput vs HuggingFace naive. |
| 2025 | **SGLang:** structured generation acelerada. **Triton** de OpenAI para escribir kernels GPU en Python. **FlashAttention en Blackwell** (NVIDIA H200/B100): 1.5x speedup sobre Hopper. |
| 2026 | Stack estándar de training: PyTorch + FSDP/DeepSpeed + FlashAttention + Triton kernels. Stack de inferencia: vLLM / TensorRT-LLM. |

### 10.2 CUDA y la Pila de GPU

**Lo que un dev necesita entender:**

```
Código Python (PyTorch) 
    → CUDA C++ (kernels que corren en GPU)
        → PTX (Assembly de NVIDIA)
            → SASS (Instruction set binario)
                → GPU Hardware (SM, Tensor Cores, HBM)
```

**Conceptos clave:**
- **SMEM (Shared Memory):** Memoria ultra-rápida (~100x más rápida que DRAM) local a un SM. Los kernels eficientes maximizan su uso.
- **Tensor Cores:** Hardware especializado para matmul en FP16/BF16/INT8. En H100, dan 1979 TFLOPS vs 67 TFLOPS de FP32 cores.
- **HBM (High Bandwidth Memory):** La VRAM de las GPUs modernas. H100 tiene 3.35 TB/s bandwidth. El problema es que es lento vs. SMEM.
- **Memory bound vs compute bound:** La mayoría de operaciones de Transformers son memory-bound (leer pesos es el bottleneck, no calcular). FlashAttention lo resuelve reduciendo accesos a HBM.

**Triton:** OpenAI creó Triton como alternativa Python a CUDA para escribir kernels GPU. Maneja la gestión de SMEM automáticamente. Permite escribir kernels custom sin conocer CUDA profundamente.

```python
# Triton kernel para softmax (simplificado)
@triton.jit
def softmax_kernel(output_ptr, input_ptr, input_row_stride, n_cols, BLOCK_SIZE: tl.constexpr):
    row_idx = tl.program_id(0)
    row_start_ptr = input_ptr + row_idx * input_row_stride
    col_offsets = tl.arange(0, BLOCK_SIZE)
    input_ptrs = row_start_ptr + col_offsets
    row = tl.load(input_ptrs, mask=col_offsets < n_cols, other=-float('inf'))
    row_minus_max = row - tl.max(row, axis=0)
    numerator = tl.exp(row_minus_max)
    denominator = tl.sum(numerator, axis=0)
    softmax_output = numerator / denominator
    output_ptrs = output_ptr + row_idx * input_row_stride + col_offsets
    tl.store(output_ptrs, softmax_output, mask=col_offsets < n_cols)
```

### 10.3 Inferencia: El Nuevo Cuello de Botella

**El problema de 2024–2026:** Training fue el foco de 2020–2023. Con modelos en producción sirviendo millones de requests, el foco se movió a *inferencia eficiente*.

**Técnicas clave:**

| Técnica | Descripción | Impacto |
|---------|-------------|---------|
| **Quantization (INT8/INT4)** | Reducir pesos de FP16 a 8/4 bits | 2-4x reducción de memoria, ~10% pérdida de calidad |
| **KV-Cache** | Cachear los key-value de atención entre tokens | Sin esto, generar 100 tokens requiere 100 forward passes completos |
| **PagedAttention (vLLM)** | KV-cache paginada como virtual memory | 24x throughput, elimina fragmentación |
| **Speculative Decoding** | Un modelo pequeño propone tokens, el grande verifica | 2-3x speedup en texto predecible |
| **Continuous Batching** | Nuevas requests entran al batch dinámicamente | vs. static batching que bloquea |
| **Tensor Parallelism** | Repartir el modelo entre GPUs para inferencia | Escala a modelos que no caben en una GPU |

---

## 11. Seguridad desde Fundamentos

### 11.1 Memory Safety: El Imperativo Nacional

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Microsoft revela que ~70% de sus CVEs son memory safety bugs (UAF, buffer overflow). Google dice lo mismo para Chrome/Android. |
| 2021 | Android comienza migración a Rust para código nuevo. Memory safety como prioridad explícita. |
| 2022 | NSA publica advisory: "NSA Recommends Adopting Memory Safe Languages". |
| 2023 | **CISA (Cybersecurity and Infrastructure Security Agency)** publica "The Case for Memory Safe Roadmaps". Linux kernel Rust. |
| 2024 | Casa Blanca publica "Back to the Building Blocks: A Path Toward Secure and Measurable Software" — recomienda memory safe languages para nuevo código federal. |
| 2025 | Rust en Linux kernel declarado no experimental. C++ Safety Profile (P3081) propuesta para añadir safety a C++. |
| 2026 | Memory safety como criterio de compliance en gobierno y regulación financiera. |

**El argumento técnico:** C y C++ permiten acceso directo a memoria sin verificación. UAF (use-after-free), buffer overflows, null dereferences son clases enteras de bugs que lenguajes como Rust *eliminan por diseño*, no por testing. Rust no puede tener estos bugs en código `safe` — es una garantía estática.

---

### 11.2 Supply Chain Security

**El evento catalizador:** SolarWinds (diciembre 2020). Atacantes comprometieron el build system de SolarWinds e inyectaron malware en actualizaciones firmadas digitalmente. ~18,000 organizaciones afectadas incluyendo agencias del gobierno US.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | **SolarWinds breach.** Todo el mundo revisa su supply chain. |
| 2021 | **Log4Shell** (log4j). Una librería de logging en Java usada en millones de apps tiene RCE crítico. Confirma que las dependencias son attack surface. **Executive Order 14028** (Biden): mandato de SBOMs para proveedores del gobierno federal. |
| 2022 | **SLSA framework** (Supply-chain Levels for Software Artifacts) v0.1 → v1.0. **Sigstore** se convierte en Linux Foundation project. npm, PyPI, Maven adoptan artifact signing. |
| 2023 | SLSA 1.0 estable. **SPDX 3.0** (SBOM standard) ratificado como ISO standard. GitHub Artifact Attestations con Sigstore. |
| 2024 | **XZ Utils backdoor** (marzo 2024): un contribuidor malicioso pasó 2 años ganando confianza para insertar backdoor en librería de compresión usada en OpenSSH. Supply chain via *social engineering*. |
| 2025 | Respuesta a XZ: revisión de mantenedores de proyectos críticos OSS. OpenSSF Scorecard. SLSA Level 2+ como requisito de procurement. |
| 2026 | SBOMs mandatorios en más jurisdicciones. Sigstore adoptado por Docker Hub, GitHub, npm como default. |

**El stack de supply chain security 2026:**

```
Código fuente → Build reproducible → SLSA provenance → Artifact signing (Sigstore)
                      ↓
              SBOM (SPDX/CycloneDX) → Vulnerability scanning → Policy enforcement (OPA)
                      ↓
                 Registro con cosign verify → Deployment
```

---

### 11.3 Seguridad Formal: Crypto Verificado

**El problema:** El código criptográfico tiene que ser *perfecto*. Un bit mal colocado puede comprometer toda la seguridad. Testing no es suficiente para probar correctness.

**Soluciones formales:**

| Proyecto | Descripción | Estado 2026 |
|---------|-------------|-------------|
| **HACL*** (INRIA/Microsoft) | Librerías crypto verificadas en F*. Código C generado formalmente correcto. | En Firefox, Linux kernel, Signal |
| **EverCrypt** | Suite completa de cripto con verificación F*. | Producción en Azure |
| **Vale** | Framework para verificar código Assembly de cripto (AES-GCM, SHA) | En Firefox/Chrome |
| **Boring Crypto** | Google's crypto library con verificación parcial | Android, Chrome |
| **Lean + zkEVM** | Verificación formal de circuitos zero-knowledge | StarkWare production |

**Zero-Knowledge Proofs:** El auge de ZK proofs (2022–2026) trajo verificación formal al mainstream. Los ZK circuits son pruebas matemáticas; si el circuit tiene un bug, la prueba puede "probar" cosas falsas. La industria blockchain invierte masivamente en Lean/Coq para verificar circuits.

---

## 12. Observabilidad Avanzada

### 12.1 OpenTelemetry: El Estándar Emergente

**Origen:** Fusión de OpenTracing y OpenCensus en 2019. OTel 1.0 en 2021.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | OpenTelemetry en beta. CNCF incubating. Vendors (Datadog, Honeycomb, Jaeger) añaden soporte. |
| 2021 | **OTel Tracing GA (1.0)**. Specs estables para traces. El problema de vendor lock-in de observabilidad comienza a resolverse. |
| 2022 | **OTel Metrics GA**. Collectors maduros. AWS, Azure, GCP con soporte nativo OTel. Datadog adopta OTLP. |
| 2023 | **OTel Logs en RC**. Profiles signal en desarrollo. OTel Collector como proxy universal de telemetría. |
| 2024 | **OTel Logs GA**. OpenTelemetry eBPF Collector: instrumentación zero-code via eBPF. Profiles signal en beta. |
| 2025 | **OBI (OpenTelemetry eBPF Instrumentation):** traces automáticos para HTTP/gRPC sin tocar código. |
| 2026 | OTel como estándar de facto. Instrumentación manual + eBPF automática como combinación híbrida. OBI hacia 1.0. |

**La visión de las tres pilares → cuatro:**
```
Logs     → Qué pasó
Metrics  → Cuánto/cuántos pasó
Traces   → Por qué pasó (distributed trace de una request)
Profiles → Dónde se gastó el CPU/memoria (continuous profiling)
```

---

### 12.2 eBPF para Observabilidad: Zero-Instrumentation

**El cambio de paradigma:** Traditionally, observabilidad requería instrumentar el código (añadir spans, métricas explícitas). Con eBPF, el kernel observa las syscalls, network calls, function calls y genera telemetría automáticamente.

**Herramientas clave:**

| Herramienta | Descripción |
|-------------|-------------|
| **Pixie** (New Relic) | Profiling y tracing automático via eBPF en K8s |
| **Parca** | Continuous profiling con eBPF |
| **Tetragon** | Runtime security + observabilidad con eBPF |
| **DeepFlow** | Distributed tracing sin modificar código |
| **Cilium Hubble** | Network observability en K8s via eBPF |
| **OBI** | OTel eBPF Instrumentation (traces OTEL sin código) |

**Cómo funciona OBI:**
```
Kernel eBPF programs → interceptan sys_read/sys_write/connect/accept
    → correlacionan con requests HTTP/gRPC
        → generan spans OTLP compatibles
            → envían al OTel Collector
                → Jaeger / Grafana / Honeycomb
```

---

### 12.3 Continuous Profiling: El Cuarto Pilar

**¿Qué es?** Profiling que corre continuamente en producción (no solo en desarrollo) con overhead tan bajo (~1-2% CPU) que es aceptable siempre.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Google-Wide Profiling (GWP): Google perfiló continuamente sus servicios desde 2010, paper público en 2010, práctica consolidada en 2020. |
| 2021 | Parca open-source. Pyroscope lanza. Grafana Phlare. |
| 2022 | **Grafana acquiere Pyroscope.** Continuous profiling como servicio cloud. |
| 2023 | **OTel Profiles signal** en borrador. Profiling integrado en el stack OTel. |
| 2024 | Grafana Alloy con profiling. DataDog continuous profiling GA. |
| 2025 | OTel Profiles en beta. Profiling como cuarto pilar de observabilidad mainstream. |
| 2026 | Continuous profiling en producción como standard. Corte de costo de infra cloud del 20-30% identificando hotspots en producción. |

---

## 13. Bases de Datos: Nuevos Paradigmas

### 13.1 DuckDB: La Revolución OLAP Embebida

**Origen:** CWI (Centro de Matemáticas e Informática, Amsterdam), Mark Raasveldt y Hannes Mühleisen, 2018. Pero se hace mainstream 2022+.

**¿Qué es?** SQLite para análisis. Una base de datos OLAP columnar que corre *in-process*, sin servidor, con performance extrema en queries analíticas.

**Cronología:**

| Año | Hito |
|-----|------|
| 2020–2021 | DuckDB en fase de investigación/early adoption. Paper en SIGMOD 2019. |
| 2022 | DuckDB 0.5/0.6. Adopción explosiva en data science. Puede leer Parquet, CSV, JSON directamente. |
| 2023 | **DuckDB 0.8/0.9**. Extensiones: httpfs (Parquet desde S3), spatial, json. Usado en Facebook, Google, Airbnb internamente. |
| 2024 | **DuckDB 1.0** (junio 2024). API estable. Motor de queries vectorizado columnar. Integración perfecta con pandas/polars. |
| 2025 | DuckDB como motor de análisis en notebooks, pipelines locales, y lambda functions. |
| 2026 | DuckDB redefine el espacio: ya no necesitas Spark para análisis de datasets hasta ~100GB. |

**Por qué importa:** El 80% de los análisis de datos no necesitan un cluster Spark. DuckDB los hace en tu laptop en segundos. Leer 1GB de Parquet desde S3 y hacer GROUP BY tarda ~2 segundos.

---

### 13.2 TigerBeetle: Base de Datos Financiera por Diseño

**Origen:** Joran Dirk Greef, ~2020. Database especializada para transacciones financieras dobles (debit/credit).

**Cronología:**

| Año | Hito |
|-----|------|
| 2020–2021 | Desarrollo en Zig. Protocolo VSR (Viewstamped Replication) custom. |
| 2022 | Paper en QCon London: "A New Era for Database Design". Diseño radical: no es una base de datos de propósito general. Solo hace double-entry accounting. |
| 2023 | TigerBeetle 0.13.x. Benchmarks: millones de transacciones/segundo en hardware modesto. |
| 2024 | TigerBeetle 0.15+. Adoptado por fintechs en producción. |
| 2025–2026 | TigerBeetle como referencia de que las bases de datos de propósito específico superan a las generales en órdenes de magnitud en su dominio. |

**Las ideas radicales de TigerBeetle:**
- Solo hace un tipo de operación: debit/credit
- Escrito en Zig para máxima predictibilidad
- Usa VSR para replicación determinística
- io_uring para I/O
- Preallocated fixed-size records (no heap allocation en el critical path)
- Formal verification de sus protocolos con TLA+

---

### 13.3 FoundationDB y el Modelo de Capas

**Origen:** Apple adquirió FoundationDB en 2015. Open-source desde 2018. Madurez industrial 2020+.

**¿Qué es?** Un key-value store ordenado transaccional con ACID y escala horizontal. La idea clave es que es una *base* sobre la que construyes otras bases de datos.

**Capas sobre FoundationDB:**
- **Record Layer:** Base de datos record/document (usado por Apple en iCloud)
- **Snowflake Metadata:** El catálogo de Snowflake corre sobre FDB
- **ClickHouse Keeper:** Zookeeper-compatible usando FDB
- **Tigris Data:** S3-compatible object store + document DB sobre FDB

**Por qué importa:** FoundationDB demostró que puedes construir semánticas de base de datos de alto nivel (document, relacional, time-series) sobre una capa de almacenamiento ACID genérica. Esto es la arquitectura que desacopla el modelo de datos del motor de almacenamiento.

---

### 13.4 Vector Databases: El Nuevo Primitivo de AI

**Origen:** Pinecone (~2021), Weaviate (2019), Milvus (2020). Explosión post-ChatGPT.

**¿Qué son?** Bases de datos optimizadas para almacenar y buscar vectores de alta dimensión (embeddings). La operación fundamental no es igualdad (`WHERE id = 123`) sino similitud (`k-nearest neighbors` en espacio vectorial).

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Milvus 1.0. FAISS (Facebook) como librería de referencia. |
| 2021 | Pinecone lanza (managed). Weaviate 1.0. |
| 2022 | Qdrant lanza (Rust-based). pgvector: extensión de PostgreSQL para vectores. |
| 2023 | Explosión post-ChatGPT. Chroma, LanceDB, Vespa. Cada framework de RAG requiere vector store. |
| 2024 | pgvector 0.7 con HNSW indexing. Postgres con extensión vectorial compite con bases especializadas. |
| 2025 | **Consolidación:** pgvector para la mayoría de casos; Pinecone/Weaviate/Qdrant para escala masiva. |
| 2026 | Vector search integrado en casi todas las bases de datos (PostgreSQL, MongoDB, Redis). Bases especializadas para casos >1B vectors. |

**HNSW (Hierarchical Navigable Small World):** El algoritmo de indexación estándar para ANN (Approximate Nearest Neighbors). Construye un grafo jerárquico donde navegar es O(log n). El trade-off: build time lento, query ultra-rápido.

---

### 13.5 Bases de Datos Time-Series Especializadas

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | InfluxDB dominante. TimescaleDB (PostgreSQL extension) madura. |
| 2021 | Prometheus con TSDB local, el estándar de monitoreo cloud-native. |
| 2022 | **QuestDB** con columnar storage ultra-rápido para time-series. |
| 2023 | **InfluxDB 3.0** reescrito: columnar storage, Apache Arrow, DataFusion como query engine. |
| 2024 | TimescaleDB en producción masiva. InfluxDB 3.0 Edge (OSS). |
| 2025 | InfluxDB 3 Core + Enterprise. Grafana Mimir para Prometheus at scale. |
| 2026 | Time-series como ciudadano de primera clase en Postgres (via TimescaleDB). InfluxDB 3 con columnar format para IoT y telemetría. |

---

## 14. Edge Computing y Serverless

### 14.1 La Bifurcación del Serverless

**El serverless 2020–2026 se bifurcó en dos modelos:**

```
Serverless Tradicional              Edge Functions
─────────────────────               ─────────────────
AWS Lambda, GCP Functions           Cloudflare Workers, Deno Deploy
Regiones (us-east-1, etc.)         PoPs globales (300+ ubicaciones)
VM/Container cold start             V8 Isolates (<1ms startup)
Cualquier runtime                   JavaScript/Wasm (restricciones)
Estado en S3/DynamoDB               KV at edge, D1, etc.
1s-5s cold start                    <5ms startup (isolates)
```

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Cloudflare Workers madura. Deno 1.0 lanzado. **Vercel Edge Functions** en beta. Fastly Compute@Edge. |
| 2021 | Cloudflare Pages con Workers integration. Netlify Edge Functions. Deno Deploy beta. |
| 2022 | **Vercel OG Image en Wasm/Edge:** 5x speedup TTFB, 15x reducción de costo. Caso de estudio seminal. Cloudflare D1 (SQLite at edge). Deno 1.x estable. |
| 2023 | Cloudflare Workers AI: inferencia ML en el edge. Deno Deploy GA. Netlify Edge mature. |
| 2024 | Cloudflare Workers KV, R2 (S3 compatible), Queues: stack completo en el edge. Bun en producción. |
| 2025 | **Workers AI** con modelos propios. Deno 2.0 con Node.js compatibility. Fastly AI inference at edge. |
| 2026 | El edge es viable para casos que antes requerían región completa. LLM inference en edge nodes para baja latencia. Wasm como deployment target universal en edge. |

---

### 14.2 V8 Isolates: El Runtime del Edge

**¿Por qué el edge puede ser tan rápido?** V8 Isolates son sandboxes dentro de un proceso V8. A diferencia de VMs o containers:
- No hay proceso OS separado
- No hay kernel isolation overhead
- Startup < 1ms vs ~500ms de una Lambda cold start
- Miles de isolates por proceso sin overhead significativo

**El precio:** No tienes un sistema de archivos real, no puedes hacer syscalls arbitrarias, no puedes correr binarios nativos (sin Wasm). Es un sandbox intencional.

**Cloudflare Workers Runtime:** Basado en V8 con un subset mínimo de APIs web. Fetch API para HTTP, Streams para I/O, Web Crypto, KV/D1/R2 como storage. Sin `process`, sin `fs`, sin `os`.

---

### 14.3 WebAssembly en el Edge

**El convergencia de Wasm + Edge es el desarrollo más importante 2023–2026:**

Con el Component Model (WASI 0.2+), puedes tener:
1. Un componente Wasm escrito en Rust con lógica de negocio
2. Un componente Wasm escrito en Go con parsing
3. Compuestos automáticamente en el edge sin overhead de red entre ellos
4. Ejecutados con aislamiento de seguridad fuerte
5. Con startup <5ms

**Proyectos clave:**
- **Spin (Fermyon):** Framework para microservicios Wasm en edge/serverless
- **wasmCloud (Cosmonic):** Actor model con Wasm; los actores pueden moverse entre hosts
- **Cloudflare Workers con Wasm:** Workers puede importar módulos Wasm directamente

---

### 14.4 Deno vs Bun vs Node: La Guerra de Runtimes

**Cronología:**

| Año | Hito |
|-----|------|
| 2020 | Deno 1.0 (Ryan Dahl, creador de Node, empieza desde cero). TypeScript nativo, web APIs. |
| 2021 | Deno Deploy beta. Deno formato TypeScript, permisos explícitos. |
| 2022 | **Bun 0.x** (Jarred Sumner): runtime Zig con JSC (JavaScriptCore de Safari) 3-10x más rápido que Node en benchmarks. |
| 2023 | **Bun 1.0** (septiembre 2023). Node.js compatible. npm compatible. Hot reloading nativo. Node.js 20 LTS con mejoras de performance. |
| 2024 | **Deno 2.0:** compatibilidad con Node.js y npm. La promesa de "mejor Node" finalmente viable. Bun 1.x series. |
| 2025 | Ecosistema: Node dominante en producción existente, Bun para proyectos nuevos que quieren performance, Deno para edge. |
| 2026 | WinterCG (Web Interoperability Community Group): estándar de APIs web compartidas entre Node, Deno, Bun, Cloudflare Workers, Deno Deploy. |

---

## Mapa de Influencias Cruzadas

```
Linear Types (teoría) → Rust ownership → Memory safety mainstream
        ↓
Algebraic Effects → OCaml 5 effects → Diseño de Roc/Gleam
        ↓
Dependent Types → Lean 4 → Verificación de cripto/ZK proofs

MLIR → Mojo (ML) → Triton kernels → Mejor inferencia AI
     ↓
Cranelift → Wasmtime → WASI 0.2 → Wasm Component Model → Edge computing

io_uring → I/O async eficiente → TigerBeetle → FoundationDB patterns
         ↓
eBPF → Observabilidad zero-code → OpenTelemetry + eBPF = OBI

CRDTs → Local-first software → Sync layers (ElectricSQL) → Offline-first apps
      ↓
Structured Concurrency → Swift Concurrency, Java Loom, Kotlin → async mainstream

Supply chain breach → SLSA/Sigstore → SBOMs → Compliance regulation
                   ↓
Memory unsafety → NSA/CISA advisories → Rust mandates → Linux kernel Rust

RISC-V → Open hardware → eBPF en RISC-V → CXL memory disaggregation
```

---

## Resumen Ejecutivo: Qué Aprender en 2026

**Nivel 1 — Fundamentos que ya no son opcionales:**
- Rust (o al menos leer código Rust y entender su model)
- WebAssembly + Component Model básico
- OpenTelemetry (traces, metrics, logs)
- Supply chain security (Sigstore, SLSA, SBOMs)
- Structured concurrency (async correcto)

**Nivel 2 — Diferenciadores técnicos:**
- Algebraic effects como modelo mental (entender OCaml 5, diseño de Koka)
- CRDTs y local-first (para apps que necesitan offline o colaboración)
- eBPF (para infra de alto rendimiento y seguridad)
- io_uring (para sistemas Linux de alta performance)
- DuckDB (para análisis de datos sin Spark)

**Nivel 3 — Frontier técnico:**
- Lean 4 / verificación formal (para código crítico o interés en PL)
- Graded types / session types (como framework conceptual, no necesariamente en prod)
- MLIR (si estás en compiladores o ML infra)
- CXL (si estás en systems/hardware)
- TLA+ (para diseñar protocolos distribuidos correctos)

---

*Fuentes principales utilizadas:*

- [WebAssembly en 2026: "Almost Ready"](https://www.javacodegeeks.com/2026/04/webassembly-in-2026-three-years-of-almost-ready.html)
- [WASI 0.2 Component Model — eunomia.dev](https://eunomia.dev/blog/2025/02/16/wasi-and-the-webassembly-component-model-current-status/)
- [Rust en Linux kernel: no experimental](https://devclass.com/2025/12/15/rust-boosted-by-permanent-adoption-for-linux-kernel-code/)
- [OCaml 5 multicore + effects](https://tarides.com/blog/2022-12-19-ocaml-5-with-multicore-support-is-here/)
- [Lean 4 en AWS, Google DeepMind](https://lean-lang.org/use-cases/mathlib/)
- [Leanstral (Mistral AI)](https://blockchain.news/news/mistral-leanstral-open-source-lean4-proof-agent)
- [SLSA y Sigstore supply chain](https://slsa.dev/)
- [OpenTelemetry eBPF 2026 Goals](https://opentelemetry.io/blog/2026/obi-goals/)
- [RISC-V Ubuntu 2026](https://canonical.com/blog/canonical-and-ubuntu-risc-v-a-2025-retro-and-looking-forward-to-2026)
- [TigerBeetle — QCon London 2023](https://qconlondon.com/presentation/mar2023/new-era-database-design-tigerbeetle)
- [Edge Computing 2026 — Cloudflare Workers](https://calmops.com/cloud/edge-computing-cloudflare-workers-complete-guide-2026/)
- [Zig 2026 milestones](https://medium.com/@anshusinghal703/the-future-of-systems-programming-rust-go-zig-and-carbon-a-2026-showdown-8714fd53a38a)
- [FlashAttention en Blackwell](https://developer.nvidia.com/blog/openai-triton-on-nvidia-blackwell-boosts-ai-performance-and-programmability/)
- [CXL + eBPF para LLMs](https://arxiv.org/html/2508.15980v1)
