<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Built-in Functions and Standard Library

A programming language without built-in functions is like a car without wheels -- technically a vehicle but practically useless. Built-in functions bridge the gap between the language's abstract world and the host system, providing I/O, string manipulation, collection operations, type inspection, and more. Your task is to design and implement a comprehensive set of built-in functions for your Monkey interpreter in Go, organized into logical modules with consistent error handling, type checking, and documentation. You will also build a framework that makes adding new built-ins trivial.

## Requirements

1. Implement a built-in function registry: define `BuiltinFunction` as `func(args ...Object) Object` and maintain a `map[string]*Builtin` where `Builtin` wraps the function with metadata (name, minimum/maximum argument count, parameter type expectations, documentation string). Implement `RegisterBuiltin(name string, fn BuiltinFunction, opts ...BuiltinOption)` with options for arity validation, type checking, and doc strings. The evaluator checks this registry when an identifier is not found in the environment.

2. Implement core collection functions: `len(obj)` returns the length of strings, arrays, and hashes. `push(array, ...values)` returns a new array with values appended (arrays are immutable -- return new copies). `pop(array)` returns a new array with the last element removed. `first(array)` and `last(array)` return the first/last element. `rest(array)` returns everything except the first element. `map(array, fn)` applies fn to each element and returns a new array. `filter(array, fn)` returns elements where fn returns truthy. `reduce(array, initial, fn)` folds the array. `zip(array1, array2)` combines two arrays into an array of pairs.

3. Implement string functions: `split(str, delimiter)` splits a string into an array. `join(array, delimiter)` joins an array of strings. `trim(str)`, `trimLeft(str)`, `trimRight(str)` remove whitespace. `upper(str)`, `lower(str)` for case conversion. `contains(str, substr)` returns boolean. `replace(str, old, new)` for substitution. `startsWith(str, prefix)`, `endsWith(str, suffix)`. `charAt(str, index)` returns character at position. `substr(str, start, length)` extracts a substring. `format(template, ...args)` for string interpolation with `{}` placeholders.

4. Implement type system functions: `type(obj)` returns a string like "INTEGER", "STRING", "FUNCTION", etc. `int(obj)` converts strings and floats to integers (error on failure). `float(obj)` converts strings and integers to floats. `str(obj)` converts any value to its string representation. `bool(obj)` converts to boolean using truthiness rules. `isNull(obj)`, `isInt(obj)`, `isString(obj)`, `isArray(obj)`, `isHash(obj)`, `isFunction(obj)` return booleans.

5. Implement I/O functions: `print(...args)` prints arguments space-separated to stdout with a newline. `printf(format, ...args)` with format specifiers (%d, %s, %f, %v). `input(prompt)` reads a line from stdin (returns a string). `readFile(path)` reads a file and returns its contents as a string. `writeFile(path, content)` writes a string to a file. All I/O functions must handle errors gracefully, returning Error objects rather than panicking.

6. Implement math functions: `abs(n)`, `min(a, b)`, `max(a, b)`, `floor(f)`, `ceil(f)`, `round(f)`, `sqrt(f)`, `pow(base, exp)`, `log(f)`, `sin(f)`, `cos(f)`, `random()` (returns float between 0 and 1), `randomInt(min, max)` (returns integer in range). These must work with both integers and floats where appropriate, with type promotion from integer to float when the function inherently returns floats (sqrt, sin, etc.).

7. Implement hash/dictionary functions: `keys(hash)` returns an array of keys. `values(hash)` returns an array of values. `entries(hash)` returns an array of [key, value] pairs. `hasKey(hash, key)` returns boolean. `merge(hash1, hash2)` returns a new hash with entries from both (hash2 overrides). `delete(hash, key)` returns a new hash without the specified key. Implement `set(hash, key, value)` that returns a new hash with the key set (immutable operations -- hashes, like arrays, are never mutated in place).

8. Write tests covering: every built-in function with valid arguments producing correct results, every built-in with wrong argument count producing an error (not a panic), every built-in with wrong argument types producing a descriptive type error, edge cases (empty arrays for first/last/rest, empty strings for split, negative indices), `map`/`filter`/`reduce` with closures that capture variables, chaining operations (`map(filter(array, fn1), fn2)`), file I/O with temporary files, the format function with mixed argument types, and a comprehensive test that uses built-ins to implement a word frequency counter reading from a string.

## Hints

- For immutable array operations (push, map, filter), always create a new slice: `newArr := make([]Object, len(old))`, copy, then append. Never modify the original.
- Type-checking arguments is repetitive. Write a helper: `func checkArgs(name string, args []Object, types ...ObjectType) *Error` that validates argument count and types, returning nil on success or an Error with a descriptive message.
- For `reduce`, the callback signature is `fn(accumulator, currentValue) -> newAccumulator`. Start with the initial value and fold left.
- For `format("Hello, {}! You are {} years old.", name, age)`, scan the template string for `{}` placeholders and replace them in order with `Inspect()` of the corresponding argument.
- I/O functions should use Go's `os` and `bufio` packages. For testability, consider accepting `io.Reader` and `io.Writer` in the environment so tests can substitute buffers.
- For `random()`, use `math/rand/v2` with automatic seeding. For `randomInt(min, max)`, use `rand.IntN(max - min + 1) + min`.

## Success Criteria

1. All collection functions produce correct results and return new objects (never mutate inputs).
2. All string functions handle edge cases (empty strings, missing delimiters, out-of-range indices).
3. Type conversion functions correctly convert between types and produce clear errors for invalid conversions.
4. `map`, `filter`, `reduce` work with closures that capture and modify outer-scope variables.
5. Error messages for wrong argument count and type include the function name, expected signature, and actual arguments received.
6. I/O functions work correctly in integration and handle file-not-found and permission errors gracefully.
7. Math functions produce correct results matching Go's `math` package output for equivalent operations.
8. The word frequency counter integration test correctly counts words in a multi-sentence input.

## Research Resources

- [Writing An Interpreter In Go - Built-in Functions (Thorsten Ball)](https://interpreterbook.com/)
- [Python Built-in Functions (for inspiration)](https://docs.python.org/3/library/functions.html)
- [JavaScript Array Methods (for collection API design)](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Array)
- [Go math Package](https://pkg.go.dev/math)
- [Go os Package for File I/O](https://pkg.go.dev/os)
- [Functional Programming in Go](https://bitfieldconsulting.com/posts/functional)
