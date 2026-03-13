# 34. CP: Basic Number Theory

**Difficulty**: Intermedio

## Introduccion

La teoria de numeros es un pilar de la programacion competitiva. Problemas de primalidad, factorizacion, aritmetica modular y GCD aparecen constantemente, tanto como problemas independientes como subrutinas dentro de problemas mas complejos.

### Overflow en Rust: Tu Mejor Amigo y Tu Peor Enemigo

Rust detecta overflow en modo debug (panic) pero lo permite en modo release (wrapping). Para CP, esto es critico:

```rust
// Esto hace panic en debug, wrapping en release
let a: u64 = u64::MAX;
let b = a + 1; // panic en debug!

// Operaciones seguras
let c = a.checked_mul(2);        // -> Option<u64>, None si overflow
let d = a.saturating_add(1);     // -> u64::MAX (no pasa del maximo)
let e = a.wrapping_add(1);       // -> 0 (wrapping explicito)
let f = a.overflowing_add(1);    // -> (0, true) (valor + flag de overflow)

// Para multiplicacion modular con numeros grandes, usa u128
let big_product = (a as u128) * (b as u128) % (m as u128);
let result = big_product as u64;
```

### Tipos Numericos Utiles

| Tipo | Rango maximo | Uso tipico |
|------|-------------|------------|
| `u32` | ~4.3 * 10^9 | Indices, valores moderados |
| `u64` | ~1.8 * 10^19 | La mayoria de problemas CP |
| `u128` | ~3.4 * 10^38 | Producto intermedio para evitar overflow |
| `i64` | +/- 9.2 * 10^18 | Cuando se necesitan negativos |

---

## Problema 1: Sieve of Eratosthenes

### Enunciado

Dado un entero `n`, encuentra todos los numeros primos menores o iguales a `n` usando la Criba de Eratosthenes. Imprime la cantidad de primos y luego los primos en orden creciente.

### Formato de Entrada

Un solo entero `n` (2 <= n <= 10^7).

### Formato de Salida

- Primera linea: la cantidad de primos <= n.
- Segunda linea: los primos separados por espacios (solo si n <= 1000, para evitar salida excesiva).

### Ejemplo

**Entrada:**
```
30
```

**Salida:**
```
10
2 3 5 7 11 13 17 19 23 29
```

**Entrada:**
```
100
```

**Salida:**
```
25
2 3 5 7 11 13 17 19 23 29 31 37 41 43 47 53 59 61 67 71 73 79 83 89 97
```

### Pistas

- Criba clasica: crea un arreglo booleano de tamano n+1, marca todos como primos inicialmente, luego para cada primo p, marca sus multiplos como compuestos.
- Optimizacion clave: empieza a marcar desde p*p (los multiplos menores ya fueron marcados por primos anteriores).
- Solo necesitas iterar hasta sqrt(n).
- Para n = 10^7, el arreglo usa ~10 MB (como `Vec<bool>`, ~1 byte por elemento). Con bitset seria ~1.25 MB.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn sieve(n: usize) -> Vec<bool> {
    let mut is_prime = vec![true; n + 1];
    is_prime[0] = false;
    if n >= 1 { is_prime[1] = false; }

    let mut p = 2;
    while p * p <= n {
        if is_prime[p] {
            // Marcar multiplos de p desde p*p
            let mut multiple = p * p;
            while multiple <= n {
                is_prime[multiple] = false;
                multiple += p;
            }
        }
        p += 1;
    }
    is_prime
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = input.trim().parse().unwrap();

    let is_prime = sieve(n);
    let primes: Vec<usize> = (2..=n).filter(|&i| is_prime[i]).collect();

    writeln!(out, "{}", primes.len()).unwrap();

    if n <= 1000 {
        let s: Vec<String> = primes.iter().map(|x| x.to_string()).collect();
        writeln!(out, "{}", s.join(" ")).unwrap();
    }
}
```

**Optimizacion: criba solo para numeros impares:**

```rust
fn sieve_odd(n: usize) -> Vec<usize> {
    if n < 2 { return vec![]; }
    let mut primes = vec![2];

    // Solo almacenamos numeros impares: is_prime[i] corresponde a 2*i+1
    let size = (n - 1) / 2 + 1;
    let mut is_prime = vec![true; size];

    let mut i = 1; // corresponde a 3
    while 2 * i + 1 <= n {
        if is_prime[i] {
            let p = 2 * i + 1;
            primes.push(p);
            // Marcar multiplos impares de p desde p*p
            let mut j = (p * p - 1) / 2;
            while j < size {
                is_prime[j] = false;
                j += p;
            }
        }
        i += 1;
    }
    primes
}
```

**Complejidad:** O(n log log n) en tiempo, O(n) en espacio.

**Nota sobre pi(n):** La cantidad de primos hasta n se aproxima por n / ln(n). Para n = 10^7, hay 664,579 primos.

</details>

---

## Problema 2: GCD/LCM (Euclid's Algorithm)

### Enunciado

Dados `q` consultas, cada una con dos numeros `a` y `b`, calcula su GCD (maximo comun divisor) y su LCM (minimo comun multiplo).

### Formato de Entrada

- Primera linea: un entero `q` (1 <= q <= 10^5).
- Siguientes `q` lineas: dos enteros `a` y `b` (1 <= a, b <= 10^18).

### Formato de Salida

Para cada consulta, una linea con dos enteros: GCD y LCM.

### Ejemplo

**Entrada:**
```
3
12 18
7 13
100 75
```

**Salida:**
```
6 36
1 91
25 300
```

### Pistas

- Algoritmo de Euclides: `gcd(a, b) = gcd(b, a % b)`, caso base `gcd(a, 0) = a`.
- LCM: `lcm(a, b) = a / gcd(a, b) * b`. Divide primero para evitar overflow.
- Cuidado con overflow: `a * b` puede exceder u64 para a, b ~ 10^18. Dividir primero resuelve el problema.
- El GCD de Euclides tiene complejidad O(log(min(a,b))).

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn gcd(a: u64, b: u64) -> u64 {
    if b == 0 { a } else { gcd(b, a % b) }
}

// Version iterativa (evita stack overflow para entradas patologicas)
fn gcd_iter(mut a: u64, mut b: u64) -> u64 {
    while b != 0 {
        let temp = b;
        b = a % b;
        a = temp;
    }
    a
}

fn lcm(a: u64, b: u64) -> u64 {
    // a / gcd(a,b) * b  (dividir primero para evitar overflow)
    a / gcd_iter(a, b) * b
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

    let q = next!(usize);
    for _ in 0..q {
        let a = next!(u64);
        let b = next!(u64);
        let g = gcd_iter(a, b);
        let l = a / g * b;
        writeln!(out, "{} {}", g, l).unwrap();
    }
}
```

**GCD extendido (Extended Euclidean Algorithm):**

El algoritmo extendido encuentra `x, y` tales que `a*x + b*y = gcd(a, b)`. Es util para encontrar inversos modulares.

```rust
fn extended_gcd(a: i64, b: i64) -> (i64, i64, i64) {
    if b == 0 {
        return (a, 1, 0);
    }
    let (g, x1, y1) = extended_gcd(b, a % b);
    (g, y1, x1 - (a / b) * y1)
}

// Inverso modular de a mod m (solo existe si gcd(a, m) == 1)
fn mod_inverse(a: i64, m: i64) -> Option<i64> {
    let (g, x, _) = extended_gcd(a, m);
    if g != 1 {
        None
    } else {
        Some(((x % m) + m) % m)
    }
}
```

**Complejidad:** O(log(min(a, b))) por consulta.

**Dato curioso:** El peor caso del algoritmo de Euclides ocurre con numeros de Fibonacci consecutivos. Para `gcd(F_n, F_{n-1})`, se necesitan exactamente n-2 divisiones.

</details>

---

## Problema 3: Modular Exponentiation (Binary Exponentiation)

### Enunciado

Dados `q` consultas, cada una con tres enteros `base`, `exp` y `mod`, calcula `base^exp mod mod` eficientemente.

### Formato de Entrada

- Primera linea: un entero `q` (1 <= q <= 10^5).
- Siguientes `q` lineas: tres enteros `base`, `exp`, `mod` (1 <= base, mod <= 10^9, 0 <= exp <= 10^18).

### Formato de Salida

Para cada consulta, una linea con el resultado.

### Ejemplo

**Entrada:**
```
3
2 10 1000
3 1000000000000000000 1000000007
7 0 13
```

**Salida:**
```
24
72972999
1
```

### Pistas

- La exponenciacion ingenua O(exp) es imposible para exp = 10^18.
- Binary exponentiation: descompone el exponente en binario y cuadra repetidamente. O(log exp).
- Cuidado con overflow: `base * base` puede exceder u64. Usa u128 para el producto intermedio.
- Caso especial: cualquier numero elevado a la 0 es 1 (incluyendo 0^0 que definimos como 1 en CP).

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn power_mod(mut base: u64, mut exp: u64, modulus: u64) -> u64 {
    if modulus == 1 { return 0; }

    let mut result: u64 = 1;
    base %= modulus;

    while exp > 0 {
        if exp & 1 == 1 {
            // result = result * base % modulus (usando u128 para evitar overflow)
            result = ((result as u128) * (base as u128) % (modulus as u128)) as u64;
        }
        exp >>= 1;
        // base = base * base % modulus
        base = ((base as u128) * (base as u128) % (modulus as u128)) as u64;
    }
    result
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

    let q = next!(usize);
    for _ in 0..q {
        let base = next!(u64);
        let exp = next!(u64);
        let modulus = next!(u64);
        writeln!(out, "{}", power_mod(base, exp, modulus)).unwrap();
    }
}
```

**Como funciona la exponenciacion binaria:**

```
base^13 = base^(1101 en binario)
        = base^8 * base^4 * base^1

Paso a paso:
  exp = 13 (1101)  -> bit 1: result *= base     -> base = base^2
  exp = 6  (110)   -> bit 0: nada                -> base = base^4
  exp = 3  (11)    -> bit 1: result *= base^4    -> base = base^8
  exp = 1  (1)     -> bit 1: result *= base^8    -> done

result = base^1 * base^4 * base^8 = base^13
```

**Aplicacion: inverso modular con Fermat:**

Si `m` es primo, el inverso modular de `a` es `a^(m-2) mod m` (por el pequeno teorema de Fermat).

```rust
fn mod_inverse_fermat(a: u64, p: u64) -> u64 {
    // Solo funciona si p es primo
    power_mod(a, p - 2, p)
}
```

**Complejidad:** O(log exp) por consulta.

</details>

---

## Problema 4: Check Prime and Prime Factorization

### Enunciado

Dados `q` numeros, para cada uno determina si es primo. Si no es primo, imprime su factorizacion en primos (en formato `p1^e1 * p2^e2 * ...`). Si es primo, imprime "PRIME".

### Formato de Entrada

- Primera linea: un entero `q` (1 <= q <= 10^4).
- Siguientes `q` lineas: un entero `n` (2 <= n <= 10^12).

### Formato de Salida

Para cada numero, una linea con "PRIME" o su factorizacion.

### Ejemplo

**Entrada:**
```
5
7
12
100
997
1000000007
```

**Salida:**
```
PRIME
2^2 * 3^1
2^2 * 5^2
PRIME
PRIME
```

### Pistas

- Test de primalidad: probar divisores hasta sqrt(n). Para n <= 10^12, sqrt(n) <= 10^6, lo cual es rapido.
- Factorizacion: dividir repetidamente por cada primo hasta sqrt(n). Si queda un residuo > 1, ese es un factor primo.
- Optimizacion: solo probar 2, 3, y luego numeros de la forma 6k+/-1.
- `checked_mul` es util si trabajas cerca del limite de u64.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn is_prime(n: u64) -> bool {
    if n < 2 { return false; }
    if n < 4 { return true; }
    if n % 2 == 0 || n % 3 == 0 { return false; }

    let mut i = 5u64;
    while i * i <= n {
        if n % i == 0 || n % (i + 2) == 0 {
            return false;
        }
        i += 6;
    }
    true
}

fn factorize(mut n: u64) -> Vec<(u64, u32)> {
    let mut factors = Vec::new();

    // Factor 2
    if n % 2 == 0 {
        let mut count = 0u32;
        while n % 2 == 0 {
            count += 1;
            n /= 2;
        }
        factors.push((2, count));
    }

    // Factor 3
    if n % 3 == 0 {
        let mut count = 0u32;
        while n % 3 == 0 {
            count += 1;
            n /= 3;
        }
        factors.push((3, count));
    }

    // Factores de la forma 6k +/- 1
    let mut i = 5u64;
    while i * i <= n {
        for &p in &[i, i + 2] {
            if n % p == 0 {
                let mut count = 0u32;
                while n % p == 0 {
                    count += 1;
                    n /= p;
                }
                factors.push((p, count));
            }
        }
        i += 6;
    }

    // Si queda un factor primo > sqrt(n_original)
    if n > 1 {
        factors.push((n, 1));
    }

    factors
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

    let q = next!(usize);
    for _ in 0..q {
        let n = next!(u64);

        if is_prime(n) {
            writeln!(out, "PRIME").unwrap();
        } else {
            let factors = factorize(n);
            let parts: Vec<String> = factors.iter()
                .map(|&(p, e)| format!("{}^{}", p, e))
                .collect();
            writeln!(out, "{}", parts.join(" * ")).unwrap();
        }
    }
}
```

**Test de primalidad de Miller-Rabin (para numeros muy grandes):**

Para n > 10^18, el test por division es demasiado lento. Miller-Rabin es probabilistico pero con testigos deterministas cubre todo u64:

```rust
fn miller_rabin(n: u64) -> bool {
    if n < 2 { return false; }
    if n < 4 { return true; }
    if n % 2 == 0 { return false; }

    // Escribir n-1 = d * 2^r
    let mut d = n - 1;
    let mut r = 0;
    while d % 2 == 0 {
        d /= 2;
        r += 1;
    }

    // Testigos suficientes para cubrir todo u64
    let witnesses = [2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37];

    'outer: for &a in &witnesses {
        if a >= n { continue; }

        let mut x = power_mod(a, d, n);
        if x == 1 || x == n - 1 { continue; }

        for _ in 0..r - 1 {
            x = ((x as u128) * (x as u128) % (n as u128)) as u64;
            if x == n - 1 { continue 'outer; }
        }
        return false;
    }
    true
}

fn power_mod(mut base: u64, mut exp: u64, modulus: u64) -> u64 {
    if modulus == 1 { return 0; }
    let mut result = 1u64;
    base %= modulus;
    while exp > 0 {
        if exp & 1 == 1 {
            result = ((result as u128) * (base as u128) % (modulus as u128)) as u64;
        }
        exp >>= 1;
        base = ((base as u128) * (base as u128) % (modulus as u128)) as u64;
    }
    result
}
```

**Complejidad:**
- is_prime por division: O(sqrt(n))
- factorize: O(sqrt(n))
- Miller-Rabin: O(k * log^2(n)) donde k es el numero de testigos

</details>

---

## Problema 5: Prime Factorization with Smallest Prime Factor Sieve

### Enunciado

Dado un rango de `q` consultas, cada una con un numero `n`, imprime la factorizacion en primos de `n`. Esta vez, se requiere que sea eficiente para muchas consultas con `n` moderado.

Precalcula el **SPF (Smallest Prime Factor)** para todos los numeros hasta un limite `L` usando una criba modificada. Luego, factoriza cada numero en O(log n) usando la tabla SPF.

### Formato de Entrada

- Primera linea: dos enteros `L` y `q` (2 <= L <= 10^7, 1 <= q <= 10^6).
- Siguientes `q` lineas: un entero `n` (2 <= n <= L).

### Formato de Salida

Para cada consulta, la factorizacion en formato `p1 p2 p3 ...` (factores primos en orden creciente, repetidos segun multiplicidad).

### Ejemplo

**Entrada:**
```
100 4
12
60
97
100
```

**Salida:**
```
2 2 3
2 2 3 5
97
2 2 5 5
```

### Pistas

- Modifica la criba de Eratosthenes para guardar el menor factor primo de cada numero en vez de un booleano.
- Para factorizar `n`: repetidamente divide por `spf[n]` hasta que `n` sea 1.
- Esto da factorizacion en O(log n) por consulta, ideal para q = 10^6.
- La criba SPF tiene la misma complejidad que la criba clasica: O(L log log L).

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn build_spf(limit: usize) -> Vec<usize> {
    let mut spf = vec![0usize; limit + 1];

    // Cada numero es su propio SPF inicialmente
    for i in 2..=limit {
        spf[i] = i;
    }

    // Criba: para cada primo p, actualizar sus multiplos
    let mut p = 2;
    while p * p <= limit {
        if spf[p] == p {
            // p es primo
            let mut multiple = p * p;
            while multiple <= limit {
                if spf[multiple] == multiple {
                    spf[multiple] = p;
                }
                multiple += p;
            }
        }
        p += 1;
    }
    spf
}

fn factorize_with_spf(mut n: usize, spf: &[usize]) -> Vec<usize> {
    let mut factors = Vec::new();
    while n > 1 {
        factors.push(spf[n]);
        n /= spf[n];
    }
    factors
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

    let limit = next!(usize);
    let q = next!(usize);

    let spf = build_spf(limit);

    for _ in 0..q {
        let n = next!(usize);
        let factors = factorize_with_spf(n, &spf);
        let s: Vec<String> = factors.iter().map(|x| x.to_string()).collect();
        writeln!(out, "{}", s.join(" ")).unwrap();
    }
}
```

**Complejidad:**
- Preprocesamiento (criba SPF): O(L log log L)
- Factorizacion por consulta: O(log n)
- Total: O(L log log L + q * log L)

**Comparacion de enfoques:**

| Metodo | Preprocesamiento | Por consulta | Rango de n | Mejor para |
|--------|-----------------|-------------|------------|------------|
| Division por trial | Ninguno | O(sqrt(n)) | Hasta 10^12 | Pocas consultas, n grande |
| Criba SPF | O(L log log L) | O(log n) | Hasta ~10^7 | Muchas consultas, n moderado |
| Pollard-Rho | Ninguno | O(n^1/4) | Hasta 10^18 | n muy grande |

</details>

---

## Conceptos Clave

### Funciones Aritmeticas Utiles

```rust
// Funcion totiente de Euler (cantidad de coprimos menores que n)
fn euler_totient(mut n: u64) -> u64 {
    let mut result = n;
    let mut p = 2u64;
    while p * p <= n {
        if n % p == 0 {
            while n % p == 0 {
                n /= p;
            }
            result -= result / p;
        }
        p += 1;
    }
    if n > 1 {
        result -= result / n;
    }
    result
}

// Cantidad de divisores
fn count_divisors(mut n: u64) -> u64 {
    let mut count = 1u64;
    let mut p = 2u64;
    while p * p <= n {
        if n % p == 0 {
            let mut exp = 0u64;
            while n % p == 0 {
                exp += 1;
                n /= p;
            }
            count *= exp + 1;
        }
        p += 1;
    }
    if n > 1 { count *= 2; }
    count
}

// Suma de divisores
fn sum_divisors(mut n: u64) -> u64 {
    let mut result = 1u64;
    let mut p = 2u64;
    while p * p <= n {
        if n % p == 0 {
            let mut power_sum = 1u64;
            let mut power = 1u64;
            while n % p == 0 {
                power *= p;
                power_sum += power;
                n /= p;
            }
            result *= power_sum;
        }
        p += 1;
    }
    if n > 1 { result *= 1 + n; }
    result
}
```

### Constantes Utiles para CP

```rust
const MOD: u64 = 1_000_000_007; // 10^9 + 7 (primo, el mas comun en CP)
const MOD2: u64 = 998_244_353;   // otro primo comun (para NTT)
```

### Overflow Prevention Checklist

1. Multiplicacion de dos u64: usa u128 intermedio.
2. LCM: divide por GCD antes de multiplicar.
3. Suma de muchos numeros: usa i64/u64, no i32.
4. Factoriales: precomputa modulo M si solo necesitas `n! mod M`.
5. Coeficientes binomiales: usa el triangulo de Pascal o inversos modulares.

---

## Ejercicios Adicionales

1. Implementa la funcion totiente de Euler usando criba (calcular phi(i) para todo i <= n).
2. Calcula `n! mod p` eficientemente para p primo y n <= 10^6.
3. Implementa el algoritmo de Pollard-Rho para factorizacion de numeros grandes (hasta 10^18).
4. Dados dos numeros, encuentra su GCD usando el algoritmo binario (GCD de Stein), que evita divisiones y usa solo shifts y restas.
5. Calcula el coeficiente binomial C(n, k) mod 10^9+7 usando el teorema de Lucas para n, k grandes.
