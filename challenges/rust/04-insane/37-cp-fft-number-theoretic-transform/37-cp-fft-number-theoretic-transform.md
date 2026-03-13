# 37. CP: FFT and Number Theoretic Transform

**Difficulty**: Insane

## The Challenge

The Fast Fourier Transform (FFT) is one of the most important algorithms ever devised, appearing on virtually every list of the top algorithms of the twentieth century. In competitive programming, FFT and its integer-exact cousin, the Number Theoretic Transform (NTT), enable polynomial multiplication in O(n log n) time — a capability that unlocks solutions to an enormous range of problems: from multiplying large numbers to counting string pattern occurrences, from computing convolutions for combinatorial problems to accelerating certain dynamic programming recurrences. Your task is to implement both FFT (over complex numbers) and NTT (over prime fields), build a suite of applications on top of them, and handle all the numerical and algorithmic subtleties that separate a working implementation from a robust, competition-ready one.

The core FFT algorithm — the Cooley-Tukey radix-2 decimation-in-time — recursively divides a polynomial evaluation problem in half, evaluates at roots of unity, and combines the results. For real-world applications, you must implement the iterative (bottom-up) version with bit-reversal permutation, which avoids the overhead of recursion and runs entirely in-place. The NTT replaces the complex roots of unity with primitive roots in a prime field (typically the prime 998244353 = 119 * 2^23 + 1, which has a primitive root of 3 and supports transforms up to length 2^23). The NTT produces exact integer results and avoids all floating-point precision issues, making it strictly superior for problems where the answer is an integer and the modulus is compatible. However, many problems require FFT over complex numbers — either because the answer is not modular, or because the modulus is not NTT-friendly — and in these cases you must handle the inherent floating-point imprecision (typically by rounding the results to the nearest integer).

Beyond the raw transforms, this challenge requires you to build practical applications: polynomial multiplication (the foundational operation), large number multiplication (treating each digit as a coefficient), string matching via convolution (counting occurrences of a pattern where wildcards and character mismatches are handled via polynomial products), and the "three-way NTT" technique for multiplying polynomials under a non-NTT-friendly modulus (e.g., 10^9 + 7) by performing three NTTs with different NTT-friendly primes and combining the results via the Chinese Remainder Theorem. Each application introduces its own subtleties: coefficient magnitudes that push floating-point precision to its limits, modular arithmetic pitfalls, and the need for careful analysis of when NTT is applicable versus when FFT is required.

## Acceptance Criteria

### Core FFT Implementation

- [ ] Implement the **iterative Cooley-Tukey FFT** (radix-2, decimation-in-time)
  - Input: a vector of complex numbers of length n (padded to the next power of 2)
  - Output: the DFT of the input (evaluation of the polynomial at the n-th roots of unity)
  - Use an in-place algorithm with bit-reversal permutation
  - Use `f64` for real and imaginary components
  - Support both the forward transform (DFT) and the inverse transform (IDFT)
  - The inverse transform divides each element by n after the butterfly operations

- [ ] Implement **bit-reversal permutation** efficiently
  - For each index i in [0, n), compute the bit-reversed index and swap if needed
  - Use the iterative bit-reversal method (not a lookup table, which wastes memory for large n)
  - Avoid redundant swaps (only swap when `i < bit_reverse(i)`)

- [ ] Pre-compute the **twiddle factors** (roots of unity)
  - Compute `e^{-2*pi*i*k/n}` for all needed k values before the butterfly loop
  - Store them in an array to avoid recomputation in the inner loop
  - For the inverse transform, use the conjugate roots

- [ ] Implement the **"split radix" or "radix-4" optimization** (optional but encouraged)
  - Reduces the number of real multiplications compared to radix-2
  - Alternatively, implement the "three-point FFT" trick for odd-length inputs

- [ ] Handle the **real-valued FFT optimization** (optional but encouraged)
  - When the input is purely real (no imaginary components), pack two real transforms into one complex transform
  - This halves the computation time for the common case of polynomial multiplication with real coefficients

### Core NTT Implementation

- [ ] Implement the **Number Theoretic Transform** over Z/pZ where p = 998244353
  - Same butterfly structure as FFT, but arithmetic is modular
  - The primitive root g = 3; the n-th root of unity is `g^((p-1)/n) mod p`
  - Support transform lengths up to 2^23 (= 8388608) which is the maximum for this prime
  - Use `i64` or `u64` for modular arithmetic; ensure no overflow in intermediate products

- [ ] Implement **modular arithmetic helpers**
  - `mod_pow(base, exp, modulus) -> i64`: fast exponentiation using binary method
  - `mod_inv(a, modulus) -> i64`: modular inverse via extended GCD or Fermat's little theorem
  - `mod_mul(a, b, modulus) -> i64`: multiplication with intermediate value fitting in `i64` (since `a, b < p < 2^30`, their product fits in `i64`)
  - Ensure these functions handle edge cases: `mod_pow(0, 0, p)` returns 1, `mod_inv(0, p)` returns an error

- [ ] Implement the **inverse NTT**
  - Apply the forward NTT with the inverse of the primitive root
  - Multiply each element by `n^{-1} mod p` after the transform
  - Verify that `NTT(INTT(x)) == x` for all test inputs

- [ ] Support **arbitrary NTT-friendly primes**
  - Parameterize the NTT by the prime and its primitive root
  - Provide a function to find a primitive root of a given prime
  - Support at least: 998244353, 985661441, 754974721 (all of the form `c * 2^k + 1` with large k)
  - These three primes are used together for the CRT-based approach (see below)

### Polynomial Multiplication

- [ ] Implement **polynomial multiplication using FFT**
  - Input: two polynomials represented as coefficient vectors `a` and `b`
  - Output: the product polynomial `c` where `c[k] = sum of a[i] * b[k-i]`
  - Steps: pad both to length `n = next_power_of_two(len(a) + len(b) - 1)`, FFT both, multiply pointwise, IFFT, round to nearest integer
  - Correctly handle the output length: `len(c) = len(a) + len(b) - 1`

- [ ] Implement **polynomial multiplication using NTT**
  - Same steps as FFT but with modular arithmetic and exact results
  - The result is the product polynomial with coefficients reduced mod p
  - No rounding needed; results are exact

- [ ] **Verify correctness**: for polynomials up to degree 1000, compare FFT-based multiplication with naive O(n^2) multiplication
  - All coefficients should match (after rounding for FFT)
  - Test with coefficients up to 10^9 to stress floating-point precision

- [ ] **Verify performance**: multiply two degree-500000 polynomials in < 500ms

### Application 1: Large Number Multiplication

- [ ] Implement **multiplication of large numbers** (up to 10^6 digits) using FFT/NTT
  - Represent each number as a polynomial where each coefficient is a digit (or a group of digits for efficiency)
  - Multiply the polynomials to get the product
  - Handle the carry propagation after the polynomial multiplication to produce the final digit representation
  - Input: two numbers as strings of digits
  - Output: their product as a string of digits

- [ ] Handle **base conversion for efficiency**
  - Using individual digits (base 10) wastes FFT capacity because coefficients are small
  - Group digits into chunks (e.g., base 10000) so each coefficient is a 4-digit number
  - This reduces the polynomial degree by a factor of 4 while keeping intermediate products within floating-point precision
  - Document the maximum digits per chunk for which FFT remains accurate (hint: for two n-digit numbers in base B, the maximum intermediate product is n * B^2, which must be < 2^53 for exact f64 representation)

- [ ] Handle **edge cases**
  - Multiplication by zero
  - One or both operands are a single digit
  - Very large operands (10^6 digits each)
  - Leading zeros in the result

- [ ] Verify against a known-good big integer library (e.g., `num-bigint` crate) for correctness

### Application 2: String Matching via Convolution

- [ ] Implement **exact string matching** using polynomial multiplication
  - Given text T of length n and pattern P of length m, find all positions where P occurs in T
  - Encode characters as integers, compute the convolution of T and the reverse of P, and identify positions where the convolution value equals the expected "perfect match" value
  - This is O(n log n) vs. naive O(nm) but has a larger constant factor; practical for very long texts

- [ ] Implement **wildcard string matching** via convolution
  - Pattern P may contain wildcard characters '?' that match any single character
  - For each character c in the alphabet, create indicator polynomials and convolve them
  - A position is a match if the sum of convolutions equals the number of non-wildcard characters in P
  - Time complexity: O(|alphabet| * n * log n) or O(n * log n) with a single polynomial trick

- [ ] Implement the **single-polynomial wildcard matching trick**
  - For each position i, define: `match(i) = sum over j of (T[i+j] - P[j])^2 * w(j)` where `w(j) = 0` if P[j] is a wildcard, 1 otherwise
  - Expand this into three convolutions and check where `match(i) == 0`
  - This handles wildcards with only 3 polynomial multiplications regardless of alphabet size

- [ ] Test with:
  - Simple patterns without wildcards (verify against naive search)
  - Patterns with wildcards at various positions (beginning, middle, end)
  - All-wildcard pattern (matches every position)
  - Pattern longer than text (no matches)
  - Large inputs: text length 10^6, pattern length 10^5

### Application 3: Convolution for Counting Problems

- [ ] Solve a **dice sum counting problem**:
  - Given N dice, each with faces 1 to K, count the number of ways to get each possible sum (from N to N*K)
  - This is the N-fold convolution of the polynomial `x + x^2 + ... + x^K`
  - Compute using repeated squaring: N convolutions -> O(log N) polynomial multiplications
  - Constraints: `1 <= N <= 10^9`, `1 <= K <= 10^5`, answer modulo 998244353

- [ ] Solve a **subset sum counting problem**:
  - Given a multiset of positive integers, count the number of subsets that sum to each value from 0 to S
  - This is the product of polynomials `(1 + x^{a_i})` for each element `a_i`
  - Use divide-and-conquer polynomial multiplication: multiply adjacent pairs, then multiply the results pairwise, etc.
  - Constraints: up to 10^5 elements, sum up to 10^6, answer modulo 998244353

### Application 4: CRT-based NTT for Arbitrary Moduli

- [ ] Implement **polynomial multiplication modulo an arbitrary prime** (e.g., 10^9 + 7) using three NTTs
  - The modulus 10^9 + 7 is NOT NTT-friendly (10^9 + 6 has no large power-of-2 factor)
  - Strategy: perform the polynomial multiplication modulo three NTT-friendly primes (998244353, 985661441, 754974721), then combine using the Chinese Remainder Theorem

- [ ] Implement the **Chinese Remainder Theorem** for three moduli
  - Given `x ≡ r1 (mod p1)`, `x ≡ r2 (mod p2)`, `x ≡ r3 (mod p3)`, compute `x mod M` where `M` is the target modulus
  - The product `p1 * p2 * p3` exceeds 2^90, so intermediate values exceed `i64`
  - Use `i128` for the CRT computation, or implement a careful step-by-step reconstruction that avoids overflow:
    1. Compute `x mod (p1*p2)` using CRT on two residues (fits in `i128`)
    2. Combine with the third residue to get `x mod (p1*p2*p3)`, then reduce mod M
  - Document the overflow analysis

- [ ] Verify that the CRT-based approach produces the same results as naive O(n^2) multiplication modulo 10^9 + 7

- [ ] Benchmark: multiply two degree-200000 polynomials modulo 10^9 + 7 in < 2 seconds

### Numerical Precision (FFT-specific)

- [ ] Document the **precision limits** of the f64-based FFT
  - For two polynomials of degree n with coefficients up to C, the maximum intermediate value after pointwise multiplication and IFFT is approximately `n * C^2`
  - This must be < 2^53 (the f64 mantissa precision) for correct rounding to the nearest integer
  - For coefficients up to 10^9 and n up to 4 * 10^6, `n * C^2 ≈ 4 * 10^24 > 2^53` — FFT is NOT safe for this case
  - Document the safe ranges and when NTT should be used instead

- [ ] Implement **split coefficient** FFT for handling large coefficients
  - Split each coefficient into high and low 15-bit halves: `a = a_hi * 2^15 + a_lo`
  - Perform 4 polynomial multiplications (or 3 with Karatsuba) on the halves
  - The intermediate values are bounded by `n * (2^15)^2 = n * 2^30`, which is safe for n up to 2^23
  - This extends the safe coefficient range to 10^9 with f64 FFT

- [ ] Implement **rounding with error detection**
  - After IFFT, round each result to the nearest integer
  - Compute the rounding error for each coefficient; if any error exceeds 0.25, emit a warning (precision loss)
  - This serves as a runtime check for whether the FFT result is trustworthy

### Performance Requirements

- [ ] FFT of length 2^20 (1M complex numbers): < 100ms
- [ ] NTT of length 2^20 (1M integers mod 998244353): < 150ms
- [ ] Polynomial multiplication of two degree-500000 polynomials: < 500ms (NTT), < 400ms (FFT with rounding)
- [ ] Large number multiplication of two 10^6-digit numbers: < 2 seconds
- [ ] CRT-based multiplication of two degree-200000 polynomials (three NTTs + CRT): < 2 seconds

- [ ] **Memory usage**:
  - FFT: O(n) for the working buffer (in-place transform)
  - NTT: O(n) for the working buffer
  - Pre-computed twiddle factors: O(n) (or O(log n) if computed on-the-fly)

### Testing

- [ ] **FFT correctness tests**:
  - Transform of a single impulse `[1, 0, 0, ..., 0]` should yield all ones
  - Transform of all ones `[1, 1, 1, ..., 1]` should yield `[n, 0, 0, ..., 0]`
  - Inverse transform of the forward transform should recover the original input (up to floating-point tolerance)
  - Polynomial multiplication of `(1 + x)(1 + x)` should yield `[1, 2, 1]`
  - Polynomial multiplication matches naive convolution for random inputs of degree up to 1000

- [ ] **NTT correctness tests**:
  - Same structural tests as FFT but with exact integer comparison
  - Verify that `NTT(INTT(x)) == x` for random inputs
  - Verify polynomial multiplication against naive O(n^2) for degrees up to 1000
  - Test with coefficients near the modulus boundary (p-1) to catch modular arithmetic bugs

- [ ] **Large number multiplication tests**:
  - `999 * 999 = 998001`
  - `123456789 * 987654321 = 121932631112635269`
  - Two random 100-digit numbers (verify against `num-bigint`)
  - Two random 10^6-digit numbers (verify a subset of digits or the digit sum)

- [ ] **String matching tests**:
  - Exact matching: find "abc" in "xabcyabcz" -> positions [1, 5]
  - Wildcard matching: find "a?c" in "abcadcaec" -> positions [0, 3, 6]
  - No matches: pattern not in text
  - Full overlap: pattern equals text -> position [0]

- [ ] **CRT-based multiplication tests**:
  - Verify against naive multiplication mod 10^9 + 7 for small inputs
  - Stress test: random polynomials of degree 200000 (verify a sampling of coefficients against a known-good implementation)

- [ ] **Performance regression tests**:
  - FFT of 2^20 elements completes in < 100ms on the target machine
  - If any benchmark exceeds 2x the expected time, the test fails

### Code Organization

- [ ] Module structure:
  - `mod fft` — complex-number FFT (forward, inverse, polynomial multiplication)
  - `mod ntt` — number theoretic transform (modular, parameterized by prime)
  - `mod poly` — polynomial type and operations (multiplication, evaluation, etc.)
  - `mod bignum` — large number multiplication
  - `mod string_match` — string matching via convolution
  - `mod crt` — Chinese Remainder Theorem utilities

- [ ] A `Complex` struct with `f64` real and imaginary parts
  - Implement `Add`, `Sub`, `Mul`, `Div` for `Complex`
  - Implement `Display` for debugging
  - Do NOT use the `num-complex` crate (implement it yourself for the learning experience)

- [ ] A `Poly<T>` type alias or struct for polynomials
  - Constructors: from coefficient vector, from roots
  - Operations: evaluation at a point, addition, subtraction
  - Multiplication via FFT/NTT is provided as a standalone function (not a trait impl, since it requires choosing the implementation)

- [ ] Provide a `main()` with subcommands for each application:
  - `./fft poly-mul < input` — polynomial multiplication
  - `./fft bignum-mul < input` — large number multiplication
  - `./fft string-match < input` — string matching
  - `./fft dice-sum < input` — dice sum counting
  - Use competitive-programming style I/O (`BufReader`/`BufWriter`)

## Starting Points

- **cp-algorithms.com FFT article**: The most comprehensive competitive programming reference for FFT. Covers the Cooley-Tukey algorithm, NTT, polynomial multiplication, and the split-coefficient technique. Includes working C++ code that you can reference.
- **e-maxx-eng**: The English translation of the Russian competitive programming reference site. Its FFT article complements cp-algorithms.
- **CLRS (Introduction to Algorithms)**: Chapter 30 on Polynomials and the FFT provides the mathematical foundation. The presentation is rigorous and covers the DFT, FFT, and polynomial multiplication with proofs.
- **competitive-programmer's-handbook** (Laaksonen): Chapter on FFT has a concise, practical treatment aimed at competitive programmers.
- **A Simple Introduction to the NTT** (various blog posts on Codeforces): Search for NTT tutorials that explain the choice of modulus, primitive roots, and the connection to FFT.
- **Three-NTT trick for arbitrary modulus**: Search Codeforces for "NTT with arbitrary mod" or "convolution mod 10^9+7" for blog posts explaining the CRT-based approach.
- **Knuth, The Art of Computer Programming, Vol. 2**: Section 4.3.3 covers large number multiplication and the connection to FFT. Dense but authoritative.
- **Wikipedia: Cooley-Tukey FFT algorithm**: A good overview of the algorithm with pseudocode and illustrations of the butterfly diagram.

## Hints

1. **The bit-reversal permutation is where most beginners get stuck.** For a transform of length n = 2^k, the bit-reversed index of `i` is obtained by reversing the k least significant bits of `i`. A simple way to compute this: maintain a variable `rev` and update it as you iterate. Alternatively, precompute the permutation: `perm[i] = (perm[i >> 1] >> 1) | ((i & 1) << (k - 1))` computes all bit-reversed indices iteratively.

2. **The butterfly operation is the core of both FFT and NTT.** For indices `i` and `j = i + half_len` in a block of size `len`, the butterfly is: `let t = w * a[j]; a[j] = a[i] - t; a[i] = a[i] + t;` where `w` is the appropriate root of unity. Make sure you understand this: it evaluates a degree-1 polynomial at two points (w and -w) and stores the results. The entire FFT is just this operation applied recursively at different scales.

3. **For NTT, the modulus 998244353 = 119 * 2^23 + 1 is not a coincidence.** The maximum NTT length is 2^23 because the multiplicative group mod p has order p-1 = 119 * 2^23, and we need (p-1) to be divisible by the transform length n (which must be a power of 2). The primitive root g = 3 generates this group, so `g^((p-1)/n) mod p` is an n-th root of unity in Z/pZ.

4. **When multiplying two polynomials, the output length is `len(a) + len(b) - 1`.** You must pad both inputs to at least this length (rounded up to the next power of 2 for the FFT/NTT). Forgetting to pad correctly is the most common bug and results in "wrap-around" errors where high-degree terms alias with low-degree terms.

5. **For the FFT precision analysis: `f64` has 53 bits of mantissa, representing integers up to 2^53 exactly.** After pointwise multiplication and IFFT, the maximum absolute value of a result coefficient is bounded by `n * max(|a_i|) * max(|b_j|)`. If this exceeds 2^53, rounding errors will cause incorrect results. For coefficients up to 10^9 and n ~ 10^6, this is about 10^24 >> 2^53. Solution: use the split-coefficient technique or NTT.

6. **The CRT reconstruction for three primes requires careful overflow handling.** The product of three ~30-bit primes is ~90 bits, which exceeds i64 but fits in i128. Use i128 for the final CRT step. The reconstruction formula is: compute `x mod (p1*p2)` first (via two-prime CRT, fits in i128), then compute `x mod (p1*p2*p3)` using the third residue, then reduce mod the target modulus.

7. **For string matching, the convolution-based approach works because convolution is "correlation with reversal."** To check if pattern P matches at position i of text T, you compute `sum over j of f(T[i+j], P[j])` where `f` is a matching function. By reversing P and convolving, this sum is computed simultaneously for all positions i in O(n log n) time.

8. **For the wildcard matching trick, the polynomial is `sum of (T[i+j] - P[j])^2 * mask[j]` where mask[j] = 0 for wildcards.** Expanding this gives three terms: `sum(T^2 * mask)`, `-2 * sum(T * P * mask)`, and `sum(P^2 * mask)`. Each is a convolution (or a constant). A match at position i has total sum 0. This requires only 3 polynomial multiplications.

9. **Competitive programming I/O matters for these problems.** Reading 10^6 integers with `stdin.lines()` and `parse()` is too slow. Use `BufReader::new(stdin())` and read bytes manually, or use a fast scanner function that reads whitespace-separated tokens. Similarly, use `BufWriter::new(stdout())` for output.

10. **Debug by comparing small cases against naive implementations.** For every polynomial multiplication, implement a naive O(n^2) function and compare results for degrees up to 100 with random coefficients. Once the base algorithm is correct, scale up to large inputs. Keep the naive function in your test suite permanently — it is your ground truth.
