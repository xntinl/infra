# 3. Operators

## Prerequisites

- OPA installed and working (exercise 01)
- Understanding of Rego data types (exercise 02)

## Learning Objectives

After completing this exercise, you will be able to:

- Assign values with `:=` and compare with `==`
- Use arithmetic, comparison, and inequality operators
- Test membership in collections with `in`
- Understand why failed comparisons produce no output (undefined) instead of `false`

## Why Operators Come First

Rego has operators for assignment, comparison, and arithmetic. The most important difference from other languages: `:=` assigns, `==` compares. Confusing the two is a common source of bugs. Understanding operators before writing rules means your rules will do what you intend.

## Assignment with `:=`

`:=` assigns a value to a local variable. It is a **bind** -- once you assign, you cannot change the value:

```bash
opa eval --format pretty 'x := 5; x'
```

```
5
```

The `;` separates two expressions on one line. First it assigns `x := 5`, then it evaluates `x`.

More examples:

```bash
opa eval --format pretty 'name := "alice"; name'
```

```
"alice"
```

```bash
opa eval --format pretty 'total := 10 + 20; total'
```

```
30
```

The variable takes the value of the expression on the right. It can be a literal, an operation, or any valid expression.

## Comparison with `==`

`==` compares two values. It does not modify anything -- it only asks "are these equal?":

```bash
opa eval --format pretty '5 == 5'
```

```
true
```

```bash
opa eval --format pretty '"hello" == "hello"'
```

```
true
```

Now pay attention to this. What happens when the comparison fails?

```bash
opa eval --format pretty '5 == 3'
```

No output. Nothing. It does not say `false` -- it simply produces no result. This is called `undefined` and is one of the most important concepts in Rego. You will learn it in depth in exercise 06. For now just remember: a failed comparison produces no result.

You can combine assignment and comparison:

```bash
opa eval --format pretty 'x := 5; x == 5'
```

```
true
```

```bash
opa eval --format pretty 'x := 5; x == 3'
```

(no output -- undefined)

## Inequality with `!=`

```bash
opa eval --format pretty '5 != 3'
```

```
true
```

```bash
opa eval --format pretty '"alice" != "bob"'
```

```
true
```

## Arithmetic Operators

The standard ones. Nothing surprising:

```bash
opa eval --format pretty '10 + 3'
```

```
13
```

```bash
opa eval --format pretty '10 - 3'
```

```
7
```

```bash
opa eval --format pretty '10 * 3'
```

```
30
```

```bash
opa eval --format pretty '10 / 3'
```

```
3.3333333333333335
```

Division always returns a decimal.

```bash
opa eval --format pretty '10 % 3'
```

```
1
```

`%` is the modulo operator: the remainder of dividing 10 by 3.

## Comparison Operators

```bash
opa eval --format pretty '5 > 3'
```

```
true
```

```bash
opa eval --format pretty '5 < 3'
```

(no output -- the comparison failed, it is undefined)

```bash
opa eval --format pretty '5 >= 5'
```

```
true
```

```bash
opa eval --format pretty '5 <= 4'
```

(no output)

Notice the pattern: when the comparison is true, you see `true`. When it is false, you see nothing. This is fundamental in Rego.

## Membership with `in`

`in` checks whether an element belongs to a collection. It is very useful because in other languages you would need a loop.

In an **array**, it searches by value:

```bash
opa eval --format pretty '"a" in ["a", "b", "c"]'
```

```
true
```

In a **set**, it searches by value:

```bash
opa eval --format pretty '"write" in {"read", "write", "execute"}'
```

```
true
```

In an **object**, it searches by **key** (not by value):

```bash
opa eval --format pretty '"name" in {"name": "alice", "role": "admin"}'
```

```
true
```

When the element is NOT found, there is no output (undefined):

```bash
opa eval --format pretty '"z" in ["a", "b", "c"]'
```

(no output)

## No `&&`, `||`, `!`

If you come from JavaScript, Python, Go, or any imperative language, you will look for logical operators. Rego does not have them. In Rego:

- **AND** = multiple lines in the body of a rule (exercise 08)
- **OR** = multiple rules with the same name (exercise 08)
- **NOT** = the `not` keyword (exercise 08)

For now, just remember they do not exist as operators.

## Practice: Create a Reference File

Create a file called `operators.rego`:

```rego
package operators

import rego.v1

# Assignment
greeting := "hello"
total := 10 + 20

# Comparison in rules
is_five := true if {
    x := 5
    x == 5
}

is_positive := true if {
    total > 0
}

# Arithmetic
sum := 10 + 3
diff := 10 - 3
product := 10 * 3
quotient := 10 / 3
remainder := 10 % 3

# Membership
allowed_roles := {"admin", "editor", "viewer"}

is_valid_role := true if {
    "admin" in allowed_roles
}
```

**Verification:**

```bash
opa eval --format pretty -d operators.rego "data.operators"
```

You should see all the defined values. Try individual ones:

```bash
opa eval --format pretty -d operators.rego "data.operators.remainder"
```

```
1
```

```bash
opa eval --format pretty -d operators.rego "data.operators.total"
```

```
30
```

## Common Mistakes

A realistic mistake is using `==` when you meant `:=`, or vice versa. Consider this:

```bash
opa eval --format pretty 'x == 5; x'
```

This produces an error or undefined because `x` was never assigned -- `==` tried to compare an unbound variable. The fix: use `:=` to assign first, then `==` to compare:

```bash
opa eval --format pretty 'x := 5; x == 5'
```

```
true
```

The rule of thumb: `:=` creates a binding, `==` checks equality. If a variable does not have a value yet, you need `:=`.

## Verify What You Learned

```bash
opa eval --format pretty 'x := 10; x == 10'
```

```
true
```

```bash
opa eval --format pretty '17 % 5'
```

```
2
```

```bash
opa eval --format pretty '"write" in {"read", "write"}'
```

```
true
```

## What's Next

You can now assign, compare, and compute values. The next exercise introduces the `input` document -- how your policies receive external data to make decisions about.

## Reference

- [Equality](https://www.openpolicyagent.org/docs/latest/policy-language/#equality) -- difference between `:=` and `==`
- [Operators](https://www.openpolicyagent.org/docs/latest/policy-reference/#operators) -- complete operator reference
- [Membership and Iteration](https://www.openpolicyagent.org/docs/latest/policy-language/#membership-and-iteration-in) -- the `in` operator

## Additional Resources

- [Rego Playground](https://play.openpolicyagent.org/) -- try operator expressions interactively
- [Rego Cheat Sheet](https://www.openpolicyagent.org/docs/latest/rego-cheat-sheet/) -- quick reference for Rego syntax
