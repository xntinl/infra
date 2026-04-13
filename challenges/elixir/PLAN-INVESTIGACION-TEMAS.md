# Plan de Investigación Detallado: Tendencias Tecnológicas 2020–2026

Documento Fuente: `/Users/consulting/Documents/consulting/infra/challenges/advanced-topics/tendencias-tecnologicas-2020-2026.md`

Objetivo: Investigación exhaustiva de cuatro paradigmas fundamentales que caracterizan el desarrollo de software moderno 2020–2026, con énfasis en implementación concreta en Go, Rust y Elixir.

Estado: Pendiente de investigación  
Última revisión: 2026-04-12

---

## 1. Introducción y Metodología

### 1.1 Alcance de la Investigación

Esta investigación se estructura en torno a cuatro pilares temáticos derivados del documento de referencia, cada uno seleccionado por su relevancia directa al desarrollo de sistemas modernos y su implementación en los tres lenguajes estudiados (Go, Rust, Elixir).

La metodología de investigación sigue un patrón consistente para cada tema:

1. Extracción de contenido del documento fuente
2. Investigación histórica: origen académico, evolución cronológica, estado actual
3. Análisis técnico: mecanismos de implementación, decisiones de diseño
4. Análisis comparativo: cómo se implementa en Go, Rust, Elixir
5. Evaluación crítica: trade-offs, limitaciones, mejores prácticas
6. Síntesis: recursos, ejemplos, conclusiones

### 1.2 Estructura Estándar por Tema

Cada tema incluirá:

- Ubicación precisa en documento fuente (secciones, líneas)
- Definiciones formales de conceptos
- Cronología detallada: pre-2020, 2020–2022, 2023–2026
- Análisis técnico profundo de cada concepto
- Implementación en tres lenguajes con código ejemplo
- Preguntas de investigación estructuradas por nivel de complejidad
- Casos de uso reales y patrones de implementación
- Análisis de trade-offs y gotchas comunes
- Recursos académicos y técnicos
- Checklist de verificación

---

## 2. TEMA 1: Paradigmas de Programación

### 2.1 Información de Ubicación

Sección del documento: 1.0 (Paradigmas de Programación)  
Subsecciones: 1.1, 1.2, 1.3  
Duración estimada de investigación: 8–10 horas

---

### 2.2 Definiciones y Conceptos Fundamentales

#### 2.2.1 Efectos Algebraicos

**Definición Formal:**

Los efectos algebraicos constituyen un mecanismo de control de flujo que desacopla la especificación semántica de una operación de su implementación operacional. En términos de teoría de lenguajes:

Sea E = {e1, e2, ..., en} un conjunto de efectos. Un programa que utiliza efectos en E se expresa como:

```
def compute(): T uses [E1, E2, ..., En]
```

Donde la función declara explícitamente qué efectos requiere, pero NO cómo se implementan. Un "handler" (manejador) de efectos proporciona la semántica operacional:

```
handle compute() with
  E1: { operation() => resume(value) }
  E2: { operation() => resume(value) }
```

**Componentes Arquitectónicos:**

1. Effect Definition
   - Especificación de operaciones que un efecto proporciona
   - Tipo de parámetros y valores de retorno
   - Contrato de interfaz

2. Effect Invocation
   - Lanzar un efecto desde código que lo declara
   - `perform` o `do` keyword según lenguaje
   - Transferencia de control al handler

3. Effect Handler
   - Implementación de interpretación para cada efecto
   - Recibe control cuando efecto es invocado
   - Puede ejecutar side effects reales o manipular continuación

4. Resumption/Continuation
   - Handler retoma ejecución del programa original
   - Paso del resultado calculado al punto de invocación
   - Permite múltiples interacciones entre programa y handler

**Diferencia Fundamental con Monads:**

Monads (Haskell):
- Requieren "monad transformers" para composición de múltiples efectos
- Transformers (ReaderT, StateT, etc.) son boilerplate pesado
- Efecto es parte de la firma de tipo, propagado en cadena de llamadas
- Ejemplo: `IO` es funtor, no se puede dejar de lado

Effect Handlers (OCaml 5.0+):
- Composición directa sin transformers
- Efecto declarado pero no en firma recursivamente
- Handler puede ser insertado arbitrariamente
- Más flexible, menos acoplamiento

**Contexto Histórico Detallado:**

- 1973: Carl Hewitt publica sobre modelos de computación con efectos
- 2009: Plotkin & Pretnar publican "Handlers of Algebraic Effects" - foundational paper
  - Define effect signature: operaciones que efecto expone
  - Define handler clauses: interpretación de cada operación
  - Demuestra composición de múltiples handlers
  
- 2013–2019: Lenguajes de investigación
  - Eff (Pretnar et al., PPDP 2013): lenguaje didáctico con effects tipados
  - Frank (Lindley & McBride, 2014): effects polimórficos
  - Effekt (Brachthäuser et al., 2016): effects y control
  - Koka (Daan Leijen, Microsoft Research): primer lenguaje práctico

- 2020: Koka 2.0 lanzado con type-directed effect inference
  - Compilador infiere qué efectos usa cada función
  - Permite omitir anotaciones de efectos en muchos casos

- 2021: OCaml Multicore mergeado con soporte de effects
  - Implementación de Dolan et al. basada en delimited continuations
  - Primero: effects son no-tipados (issue: type safety)
  - Segundo: compiler se vuelve más agresivo en optimizaciones

- 2022: OCaml 5.0 lanzado (12 diciembre) con effect handlers production-ready
  - Eio library (Dolan, KC Sivaramakrishnan)
  - Demuestra IO library sin monads
  - Performance competitiva con async/await en Rust

- 2024–2026: Adopción en nuevos lenguajes
  - Roc language (compilador Wasm, FP puro)
  - Unison language (contenido-direccionado)
  - Propuesta: WebAssembly Effect Handlers extension

**Por qué Importa en 2026:**

1. Soluciona "async/await viral problem": en JavaScript/Rust, async contamina toda cadena
2. Composición sin overhead de monad transformers
3. Testabilidad: handler permite inyectar implementaciones fake
4. Explicitness: efectos declarados hacen código más legible
5. Seguridad: type system puede garantizar que solo efectos permitidos se usan

---

#### 2.2.2 Programación Funcional Aplicada

**Tesis Central:**

Entre 2020–2026, la programación funcional transitó de ser "paradigma académico" a ser "arquitectura por defecto" en sistemas modernos. Este cambio no ocurrió por evangelismo, sino porque:

1. Lenguajes mainstream adoptaron ideas FP (Rust ownership, Python match, Java records)
2. Tooling (compiladores, debuggers) aprovecha inmutabilidad para optimizaciones
3. Sistemas distribuidos/concurrentes son más fáciles con inmutabilidad
4. Machine learning frameworks usan transformaciones funcionales

**Conceptos Clave de FP Aplicada:**

**1. Inmutabilidad por Defecto**

Definición: Todos los datos son no-mutables por defecto; mutación explícita requiere ceremonial.

Implementación por lenguaje:

Rust:
```rust
let x = 5;           // immutable, no se puede asignar de nuevo
let mut y = 5;       // mutable, requiere keyword explícito
```
- Semántica: ownership vinculado a mutabilidad
- Ventaja: borrow checker previene data races
- Trade-off: lenguaje requiere anotación `mut`

Elixir:
```elixir
x = 5     # binding, no asignación
x = 6     # nuevo binding de x a 6, el 5 anterior no muta
```
- Semántica: pattern matching + bindings
- Ventaja: imposible mutar, garantía de compilador
- Trade-off: ninguno, es el diseño del lenguaje

Go:
```go
var x int = 5
x = 6  // mutación permitida, sin restricción
```
- Semántica: variables mutables por defecto
- Ventaja: simple, explícito
- Trade-off: requiere disciplina del programador

**Impacto Técnico:**

- Compiler optimizations: sin aliases, puede asumir independence de datos
- Concurrencia: sin races de lectura-escritura por construcción
- Testing: estado no sorpresas por mutaciones ocultas
- Debugging: reproducibilidad (entrada → salida siempre igual)

**2. Pattern Matching**

Definición: Deconstrucción estructurada de datos en rama de control, combinada con guards lógicos.

Pre-2020: Solo en lenguajes funcionales (Haskell, Erlang, OCaml)
2020–2026: Adopción masiva en lenguajes mainstream

Timeline detallada:

2020:
- PEP 622 propuesto para Python (Guido van Rossum)
- Debate en comunidad: ¿es necesario en Python?
- Java: switch es limitado (solo valores primitivos)

2021:
- Python 3.10 lanzado (octubre) con match/case
- Scala 3.0 lanzado con pattern matching mejorado
- TypeScript 4.4: const type parameters (mejora type inference)

2023:
- Java 21 (LTS) con pattern matching exhaustivo
- C# 8+ con pattern matching expressions
- JavaScript: proposal for destructuring mejorado

2026:
- Pattern matching es estándar en todo lenguaje nuevo
- Bases de código Python usan 3.10+ por esta razón

**Estructura Técnica:**

```python
# Python 3.10+ (pattern matching)
match point:
  case (0, 0):
    print("origin")
  case (0, y):
    print(f"on y-axis at {y}")
  case (x, 0):
    print(f"on x-axis at {x}")
  case (x, y):
    print(f"at ({x}, {y})")
  case _:
    print("not a point")
```

Componentes:
- Pattern: estructura a deconstructir (tupla, lista, clase)
- Guard: condición lógica adicional (if clause)
- Action: rama de código ejecutado si match

Ventajas sobre if/else encadenado:
- Exhaustiveness checking: compilador verifica todos casos cubiertos
- Type refinement: dentro de rama, tipos son más específicos
- Legibilidad: estructura es clara
- Seguridad: imposible olvidar caso

**3. Union Types / Algebraic Data Types**

Definición: Tipo que puede ser una de varias variantes distintas, mutuamente excluyentes.

Formalismo:

```
data Result a e = Ok a | Err e
data Maybe a = Just a | Nothing
data Status = Pending | Active Duration | Error String
```

Cada variante puede tener datos asociados diferentes.

**Por qué Importa:**

Null Pointer Problem Clásico:
```java
// Java traditional
String result = getResult();  // ¿es null o no?
```

Solución con Union Types:
```rust
// Rust
enum Result<T, E> {
  Ok(T),
  Err(E),
}

fn getResult() -> Result<String, Error> {
  // DEBE retornar Ok o Err explícitamente
}

// Uso:
match result {
  Ok(value) => println!("{}", value),
  Err(e) => eprintln!("{}", e),
}
```

Ventajas:
1. Type safety: no existe "null", debe ser explícito
2. Exhaustiveness: compilador fuerza todos casos
3. Decoupling: receptor no debe asumir tipo
4. Semantics: diferencia entre "valor no disponible" vs "error"

Implementación por lenguaje:

Rust:
```rust
enum Status {
  Idle,
  Running(Duration),
  Error(String),
}

match status {
  Status::Idle => {},
  Status::Running(d) => println!("Running for {:?}", d),
  Status::Error(msg) => eprintln!("Error: {}", msg),
}
```

Elixir:
```elixir
case status do
  :idle -> :ok
  {:running, duration} -> IO.puts("Running for #{duration}ms")
  {:error, msg} -> IO.puts("Error: #{msg}")
end
```

Go (sin union types nativos, workaround con interface{}):
```go
// Go debe usar interface{} y type assertion
var status interface{}

switch v := status.(type) {
case nil:
  fmt.Println("Idle")
case time.Duration:
  fmt.Printf("Running for %v\n", v)
case error:
  fmt.Printf("Error: %v\n", v.Error())
}
```

**4. Composición sobre Herencia**

Definición: Construir comportamiento combinando objetos/tipos pequeños vs subclasificación.

Pre-OOP Moderno:
```java
// Herencia (1980s–2000s paradigma)
class Animal { }
class Dog extends Animal { }
class Cat extends Animal { }
// Problema: rigid hierarchy, difficulty with multiple aspects
```

Composición (2010s+):
```go
// Go interfaces + composition
type Reader interface {
  Read(p []byte) (n int, err error)
}

type Writer interface {
  Write(p []byte) (n int, err error)
}

type ReadWriter struct {
  reader Reader
  writer Writer
}
```

Ventajas:
1. Flexibility: comportamiento puede venir de múltiples fuentes
2. Decoupling: no existe "superclase" rígida
3. Testability: componentes pueden ser mockeados independientemente
4. Reusability: composición permite patterns más variados

Implementación por lenguaje:

Rust:
```rust
trait Reader {
  fn read(&mut self) -> Result<Vec<u8>>;
}

trait Writer {
  fn write(&mut self, data: &[u8]) -> Result<()>;
}

struct Channel {
  reader: Box<dyn Reader>,
  writer: Box<dyn Writer>,
}
```

Go:
```go
type Handler struct {
  logger Logger
  db     Database
  cache  Cache
}

// Métodos de Handler componen comportamiento
func (h Handler) ProcessRequest(req Request) Response {
  h.logger.Log("Processing")
  // usa h.db, h.cache
}
```

Elixir:
```elixir
defmodule Pipeline do
  def process(data, handlers) do
    Enum.reduce(handlers, data, fn handler, acc ->
      handler.(acc)
    end)
  end
end
```

---

#### 2.2.3 Programación Lógica: Datalog

**Contexto Histórico:**

- 1970s: Prolog creado (Colmeraurer, Kowalski)
- 1980s–1990s: Auge y caída de sistemas expertos basados en Prolog
- 1989: Datalog formalizado como subconjunto de Prolog (Ceri, Gottlob, Tanca)
  - Datalog = Prolog sin cut (!) y sin variables anónimas en ciertos contextos
  - Semántica declarativa fija: todas las soluciones, no primera solución
  
- 2010s: Resurgimiento silencioso
  - Soufflé (2013): Datalog compilado a C++ para análisis estático
  - CodeQL (antes Semmle, 2014): análisis de vulnerabilidades
  - DOOP: análisis de programas Java en escala
  
- 2020–2026: Explosión de aplicaciones
  - GitHub CodeQL: usado para scanear 100M+ repositorios
  - XTDB: base de datos con Datalog como query language
  - LLM reasoning: Minikanren (unificación) influencia en reasoning

**Definición Formal de Datalog:**

Un programa Datalog consiste en:
1. Hechos (facts): predicados base
2. Reglas (rules): implicaciones lógicas
3. Queries: preguntas sobre hechos derivados

Ejemplo:

```prolog
% Hechos
parent("alice", "bob").
parent("bob", "charlie").

% Regla: X es abuelo de Z si existe Y tal que X es padre de Y y Y es padre de Z
ancestor(X, Z) :- parent(X, Y), parent(Y, Z).

% Query: ¿quién es ancestro de charlie?
?- ancestor(X, "charlie").
% Resultado: X = "alice"
```

**Semántica:**

Forward Chaining (bottom-up):
1. Comienza con hechos conocidos
2. Aplica reglas iterativamente para derivar nuevos hechos
3. Continúa hasta punto fijo (no nuevos hechos pueden derivarse)
4. Responde query con todos hechos derivados que coinciden

Backward Chaining (top-down):
1. Comienza con goal (query)
2. Busca reglas que pueden probar goal
3. Recursivamente prueba condiciones de la regla
4. Retorna todas soluciones encontradas

**Diferencia Crítica con Prolog:**

Prolog:
- Tiene cut (!): detiene búsqueda tras primera solución
- Orden de cláusulas importa
- Negación como failure: ¬P es verdadero si P no puede probarse
- Turing-completo (puede loop infinitamente)

Datalog:
- Sin cut: todas soluciones por defecto
- Orden no importa (semántica fija)
- Negación estratificada: debe estar bien definida
- Garantía de terminación: siempre termina

**Aplicaciones Modernas en Detalle:**

**1. Análisis Estático y Seguridad:**

CodeQL (GitHub):
```sql
-- Detectar SQL injection
select source, sink
where untrustedInput(source)
  and sqlQuery(sink)
  and dataFlow(source, sink)
```

Predicados que se escriben una sola vez, pero se ejecutan contra millones de líneas de código en GitHub. Eficiencia logarítmica en tamaño del programa.

**2. Sistemas de Reglas Empresariales:**

```prolog
% Compliance rule
high_risk_transaction(Transaction) :-
  amount(Transaction, Amount),
  Amount > 10000,
  first_time_customer(Customer),
  customer_of(Transaction, Customer).

alert(Transaction, "high_risk") :-
  high_risk_transaction(Transaction).
```

Ventaja: regla es verificable (puede ser auditada), no buried en código imperativo.

**3. Knowledge Graphs y Semantic Web:**

```turtle
@prefix rdf: <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix dbo: <http://dbpedia.org/ontology/> .

dbr:Albert_Einstein dbo:knownFor dbr:Theory_of_relativity .

% En Datalog:
discovered(Person, Theory) :- knownFor(Person, Theory).
```

**4. Reescritura de Términos y Optimización:**

e-graphs (Egg project):
- Representar múltiples expresiones equivalentes en DAG comprimido
- Usar Datalog para encontrar equivalencias
- Aplicar transformaciones más eficientes

```
f(a) + g(b) = (using commutativity)
g(b) + f(a) = (using distribution)
g(b) * 1 + f(a) * 1 = ...
```

---

### 2.3 Análisis Técnico Profundo por Lenguaje

#### 2.3.1 Rust: Ownership como Sistema de Efectos

**Tesis:** El sistema de ownership + borrowing en Rust es una implementación de un sistema de efectos que previene data races, use-after-free, y otras vulnerabilidades de memoria.

**Mecanismos:**

1. Ownership (Propiedad Única)
   - Todo valor en Rust tiene exactamente un "dueño"
   - Cuando dueño sale de scope, valor se drop (libera)
   - Transferencia de propiedad es explícita

   ```rust
   let s = String::from("hello");  // s es dueño
   let s2 = s;                     // propiedad trasferida a s2
   // println!("{}", s);           // ERROR: s ya no es dueño
   ```

2. Borrowing (Préstamo)
   - Referencia (&) permite acceso sin transferencia
   - Shared references (&T): múltiples lectores, sin escritura
   - Mutable references (&mut T): escritor exclusivo

   ```rust
   let s = String::from("hello");
   let r1 = &s;     // shared borrow
   let r2 = &s;     // ok, múltiples shared borrows
   // let r3 = &mut s;  // ERROR: no puede haber mut borrow si hay shared
   ```

3. Lifetimes ('a)
   - Anotación que vincula duración de referencia con variable que referencia
   - Previene dangling pointers

   ```rust
   fn longest<'a>(s1: &'a str, s2: &'a str) -> &'a str {
     if s1.len() > s2.len() { s1 } else { s2 }
   }
   ```

**Cómo es Sistema de Efectos:**

- Effect: "acceso a memoria"
- Declaración: firma de función specifica ownership/borrowing
- Interpretation: borrow checker verifica seguridad
- Ventaja: memory safety sin garbage collector

**Inmutabilidad en Rust:**

```rust
let mut x = 5;        // mutable binding
x = 6;                // ok

let y = 5;            // immutable binding
// y = 6;             // ERROR

// Pero: todo es por defecto immutable
struct Point {
  x: i32,
  y: i32,
}
let p = Point { x: 1, y: 2 };
// p.x = 10;          // ERROR: p no es mut
```

**Pattern Matching en Rust:**

```rust
enum Message {
  Quit,
  Move { x: i32, y: i32 },
  Write(String),
  ChangeColor(i32, i32, i32),
}

match msg {
  Message::Quit => println!("Quit"),
  Message::Move { x, y } => println!("Move to ({}, {})", x, y),
  Message::Write(s) => println!("Write: {}", s),
  Message::ChangeColor(r, g, b) => println!("RGB({}, {}, {})", r, g, b),
}
```

Exhaustiveness checking: compilador verifica que TODOS casos de enum estén cubiertos.

---

#### 2.3.2 Elixir: Pattern Matching y Procesos

**Tesis:** Elixir implementa programación funcional pura mediante pattern matching obligatorio y procesos como actores que modelan estado.

**Pattern Matching (Core del Lenguaje):**

A diferencia de Rust (match es control flow), en Elixir pattern matching es ASIGNACIÓN:

```elixir
{:ok, value} = result      # Destructuring assignment
# Si result no es {:ok, _}, raises error

# En function heads:
def handle({:ok, value}) do
  IO.puts("Success: #{value}")
end

def handle({:error, reason}) do
  IO.puts("Error: #{reason}")
end

def handle(_) do
  IO.puts("Unknown")
end
```

**Ventaja:** Pattern matching es obligatorio, no opcional.

**Inmutabilidad Radical:**

```elixir
x = 1
x = 2     # Nuevo binding de x, no mutación
          # El 1 anterior sigue siendo 1 en otro contexto

list = [1, 2, 3]
new_list = [0 | list]    # Construye nueva lista con prepend
# list sigue siendo [1, 2, 3]
# new_list es [0, 1, 2, 3]
```

Implicación: toda operación retorna nuevo dato, datos nunca cambian in-place.

**Procesos y Estado:**

En Elixir, estado se modela con procesos (actores):

```elixir
defmodule Counter do
  def start_link(initial) do
    {:ok, spawn_link(fn -> loop(initial) end)}
  end

  defp loop(count) do
    receive do
      {:increment} ->
        loop(count + 1)
      {:get, reply_to} ->
        send(reply_to, count)
        loop(count)
      {:stop} ->
        :ok
    end
  end
end
```

- Proceso es isolado (actor)
- Estado es local a proceso (count)
- Mensajes son únicos medio de comunicación
- Fault tolerance: supervisor puede restart proceso

**Functional Programming Practical:**

```elixir
# Pipe operator: composición
data
  |> Enum.map(&process/1)
  |> Enum.filter(&valid?/1)
  |> Enum.reduce(0, &sum/2)

# Equivalente imperativo:
results = []
for item in data:
  processed = process(item)
  if valid?(processed):
    results.append(processed)
return sum(results)
```

Pipe operator es azúcar sintáctico que convierte:
```
x |> f |> g |> h
```
en:
```
h(g(f(x)))
```

---

#### 2.3.3 Go: Pragmatismo sobre Pureza

**Tesis:** Go adopta ideas de FP donde son prácticas, pero rechaza pureza en favor de simplicidad.

**Ausencia de Pattern Matching Nativo:**

Go NO tiene pattern matching. Debe usarse switch/if:

```go
switch v := result.(type) {
case nil:
  fmt.Println("No result")
case error:
  fmt.Printf("Error: %v\n", v)
default:
  fmt.Printf("Success: %v\n", v)
}
```

Limitaciones:
- Type assertions (.(type)) son verbosas
- No hay exhaustiveness checking
- Menos seguro que pattern matching

**Composición sobre Herencia:**

```go
// Go tiene zero inheritance
// Composición via embedding:

type Reader interface {
  Read(p []byte) (n int, err error)
}

type Writer interface {
  Write(p []byte) (n int, err error)
}

type File struct {
  // Compose Reader y Writer por embedding
  Reader
  Writer
  path string
}
```

Interfaces son implícitas:
```go
// Qualquier tipo que implemente Read() es Reader
// No necesita explícitar "implements Reader"
```

**Funciones como First-Class:**

```go
// Higher-order functions
func map(arr []int, f func(int) int) []int {
  result := make([]int, 0)
  for _, v := range arr {
    result = append(result, f(v))
  }
  return result
}

map([]int{1, 2, 3}, func(x int) int { return x * 2 })
```

**Errores como Valores:**

```go
f, err := os.Open("file.txt")
if err != nil {
  log.Fatal(err)
}

// Parecido a Result<T, E> en Rust, pero manual:
// - Cargo del programador verificar err
// - No hay exhaustiveness checking
// - Fácil olvidar chequear error
```

**Concurrencia: Goroutines + Channels**

```go
// Goroutine: lightweight thread
go func() {
  fmt.Println("Running concurrently")
}()

// Channel: comunicación entre goroutines
ch := make(chan int)
go func() {
  ch <- 42  // send
}()
v := <-ch  // receive
```

---

### 2.4 Cronología Detallada: Evolución 2020–2026

**AÑO 2020**

Enero–Marzo:
- Python PEP 622 propuesto (pattern matching)
- Debate comienza en comunidad
- Rust 1.40 con async/await improvements

Abril–Junio:
- Rust Foundation creada (mayo)
- Soufflé continúa evolución
- GitHub Semmle (future CodeQL) es herramienta privada

Julio–Septiembre:
- JavaScript TC39 discute pattern matching
- Python 3.9 lanzado (octubre) - sin pattern matching aún
- CodeQL comienza migración a GitHub (septiembre)

Octubre–Diciembre:
- PEP 622 modificada (mejoras sintácticas)
- Efectos algebraicos: Koka 2.0 road map
- Publicación papers: "Practical Effects" (varios)

**AÑO 2021**

Enero–Marzo:
- Scala 3.0 lanzado (mayo): union types, pattern matching
- Rust 2021 Edition roadmap
- OCaml Multicore propuesta oficial

Abril–Junio:
- Python 3.10 RC1 lanzado (agosto)
- Pattern matching es feature principal
- Elixir 1.12 con improvements en pattern matching

Julio–Septiembre:
- Python 3.10 release (octubre 4)
- Pattern matching adopción comienza
- Rust Async book: recursos sobre colored functions

Octubre–Diciembre:
- Koka publicado con type-directed effects
- CodeQL en GitHub (producto público)
- OCaml Multicore: efects mergeado al trunk (PR #10470)

**AÑO 2022**

Enero–Marzo:
- OCaml 4.14 release (marzo)
- Eff language madura
- Gleam 0.17: FP que compila a Erlang BEAM

Abril–Junio:
- Linux kernel: RFC para Rust soporte
- Java 19: virtual threads announcement (Project Loom)
- Rust 1.62: #[must_use] attributes

Julio–Septiembre:
- Efectos algebraicos: Scala 3.1 Capabilities discussion
- OCaml 5.0 release (diciembre 17)
- Eio library preview (effect handlers para IO)

Octubre–Diciembre:
- OCaml 5.0 lanzado, production-ready
- Eio 0.1 release
- Rust 1.65: keyword async fn

**AÑO 2023**

Enero–Marzo:
- Eio 0.10 estable
- CodeQL scans 10M+ repos en GitHub
- Java 21 preview: pattern matching exhaustiveness

Abril–Junio:
- Datalog más allá de seguridad: Datomic adopción crece
- Roc language avanzando
- Python 3.11: structural pattern matching improvements

Julio–Septiembre:
- Gleam 0.30+ stabilization
- Effect handlers: Haskell proposal discutido
- Rust async: trait methods

Octubre–Diciembre:
- Java 21 (LTS) release: pattern matching stabilo
- XTDB 2.0 with Datalog query
- Scala 3.3 with union type improvements

**AÑO 2024**

Enero–Marzo:
- Rust 1.75: async fn improvement
- CodeQL: 100M+ repos analizados
- Roc 0.14: compilador casi self-hosted

Abril–Junio:
- Rust 1.78: enhanced error messages
- Java 23: record patterns (deeper pattern matching)
- Effekt language: polimórficos effects

Julio–Septiembre:
- Roc avanzando hacia 1.0
- WebAssembly: effect handlers proposal avanzada
- Kotlin: improvements en sealed classes

Octubre–Diciembre:
- Rust 1.85 (Rust 2024 Edition)
- async closures stabilizadas
- `gen` keyword reserved para generators

**AÑO 2025**

Enero–Junio:
- Rust 1.8x: ergonomics focus
- async traits: completamente estabilizadas
- OCaml 5.2: más optimizaciones

Julio–Diciembre:
- Rust en Linux kernel: declarado "no experimental" (december)
- Roc language: 1.0 release esperado para 2026
- Datalog como IR para compiladores

**AÑO 2026**

Enero–Junio:
- Efectos algebraicos: fundamento de lenguajes nuevos
- Pattern matching: estándar universal
- Roc language: production use
- WebAssembly: effect handlers disponible

---

### 2.5 Preguntas de Investigación Estructuradas

**Nivel 1: Conceptual (Fundamentos)**

1. ¿Cuál es la diferencia fundamental entre una mónada y un effect handler?
   - Respuesta esperada: Monad es tipo que envuelve computación, effect handler es interpretación de efecto específico
   - Profundidad: comparar composición, acoplamiento, flexibilidad

2. ¿Cómo pattern matching previene bugs comparado con if/else encadenado?
   - Respuesta esperada: exhaustiveness checking, type refinement, no valores olvidados
   - Profundidad: mostrar ejemplo donde if/else falla

3. ¿Qué es "null safety" y cómo union types lo logran?
   - Respuesta esperada: null es implícito, union types lo hacen explícito en tipo
   - Profundidad: estadísticas de bugs causados por null

4. ¿Por qué Elixir es más "funcional" que Go fundamentalmente?
   - Respuesta esperada: Elixir: inmutable, pattern matching obligatorio, procesos; Go: mutable, pattern matching optional
   - Profundidad: trade-offs de cada enfoque

**Nivel 2: Técnico (Implementación)**

5. ¿Cómo implementa Rust memory safety sin garbage collector?
   - Respuesta esperada: ownership + borrowing + borrow checker
   - Profundidad: ejemplos de: use-after-free prevención, data race prevention

6. ¿Cómo el borrow checker de Rust es un sistema de efectos?
   - Respuesta esperada: efecto = acceso a memoria, handler = verificador estático
   - Profundidad: paralelos teóricos, limitaciones del enfoque

7. ¿Cómo funciona "forward chaining" en Datalog vs "backward chaining" en Prolog?
   - Respuesta esperada: forward: hechos → derivados iterativamente; backward: goal → pruebas de condiciones
   - Profundidad: complejidad, optimizaciones, cuándo elegir cuál

8. ¿Por qué OCaml 5.0 logró implementar effect handlers production-ready?
   - Respuesta esperada: arquitectura del runtime, delimited continuations, optimizaciones
   - Profundidad: comparación con otras implementaciones

**Nivel 3: Arquitectónico (Sistemas)**

9. ¿Cómo diseñar un sistema altamente concurrente en Elixir vs Go vs Rust?
   - Respuesta esperada: Elixir: procesos/supervisors; Go: goroutines/channels; Rust: tokio/async
   - Profundidad: casos de uso donde cada brilla/falla

10. ¿Cómo CodeQL usa Datalog para detectar vulnerabilidades de seguridad?
    - Respuesta esperada: define predicados para source/sink/dataflow, queries buscan patterns inseguros
    - Profundidad: ejemplos específicos de detección, falsos positivos

11. ¿Cómo sería un compilador moderno que use Datalog como intermediate representation?
    - Respuesta esperada: optimizaciones como query sobre IR, pattern matching sobre AST
    - Profundidad: MLIR, e-graphs, reescritura simbólica

12. ¿Cuál es la relación entre pattern matching, union types, y exhaustiveness checking?
    - Respuesta esperada: pattern matching deconstruye union types, compilador verifica todos casos
    - Profundidad: cómo los tres trabajan juntos para type safety

---

### 2.6 Casos de Uso Reales y Ejemplos Prácticos

**Caso 1: Sistema de Procesamiento de Eventos en Rust**

Requisito: procesar eventos de usuario, aplicar efectos (logging, base datos, caching), composición limpia de efectos.

```rust
// Sin algebraic effects (monadic):
type Result<T> = std::result::Result<T, Error>;

async fn process(event: Event) -> Result<()> {
  log(&event).await?;
  let user = fetch_user(&event.user_id).await?;
  cache_user(&user).await?;
  Ok(())
}

// Con effect handlers (hypothetical Rust):
// (Rust aún no tiene, pero concepto)
#[effects(Log, Fetch, Cache)]
fn process(event: Event) {
  perform Log::log(event);
  let user = perform Fetch::user(event.user_id);
  perform Cache::cache_user(user);
}

handle process(event) with
  | Log::log(e, k) => { println!("{:?}", e); k() }
  | Fetch::user(id, k) => { k(database.get(id)) }
  | Cache::cache_user(u, k) => { redis.set(u.id, u); k() }
```

Ventaja del segundo: composición sin modificar `process`, inyección de comportamiento en handler.

**Caso 2: Sistema de Análisis de Seguridad en Datalog**

```prolog
% Definir qué es "user input"
user_input(Var) :- 
  call_to(_, "request.getParameter", [Var | _]).

user_input(Var) :-
  call_to(_, "Scanner.nextLine", [Var | _]).

% Definir qué es "SQL query"
sql_query(Var) :-
  call_to(_, "PreparedStatement.setString", [_, Var]).

% Definir data flow
flows_to(From, To) :-
  assignment(From, To).

flows_to(From, To) :-
  flows_to(From, Mid),
  assignment(Mid, To).

% Query: encontrar SQL injections
vulnerability("sql_injection", Var) :-
  user_input(Var),
  sql_query(Var),
  flows_to(Var, Var).

% Ejecutar query sobre millones de líneas de código
?- vulnerability(Type, Var).
```

Un conjunto pequeño de predicados computa análisis sobre repositorios enteros.

**Caso 3: Orquestación de Procesos en Elixir**

```elixir
defmodule OrderProcessor do
  def process_order(order_id) do
    order = fetch_order(order_id)
    payment = process_payment(order)
    
    case payment do
      {:ok, receipt} ->
        shipment = request_shipment(order)
        notify_customer(order, receipt, shipment)
        {:ok, "Order processed"}
      
      {:error, reason} ->
        log_failure(order_id, reason)
        notify_customer_failure(order_id, reason)
        {:error, reason}
    end
  end
end

# Ventaja: pattern matching es obligatorio
# No existe path donde error es ignorado
# Elixir compiler garantiza exhaustiveness
```

---

### 2.7 Trade-offs y Gotchas

**Efectos Algebraicos:**

- Gotcha: No hay estándar. OCaml usa effects no-tipados, Scala propone tipados, Haskell quiere otro enfoque
- Trade-off: Composición limpia vs complejo runtime (delimited continuations)
- Gotcha: Aún hay investigación activa; no es "solved problem"
- Performance: Continuations pueden ser overhead si no optimizadas

**Programación Funcional:**

- Gotcha: Inmutabilidad requiere copias. Lenguajes como Elixir usan structural sharing para eficiencia
- Trade-off: Seguridad vs rendimiento. Copia profunda es segura pero lenta
- Gotcha: Developers acostumbrados a mutación pueden escribir FP incorrectamente
- Performance: GC pressure si no se optimiza copy-on-write

**Datalog:**

- Gotcha: Negación es complicada (stratified negation). ¬P requiere P sea ground
- Trade-off: Expresividad vs declaratividad. Algunas optimizaciones requieren hints
- Gotcha: Loops infinitos si no se es cuidadoso (aunque garantía de terminación existe)
- Scalability: Naïve implementation es O(n³). Optimizaciones (tabling) necesarias

---

### 2.8 Recursos Académicos y Técnicos

**Papers Foundacionales:**

- Plotkin, G., & Pretnar, M. (2009). "Handlers of Algebraic Effects". ICFP
- Ceri, C., Gottlob, G., & Tanca, L. (1989). "What You Always Wanted to Know About Datalog (And Never Dared to Ask)". IEEE TKDE
- Gordon, A. D. (2020). "An Introduction to Algebraic Effects and Handlers"

**Lenguajes y Documentación:**

- OCaml 5.0 Manual: https://ocaml.org/releases/5.0/manual/effects.html
- Koka Language: https://koka-lang.github.io/
- Datalog Soufflé: https://souffle-lang.github.io/
- CodeQL: https://codeql.github.com/

**Artículos Técnicos:**

- "The Death of OOP" (análisis de por qué FP domina)
- "Colored Functions Considered Harmful" (debate sobre async)
- "Thinking in Datalog" (tutorial práctico)

---

### 2.9 Checklist de Verificación - Tema 1

- [ ] Comprendo diferencia fundamental entre monad y effect handler
- [ ] Puedo explicar por qué pattern matching es mejor que if/else con ejemplos
- [ ] Entiendo cómo union types previenen null bugs
- [ ] Conozco aplicaciones reales de Datalog (mínimo 3)
- [ ] Puedo escribir y explicar pattern matching en Rust y Elixir
- [ ] Comprendo por qué Go no tiene pattern matching nativo
- [ ] Conozco timeline histórico: pre-2020, 2020–2022, 2023–2026
- [ ] Puedo exemplificar "FP aplicada" en sistemas que uso
- [ ] Comprendo trade-offs: seguridad vs pragmatismo vs rendimiento
- [ ] He documentado mínimo 5 ejemplos de código en Rust/Elixir/Go
- [ ] Entiendo relación entre ownership (Rust) y sistema de efectos
- [ ] Sé diferenciar forward chaining vs backward chaining en Datalog

---

## 3. TEMA 2: Sistemas de Tipos Avanzados

**Duración estimada:** 8–10 horas

### 3.1 Ubicación en Documento Fuente

Sección: 3 (Sistemas de Tipos Avanzados)
Estado: Pendiente de investigación profunda

### 3.2 Estructura de Investigación

**Conceptos Clave a Investigar:**

1. Union Types / Tipos Suma
   - Definición formal
   - Implementación en Rust, TypeScript, Kotlin
   - Comparación con nullable types
   - Exhaustiveness checking

2. Type Inference
   - Hindley-Milner algorithm
   - Algoritmos de unificación
   - Limitaciones y cuando falla

3. Refinement Types
   - Tipos que restringen valores
   - Newtype pattern
   - Liquid types (investigación)

4. Dependent Types
   - Tipos que dependen de valores
   - Lenguajes: Idris, Agda
   - Aplicaciones prácticas

5. Type-Level Programming
   - Const generics en Rust
   - Associated types
   - GATs (Generic Associated Types)

6. Gradual Typing
   - Tipos opcionales vs obligatorios
   - Python/Elixir approach
   - Trade-offs

**Preguntas de Investigación Principales:**

1. ¿Cómo Rust usa el sistema de tipos para garantizar memory safety?
2. ¿Cuál es la diferencia entre static y dynamic typing en 2026?
3. ¿Qué es "colored functions" problem y cómo type systems lo abordan?
4. ¿Cómo Elixir combina dynamic typing con type checking (dialyzer)?
5. ¿Por qué Go rechaza type inference sofisticado?

---

## 4. TEMA 3: Sistemas Distribuidos

**Duración estimada:** 10–12 horas

### 4.1 Ubicación en Documento Fuente

Sección: 9 (Sistemas Distribuidos)

### 4.2 Conceptos Clave a Investigar

1. Consensus Algorithms
   - Raft: mecanismo detallado
   - Paxos: historia y complejidad
   - Byzantine Fault Tolerance
   - Aplicaciones en bases de datos

2. Event Sourcing & CQRS
   - Arquitectura basada en eventos
   - Time-travel queries
   - Proyecciones y snapshots
   - Eventual consistency

3. CAP Theorem
   - Consistency, Availability, Partition Tolerance
   - Implicaciones en 2026
   - Trade-offs en sistemas reales

4. Observabilidad Distribuida
   - Tracing, metrics, logs (the three pillars)
   - OpenTelemetry
   - Instrumentación en microservicios

5. Service Mesh & Resilience
   - Circuit breakers
   - Retries, timeouts, bulkheads
   - Istio, Linkerd, Envoy

**Preguntas de Investigación Principales:**

1. ¿Cómo Raft garantiza safety en consenso?
2. ¿Cuándo elegir event sourcing vs CRUD tradicional?
3. ¿Cómo implementar observabilidad distribuida con OpenTelemetry?
4. ¿Qué es CAP theorem y cómo impacta decisiones arquitectónicas?

---

## 5. TEMA 4: Modelos de Concurrencia

**Duración estimada:** 10–12 horas

### 5.1 Ubicación en Documento Fuente

Sección: 6 (Modelos de Concurrencia)

### 5.2 Conceptos Clave a Investigar

1. Actor Model
   - Origen y historia
   - Erlang/BEAM implementación
   - Supervisors y hierarchies
   - Comparación con otras modelos

2. Async/Await
   - Coroutines stackless vs stackful
   - Runtime vs no-runtime
   - Colored functions problem
   - Evolución 2020–2026

3. Structured Concurrency
   - Nurseries / scopes
   - Composabilidad
   - Manejo de errores en contexto
   - Implementaciones (Trio, Tokio)

4. Green Threads
   - Lightweight threads
   - Goroutines vs Processes
   - Scheduling
   - Escalabilidad

5. Concurrency Primitives
   - Channels, mutexes, atomics
   - Memory ordering
   - Deadlock prevention

**Preguntas de Investigación Principales:**

1. ¿Por qué Elixir (actor model) vs Go (goroutines)?
2. ¿Cómo Rust resuelve el "colored functions" problem?
3. ¿Cómo escala concurrencia en Elixir vs Go en máquinas con miles de cores?
4. ¿Qué es structured concurrency y por qué es importante?

---

## 6. Metodología de Ejecución para Agente

### 6.1 Fases de Investigación

**Fase 1: Extracción de Contenido (2 horas)**
- Leer secciones relevantes del documento fuente
- Extraer cronologías, conceptos, referencias
- Identificar gaps
- Crear notas de lectura

**Fase 2: Investigación Profunda (4–6 horas por tema)**
- Investigar cada concepto clave
- Buscar papers, artículos, documentación oficial
- Crear ejemplos de código en Rust/Elixir/Go
- Documentar hallazgos

**Fase 3: Síntesis y Documentación (2–3 horas por tema)**
- Escribir resúmenes ejecutivos
- Crear comparativas y tablas
- Documentar conexiones entre temas
- Escribir checklists

**Fase 4: Revisión y Validación (1–2 horas por tema)**
- Revisar con lenguajes reales
- Verificar ejemplos
- Actualizar con feedback

### 6.2 Formato de Salida Esperado

Para cada tema, entregar:

1. Archivo markdown principal con contenido completo
2. Ejemplos de código en archivos separados (rust/, elixir/, go/)
3. Referencias bibliográficas consolidadas
4. Checklist de aprendizaje
5. Notas sobre gotchas y trade-offs

### 6.3 Estándares de Calidad

- Mínimo 3 fuentes por concepto clave
- Ejemplos de código ejecutable
- Comparativas entre lenguajes
- Casos de uso reales documentados
- Referencias a autores y papers
- Explicaciones con fundamentación académica

---

## 7. Cronograma Estimado

- Tema 1 (Paradigmas): 2–3 días
- Tema 2 (Tipos): 2–3 días
- Tema 3 (Distribuidos): 2–3 días
- Tema 4 (Concurrencia): 2–3 días
- Revisión y consolidación: 1 día

**Total: 10–15 días de investigación continua**

---

## 8. Notas Finales

Esta investigación es complementaria a la lectura del documento fuente, no sustitutiva. El agente debe:

1. Citar siempre el documento de referencia
2. Ampliarse con fuentes externas
3. Conectar conceptos entre temas
4. Mantener rigor académico
5. Proporcionar explicaciones tanto teóricas como prácticas

La meta final es documentación de referencia que pueda ser usada por otros desarrolladores para entender estos paradigmas en profundidad.
