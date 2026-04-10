# 40. Build a WebAssembly Interpreter

**Difficulty**: Insane

## Prerequisites

- Manipulación de binarios en Elixir: `<<>>`, `:binary`, bitstring comprehensions
- Stack machines y evaluación de expresiones en notación postfix
- Comprensión básica de compiladores: parsing, AST, evaluación
- LEB128 encoding/decoding (enteros de longitud variable)
- Tipos numéricos de bajo nivel: i32, i64, f32, f64 y sus operaciones
- Recursión y trampolining para call stacks profundos

## Problem Statement

Implementa un intérprete completo de WebAssembly en Elixir puro, capaz de parsear el formato binario `.wasm` y ejecutar módulos WASM sin dependencias externas de ML o runtime.

El intérprete sigue la especificación oficial de WebAssembly 1.0 (MVP). Debe parsear el formato binario (magic number `\0asm`, version 1), leer todas las secciones relevantes (Type, Import, Function, Table, Memory, Global, Export, Code), validar el módulo según las reglas de tipado de WASM, y ejecutar las instrucciones en una stack machine.

La ejecución usa una stack machine donde las instrucciones consumen y producen valores en la pila. El modelo de memoria es lineal: un array de bytes (en Elixir, un binario mutable simulado con ETS o `:array`) que puede crecer con la instrucción `memory.grow`. Las funciones pueden llamarse recursivamente con su propio frame de locals.

El intérprete debe poder ejecutar módulos WASM reales compilados desde C o Rust (por ejemplo, un módulo que calcule Fibonacci, ordene un array, o implemente un algoritmo de compresión simple).

## Acceptance Criteria

- [ ] Parser: lee el encabezado WASM (`\0asm` + version 4 bytes); parsea cada sección por su ID numérico; decodifica enteros LEB128 sin signo (uleb128) y con signo (sleb128) correctamente; ignora secciones custom sin error; devuelve un mapa estructurado del módulo
- [ ] Type section: parsea function types `(params...) -> (results...)`; soporta los tipos `i32`, `i64`, `f32`, `f64`; una función puede tener múltiples parámetros y resultados (WASM 1.0 limita a un resultado, pero el parser debe ser correcto)
- [ ] Code section: parsea el cuerpo de cada función: locals declarados (tipo y count), seguido del bytecode de instrucciones; decodifica instrucciones por su opcode (al menos las 80 instrucciones más comunes del MVP)
- [ ] Execution: stack machine que mantiene `%Frame{locals, stack, instructions, pc}` por función activa; ejecuta instrucciones una a una: aritmética (`i32.add`, `i32.mul`, etc.), comparaciones, control flow (`if`, `block`, `loop`, `br`, `br_if`), y llamadas a función (`call`, `call_indirect`)
- [ ] Memory model: memoria lineal implementada como binario en un proceso Agent o ETS; `memory.load` y `memory.store` con distintos tamaños (i32.load8_s, i32.load, i64.load, etc.); `memory.grow` añade páginas de 64KB; acceso fuera de bounds devuelve trap
- [ ] Function calls: cada `call` crea un nuevo frame con los locals inicializados (params de la stack + locals en cero); `return` desapila el frame y devuelve los valores de retorno al frame padre; soporta llamadas recursivas sin stack overflow de Erlang (usa trampolining o iteración explícita)
- [ ] Imports/Exports: al instanciar el módulo, recibe un mapa de funciones importadas `%{"env" => %{"print_i32" => fn(i32) -> :ok end}}`; las funciones exportadas son accesibles con `Module.call_export(module, "main", [42])`; los imports inválidos (firma no coincide) causan error de instanciación
- [ ] Validation: antes de ejecutar, verifica: tipos de función correctos en calls, stack balance en bloques, tipos de load/store compatibles con la declaración, no hay acceso a locals inexistentes; módulo inválido devuelve `{:error, :validation_failed, reason}` sin ejecutar nada

## What You Will Learn

- Parsing de formatos binarios complejos con especificación formal (LEB128, secciones WASM)
- Implementación de una stack machine: el modelo de ejecución más simple para un intérprete
- Validación de tipos en un sistema de tipos estáticos (el type system de WASM)
- Memoria lineal y por qué WASM eligió este modelo de memoria
- Trampolining: cómo evitar stack overflows en lenguajes con recursión limitada
- Cómo funciona la portabilidad de WASM: el mismo binario corre en cualquier intérprete conforme

## Hints

- Empieza con el parser: `<<0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00, rest::binary>> = wasm_bytes`
- LEB128 unsigned: lee bytes hasta que el bit alto sea 0, acumula grupos de 7 bits: `decode_uleb128(<<0::1, value::7, rest::binary>>, acc, shift)` y `decode_uleb128(<<1::1, value::7, rest::binary>>, acc, shift)` (recursivo)
- Para la stack machine, representa la pila como una lista Elixir (la cabeza es el top); los frames son una lista de `%Frame{}` (call stack)
- Instrucciones de control flow (`block`, `loop`, `if`) crean etiquetas; `br N` salta a la etiqueta N niveles hacia afuera; implementa como un mapa `%{label_depth => {type, continuation_instructions}}`
- La memoria lineal puede ser `:array` de Erlang o un binario en un proceso; para simulación, `Agent.update(mem, fn bin -> put_binary(bin, offset, value) end)` funciona pero es lento; ETS con key=offset es más rápido
- Para validación de tipos: haz un pase separado que simula la stack con tipos en lugar de valores; si la simulación tiene éxito, la ejecución no puede tener errores de tipo

## Reference Material

- WebAssembly specification 1.0: https://webassembly.github.io/spec/core/
- WebAssembly binary format: https://webassembly.github.io/spec/core/binary/
- LEB128 encoding: https://en.wikipedia.org/wiki/LEB128
- WebAssembly opcode table: https://webassembly.github.io/spec/core/binary/instructions.html
- "Programming WebAssembly with Rust" — Kevin Hoffman (para entender el modelo de ejecución)
- wat2wasm tool: para compilar módulos WASM de texto a binario para testing

## Difficulty Rating ★★★★★★★

WebAssembly tiene una especificación formal precisa y el número de instrucciones a implementar es grande. La validación de tipos estáticos requiere entender el type system de WASM en profundidad. Las instrucciones de control flow con etiquetas anidadas son el caso de borde más complejo del intérprete.

## Estimated Time

40–60 horas
