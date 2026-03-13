# 23. CP: Stack and Queue Problems

**Difficulty**: Intermedio

## Prerequisites

- `Vec<T>` as a growable array
- `VecDeque<T>` from `std::collections`
- Pattern matching and `Option<T>`
- Iterators and byte/char processing
- Competitive programming I/O patterns (see exercise 20)

## Learning Objectives

1. Use `Vec<T>` as a stack with `push`, `pop`, `last`
2. Use `VecDeque<T>` as a queue with `push_back`, `pop_front`
3. Solve the classic "monotonic stack" pattern (next greater element, daily temperatures)
4. Parse and evaluate expressions using a stack
5. Simulate a queue using two stacks (amortized O(1) operations)

---

## Concepts

### Stack in Rust: `Vec<T>`

A stack is LIFO (Last In, First Out). Rust's `Vec<T>` is a natural stack:

```rust
let mut stack: Vec<i32> = Vec::new();

stack.push(10);          // push onto top
stack.push(20);
stack.push(30);

stack.last();            // Some(&30) -- peek without removing
stack.pop();             // Some(30)  -- remove and return top
stack.is_empty();        // false
stack.len();             // 2
```

```
  push 10, 20, 30:         pop:

  | 30 | <-- top            | 20 | <-- new top
  | 20 |                    | 10 |
  | 10 |                    +----+
  +----+
```

### Queue in Rust: `VecDeque<T>`

A queue is FIFO (First In, First Out). Use `VecDeque`:

```rust
use std::collections::VecDeque;

let mut queue: VecDeque<i32> = VecDeque::new();

queue.push_back(10);     // enqueue
queue.push_back(20);
queue.push_back(30);

queue.front();           // Some(&10) -- peek front
queue.pop_front();       // Some(10)  -- dequeue
queue.back();            // Some(&30) -- peek back
queue.pop_back();        // Some(30)  -- remove from back (deque feature)
```

```
  push_back 10, 20, 30:         pop_front:

  front --> | 10 | 20 | 30 | <-- back     front --> | 20 | 30 | <-- back
            dequeue ^                                dequeue ^
```

### Monotonic Stack Pattern

A monotonic stack maintains elements in increasing or decreasing order. When a new
element violates the ordering, we pop elements until the invariant is restored.

```
Problem: Next Greater Element
  arr:     [2, 1, 2, 4, 3]
  answer:  [4, 2, 4, -1, -1]

  Process right to left, maintaining a decreasing stack:

  i=4: val=3, stack=[]         => no greater, answer[4]=-1, push 3
       stack: [3]

  i=3: val=4, stack=[3]       => pop 3 (3 < 4), stack empty
       => no greater, answer[3]=-1, push 4
       stack: [4]

  i=2: val=2, stack=[4]       => 4 > 2, answer[2]=4, push 2
       stack: [4, 2]

  i=1: val=1, stack=[4, 2]    => 2 > 1, answer[1]=2, push 1
       stack: [4, 2, 1]

  i=0: val=2, stack=[4, 2, 1] => pop 1 (1 < 2), top=2, 2 is NOT > 2,
       pop 2, top=4, 4 > 2, answer[0]=4, push 2
       stack: [4, 2]
```

### Stack for Expression Evaluation

Stacks are natural for evaluating postfix (RPN) expressions and matching parentheses:

```
Expression: 3 4 + 2 * 7 /

  Token  | Stack
  -------+--------
  3      | [3]
  4      | [3, 4]
  +      | [7]         (pop 4, 3, push 3+4=7)
  2      | [7, 2]
  *      | [14]        (pop 2, 7, push 7*2=14)
  7      | [14, 7]
  /      | [2]         (pop 7, 14, push 14/7=2)

  Result: 2
```

---

## Problem 1: Valid Parentheses

### Statement

Given a string `s` containing only the characters `(`, `)`, `{`, `}`, `[`, `]`,
determine if the input string is valid.

A string is valid if:
1. Open brackets are closed by the same type of brackets.
2. Open brackets are closed in the correct order.
3. Every close bracket has a corresponding open bracket.

### Input Format

```
s
```

### Output Format

Print `YES` if valid, `NO` otherwise.

### Constraints

- 1 <= |s| <= 10^5

### Examples

```
Input:
()[]{}

Output:
YES
```

```
Input:
(]

Output:
NO
```

```
Input:
([)]

Output:
NO
```

```
Input:
{[]}

Output:
YES
```

### Hints

1. Use a `Vec<u8>` or `Vec<char>` as a stack.
2. For each opening bracket, push its corresponding closing bracket.
3. For each closing bracket, check if it matches the top of the stack.
4. At the end, the stack must be empty.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let s = input.trim().as_bytes();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut stack: Vec<u8> = Vec::new();
    let mut valid = true;

    for &ch in s {
        match ch {
            b'(' => stack.push(b')'),
            b'{' => stack.push(b'}'),
            b'[' => stack.push(b']'),
            // TODO: For closing brackets, check if stack is non-empty
            //       and top matches ch. If not, set valid = false and break.
            _ => {
                // TODO: handle closing bracket
            }
        }
    }

    // TODO: Also check that the stack is empty (no unmatched openers)

    writeln!(out, "{}", if valid { "YES" } else { "NO" }).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let s = input.trim().as_bytes();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut stack: Vec<u8> = Vec::new();
    let mut valid = true;

    for &ch in s {
        match ch {
            b'(' => stack.push(b')'),
            b'{' => stack.push(b'}'),
            b'[' => stack.push(b']'),
            b')' | b'}' | b']' => {
                if stack.pop() != Some(ch) {
                    valid = false;
                    break;
                }
            }
            _ => {} // ignore unexpected characters
        }
    }

    if !stack.is_empty() {
        valid = false;
    }

    writeln!(out, "{}", if valid { "YES" } else { "NO" }).unwrap();
}
```

**Elegant trick**: Push the *expected* closing bracket instead of the opening one. This
way, `pop()` directly gives us what we expect.

**Complexity**: O(n) time, O(n) space worst case.

</details>

---

## Problem 2: Next Greater Element

### Statement

Given an array of `n` integers, for each element, find the **next greater element** to
its right. The next greater element of an element `x` is the first element to the right
that is strictly greater than `x`. If no such element exists, the answer is `-1`.

### Input Format

```
n
a_1 a_2 ... a_n
```

### Output Format

`n` integers on a single line, space-separated.

### Constraints

- 1 <= n <= 10^5
- -10^9 <= a_i <= 10^9

### Examples

```
Input:
5
2 1 2 4 3

Output:
4 2 4 -1 -1
```

```
Input:
4
4 3 2 1

Output:
-1 -1 -1 -1
```

### Hints

1. Process the array from **right to left**.
2. Maintain a stack of elements seen so far (from the right).
3. For each element, pop stack elements that are <= current element (they cannot be
   the "next greater" for anything further left).
4. The stack top (if it exists) is the next greater element.
5. Push current element onto the stack.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut result = vec![-1i64; n];
    let mut stack: Vec<i64> = Vec::new();

    // TODO: Iterate from right to left (i from n-1 down to 0)
    //   - Pop elements from stack that are <= a[i]
    //   - If stack is non-empty, result[i] = *stack.last().unwrap()
    //   - Push a[i] onto stack

    let result_str: Vec<String> = result.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result_str.join(" ")).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let a: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let mut result = vec![-1i64; n];
    let mut stack: Vec<i64> = Vec::new();

    for i in (0..n).rev() {
        // Pop elements that are not greater than a[i]
        while let Some(&top) = stack.last() {
            if top <= a[i] {
                stack.pop();
            } else {
                break;
            }
        }

        // If stack has elements, the top is the next greater
        if let Some(&top) = stack.last() {
            result[i] = top;
        }

        stack.push(a[i]);
    }

    let result_str: Vec<String> = result.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result_str.join(" ")).unwrap();
}
```

**Why O(n)?** Each element is pushed once and popped at most once. Total operations
across all iterations: at most 2n.

**Complexity**: O(n) time, O(n) space.

</details>

---

## Problem 3: Evaluate Reverse Polish Notation

### Statement

Evaluate an expression given in Reverse Polish Notation (postfix). Valid operators are
`+`, `-`, `*`, `/`. Each operand is an integer. Division truncates toward zero.

### Input Format

```
n
token_1 token_2 ... token_n
```

Where each token is either an integer or one of `+`, `-`, `*`, `/`.

### Output Format

A single integer: the result of the expression.

### Constraints

- 1 <= n <= 10^4
- The expression is always valid.
- No division by zero occurs.
- Intermediate results fit in `i64`.

### Examples

```
Input:
5
2 1 + 3 *

Output:
9
```

Explanation: `((2 + 1) * 3) = 9`

```
Input:
5
4 13 5 / +

Output:
6
```

Explanation: `(4 + (13 / 5)) = (4 + 2) = 6`

```
Input:
13
10 6 9 3 + -11 * / * 17 + 5 +

Output:
22
```

### Hints

1. Use a `Vec<i64>` as a stack.
2. For numbers, push them.
3. For operators, pop two operands (second popped is the left operand), compute the
   result, push it back.
4. The final stack element is the answer.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        () => { iter.next().unwrap() };
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let mut stack: Vec<i64> = Vec::new();

    for _ in 0..n {
        let token = next!();

        match token {
            "+" | "-" | "*" | "/" => {
                // TODO: Pop two operands
                //   let b = stack.pop().unwrap();
                //   let a = stack.pop().unwrap();
                // TODO: Compute result based on operator
                // TODO: Push result
            }
            _ => {
                // TODO: Parse as i64 and push
            }
        }
    }

    writeln!(out, "{}", stack.pop().unwrap()).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        () => { iter.next().unwrap() };
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let mut stack: Vec<i64> = Vec::new();

    for _ in 0..n {
        let token = next!();

        match token {
            "+" | "-" | "*" | "/" => {
                let b = stack.pop().unwrap();
                let a = stack.pop().unwrap();
                let result = match token {
                    "+" => a + b,
                    "-" => a - b,
                    "*" => a * b,
                    "/" => a / b,  // Rust i64 division truncates toward zero
                    _ => unreachable!(),
                };
                stack.push(result);
            }
            _ => {
                let num: i64 = token.parse().unwrap();
                stack.push(num);
            }
        }
    }

    writeln!(out, "{}", stack.pop().unwrap()).unwrap();
}
```

**Rust note**: Integer division in Rust truncates toward zero, which matches the problem
requirement. For example: `-7 / 2 == -3` (not -4).

**Complexity**: O(n) time, O(n) space.

</details>

---

## Problem 4: Daily Temperatures

### Statement

Given an array of `n` daily temperatures, for each day, find how many days you have to
wait until a warmer temperature. If there is no future day with a warmer temperature,
output `0`.

### Input Format

```
n
t_1 t_2 ... t_n
```

### Output Format

`n` integers on a single line, space-separated.

### Constraints

- 1 <= n <= 10^5
- 30 <= t_i <= 100

### Examples

```
Input:
8
73 74 75 71 69 72 76 73

Output:
1 1 4 2 1 1 0 0
```

Explanation:
- Day 0 (73): Day 1 (74) is warmer. Wait 1 day.
- Day 2 (75): Day 6 (76) is warmer. Wait 4 days.
- Day 6 (76): No warmer day. Wait 0 days.

### Hints

1. This is a monotonic stack problem, similar to Next Greater Element.
2. But instead of values, store **indices** on the stack.
3. Process left to right. For each temperature, pop all stack indices whose
   temperature is less than current. The wait for each popped index is
   `current_index - popped_index`.

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let temps: Vec<i32> = (0..n).map(|_| next!(i32)).collect();

    let mut result = vec![0usize; n];
    let mut stack: Vec<usize> = Vec::new(); // stores indices

    for i in 0..n {
        // TODO: While stack is non-empty and temps[stack.top()] < temps[i]:
        //   - Pop the index `j`
        //   - Set result[j] = i - j

        // TODO: Push current index i onto the stack
    }

    // Remaining indices in the stack have no warmer day (result stays 0)

    let result_str: Vec<String> = result.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result_str.join(" ")).unwrap();
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() };
    }
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = next!(usize);
    let temps: Vec<i32> = (0..n).map(|_| next!(i32)).collect();

    let mut result = vec![0usize; n];
    let mut stack: Vec<usize> = Vec::new();

    for i in 0..n {
        while let Some(&top_idx) = stack.last() {
            if temps[top_idx] < temps[i] {
                stack.pop();
                result[top_idx] = i - top_idx;
            } else {
                break;
            }
        }
        stack.push(i);
    }

    let result_str: Vec<String> = result.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", result_str.join(" ")).unwrap();
}
```

**Comparison with Problem 2**: Next Greater Element processes right-to-left and stores
values. Daily Temperatures processes left-to-right and stores indices. Both use a
decreasing monotonic stack, but the direction and what we store differ.

**Complexity**: O(n) time (each index is pushed and popped at most once), O(n) space.

</details>

---

## Problem 5: Implement Queue Using Two Stacks

### Statement

Implement a FIFO queue using only two stacks (`Vec<i32>`). The queue must support:
- `PUSH x` -- push element `x` to the back of the queue
- `POP` -- remove and print the front element
- `PEEK` -- print the front element without removing it

Process `q` operations.

### Input Format

```
q
operation_1
operation_2
...
operation_q
```

Each operation is one of:
- `PUSH x` (where x is an integer)
- `POP`
- `PEEK`

### Output Format

For each `POP` or `PEEK` operation, print the result on a separate line.

### Constraints

- 1 <= q <= 10^5
- -10^9 <= x <= 10^9
- `POP` and `PEEK` are only called when the queue is non-empty.

### Examples

```
Input:
7
PUSH 1
PUSH 2
PEEK
POP
PEEK
PUSH 3
POP

Output:
1
1
2
2
```

### Hints

1. Use two stacks: `inbox` (for pushes) and `outbox` (for pops/peeks).
2. On `PUSH`: push to `inbox`.
3. On `POP`/`PEEK`: if `outbox` is empty, transfer all elements from `inbox` to
   `outbox` (this reverses the order, giving FIFO access). Then pop/peek from `outbox`.
4. **Amortized O(1)**: Each element is moved at most twice (once to inbox, once to outbox).

```
PUSH 1, PUSH 2, PUSH 3:
  inbox:  [1, 2, 3]    outbox: []

POP (outbox empty, transfer):
  inbox:  []            outbox: [3, 2, 1]
  pop from outbox => 1
  inbox:  []            outbox: [3, 2]

PUSH 4:
  inbox:  [4]           outbox: [3, 2]

POP:
  outbox not empty, pop => 2
  inbox:  [4]           outbox: [3]
```

### Solution Template

```rust
use std::io::{self, Read, Write, BufWriter};

struct MyQueue {
    inbox: Vec<i32>,
    outbox: Vec<i32>,
}

impl MyQueue {
    fn new() -> Self {
        MyQueue {
            inbox: Vec::new(),
            outbox: Vec::new(),
        }
    }

    fn push(&mut self, x: i32) {
        // TODO: Push x to inbox
    }

    fn transfer(&mut self) {
        // TODO: If outbox is empty, move all elements from inbox to outbox
        //       (pop from inbox, push to outbox)
    }

    fn pop(&mut self) -> i32 {
        self.transfer();
        // TODO: Pop from outbox
        todo!()
    }

    fn peek(&self) -> i32 {
        // TODO: Return the front element without removing
        // Hint: if outbox is non-empty, it's outbox.last()
        //       otherwise it's inbox.first() -- but we should transfer first
        // Actually, since peek might not want to mutate, we need a &mut self version
        // or we can just make peek call transfer too
        todo!()
    }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut lines = input.lines();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let q: usize = lines.next().unwrap().trim().parse().unwrap();
    let mut queue = MyQueue::new();

    for _ in 0..q {
        let line = lines.next().unwrap().trim();

        if line.starts_with("PUSH") {
            let x: i32 = line[5..].parse().unwrap();
            queue.push(x);
        } else if line == "POP" {
            writeln!(out, "{}", queue.pop()).unwrap();
        } else if line == "PEEK" {
            // TODO: Call peek and print
        }
    }
}
```

<details>
<summary>Solution</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

struct MyQueue {
    inbox: Vec<i32>,
    outbox: Vec<i32>,
}

impl MyQueue {
    fn new() -> Self {
        MyQueue {
            inbox: Vec::new(),
            outbox: Vec::new(),
        }
    }

    fn push(&mut self, x: i32) {
        self.inbox.push(x);
    }

    fn transfer(&mut self) {
        if self.outbox.is_empty() {
            while let Some(val) = self.inbox.pop() {
                self.outbox.push(val);
            }
        }
    }

    fn pop(&mut self) -> i32 {
        self.transfer();
        self.outbox.pop().unwrap()
    }

    fn peek(&mut self) -> i32 {
        self.transfer();
        *self.outbox.last().unwrap()
    }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let mut lines = input.lines();

    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let q: usize = lines.next().unwrap().trim().parse().unwrap();
    let mut queue = MyQueue::new();

    for _ in 0..q {
        let line = lines.next().unwrap().trim();

        if line.starts_with("PUSH") {
            let x: i32 = line[5..].parse().unwrap();
            queue.push(x);
        } else if line == "POP" {
            writeln!(out, "{}", queue.pop()).unwrap();
        } else if line == "PEEK" {
            writeln!(out, "{}", queue.peek()).unwrap();
        }
    }
}
```

**Amortized analysis**: Each element is moved from `inbox` to `outbox` at most once.
Over `n` operations, the total work for transfers is O(n), giving amortized O(1) per
operation.

**Rust note**: We made `peek` take `&mut self` because it may need to call `transfer`.
In production code you might use `RefCell` or reorganize, but for CP this is fine.

**Complexity**: Amortized O(1) per operation, O(n) total.

</details>

---

## Summary Cheat Sheet

| Data Structure  | Rust Type            | Push         | Pop/Dequeue      | Peek            |
|-----------------|----------------------|--------------|------------------|-----------------|
| Stack           | `Vec<T>`             | `push(x)`    | `pop()` -> last  | `last()`        |
| Queue           | `VecDeque<T>`        | `push_back`  | `pop_front()`    | `front()`       |
| Deque           | `VecDeque<T>`        | both ends    | both ends        | `front`/`back`  |
| Monotonic Stack | `Vec<T>` + invariant | conditional  | conditional pop  | `last()`        |

### Common Stack/Queue Patterns

```
Pattern                  | Stack Type         | Direction       | Example
-------------------------+--------------------+-----------------+-------------------
Matching brackets        | Last-seen opener   | Left to right   | Valid parentheses
Next greater element     | Decreasing values  | Right to left   | NGE, stock span
Next greater (index)     | Decreasing indices | Left to right   | Daily temperatures
Expression evaluation    | Operand stack      | Left to right   | RPN calculator
Sliding window maximum   | Decreasing deque   | Left to right   | Max in window of k
```

### Common Pitfalls

- **Empty stack `pop()`**: Always check `is_empty()` or use `if let Some(val) = stack.pop()`.
- **`while let` with break**: `while let Some(&top) = stack.last() { if condition { stack.pop(); } else { break; } }` is the idiomatic monotonic stack pattern.
- **Using `VecDeque` as stack**: It works but `Vec` is simpler and faster for pure stack use.
- **Index type**: Store `usize` indices on the stack when you need positions, not values.
