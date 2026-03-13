# 1. Hello OPA

## Prerequisites

- A terminal (macOS or Linux)
- Homebrew (macOS) or curl (Linux) for installation

## Learning Objectives

After completing this exercise, you will be able to:

- Install OPA and verify the installation
- Evaluate Rego expressions from the command line with `opa eval`
- Use the interactive REPL with `opa run`
- Choose the right tool (`opa eval` vs `opa run`) for the task at hand

## Why Start Here

Before writing policies, you need a working OPA installation and a feel for how Rego evaluates expressions. This exercise gives you both in under five minutes. Everything happens in the terminal -- no files to create yet.

## What Is OPA and What Is Rego

They are two distinct things:

- **OPA** (Open Policy Agent) is an **engine**. It is the program you install and run.
- **Rego** is the **language** you write policies in. OPA executes Rego.

The relationship is like Node.js and JavaScript: Node is the runtime, JavaScript is the language. OPA is the runtime, Rego is the language.

## Install OPA

On macOS:

```bash
brew install opa
```

On Linux:

```bash
curl -L -o opa https://openpolicyagent.org/downloads/v1.4.2/opa_linux_amd64_static
chmod 755 opa
sudo mv opa /usr/local/bin/
```

**Verification:**

```bash
opa version
```

```
Version: 1.4.2
...
```

If you see the version number, OPA is ready.

## Your First Evaluation: `opa eval`

`opa eval` takes a Rego expression and evaluates it. No files needed. Think of it as a calculator:

```bash
opa eval "1 + 1"
```

The output is a JSON structure with a lot of wrapping:

```json
{
  "result": [
    {
      "expressions": [
        {
          "value": 2,
          "text": "1 + 1",
          "location": { "row": 1, "col": 1 }
        }
      ]
    }
  ]
}
```

The result (`2`) is there, but buried. To see just the value, use `--format pretty`:

```bash
opa eval --format pretty "1 + 1"
```

```
2
```

Much better. You will use `--format pretty` for almost everything.

## Try More Expressions

OPA can evaluate any Rego expression. Try these one by one:

```bash
opa eval --format pretty '"hello"'
```

```
"hello"
```

```bash
opa eval --format pretty "true"
```

```
true
```

```bash
opa eval --format pretty "5 > 3"
```

```
true
```

```bash
opa eval --format pretty "10 * 3 + 2"
```

```
32
```

```bash
opa eval --format pretty "[1, 2, 3]"
```

```
[
  1,
  2,
  3
]
```

Notice that strings use double quotes (`"hello"`), and in the terminal you need to wrap them with single quotes on the outside so the shell does not strip them: `'"hello"'`.

## The Interactive REPL: `opa run`

`opa eval` evaluates a single expression and exits. If you want to experiment with multiple expressions in a row, the REPL is more convenient:

```bash
opa run
```

This opens an interactive prompt. Type expressions and see results immediately:

```
> 1 + 1
2
> "hello"
"hello"
> true
true
> 5 > 3
true
> [1, 2, 3]
[
  1,
  2,
  3
]
> exit
```

Type `exit` to quit.

## When to Use Each One

| | `opa eval` | `opa run` |
|---|---|---|
| **Mode** | Single evaluation | Interactive (REPL) |
| **Best for** | Verifying a specific result, scripts, CI/CD | Exploring, experimenting, learning |
| **Output** | JSON or pretty (your choice) | Pretty always |

Throughout these exercises you will use both. `opa eval` when you want to verify a concrete result. `opa run` when you want to explore.

## Common Mistakes

A frequent early mistake is forgetting `--format pretty` and getting confused by the raw JSON output. Compare:

```bash
opa eval "1 + 1"
```

The nested JSON makes it hard to spot the actual result. Always add `--format pretty` unless you specifically need the full JSON structure (for example, when parsing OPA output in a script).

Another common mistake is quoting strings incorrectly. If you run `opa eval --format pretty "hello"` without inner quotes, OPA interprets `hello` as a variable reference, not a string:

```bash
opa eval --format pretty "hello"
```

```
undefined
```

The fix is to use single quotes outside and double quotes inside:

```bash
opa eval --format pretty '"hello"'
```

```
"hello"
```

## Verify What You Learned

```bash
opa eval --format pretty "2 + 3"
```

```
5
```

```bash
opa eval --format pretty "10 > 7"
```

```
true
```

```bash
opa eval --format pretty '"opa works"'
```

```
"opa works"
```

If all three produce the expected result, you are ready for the next exercise.

## What's Next

Now that OPA is installed and you can evaluate expressions, the next exercise covers the data types available in Rego -- strings, numbers, booleans, arrays, objects, and sets.

## Reference

- [What Is OPA](https://www.openpolicyagent.org/docs/latest/)
- [OPA CLI Reference](https://www.openpolicyagent.org/docs/latest/cli/)

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- experiment in the browser without installing anything
- [OPA Interactive Tutorial](https://academy.styra.com/) -- Styra Academy's guided OPA courses
