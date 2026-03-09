# 2. Data Types

## Prerequisites

- OPA installed and working (exercise 01)
- Familiarity with `opa eval --format pretty`

## Learning Objectives

After completing this exercise, you will be able to:

- Identify and use the 7 Rego data types (4 scalar, 3 composite)
- Access elements in arrays, objects, and sets
- Distinguish between `{}` (empty object) and `set()` (empty set)
- Inspect types at runtime with `type_name()`

## Why Data Types Matter

Everything in Rego is data. Before you can write policies that inspect requests, check permissions, or validate configurations, you need to know what kinds of values Rego can work with. There are 7 types total: 4 scalars and 3 composites.

## Scalars

Scalars are individual values. There are 4 types.

**Strings** -- always with double quotes:

```bash
opa eval --format pretty '"hello world"'
```

```
"hello world"
```

**Numbers** -- integers or decimals, no quotes:

```bash
opa eval --format pretty "42"
```

```
42
```

```bash
opa eval --format pretty "3.14"
```

```
3.14
```

**Booleans** -- only `true` and `false`:

```bash
opa eval --format pretty "true"
```

```
true
```

**Null** -- represents the intentional absence of a value:

```bash
opa eval --format pretty "null"
```

```
null
```

## Arrays

An array is an ordered list of values. Created with square brackets `[]`. Elements are indexed starting at 0:

```bash
opa eval --format pretty '[1, 2, 3]'
```

```
[
  1,
  2,
  3
]
```

To access an element, use the index in brackets:

```bash
opa eval --format pretty '[10, 20, 30][0]'
```

```
10
```

```bash
opa eval --format pretty '[10, 20, 30][2]'
```

```
30
```

Arrays can mix types:

```bash
opa eval --format pretty '["hello", 42, true, null]'
```

```
[
  "hello",
  42,
  true,
  null
]
```

To count how many elements an array has, use `count()`:

```bash
opa eval --format pretty 'count([1, 2, 3])'
```

```
3
```

## Objects

An object is a collection of key-value pairs. Created with curly braces `{}` and each pair has the format `"key": value`. It is the same as a JSON object:

```bash
opa eval --format pretty '{"name": "alice", "role": "admin"}'
```

```
{
  "name": "alice",
  "role": "admin"
}
```

To access a value, you can use dot notation or bracket notation:

```bash
opa eval --format pretty '{"name": "alice", "role": "admin"}.name'
```

```
"alice"
```

```bash
opa eval --format pretty '{"name": "alice", "role": "admin"}["role"]'
```

```
"admin"
```

`count()` returns the number of keys:

```bash
opa eval --format pretty 'count({"a": 1, "b": 2, "c": 3})'
```

```
3
```

## Sets

A set is a collection of unique values, with no guaranteed order. Created with curly braces `{}` but WITHOUT the `key: value` format -- just values:

```bash
opa eval --format pretty '{"read", "write", "admin"}'
```

```
[
  "admin",
  "read",
  "write"
]
```

Note that OPA displays sets as alphabetically sorted arrays in the output, but internally they are sets (no order, no duplicates).

Sets automatically eliminate duplicates:

```bash
opa eval --format pretty '{"a", "b", "a", "c"}'
```

```
[
  "a",
  "b",
  "c"
]
```

Only 3 elements, even though you wrote 4. The second `"a"` was discarded.

## The Critical Gotcha: `{}` vs `set()`

This is the trap that catches every beginner. Empty curly braces `{}` are an **empty object**, NOT an empty set:

```bash
opa eval --format pretty 'type_name({})'
```

```
"object"
```

To create an empty set, you need the `set()` function:

```bash
opa eval --format pretty 'type_name(set())'
```

```
"set"
```

Summary to keep it straight:

| Expression | Type | Why |
|---|---|---|
| `{}` | object | Empty braces = empty object |
| `set()` | set | Only way to create an empty set |
| `{"a"}` | set | Braces with a bare value = set |
| `{"a": 1}` | object | Braces with `key: value` = object |

## Inspecting Types with `type_name()`

If you are not sure what type something is, `type_name()` tells you:

```bash
opa eval --format pretty 'type_name("hello")'
```

```
"string"
```

```bash
opa eval --format pretty 'type_name(42)'
```

```
"number"
```

```bash
opa eval --format pretty 'type_name(true)'
```

```
"boolean"
```

```bash
opa eval --format pretty 'type_name(null)'
```

```
"null"
```

```bash
opa eval --format pretty 'type_name([1, 2])'
```

```
"array"
```

```bash
opa eval --format pretty 'type_name({"key": "val"})'
```

```
"object"
```

```bash
opa eval --format pretty 'type_name({"a", "b"})'
```

```
"set"
```

## Practice: Create a Reference File

Now you will write your first `.rego` file. Create a file called `types.rego` with this content:

```rego
package types

import rego.v1

# Scalars
my_string := "hello"
my_number := 42
my_float := 3.14
my_bool := true
my_null := null

# Composites
my_array := [1, "two", 3, true]
my_object := {"name": "alice", "age": 30, "admin": true}
my_set := {"read", "write", "execute"}

# Element access
first_element := my_array[0]
user_name := my_object.name
element_count := count(my_array)

# Types
string_type := type_name(my_string)
array_type := type_name(my_array)
object_type := type_name(my_object)
set_type := type_name(my_set)
```

Do not worry about `package` or `import rego.v1` yet -- you will learn those in exercise 05. For now, just know that every `.rego` file starts with a `package`.

Evaluate the file:

```bash
opa eval --format pretty -d types.rego "data.types"
```

This loads the file (`-d types.rego`) and evaluates everything defined in the `types` package (`data.types`). You should see all the values you defined.

**Verification:**

You can also view a specific value:

```bash
opa eval --format pretty -d types.rego "data.types.first_element"
```

```
1
```

```bash
opa eval --format pretty -d types.rego "data.types.set_type"
```

```
"set"
```

Or explore in the REPL:

```bash
opa run types.rego
```

```
> data.types.my_array
[1, "two", 3, true]
> data.types.my_array[0]
1
> data.types.my_object.name
"alice"
> type_name(data.types.my_set)
"set"
> exit
```

## Common Mistakes

A realistic mistake is assuming `{}` creates an empty set. Suppose you want to check if a collection is a set:

```bash
opa eval --format pretty 'type_name({})'
```

```
"object"
```

You expected `"set"` but got `"object"`. The fix: use `set()` for an empty set. Once you add at least one bare value like `{"a"}`, Rego correctly infers it as a set. But when empty, `{}` is always an object.

## Verify What You Learned

```bash
opa eval --format pretty 'type_name({})'
```

```
"object"
```

```bash
opa eval --format pretty 'type_name({"a", "b"})'
```

```
"set"
```

```bash
opa eval --format pretty -d types.rego "data.types.first_element"
```

```
1
```

```bash
opa eval --format pretty -d types.rego "data.types.element_count"
```

```
4
```

## What's Next

You now know all 7 data types in Rego. The next exercise covers operators -- how to assign, compare, and compute with these values.

## Reference

- [Scalar Values](https://www.openpolicyagent.org/docs/latest/policy-language/#scalar-values)
- [Composite Values](https://www.openpolicyagent.org/docs/latest/policy-language/#composite-values)
- [Built-in Functions](https://www.openpolicyagent.org/docs/latest/policy-reference/#built-in-functions) -- includes `type_name` and `count`

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- try data type expressions interactively
- [OPA Policy Reference](https://www.openpolicyagent.org/docs/latest/policy-reference/) -- complete list of built-in functions
