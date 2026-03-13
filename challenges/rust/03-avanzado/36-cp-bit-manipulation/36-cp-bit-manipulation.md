# 36. CP: Bit Manipulation

## Difficulty: Avanzado

## Introduction

Bit manipulation is the foundation of many competitive programming problems. At its core, it replaces expensive arithmetic and data structure operations with constant-time bitwise instructions that map directly to single CPU instructions. In Rust, this is particularly clean: the language provides all six bitwise operators (`&`, `|`, `^`, `!`, `<<`, `>>`), the integer types have built-in methods like `count_ones()`, `trailing_zeros()`, and `leading_zeros()`, and the type system prevents accidental sign-extension bugs that plague C/C++ solutions.

This exercise covers the essential bit manipulation toolkit: fundamental operations, classic tricks, bitmask-based subset enumeration, XOR identities, and five competition problems that require these techniques.

---

## Bitwise Operators in Rust

Rust provides six bitwise operators that work on all integer types (`u8`, `u16`, `u32`, `u64`, `u128`, `usize`, and their signed counterparts):

```rust
fn main() {
    let a: u8 = 0b1100_1010; // 202
    let b: u8 = 0b1010_0110; // 166

    // AND: both bits must be 1
    println!("a & b  = {:08b}", a & b);   // 10000010

    // OR: at least one bit must be 1
    println!("a | b  = {:08b}", a | b);   // 11101110

    // XOR: exactly one bit must be 1
    println!("a ^ b  = {:08b}", a ^ b);   // 01101100

    // NOT: flip all bits (bitwise complement)
    println!("!a     = {:08b}", !a);       // 00110101

    // Left shift: multiply by 2^n
    println!("a << 2 = {:08b}", (a as u16) << 2); // wider type to see all bits

    // Right shift: divide by 2^n (logical for unsigned, arithmetic for signed)
    println!("a >> 3 = {:08b}", a >> 3);   // 00011001
}
```

A critical Rust detail: `!` is the bitwise NOT operator (not `~` as in C/C++). The logical NOT for `bool` also uses `!`, but the compiler disambiguates by type.

---

## Essential Bit Tricks

These are the building blocks for nearly every bit manipulation problem. Each operates in O(1) time.

### Check, Set, Clear, Toggle a Single Bit

```rust
fn main() {
    let mut x: u32 = 0b1010_1100;
    let pos = 3; // 0-indexed from the right

    // Check if bit at position `pos` is set
    let is_set = (x >> pos) & 1 == 1;
    println!("Bit {pos} is set: {is_set}"); // false (bit 3 is 0)

    // Set bit at position `pos`
    x |= 1 << pos;
    println!("After set:    {:08b}", x); // 10110100

    // Clear bit at position `pos`
    x &= !(1 << pos);
    println!("After clear:  {:08b}", x); // 10101100

    // Toggle bit at position `pos`
    x ^= 1 << pos;
    println!("After toggle: {:08b}", x); // 10110100
}
```

### Count Set Bits (Population Count)

```rust
fn main() {
    let x: u32 = 0b1101_0110_1010_0011;

    // Built-in method -- compiles to a single POPCNT instruction on x86
    println!("count_ones: {}", x.count_ones()); // 9

    // Manual implementation (Brian Kernighan's algorithm)
    // Each iteration clears the lowest set bit, so it runs exactly popcount times.
    fn popcount(mut n: u32) -> u32 {
        let mut count = 0;
        while n != 0 {
            n &= n - 1; // clear lowest set bit
            count += 1;
        }
        count
    }
    println!("popcount:   {}", popcount(x)); // 9
}
```

### Lowest Set Bit

```rust
fn main() {
    let x: u32 = 0b1010_1000;

    // Isolate the lowest set bit
    // x & (!x + 1) is equivalent to x & x.wrapping_neg() for unsigned
    let lowest = x & x.wrapping_neg();
    println!("Lowest set bit: {:08b}", lowest); // 00001000

    // Position of the lowest set bit (0-indexed)
    println!("Position: {}", x.trailing_zeros()); // 3

    // Remove the lowest set bit
    let cleared = x & (x - 1);
    println!("After removing lowest: {:08b}", cleared); // 10100000
}
```

### Power of Two Check

```rust
fn is_power_of_two(n: u32) -> bool {
    // A power of two has exactly one bit set.
    // n & (n - 1) clears that single bit, leaving zero.
    n != 0 && (n & (n - 1)) == 0
}

fn main() {
    for n in [0, 1, 2, 3, 4, 16, 31, 32, 64, 100] {
        println!("{n:>3}: {}", is_power_of_two(n));
    }
}
```

### Extracting and Replacing Bit Fields

```rust
fn main() {
    let value: u32 = 0xDEAD_BEEF;

    // Extract bits [11:4] (8 bits starting at position 4)
    let field = (value >> 4) & 0xFF;
    println!("Bits [11:4] = 0x{:02X}", field); // 0xEE

    // Replace bits [11:4] with a new value
    let new_field: u32 = 0x42;
    let mask = !(0xFF << 4); // clear the target field
    let result = (value & mask) | (new_field << 4);
    println!("After replace: 0x{:08X}", result); // 0xDEADB42F
}
```

---

## XOR Properties

XOR has algebraic properties that make it uniquely powerful for competitive programming:

```rust
fn main() {
    let a: u32 = 42;
    let b: u32 = 17;

    // 1. Self-inverse: a ^ a = 0
    assert_eq!(a ^ a, 0);

    // 2. Identity: a ^ 0 = a
    assert_eq!(a ^ 0, a);

    // 3. Commutative: a ^ b = b ^ a
    assert_eq!(a ^ b, b ^ a);

    // 4. Associative: (a ^ b) ^ c = a ^ (b ^ c)
    let c: u32 = 99;
    assert_eq!((a ^ b) ^ c, a ^ (b ^ c));

    // 5. Cancellation: if a ^ b = c, then a ^ c = b and b ^ c = a
    let c = a ^ b;
    assert_eq!(a ^ c, b);
    assert_eq!(b ^ c, a);

    // Consequence: XOR of a list where every element appears twice = 0
    let pairs = [3, 7, 3, 5, 7, 9, 5, 9, 42];
    let unique = pairs.iter().fold(0u32, |acc, &x| acc ^ x);
    println!("Unique element: {unique}"); // 42

    // Swap two variables without a temporary
    let mut x = 10u32;
    let mut y = 25u32;
    x ^= y;
    y ^= x;
    x ^= y;
    println!("After swap: x={x}, y={y}"); // x=25, y=10
}
```

---

## Bitmask Subset Enumeration

A bitmask of `n` bits represents a subset of an `n`-element set, where bit `i` is 1 if element `i` is included. This lets you enumerate all 2^n subsets in O(2^n) time and all subsets of a given mask in O(3^n) total across all masks.

### All Subsets of {0, 1, ..., n-1}

```rust
fn main() {
    let n = 3;
    let elements = ['A', 'B', 'C'];

    println!("All subsets of {:?}:", elements);
    for mask in 0..(1u32 << n) {
        let subset: Vec<char> = (0..n)
            .filter(|&i| mask & (1 << i) != 0)
            .map(|i| elements[i])
            .collect();
        println!("  {:03b} -> {:?}", mask, subset);
    }
    // Output:
    //   000 -> []
    //   001 -> ['A']
    //   010 -> ['B']
    //   011 -> ['A', 'B']
    //   100 -> ['C']
    //   101 -> ['A', 'C']
    //   110 -> ['B', 'C']
    //   111 -> ['A', 'B', 'C']
}
```

### Iterating Over Submasks of a Given Mask

This is a classic trick: to enumerate all submasks of a bitmask `mask` (including `mask` itself and 0), use the decrement-and-AND loop:

```rust
fn submasks(mask: u32) -> Vec<u32> {
    let mut result = Vec::new();
    let mut sub = mask;
    loop {
        result.push(sub);
        if sub == 0 {
            break;
        }
        sub = (sub - 1) & mask;
    }
    result
}

fn main() {
    let mask = 0b1011u32; // elements {0, 1, 3}
    let elements = ['A', 'B', 'C', 'D'];

    println!("Submasks of {:04b}:", mask);
    for sub in submasks(mask) {
        let subset: Vec<char> = (0..4)
            .filter(|&i| sub & (1 << i) != 0)
            .map(|i| elements[i])
            .collect();
        println!("  {:04b} -> {:?}", sub, subset);
    }
}
```

The total work across all masks in a bitmask DP is O(3^n), because each element is independently "in the mask and in the submask", "in the mask but not in the submask", or "not in the mask".

---

## Bitmask Dynamic Programming

Bitmask DP uses an integer bitmask as the DP state, typically representing which elements from a set have been "used" or "visited". This is the standard approach for problems on small sets (n <= 20).

### Example: Traveling Salesman (TSP)

```rust
/// Solve TSP on n cities with distance matrix `dist`.
/// Returns the minimum cost to visit all cities starting and ending at city 0.
fn tsp(dist: &[Vec<i64>]) -> i64 {
    let n = dist.len();
    let full_mask = (1 << n) - 1;

    // dp[mask][i] = minimum cost to have visited exactly the cities in `mask`,
    // ending at city `i`.
    let inf = i64::MAX / 2;
    let mut dp = vec![vec![inf; n]; 1 << n];
    dp[1][0] = 0; // start at city 0, only city 0 visited

    for mask in 1..=full_mask {
        for last in 0..n {
            if dp[mask][last] == inf {
                continue;
            }
            if mask & (1 << last) == 0 {
                continue;
            }
            // Try visiting each unvisited city
            for next in 0..n {
                if mask & (1 << next) != 0 {
                    continue; // already visited
                }
                let new_mask = mask | (1 << next);
                let new_cost = dp[mask][last] + dist[last][next];
                if new_cost < dp[new_mask][next] {
                    dp[new_mask][next] = new_cost;
                }
            }
        }
    }

    // Find minimum cost to return to city 0 from any last city
    (0..n)
        .map(|last| dp[full_mask][last].saturating_add(dist[last][0]))
        .min()
        .unwrap()
}

fn main() {
    let dist = vec![
        vec![0, 10, 15, 20],
        vec![10, 0, 35, 25],
        vec![15, 35, 0, 30],
        vec![20, 25, 30, 0],
    ];
    println!("TSP minimum cost: {}", tsp(&dist)); // 80
}
```

---

## Rust-Specific Bit Manipulation Tools

Rust's standard library provides methods that map to efficient hardware instructions:

```rust
fn main() {
    let x: u32 = 0b0010_1000_1100_0000;

    // Population count
    println!("count_ones:      {}", x.count_ones());      // 4
    println!("count_zeros:     {}", x.count_zeros());     // 28

    // Leading/trailing zeros
    println!("leading_zeros:   {}", x.leading_zeros());   // 18 (for u32 - 14 = 18? let's compute)
    println!("trailing_zeros:  {}", x.trailing_zeros());  // 6

    // Leading/trailing ones
    let y: u32 = 0xFF00_0000;
    println!("leading_ones:    {}", y.leading_ones());    // 8

    // Rotate bits
    let z: u8 = 0b1100_0011;
    println!("rotate_left(2):  {:08b}", z.rotate_left(2));  // 00001111
    println!("rotate_right(2): {:08b}", z.rotate_right(2)); // 11110000

    // Reverse bits
    println!("reverse_bits:    {:08b}", z.reverse_bits());   // 11000011 reversed

    // Swap bytes
    let w: u32 = 0x12345678;
    println!("swap_bytes:      0x{:08X}", w.swap_bytes()); // 0x78563412

    // Next power of two
    println!("next_power_of_two(5): {}", 5u32.next_power_of_two()); // 8
    println!("next_power_of_two(8): {}", 8u32.next_power_of_two()); // 8

    // Checked power of two
    println!("is_power_of_two(8):  {}", 8u32.is_power_of_two());  // true
    println!("is_power_of_two(10): {}", 10u32.is_power_of_two()); // false
}
```

---

## Problem 1: Single Number (XOR)

Given an array of integers where every element appears exactly twice except one, find the element that appears only once. You must use O(1) extra space.

**Constraints**: 1 <= nums.len() <= 30,000. Every element appears twice except one.

**Key insight**: XOR of identical values cancels out. XOR the entire array; pairs cancel to 0, leaving only the unique value.

**Hints**:
- `a ^ a = 0` and `a ^ 0 = a`
- A single fold/reduce over the array is sufficient
- This generalizes: if every element appears `k` times except one that appears once, you need bit counting modulo `k`

<details>
<summary>Solution</summary>

```rust
fn single_number(nums: &[i32]) -> i32 {
    nums.iter().fold(0, |acc, &x| acc ^ x)
}

fn main() {
    assert_eq!(single_number(&[2, 2, 1]), 1);
    assert_eq!(single_number(&[4, 1, 2, 1, 2]), 4);
    assert_eq!(single_number(&[1]), 1);
    assert_eq!(single_number(&[-1, -1, -2]), -2);

    // Stress test with a larger array
    let mut nums: Vec<i32> = (1..=1000).flat_map(|x| [x, x]).collect();
    nums.push(9999);
    assert_eq!(single_number(&nums), 9999);

    println!("All single_number tests passed.");
}
```

**Complexity**: O(n) time, O(1) space. Each XOR is a single CPU instruction.

</details>

---

## Problem 2: Counting Bits

Given a non-negative integer `n`, return an array `ans` of length `n + 1` where `ans[i]` is the number of 1s in the binary representation of `i`.

**Constraints**: 0 <= n <= 100,000. Expected O(n) time (not O(n log n) by calling `count_ones` on each).

**Key insight**: There is a recurrence relation. For any number `i`, the number of set bits is related to `i >> 1` (which you already computed) plus the lowest bit of `i`.

**Hints**:
- `popcount(i) = popcount(i >> 1) + (i & 1)`
- Alternatively: `popcount(i) = popcount(i & (i - 1)) + 1` (Brian Kernighan relation)
- Both give O(n) DP solutions

<details>
<summary>Solution</summary>

```rust
fn counting_bits(n: usize) -> Vec<u32> {
    let mut ans = vec![0u32; n + 1];
    for i in 1..=n {
        // i >> 1 has already been computed, and we add the lowest bit of i
        ans[i] = ans[i >> 1] + (i as u32 & 1);
    }
    ans
}

// Alternative using Brian Kernighan's trick
fn counting_bits_kernighan(n: usize) -> Vec<u32> {
    let mut ans = vec![0u32; n + 1];
    for i in 1..=n {
        // i & (i-1) clears the lowest set bit, so its popcount is one less
        ans[i] = ans[i & (i - 1)] + 1;
    }
    ans
}

fn main() {
    assert_eq!(counting_bits(0), vec![0]);
    assert_eq!(counting_bits(2), vec![0, 1, 1]);
    assert_eq!(counting_bits(5), vec![0, 1, 1, 2, 1, 2]);
    assert_eq!(counting_bits(8), vec![0, 1, 1, 2, 1, 2, 2, 3, 1]);

    // Verify both implementations agree
    let n = 10_000;
    let a = counting_bits(n);
    let b = counting_bits_kernighan(n);
    assert_eq!(a, b);

    // Verify against built-in count_ones
    for i in 0..=n {
        assert_eq!(a[i], (i as u32).count_ones());
    }

    println!("All counting_bits tests passed.");
}
```

**Complexity**: O(n) time, O(n) space. Each element is computed in O(1) from a previously computed value.

</details>

---

## Problem 3: Subsets of a Set via Bitmask

Given a list of distinct integers, return all possible subsets (the power set). The solution must not contain duplicate subsets.

**Constraints**: 1 <= nums.len() <= 20. All elements are distinct.

**Key insight**: With `n` elements, there are exactly 2^n subsets. Each subset corresponds to an `n`-bit mask. Iterate from 0 to 2^n - 1, and for each mask include the elements whose corresponding bits are set.

**Hints**:
- For `n` elements, iterate `mask` from `0` to `(1 << n) - 1`
- For each mask, check each bit position `i` with `mask & (1 << i) != 0`
- This approach is inherently O(n * 2^n), which matches the output size

<details>
<summary>Solution</summary>

```rust
fn subsets(nums: &[i32]) -> Vec<Vec<i32>> {
    let n = nums.len();
    let mut result = Vec::with_capacity(1 << n);

    for mask in 0..(1u32 << n) {
        let subset: Vec<i32> = (0..n)
            .filter(|&i| mask & (1 << i) != 0)
            .map(|i| nums[i])
            .collect();
        result.push(subset);
    }

    result
}

/// Bonus: enumerate all subsets of a given subset (submask iteration)
fn subsets_of_mask(nums: &[i32], indices: &[usize]) -> Vec<Vec<i32>> {
    let n = nums.len();
    // Build the mask from the given indices
    let mut mask = 0u32;
    for &i in indices {
        mask |= 1 << i;
    }

    let mut result = Vec::new();
    let mut sub = mask;
    loop {
        let subset: Vec<i32> = (0..n)
            .filter(|&i| sub & (1 << i) != 0)
            .map(|i| nums[i])
            .collect();
        result.push(subset);
        if sub == 0 {
            break;
        }
        sub = (sub - 1) & mask;
    }

    result
}

fn main() {
    // Basic subsets
    let result = subsets(&[1, 2, 3]);
    assert_eq!(result.len(), 8); // 2^3 = 8
    println!("Subsets of [1, 2, 3]:");
    for s in &result {
        println!("  {:?}", s);
    }

    // Empty set
    let result = subsets(&[]);
    assert_eq!(result.len(), 1);
    assert_eq!(result[0], vec![]);

    // Single element
    let result = subsets(&[42]);
    assert_eq!(result.len(), 2);

    // Verify subset count for n=10
    let nums: Vec<i32> = (0..10).collect();
    assert_eq!(subsets(&nums).len(), 1024);

    // Submask iteration
    let nums = vec![10, 20, 30, 40, 50];
    let sub_result = subsets_of_mask(&nums, &[0, 2, 4]); // subsets of {10, 30, 50}
    assert_eq!(sub_result.len(), 8); // 2^3 = 8
    println!("\nSubsets of {{10, 30, 50}}:");
    for s in &sub_result {
        println!("  {:?}", s);
    }

    println!("\nAll subsets tests passed.");
}
```

**Complexity**: O(n * 2^n) time for generating all subsets (each of 2^n masks requires O(n) to decode). Submask enumeration over all masks is O(3^n) total.

</details>

---

## Problem 4: Maximum XOR of Two Numbers

Given an array of non-negative integers, find the maximum XOR of any two elements.

**Constraints**: 2 <= nums.len() <= 200,000. 0 <= nums[i] < 2^31.

**Key insight**: Build the answer bit by bit from the most significant bit. At each step, greedily try to set the current bit to 1. Use a HashSet to check if any pair of numbers can achieve the desired prefix.

**Hints**:
- Process bits from the MSB (bit 30) down to bit 0
- For each bit position, extract the prefixes of all numbers up to that bit
- If `a ^ b = target`, then `a ^ target = b`. So insert all prefixes into a set, and for each prefix `p`, check if `p ^ candidate` is in the set
- The candidate at each step is `current_best | (1 << bit)`

<details>
<summary>Solution</summary>

```rust
use std::collections::HashSet;

fn maximum_xor(nums: &[i32]) -> i32 {
    let mut max_xor = 0i32;
    let mut mask = 0i32;

    // Process from the most significant bit down
    for bit in (0..31).rev() {
        // Include this bit in the mask
        mask |= 1 << bit;

        // Collect all prefixes (numbers masked to the current bit range)
        let prefixes: HashSet<i32> = nums.iter().map(|&n| n & mask).collect();

        // Greedily try to set this bit in the result
        let candidate = max_xor | (1 << bit);

        // Check if any two prefixes XOR to the candidate
        // If a ^ b = candidate, then a ^ candidate = b
        let achievable = prefixes.iter().any(|&p| prefixes.contains(&(p ^ candidate)));

        if achievable {
            max_xor = candidate;
        }
    }

    max_xor
}

/// Alternative: Trie-based solution for O(n * 31) without hashing overhead
struct TrieNode {
    children: [Option<Box<TrieNode>>; 2],
}

impl TrieNode {
    fn new() -> Self {
        TrieNode {
            children: [None, None],
        }
    }
}

fn maximum_xor_trie(nums: &[i32]) -> i32 {
    let mut root = TrieNode::new();

    // Insert a number into the trie (MSB first, 31 bits)
    let insert = |root: &mut TrieNode, num: i32| {
        let mut node = root;
        for bit in (0..31).rev() {
            let b = ((num >> bit) & 1) as usize;
            node = node.children[b].get_or_insert_with(|| Box::new(TrieNode::new()));
        }
    };

    // Query the maximum XOR achievable with `num` against all inserted numbers
    let query = |root: &TrieNode, num: i32| -> i32 {
        let mut node = root;
        let mut result = 0;
        for bit in (0..31).rev() {
            let b = ((num >> bit) & 1) as usize;
            let want = 1 - b; // we want the opposite bit to maximize XOR
            if node.children[want].is_some() {
                result |= 1 << bit;
                node = node.children[want].as_ref().unwrap();
            } else {
                node = node.children[b].as_ref().unwrap();
            }
        }
        result
    };

    insert(&mut root, nums[0]);
    let mut best = 0;
    for &num in &nums[1..] {
        best = best.max(query(&root, num));
        insert(&mut root, num);
    }
    best
}

fn main() {
    // Test case 1
    assert_eq!(maximum_xor(&[3, 10, 5, 25, 2, 8]), 28); // 5 ^ 25 = 28

    // Test case 2
    assert_eq!(maximum_xor(&[0, 0, 0]), 0);

    // Test case 3
    assert_eq!(maximum_xor(&[14, 70, 53, 83, 49, 91, 36, 80, 92, 51, 66, 70]), 127);

    // Verify both approaches agree
    let nums = vec![3, 10, 5, 25, 2, 8];
    assert_eq!(maximum_xor(&nums), maximum_xor_trie(&nums));

    let nums = vec![14, 70, 53, 83, 49, 91, 36, 80, 92, 51, 66, 70];
    assert_eq!(maximum_xor(&nums), maximum_xor_trie(&nums));

    println!("All maximum_xor tests passed.");
}
```

**Complexity**: HashSet approach is O(31 * n) time, O(n) space. Trie approach is O(31 * n) time, O(31 * n) space in the worst case. Both are effectively O(n) since the bit width is a constant.

</details>

---

## Problem 5: Divide Two Integers Without Division

Given two integers `dividend` and `divisor`, divide them without using multiplication, division, or modulo operators. Return the quotient truncated toward zero.

**Constraints**: -2^31 <= dividend, divisor <= 2^31 - 1. divisor != 0. If the result overflows 32-bit signed integer range, return 2^31 - 1.

**Key insight**: Division is repeated subtraction. But instead of subtracting the divisor one at a time (O(dividend/divisor)), subtract the largest possible power-of-two multiple of the divisor in each step. This is essentially binary long division, running in O(log^2(n)) or O(32) steps.

**Hints**:
- Work with absolute values (as `i64` to handle overflow of `-2^31`)
- Find the largest `k` such that `divisor << k <= dividend`
- Subtract `divisor << k` from the dividend and add `1 << k` to the quotient
- Repeat until the dividend is less than the divisor
- Handle the sign separately

<details>
<summary>Solution</summary>

```rust
fn divide(dividend: i32, divisor: i32) -> i32 {
    // Handle overflow: -2^31 / -1 = 2^31 which overflows i32
    if dividend == i32::MIN && divisor == -1 {
        return i32::MAX;
    }

    // Determine sign of result
    let negative = (dividend < 0) ^ (divisor < 0);

    // Work with absolute values as i64 to avoid overflow
    let mut dvd = (dividend as i64).abs();
    let dvs = (divisor as i64).abs();

    let mut quotient: i32 = 0;

    // Binary long division
    // Find the highest bit position where (dvs << shift) <= dvd
    while dvd >= dvs {
        let mut shift = 0;
        while dvd >= (dvs << (shift + 1)) {
            shift += 1;
        }
        // Subtract the largest aligned multiple
        dvd -= dvs << shift;
        quotient += 1 << shift;
    }

    if negative {
        -quotient
    } else {
        quotient
    }
}

fn main() {
    // Basic cases
    assert_eq!(divide(10, 3), 3);
    assert_eq!(divide(7, -2), -3);
    assert_eq!(divide(-7, 2), -3);
    assert_eq!(divide(-7, -2), 3);

    // Edge cases
    assert_eq!(divide(0, 1), 0);
    assert_eq!(divide(1, 1), 1);
    assert_eq!(divide(-1, 1), -1);
    assert_eq!(divide(i32::MIN, -1), i32::MAX); // overflow protection
    assert_eq!(divide(i32::MIN, 1), i32::MIN);
    assert_eq!(divide(i32::MIN, i32::MIN), 1);
    assert_eq!(divide(i32::MAX, i32::MAX), 1);
    assert_eq!(divide(i32::MAX, 1), i32::MAX);

    // Larger values
    assert_eq!(divide(100, 7), 14);
    assert_eq!(divide(1_000_000, 3), 333_333);

    println!("All divide tests passed.");
}
```

**Complexity**: O(log^2(n)) time where n is the magnitude of the dividend. The outer loop runs at most 32 times (one per bit), and the inner shift-finding loop also runs at most 32 times. Space is O(1).

</details>

---

## Complexity Summary

| Problem | Time | Space | Technique |
|---------|------|-------|-----------|
| Single Number | O(n) | O(1) | XOR fold |
| Counting Bits | O(n) | O(n) | DP with bit recurrence |
| Subsets via Bitmask | O(n * 2^n) | O(n * 2^n) | Bitmask enumeration |
| Maximum XOR | O(31 * n) | O(n) | Greedy bit-by-bit + HashSet |
| Divide Without Division | O(log^2 n) | O(1) | Binary long division via shifts |

---

## Verification

Create a test project and run all solutions:

```bash
cargo new bit-manipulation-lab && cd bit-manipulation-lab
```

Paste any solution into `src/main.rs` and run:

```bash
cargo run
```

To verify the bitmask DP (TSP) example:

```bash
# Add to main.rs and run -- the 4-city example should output 80
cargo run
```

Run clippy for idiomatic checks:

```bash
cargo clippy -- -W clippy::pedantic
```

---

## What You Learned

- **Bitwise operators** (`&`, `|`, `^`, `!`, `<<`, `>>`) map to single CPU instructions and form the basis of all bit manipulation. Rust uses `!` for bitwise NOT (not `~`).
- **Core tricks** -- checking, setting, clearing, and toggling bits, isolating the lowest set bit (`x & x.wrapping_neg()`), and clearing the lowest set bit (`x & (x - 1)`) -- appear constantly in competitive programming.
- **XOR properties** (self-inverse, identity, commutativity, associativity) enable problems like finding the unique element in O(n) time with O(1) space, and building maximum XOR values greedily.
- **Bitmask subset enumeration** maps each subset to an integer, enabling O(2^n) enumeration and O(3^n) submask DP -- the standard approach for problems on small sets (n <= 20).
- **Bitmask DP** (e.g., TSP) uses a bitmask as the DP state dimension, compressing exponential state spaces into manageable O(2^n * n) tables.
- **Rust's built-in methods** (`count_ones`, `trailing_zeros`, `leading_zeros`, `rotate_left`, `is_power_of_two`, `next_power_of_two`) compile to efficient hardware instructions and eliminate the need for manual bit-counting routines.
- **Binary long division** via bit shifts replaces multiplication/division with logarithmic-time shift-and-subtract loops, demonstrating how bit manipulation can replace arithmetic operators entirely.
