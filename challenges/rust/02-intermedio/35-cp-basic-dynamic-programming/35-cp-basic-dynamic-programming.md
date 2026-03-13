# 35. CP: Basic Dynamic Programming

**Difficulty**: Intermedio

## Introduccion

La programacion dinamica (DP) es probablemente la tecnica mas importante en programacion competitiva. Resuelve problemas que tienen dos propiedades clave:

1. **Subestructura optima**: la solucion optima al problema se construye a partir de soluciones optimas de subproblemas.
2. **Subproblemas superpuestos**: los mismos subproblemas se resuelven multiples veces.

### Top-Down vs Bottom-Up

| Enfoque | Implementacion | Pros | Contras |
|---------|---------------|------|---------|
| **Top-Down** (Memoization) | Recursion + cache | Solo calcula lo necesario, mas natural | Overhead de recursion, riesgo de stack overflow |
| **Bottom-Up** (Tabulation) | Loops iterativos | Sin recursion, mas eficiente | Puede calcular estados innecesarios |

### Memoization en Rust

```rust
use std::collections::HashMap;

// Con HashMap (flexible, cualquier tipo de clave)
fn solve(state: (usize, usize), memo: &mut HashMap<(usize, usize), i64>) -> i64 {
    if let Some(&cached) = memo.get(&state) {
        return cached;
    }
    let result = /* calcular */;
    memo.insert(state, result);
    result
}

// Con array (mas rapido, si los estados son indices enteros)
fn solve_arr(i: usize, dp: &mut Vec<Option<i64>>) -> i64 {
    if let Some(cached) = dp[i] {
        return cached;
    }
    let result = /* calcular */;
    dp[i] = Some(result);
    result
}
```

### Metodologia para Definir Estados

1. **Identificar el subproblema**: que decision se toma en cada paso.
2. **Definir el estado**: que informacion necesitas para resolver el subproblema (usualmente, indices, cantidades restantes).
3. **Escribir la recurrencia**: como combinas subproblemas para resolver el problema actual.
4. **Caso base**: cuando puedes responder directamente sin recurrir.
5. **Orden de llenado**: para bottom-up, en que orden llenar la tabla.

---

## Problema 1: Fibonacci (Memoization + Tabulation)

### Enunciado

Dado un entero `n`, calcula el n-esimo numero de Fibonacci modulo 10^9 + 7. Se define F(0) = 0, F(1) = 1, F(n) = F(n-1) + F(n-2).

### Formato de Entrada

Un solo entero `n` (0 <= n <= 10^6).

### Formato de Salida

F(n) mod 10^9 + 7.

### Ejemplo

**Entrada:**
```
10
```

**Salida:**
```
55
```

**Entrada:**
```
1000000
```

**Salida:**
```
918987807
```

### Pistas

- La version recursiva sin memoization es O(2^n): completamente inviable para n grande.
- Con memoization (top-down): O(n) en tiempo y espacio, pero la recursion profunda puede causar stack overflow para n = 10^6.
- Con tabulation (bottom-up): O(n) en tiempo y espacio, sin riesgo de stack overflow.
- Optimizacion de espacio: solo necesitas los dos valores anteriores, no toda la tabla. O(1) en espacio.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

const MOD: u64 = 1_000_000_007;

// Version 1: Bottom-Up con tabla completa
fn fib_table(n: usize) -> u64 {
    if n == 0 { return 0; }
    if n == 1 { return 1; }

    let mut dp = vec![0u64; n + 1];
    dp[0] = 0;
    dp[1] = 1;
    for i in 2..=n {
        dp[i] = (dp[i - 1] + dp[i - 2]) % MOD;
    }
    dp[n]
}

// Version 2: Bottom-Up con espacio O(1)
fn fib_optimized(n: usize) -> u64 {
    if n == 0 { return 0; }
    if n == 1 { return 1; }

    let mut prev2 = 0u64;
    let mut prev1 = 1u64;
    for _ in 2..=n {
        let current = (prev1 + prev2) % MOD;
        prev2 = prev1;
        prev1 = current;
    }
    prev1
}

// Version 3: Top-Down con memoization (cuidado con stack overflow para n grande)
fn fib_memo(n: usize, memo: &mut Vec<Option<u64>>) -> u64 {
    if let Some(val) = memo[n] {
        return val;
    }
    let result = if n == 0 {
        0
    } else if n == 1 {
        1
    } else {
        (fib_memo(n - 1, memo) + fib_memo(n - 2, memo)) % MOD
    };
    memo[n] = Some(result);
    result
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = input.trim().parse().unwrap();

    // Usar la version optimizada en espacio
    writeln!(out, "{}", fib_optimized(n)).unwrap();
}
```

**Version 4: Exponenciacion de matrices O(log n):**

Para n extremadamente grande (10^18), se puede calcular F(n) en O(log n) usando exponenciacion de matrices:

```rust
type Matrix = [[u64; 2]; 2];

fn mat_mul(a: &Matrix, b: &Matrix, m: u64) -> Matrix {
    let mut c = [[0u64; 2]; 2];
    for i in 0..2 {
        for j in 0..2 {
            for k in 0..2 {
                c[i][j] = (c[i][j] + (a[i][k] as u128 * b[k][j] as u128 % m as u128) as u64) % m;
            }
        }
    }
    c
}

fn mat_pow(mut base: Matrix, mut exp: u64, m: u64) -> Matrix {
    let mut result: Matrix = [[1, 0], [0, 1]]; // identidad
    while exp > 0 {
        if exp & 1 == 1 {
            result = mat_mul(&result, &base, m);
        }
        base = mat_mul(&base, &base, m);
        exp >>= 1;
    }
    result
}

fn fib_matrix(n: u64) -> u64 {
    if n == 0 { return 0; }
    let base: Matrix = [[1, 1], [1, 0]];
    let result = mat_pow(base, n, MOD);
    result[0][1]
}
```

**Complejidades comparadas:**

| Metodo | Tiempo | Espacio | Rango de n |
|--------|--------|---------|------------|
| Recursion ingenua | O(2^n) | O(n) | n <= 40 |
| Memoization | O(n) | O(n) | n <= ~50,000 (por stack) |
| Tabulation | O(n) | O(n) | n <= ~10^7 |
| Tabulation optimizada | O(n) | O(1) | n <= ~10^8 |
| Exponenciacion de matrices | O(log n) | O(1) | n <= 10^18 |

</details>

---

## Problema 2: Climbing Stairs

### Enunciado

Hay una escalera con `n` peldanos. Puedes subir 1 o 2 peldanos a la vez. De cuantas formas distintas puedes llegar al peldano `n` desde el peldano 0? Imprime el resultado modulo 10^9 + 7.

**Variante avanzada:** Ahora puedes subir 1, 2, o `k` peldanos a la vez (k dado como entrada).

### Formato de Entrada

Dos enteros `n` y `k` (1 <= n <= 10^6, 1 <= k <= n).

Para la version basica (solo 1 o 2 peldanos), k = 2.

### Formato de Salida

El numero de formas modulo 10^9 + 7.

### Ejemplo

**Entrada:**
```
5 2
```

**Salida:**
```
8
```

**Explicacion (n=5, k=2):** Las formas son: 11111, 1112, 1121, 1211, 2111, 122, 212, 221 = 8 formas.

**Entrada:**
```
5 3
```

**Salida:**
```
13
```

### Pistas

- Para k=2: `dp[i] = dp[i-1] + dp[i-2]` (es Fibonacci desplazado).
- Para k general: `dp[i] = dp[i-1] + dp[i-2] + ... + dp[i-k]`.
- Caso base: `dp[0] = 1` (hay una forma de estar en el peldano 0: no moverse).
- Optimizacion: la suma de los ultimos k elementos se puede mantener con una variable acumuladora (sliding window).

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

const MOD: u64 = 1_000_000_007;

fn climbing_stairs(n: usize, k: usize) -> u64 {
    let mut dp = vec![0u64; n + 1];
    dp[0] = 1;

    // Para cada peldano, sumar las formas de llegar desde los k peldanos anteriores
    for i in 1..=n {
        for step in 1..=k {
            if i >= step {
                dp[i] = (dp[i] + dp[i - step]) % MOD;
            }
        }
    }
    dp[n]
}

// Version optimizada con sliding window: O(n) en vez de O(n*k)
fn climbing_stairs_optimized(n: usize, k: usize) -> u64 {
    let mut dp = vec![0u64; n + 1];
    dp[0] = 1;
    let mut window_sum = 1u64; // suma de dp[max(0, i-k)..i]

    for i in 1..=n {
        dp[i] = window_sum;
        window_sum = (window_sum + dp[i]) % MOD;
        if i >= k {
            // Remover el elemento que sale de la ventana
            window_sum = (window_sum + MOD - dp[i - k]) % MOD;
        }
    }
    dp[n]
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    let n: usize = iter.next().unwrap().parse().unwrap();
    let k: usize = iter.next().unwrap().parse().unwrap();

    writeln!(out, "{}", climbing_stairs_optimized(n, k)).unwrap();
}
```

**Analisis del estado:**

```
Estado: dp[i] = numero de formas de llegar al peldano i
Recurrencia: dp[i] = sum(dp[i-j]) para j = 1..k, si i-j >= 0
Caso base: dp[0] = 1
Orden de llenado: de izquierda a derecha (i creciente)
```

**Complejidad:**
- Version basica: O(n * k)
- Version optimizada: O(n)
- Espacio: O(n) (se puede reducir a O(k) manteniendo solo los ultimos k valores)

</details>

---

## Problema 3: Coin Change

### Enunciado

Dados `n` tipos de monedas con valores dados y un monto objetivo `amount`, encuentra el numero minimo de monedas necesarias para formar exactamente `amount`. Si no es posible, imprime -1.

### Formato de Entrada

- Primera linea: dos enteros `n` y `amount` (1 <= n <= 100, 0 <= amount <= 10^6).
- Segunda linea: `n` enteros positivos con los valores de las monedas.

### Formato de Salida

El numero minimo de monedas, o -1 si es imposible.

### Ejemplo

**Entrada:**
```
3 11
1 5 6
```

**Salida:**
```
2
```

**Explicacion:** 5 + 6 = 11, solo 2 monedas. Nota: el enfoque greedy (tomar la moneda mas grande primero) daria 6 + 5 = 11 (2 monedas tambien en este caso). Pero para `amount = 10` con monedas `[1, 5, 6]`, greedy daria 6+1+1+1+1 = 5 monedas, mientras que DP da 5+5 = 2 monedas.

**Entrada:**
```
2 3
2 5
```

**Salida:**
```
-1
```

### Pistas

- Estado: `dp[i]` = minimo numero de monedas para formar el monto `i`.
- Recurrencia: `dp[i] = min(dp[i - coin] + 1)` para cada moneda cuyo valor <= i.
- Caso base: `dp[0] = 0`.
- Inicializar `dp[i] = infinito` (o `amount + 1`) para todos los demas.
- Si `dp[amount]` sigue siendo infinito, la respuesta es -1.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn coin_change(coins: &[usize], amount: usize) -> i64 {
    let inf = amount + 1;
    let mut dp = vec![inf; amount + 1];
    dp[0] = 0;

    for i in 1..=amount {
        for &coin in coins {
            if coin <= i && dp[i - coin] + 1 < dp[i] {
                dp[i] = dp[i - coin] + 1;
            }
        }
    }

    if dp[amount] == inf { -1 } else { dp[amount] as i64 }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() }
    }

    let n = next!(usize);
    let amount = next!(usize);
    let coins: Vec<usize> = (0..n).map(|_| next!(usize)).collect();

    writeln!(out, "{}", coin_change(&coins, amount)).unwrap();
}
```

**Variante: contar el numero de formas de dar cambio:**

```rust
fn coin_change_count(coins: &[usize], amount: usize) -> u64 {
    let modulus = 1_000_000_007u64;
    let mut dp = vec![0u64; amount + 1];
    dp[0] = 1;

    // Importante: iterar monedas en el bucle externo para evitar contar permutaciones
    for &coin in coins {
        for i in coin..=amount {
            dp[i] = (dp[i] + dp[i - coin]) % modulus;
        }
    }
    dp[amount]
}
```

**Diferencia sutil:** En "numero minimo de monedas", el orden de los bucles no importa. En "contar formas", si el bucle externo es sobre montos y el interno sobre monedas, se cuentan **permutaciones** (1+2 y 2+1 son diferentes). Si el bucle externo es sobre monedas y el interno sobre montos, se cuentan **combinaciones** (1+2 y 2+1 son la misma).

**Reconstruccion de la solucion:**

```rust
fn coin_change_with_reconstruction(coins: &[usize], amount: usize) -> Option<Vec<usize>> {
    let inf = amount + 1;
    let mut dp = vec![inf; amount + 1];
    let mut parent = vec![0usize; amount + 1]; // que moneda se uso
    dp[0] = 0;

    for i in 1..=amount {
        for &coin in coins {
            if coin <= i && dp[i - coin] + 1 < dp[i] {
                dp[i] = dp[i - coin] + 1;
                parent[i] = coin;
            }
        }
    }

    if dp[amount] == inf {
        return None;
    }

    let mut result = Vec::new();
    let mut remaining = amount;
    while remaining > 0 {
        result.push(parent[remaining]);
        remaining -= parent[remaining];
    }
    Some(result)
}
```

**Complejidad:** O(amount * n) en tiempo, O(amount) en espacio.

</details>

---

## Problema 4: Longest Common Subsequence (LCS)

### Enunciado

Dadas dos cadenas `s` y `t`, encuentra la longitud de la subsecuencia comun mas larga. Una subsecuencia se obtiene eliminando cero o mas caracteres de la cadena sin cambiar el orden de los restantes.

Ademas, imprime una LCS (la lexicograficamente menor si hay varias del mismo largo).

### Formato de Entrada

- Primera linea: la cadena `s` (1 <= |s| <= 5000).
- Segunda linea: la cadena `t` (1 <= |t| <= 5000).

### Formato de Salida

- Primera linea: la longitud de la LCS.
- Segunda linea: una LCS.

### Ejemplo

**Entrada:**
```
abcde
ace
```

**Salida:**
```
3
ace
```

**Entrada:**
```
AGGTAB
GXTXAYB
```

**Salida:**
```
4
GTAB
```

### Pistas

- Estado: `dp[i][j]` = longitud de la LCS de `s[0..i]` y `t[0..j]`.
- Recurrencia:
  - Si `s[i-1] == t[j-1]`: `dp[i][j] = dp[i-1][j-1] + 1`
  - Si no: `dp[i][j] = max(dp[i-1][j], dp[i][j-1])`
- Caso base: `dp[0][j] = dp[i][0] = 0`.
- Para reconstruir la LCS, retrocede desde `dp[m][n]` siguiendo las decisiones.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn lcs(s: &[u8], t: &[u8]) -> (usize, String) {
    let m = s.len();
    let n = t.len();

    // Tabla DP
    let mut dp = vec![vec![0usize; n + 1]; m + 1];

    for i in 1..=m {
        for j in 1..=n {
            if s[i - 1] == t[j - 1] {
                dp[i][j] = dp[i - 1][j - 1] + 1;
            } else {
                dp[i][j] = dp[i - 1][j].max(dp[i][j - 1]);
            }
        }
    }

    let length = dp[m][n];

    // Reconstruccion
    let mut result = Vec::new();
    let mut i = m;
    let mut j = n;
    while i > 0 && j > 0 {
        if s[i - 1] == t[j - 1] {
            result.push(s[i - 1]);
            i -= 1;
            j -= 1;
        } else if dp[i - 1][j] > dp[i][j - 1] {
            i -= 1;
        } else {
            j -= 1;
        }
    }
    result.reverse();

    let lcs_string = String::from_utf8(result).unwrap();
    (length, lcs_string)
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut lines = input.lines();
    let s: Vec<u8> = lines.next().unwrap().trim().bytes().collect();
    let t: Vec<u8> = lines.next().unwrap().trim().bytes().collect();

    let (length, subsequence) = lcs(&s, &t);
    writeln!(out, "{}", length).unwrap();
    writeln!(out, "{}", subsequence).unwrap();
}
```

**Optimizacion de espacio (solo longitud, O(min(m,n)) espacio):**

```rust
fn lcs_length_optimized(s: &[u8], t: &[u8]) -> usize {
    // Asegurar que t es el mas corto para minimizar espacio
    let (s, t) = if s.len() < t.len() { (t, s) } else { (s, t) };
    let m = s.len();
    let n = t.len();

    let mut prev = vec![0usize; n + 1];
    let mut curr = vec![0usize; n + 1];

    for i in 1..=m {
        for j in 1..=n {
            if s[i - 1] == t[j - 1] {
                curr[j] = prev[j - 1] + 1;
            } else {
                curr[j] = prev[j].max(curr[j - 1]);
            }
        }
        std::mem::swap(&mut prev, &mut curr);
        curr.fill(0);
    }
    prev[n]
}
```

**Complejidad:**
- Tiempo: O(m * n)
- Espacio: O(m * n) con reconstruccion, O(min(m, n)) solo para longitud
- Para m, n = 5000: 25 millones de operaciones, factible

</details>

---

## Problema 5: 0/1 Knapsack

### Enunciado

Tienes una mochila con capacidad `W` y `n` objetos, cada uno con un peso `w_i` y un valor `v_i`. Selecciona un subconjunto de objetos que maximice el valor total sin exceder la capacidad. Cada objeto puede incluirse como maximo una vez.

### Formato de Entrada

- Primera linea: dos enteros `n` y `W` (1 <= n <= 1000, 1 <= W <= 10^5).
- Siguientes `n` lineas: dos enteros `w_i` y `v_i` (1 <= w_i <= W, 1 <= v_i <= 10^9).

### Formato de Salida

- Primera linea: el valor maximo alcanzable.
- Segunda linea: los indices (1-indexados) de los objetos seleccionados, en orden creciente.

### Ejemplo

**Entrada:**
```
4 7
1 1
3 4
4 5
5 7
```

**Salida:**
```
9
2 3
```

**Explicacion:** Objetos 2 (peso=3, valor=4) y 3 (peso=4, valor=5): peso total = 7, valor total = 9.

### Pistas

- Estado: `dp[i][w]` = maximo valor usando los primeros `i` objetos con capacidad `w`.
- Recurrencia:
  - No tomar objeto i: `dp[i][w] = dp[i-1][w]`
  - Tomar objeto i (si cabe): `dp[i][w] = dp[i-1][w-w_i] + v_i`
  - `dp[i][w] = max(no tomar, tomar)`
- Optimizacion de espacio: usar una sola fila, iterando `w` de derecha a izquierda (para no sobreescribir valores de la fila anterior que aun necesitamos).

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn knapsack(n: usize, capacity: usize, weights: &[usize], values: &[i64]) -> (i64, Vec<usize>) {
    // dp[i][w] = maximo valor con los primeros i objetos y capacidad w
    let mut dp = vec![vec![0i64; capacity + 1]; n + 1];

    for i in 1..=n {
        for w in 0..=capacity {
            // No tomar el objeto i
            dp[i][w] = dp[i - 1][w];
            // Tomar el objeto i (si cabe)
            if weights[i - 1] <= w {
                let take = dp[i - 1][w - weights[i - 1]] + values[i - 1];
                dp[i][w] = dp[i][w].max(take);
            }
        }
    }

    let max_value = dp[n][capacity];

    // Reconstruccion: rastrear que objetos se tomaron
    let mut selected = Vec::new();
    let mut w = capacity;
    for i in (1..=n).rev() {
        if dp[i][w] != dp[i - 1][w] {
            // Se tomo el objeto i
            selected.push(i);
            w -= weights[i - 1];
        }
    }
    selected.reverse();

    (max_value, selected)
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() }
    }

    let n = next!(usize);
    let capacity = next!(usize);

    let mut weights = Vec::with_capacity(n);
    let mut values = Vec::with_capacity(n);
    for _ in 0..n {
        weights.push(next!(usize));
        values.push(next!(i64));
    }

    let (max_value, selected) = knapsack(n, capacity, &weights, &values);

    writeln!(out, "{}", max_value).unwrap();
    let s: Vec<String> = selected.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", s.join(" ")).unwrap();
}
```

**Version con espacio optimizado (solo valor, sin reconstruccion):**

```rust
fn knapsack_optimized(n: usize, capacity: usize, weights: &[usize], values: &[i64]) -> i64 {
    let mut dp = vec![0i64; capacity + 1];

    for i in 0..n {
        // Iterar de derecha a izquierda para no usar el mismo objeto dos veces
        for w in (weights[i]..=capacity).rev() {
            dp[w] = dp[w].max(dp[w - weights[i]] + values[i]);
        }
    }
    dp[capacity]
}
```

**Por que iterar de derecha a izquierda:** Si iteramos de izquierda a derecha, `dp[w - weights[i]]` ya habria sido actualizado en esta iteracion, lo que significa que estamos considerando tomar el objeto `i` multiples veces (eso seria el **unbounded knapsack**). Al iterar de derecha a izquierda, `dp[w - weights[i]]` aun tiene el valor de la iteracion anterior, garantizando que cada objeto se tome como maximo una vez.

**Complejidad:**
- Tiempo: O(n * W)
- Espacio: O(n * W) con reconstruccion, O(W) sin reconstruccion
- Para n=1000, W=10^5: 10^8 operaciones, factible pero ajustado

</details>

---

## Conceptos Clave

### Clasificacion de Problemas DP Comunes

| Patron | Ejemplo | Estado tipico |
|--------|---------|---------------|
| **Lineal** | Fibonacci, Climbing Stairs | dp[i] |
| **Grid/Caminos** | Caminos unicos en grilla | dp[i][j] |
| **Substring/Subsequence** | LCS, Edit Distance | dp[i][j] |
| **Knapsack** | 0/1 Knapsack, Subset Sum | dp[i][w] |
| **Particion** | Partition Equal Subset Sum | dp[target] |
| **Interval** | Matrix Chain Multiplication | dp[l][r] |
| **Bitmask** | TSP, Hamiltonian Path | dp[mask][i] |
| **Digit** | Contar numeros con propiedad | dp[pos][tight][state] |

### Errores Comunes en DP

1. **Estado insuficiente**: olvidar una dimension necesaria (ejemplo: en knapsack, olvidar el peso restante).
2. **Orden de llenado incorrecto**: en bottom-up, llenar en orden que viola dependencias.
3. **Caso base incorrecto**: `dp[0]` no siempre es 0 o 1.
4. **Overflow**: multiplicaciones o sumas que exceden i64/u64. Usar modulo cuando se pide.
5. **Espacio excesivo**: dp[n][m] con n, m = 10^5 requiere 10^10 celdas. Optimizar a 1D si es posible.

### Optimizacion de Espacio: Regla General

Si `dp[i]` solo depende de `dp[i-1]` (y no de `dp[i-2]`, `dp[i-3]`, etc.), puedes reducir una dimension usando dos filas alternadas o una sola fila actualizada cuidadosamente.

```rust
// Dos filas alternadas (seguro y claro)
let mut prev = vec![0i64; m + 1];
let mut curr = vec![0i64; m + 1];

for i in 1..=n {
    for j in 1..=m {
        curr[j] = /* usar prev[...] */;
    }
    std::mem::swap(&mut prev, &mut curr);
    curr.fill(0);
}
// Respuesta en prev[m]

// Una sola fila (mas eficiente, requiere cuidado con el orden)
let mut dp = vec![0i64; m + 1];
for i in 1..=n {
    // Si la recurrencia usa dp[j-1] del paso actual: iterar izquierda a derecha
    // Si la recurrencia usa dp[j-1] del paso anterior: iterar derecha a izquierda
    for j in (1..=m).rev() {
        dp[j] = /* usar dp[...] */;
    }
}
```

### Template Completo para CP con DP

```rust
use std::io::{self, Read, Write, BufWriter};
use std::collections::HashMap;

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() }
    }

    let n = next!(usize);

    // Ejemplo: DP con tabla
    let mut dp = vec![0i64; n + 1];
    dp[0] = 1; // caso base

    for i in 1..=n {
        // recurrencia
        dp[i] = dp[i - 1]; // + ...
    }

    writeln!(out, "{}", dp[n]).unwrap();
}
```

---

## Ejercicios Adicionales

1. **Edit Distance**: dadas dos cadenas, encuentra el minimo numero de operaciones (insertar, eliminar, reemplazar) para transformar una en la otra.
2. **Longest Increasing Subsequence (LIS)**: encuentra la longitud de la subsecuencia creciente mas larga en un arreglo. Implementa tanto la version O(n^2) como la O(n log n) con busqueda binaria.
3. **Partition Equal Subset Sum**: dado un arreglo de enteros positivos, determina si se puede particionar en dos subconjuntos con igual suma.
4. **Minimum Path Sum**: en una grilla m x n con costos, encuentra el camino de esquina superior izquierda a esquina inferior derecha con suma minima (solo movimientos derecha y abajo).
5. **Unbounded Knapsack**: igual que el 0/1 knapsack pero cada objeto se puede usar multiples veces. Nota la diferencia clave en el orden de iteracion.
