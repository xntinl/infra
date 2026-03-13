# 32. CP: Prefix Sums and Difference Arrays

**Difficulty**: Intermedio

## Introduccion

Los prefix sums (sumas de prefijos) y los difference arrays (arreglos de diferencias) son tecnicas fundamentales que transforman operaciones costosas en operaciones O(1). Son la base de innumerables problemas en competencias de programacion.

### Prefix Sum: La Idea Central

Dado un arreglo `a[0..n]`, el prefix sum `p[i] = a[0] + a[1] + ... + a[i-1]` permite calcular la suma de cualquier subrango `a[l..r]` en O(1):

```
sum(l, r) = p[r+1] - p[l]
```

### Difference Array: La Operacion Inversa

Un difference array permite aplicar multiples actualizaciones de rango en O(1) cada una. Para sumar `val` al rango `[l, r]`:

```
diff[l] += val
diff[r+1] -= val
```

Luego, el prefix sum de `diff` reconstruye el arreglo original modificado.

### Patrones de Rust Relevantes

```rust
// Construir prefix sum con scan
let prefix: Vec<i64> = std::iter::once(0)
    .chain(arr.iter().scan(0i64, |acc, &x| {
        *acc += x;
        Some(*acc)
    }))
    .collect();

// Usando windows para pares consecutivos
let diffs: Vec<i64> = arr.windows(2).map(|w| w[1] - w[0]).collect();

// Usando chunks para procesar en bloques
let block_sums: Vec<i64> = arr.chunks(k).map(|chunk| chunk.iter().sum()).collect();
```

---

## Problema 1: Range Sum Query (Immutable)

### Enunciado

Dado un arreglo de `n` enteros y `q` consultas, cada consulta pide la suma de los elementos en el rango `[l, r]` (0-indexado, ambos inclusive).

### Formato de Entrada

- Primera linea: dos enteros `n` y `q` (1 <= n <= 10^5, 1 <= q <= 10^5).
- Segunda linea: `n` enteros separados por espacios.
- Siguientes `q` lineas: dos enteros `l` y `r` (0 <= l <= r < n).

### Formato de Salida

Para cada consulta, una linea con la suma del rango.

### Ejemplo

**Entrada:**
```
5 3
1 3 5 7 9
0 2
1 3
0 4
```

**Salida:**
```
9
15
25
```

### Pistas

- Precalcula el arreglo de prefix sums en O(n).
- Cada consulta se responde en O(1) con `prefix[r+1] - prefix[l]`.
- Usa `i64` para evitar overflow si los valores pueden ser grandes.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

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
    let q = next!(usize);

    let arr: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    // Construir prefix sum
    // prefix[0] = 0, prefix[i] = arr[0] + arr[1] + ... + arr[i-1]
    let mut prefix = vec![0i64; n + 1];
    for i in 0..n {
        prefix[i + 1] = prefix[i] + arr[i];
    }

    // Alternativa funcional con scan:
    // let prefix: Vec<i64> = std::iter::once(0)
    //     .chain(arr.iter().scan(0i64, |acc, &x| { *acc += x; Some(*acc) }))
    //     .collect();

    for _ in 0..q {
        let l = next!(usize);
        let r = next!(usize);
        writeln!(out, "{}", prefix[r + 1] - prefix[l]).unwrap();
    }
}
```

**Complejidad:** O(n) preprocesamiento, O(1) por consulta, O(n + q) total.

**Nota sobre el patron `scan`:** El metodo `scan` de los iteradores de Rust es perfecto para prefix sums. Mantiene un acumulador y produce un nuevo valor en cada paso, exactamente lo que necesitamos.

</details>

---

## Problema 2: Count Subarrays with Given Sum

### Enunciado

Dado un arreglo de `n` enteros (pueden ser negativos) y un entero `k`, cuenta el numero de subarreglos contiguos cuya suma es exactamente `k`.

### Formato de Entrada

- Primera linea: dos enteros `n` y `k` (1 <= n <= 10^5, -10^9 <= k <= 10^9).
- Segunda linea: `n` enteros separados por espacios (-10^4 <= a_i <= 10^4).

### Formato de Salida

Un solo entero: la cantidad de subarreglos con suma `k`.

### Ejemplo

**Entrada:**
```
5 5
1 2 3 4 5
```

**Salida:**
```
2
```

**Explicacion:** Los subarreglos `[2, 3]` y `[5]` suman 5.

**Entrada:**
```
4 0
1 -1 1 -1
```

**Salida:**
```
4
```

**Explicacion:** Los subarreglos `[1,-1]` (pos 0-1), `[-1,1]` (pos 1-2), `[1,-1]` (pos 2-3), `[1,-1,1,-1]` (pos 0-3).

### Pistas

- Usa prefix sums combinado con un HashMap.
- Un subarreglo `a[l..=r]` tiene suma `k` si `prefix[r+1] - prefix[l] = k`, es decir, `prefix[l] = prefix[r+1] - k`.
- Mientras recorres construyendo el prefix sum, busca cuantas veces has visto `prefix_actual - k` previamente.
- Inicializa el HashMap con `{0: 1}` (el prefix sum vacio).

<details>
<summary>Solucion</summary>

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
    let k = next!(i64);

    let mut count: i64 = 0;
    let mut prefix_sum: i64 = 0;
    let mut freq: HashMap<i64, i64> = HashMap::new();
    freq.insert(0, 1); // prefix sum de 0 aparece una vez (antes del primer elemento)

    for _ in 0..n {
        let val = next!(i64);
        prefix_sum += val;

        // Buscamos cuantas veces hemos visto prefix_sum - k
        if let Some(&f) = freq.get(&(prefix_sum - k)) {
            count += f;
        }

        *freq.entry(prefix_sum).or_insert(0) += 1;
    }

    writeln!(out, "{}", count).unwrap();
}
```

**Complejidad:** O(n) en tiempo y espacio.

**Por que funciona:** Si `prefix[j] - prefix[i] = k`, entonces el subarreglo `a[i..j]` suma `k`. Al ir calculando prefix sums de izquierda a derecha y registrando cuantas veces aparece cada valor, podemos contar en O(1) cuantos subarreglos terminando en la posicion actual tienen la suma deseada.

**Patron clave:** Este es el clasico "prefix sum + HashMap" que aparece en muchas variantes: subarreglos divisibles por k, subarreglos con igual cantidad de 0s y 1s, etc.

</details>

---

## Problema 3: 2D Prefix Sums

### Enunciado

Dada una matriz de `m x n` enteros y `q` consultas, cada consulta pide la suma de los elementos en un subrectangulo definido por su esquina superior izquierda `(r1, c1)` y su esquina inferior derecha `(r2, c2)` (0-indexado).

### Formato de Entrada

- Primera linea: tres enteros `m`, `n`, `q` (1 <= m, n <= 500, 1 <= q <= 10^5).
- Siguientes `m` lineas: `n` enteros separados por espacios.
- Siguientes `q` lineas: cuatro enteros `r1 c1 r2 c2`.

### Formato de Salida

Para cada consulta, la suma del subrectangulo en una linea.

### Ejemplo

**Entrada:**
```
3 3 3
1 2 3
4 5 6
7 8 9
0 0 1 1
1 1 2 2
0 0 2 2
```

**Salida:**
```
12
28
45
```

**Explicacion:**
- `(0,0)-(1,1)`: 1+2+4+5 = 12
- `(1,1)-(2,2)`: 5+6+8+9 = 28
- `(0,0)-(2,2)`: toda la matriz = 45

### Pistas

- Extiende la idea de prefix sums a 2D.
- `prefix[r][c]` = suma de todos los elementos en el rectangulo `(0,0)-(r-1,c-1)`.
- Construccion: `prefix[r][c] = matrix[r-1][c-1] + prefix[r-1][c] + prefix[r][c-1] - prefix[r-1][c-1]`.
- Consulta: `sum = prefix[r2+1][c2+1] - prefix[r1][c2+1] - prefix[r2+1][c1] + prefix[r1][c1]`.
- Es el principio de inclusion-exclusion en 2D.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() }
    }

    let m = next!(usize);
    let n = next!(usize);
    let q = next!(usize);

    let mut matrix = vec![vec![0i64; n]; m];
    for r in 0..m {
        for c in 0..n {
            matrix[r][c] = next!(i64);
        }
    }

    // Construir prefix sum 2D
    // prefix[r][c] = suma del rectangulo (0,0) a (r-1, c-1)
    let mut prefix = vec![vec![0i64; n + 1]; m + 1];
    for r in 1..=m {
        for c in 1..=n {
            prefix[r][c] = matrix[r - 1][c - 1]
                + prefix[r - 1][c]
                + prefix[r][c - 1]
                - prefix[r - 1][c - 1];
        }
    }

    // Responder consultas
    for _ in 0..q {
        let r1 = next!(usize);
        let c1 = next!(usize);
        let r2 = next!(usize);
        let c2 = next!(usize);

        // Inclusion-exclusion
        let sum = prefix[r2 + 1][c2 + 1]
            - prefix[r1][c2 + 1]
            - prefix[r2 + 1][c1]
            + prefix[r1][c1];

        writeln!(out, "{}", sum).unwrap();
    }
}
```

**Complejidad:** O(m*n) preprocesamiento, O(1) por consulta.

**Visualizacion de inclusion-exclusion:**

```
+-------+-------+
|   A   |   B   |
|       |       |
+-------+-------+ (r1, c2+1)
|   C   | query |
|       |       |
+-------+-------+ (r2+1, c2+1)
(r2+1, c1)

sum(query) = total - A_y_B - A_y_C + A
           = prefix[r2+1][c2+1] - prefix[r1][c2+1] - prefix[r2+1][c1] + prefix[r1][c1]
```

</details>

---

## Problema 4: Difference Array for Range Updates

### Enunciado

Dado un arreglo inicialmente lleno de ceros de tamano `n`, aplica `q` operaciones de actualizacion. Cada operacion suma un valor `val` a todos los elementos en el rango `[l, r]` (0-indexado, ambos inclusive). Despues de todas las operaciones, imprime el arreglo resultante.

### Formato de Entrada

- Primera linea: dos enteros `n` y `q` (1 <= n <= 10^6, 1 <= q <= 10^5).
- Siguientes `q` lineas: tres enteros `l`, `r`, `val` (0 <= l <= r < n, -10^9 <= val <= 10^9).

### Formato de Salida

Una linea con `n` enteros: el arreglo resultante.

### Ejemplo

**Entrada:**
```
5 3
0 2 5
1 4 3
2 3 -2
```

**Salida:**
```
5 8 6 1 3
```

**Explicacion:**
- Despues de op1: [5, 5, 5, 0, 0]
- Despues de op2: [5, 8, 8, 3, 3]
- Despues de op3: [5, 8, 6, 1, 3]

### Pistas

- Sin el difference array, cada operacion seria O(n) y el total O(n*q). Con n=10^6 y q=10^5, eso es 10^11 operaciones.
- Con el difference array, cada operacion es O(1): `diff[l] += val; diff[r+1] -= val`.
- Al final, un solo recorrido (prefix sum del diff) da el arreglo final en O(n).
- Cuidado con el indice `r+1`: verifica que no exceda el tamano del arreglo.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

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
    let q = next!(usize);

    // Difference array (tamano n+1 para evitar chequeo de limites)
    let mut diff = vec![0i64; n + 1];

    for _ in 0..q {
        let l = next!(usize);
        let r = next!(usize);
        let val = next!(i64);

        diff[l] += val;
        if r + 1 <= n {
            diff[r + 1] -= val;
        }
    }

    // Reconstruir arreglo con prefix sum del difference array
    let mut result = Vec::with_capacity(n);
    let mut current = 0i64;
    for i in 0..n {
        current += diff[i];
        result.push(current);
    }

    // Salida eficiente
    let s: Vec<String> = result.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", s.join(" ")).unwrap();
}
```

**Alternativa con scan de iteradores:**

```rust
// Reconstruir usando scan (mas idiomatico)
let result: Vec<i64> = diff[..n].iter()
    .scan(0i64, |acc, &x| {
        *acc += x;
        Some(*acc)
    })
    .collect();
```

**Complejidad:** O(n + q) total: O(1) por operacion de actualizacion, O(n) para la reconstruccion final.

**Nota:** El difference array es la operacion inversa del prefix sum. Si `P` es el operador de prefix sum y `D` es el operador de difference, entonces `P(D(a)) = a` y `D(P(a)) = a`. Esta dualidad es la razon por la que el truco funciona.

</details>

---

## Problema 5: Equilibrium Index

### Enunciado

Dado un arreglo de `n` enteros, encuentra el **equilibrium index**: un indice `i` tal que la suma de los elementos a la izquierda de `i` es igual a la suma de los elementos a la derecha de `i`. El elemento en la posicion `i` no se incluye en ninguna de las dos sumas.

Si hay multiples, imprime el mas pequeno. Si no existe, imprime `-1`.

### Formato de Entrada

- Primera linea: un entero `n` (1 <= n <= 10^5).
- Segunda linea: `n` enteros separados por espacios (-10^9 <= a_i <= 10^9).

### Formato de Salida

Un solo entero: el equilibrium index mas pequeno, o `-1`.

### Ejemplo

**Entrada:**
```
7
-7 1 5 2 -4 3 0
```

**Salida:**
```
3
```

**Explicacion:** Indice 3 (valor 2): izquierda = -7+1+5 = -1, derecha = -4+3+0 = -1.

**Entrada:**
```
3
1 2 3
```

**Salida:**
```
-1
```

### Pistas

- Calcula la suma total `S` del arreglo.
- Recorre de izquierda a derecha manteniendo la suma izquierda `left_sum`.
- En cada posicion `i`: `right_sum = S - left_sum - arr[i]`.
- Si `left_sum == right_sum`, encontraste el indice de equilibrio.
- Este enfoque es O(n) sin necesidad de un arreglo de prefix sums explicito.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

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
    let arr: Vec<i64> = (0..n).map(|_| next!(i64)).collect();

    let total: i64 = arr.iter().sum();
    let mut left_sum: i64 = 0;
    let mut result: i64 = -1;

    for i in 0..n {
        let right_sum = total - left_sum - arr[i];
        if left_sum == right_sum {
            result = i as i64;
            break;
        }
        left_sum += arr[i];
    }

    writeln!(out, "{}", result).unwrap();
}
```

**Solucion alternativa con prefix sums explicitos:**

```rust
fn equilibrium_with_prefix(arr: &[i64]) -> Option<usize> {
    let n = arr.len();
    let mut prefix = vec![0i64; n + 1];
    for i in 0..n {
        prefix[i + 1] = prefix[i] + arr[i];
    }

    let total = prefix[n];
    for i in 0..n {
        let left = prefix[i];
        let right = total - prefix[i + 1];
        if left == right {
            return Some(i);
        }
    }
    None
}
```

**Complejidad:** O(n) en tiempo, O(1) en espacio adicional (primera version), O(n) en espacio (segunda version).

</details>

---

## Conceptos Clave

### Resumen de Tecnicas

| Tecnica | Preprocesamiento | Consulta/Operacion | Uso Principal |
|---------|------------------|--------------------|---------------|
| Prefix Sum 1D | O(n) | O(1) por consulta | Suma de rango |
| Prefix Sum 2D | O(m*n) | O(1) por consulta | Suma de subrectangulo |
| Difference Array | O(n) reconstruccion | O(1) por actualizacion | Actualizaciones de rango |
| Prefix Sum + HashMap | O(n) | Integrado | Contar subarreglos con propiedad |

### Patrones de Rust para Prefix Sums

```rust
// 1. Prefix sum con scan (lazy, no aloca arreglo intermedio si no es necesario)
let prefix_iter = arr.iter().scan(0i64, |acc, &x| { *acc += x; Some(*acc) });

// 2. Prefix sum como Vec
let prefix: Vec<i64> = std::iter::once(0)
    .chain(arr.iter().scan(0i64, |acc, &x| { *acc += x; Some(*acc) }))
    .collect();

// 3. windows() para diferencias consecutivas
let deltas: Vec<i64> = arr.windows(2).map(|w| w[1] - w[0]).collect();

// 4. chunks() para sumas por bloques
let block_sums: Vec<i64> = arr.chunks(block_size)
    .map(|chunk| chunk.iter().sum())
    .collect();

// 5. Prefix XOR (util para problemas de XOR en rangos)
let prefix_xor: Vec<u64> = std::iter::once(0u64)
    .chain(arr.iter().scan(0u64, |acc, &x| { *acc ^= x; Some(*acc) }))
    .collect();
```

### Variantes Comunes en CP

1. **Prefix sum modular**: cuando las sumas pueden ser muy grandes y se pide el resultado modulo algún primo.
2. **Prefix sum de frecuencias**: para contar caracteres en rangos de strings.
3. **Difference array 2D**: para actualizaciones de subrectangulos (se necesitan dos pasadas de prefix sum).
4. **Prefix max/min**: similar pero con `max`/`min` en vez de suma (pero no es reversible).

```rust
// Prefix max
let prefix_max: Vec<i64> = arr.iter()
    .scan(i64::MIN, |acc, &x| {
        *acc = (*acc).max(x);
        Some(*acc)
    })
    .collect();

// Suffix max (recorrer al reves)
let suffix_max: Vec<i64> = arr.iter().rev()
    .scan(i64::MIN, |acc, &x| {
        *acc = (*acc).max(x);
        Some(*acc)
    })
    .collect::<Vec<_>>()
    .into_iter()
    .rev()
    .collect();
```

---

## Ejercicios Adicionales

1. Dado un arreglo binario (0s y 1s), encuentra el subarreglo mas largo con igual cantidad de 0s y 1s (pista: reemplaza 0 por -1 y usa prefix sum + HashMap).
2. Dada una matriz 2D y multiples actualizaciones de subrectangulos, calcula el estado final usando difference array 2D.
3. Encuentra la maxima suma de subarreglo de tamano exactamente `k` usando prefix sums.
4. Dado un arreglo, cuenta cuantos subarreglos tienen suma divisible por `k` (pista: prefix sum modulo k + HashMap).
5. Implementa un "range update, point query" eficiente usando difference arrays, y compara con un "point update, range query" usando prefix sums con BIT/Fenwick tree.
