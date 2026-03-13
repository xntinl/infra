# 33. CP: Simulation and Implementation

**Difficulty**: Intermedio

## Introduccion

Los problemas de simulacion e implementacion no requieren algoritmos sofisticados, pero demandan precision en la codificacion. Son problemas donde "simplemente haces lo que dice el enunciado", pero los detalles importan: indices off-by-one, manejo de bordes, aritmetica modular, y traduccion exacta de reglas a codigo.

En Rust, estos problemas son una excelente oportunidad para practicar el manejo idiomatico de matrices 2D, iteradores, y patrones de indexacion.

### Matrices 2D en Rust

```rust
// Crear matriz de m filas x n columnas inicializada en 0
let mut grid = vec![vec![0i32; n]; m];

// Acceder: grid[fila][columna]
grid[r][c] = 42;

// Iterar sobre todas las celdas
for r in 0..m {
    for c in 0..n {
        // procesar grid[r][c]
    }
}

// Clonar una matriz completa (para "snapshot" del estado)
let snapshot = grid.clone();
```

### Aritmetica Modular

```rust
// Modulo con numeros negativos: Rust da resultado negativo
// -1 % 5 == -1 en Rust (no 4)
// Solucion:
fn modulo(a: i32, m: i32) -> i32 {
    ((a % m) + m) % m
}

// Para movimiento circular en una grilla:
let next_r = ((r as i32 + dr) % m as i32 + m as i32) as usize % m;
```

### Direcciones en una Grid

```rust
// 4 direcciones: arriba, derecha, abajo, izquierda
const DIRS: [(i32, i32); 4] = [(-1, 0), (0, 1), (1, 0), (0, -1)];

// 8 direcciones (incluyendo diagonales)
const DIRS8: [(i32, i32); 8] = [
    (-1, -1), (-1, 0), (-1, 1),
    (0, -1),           (0, 1),
    (1, -1),  (1, 0),  (1, 1),
];
```

---

## Problema 1: Spiral Matrix Traversal

### Enunciado

Dada una matriz de `m x n` enteros, imprime sus elementos en orden espiral (en sentido horario, comenzando desde la esquina superior izquierda).

### Formato de Entrada

- Primera linea: dos enteros `m` y `n` (1 <= m, n <= 100).
- Siguientes `m` lineas: `n` enteros separados por espacios.

### Formato de Salida

Una sola linea con todos los elementos en orden espiral, separados por espacios.

### Ejemplo

**Entrada:**
```
3 4
1  2  3  4
5  6  7  8
9 10 11 12
```

**Salida:**
```
1 2 3 4 8 12 11 10 9 5 6 7
```

**Entrada:**
```
4 4
1  2  3  4
5  6  7  8
9  10 11 12
13 14 15 16
```

**Salida:**
```
1 2 3 4 8 12 16 15 14 13 9 5 6 7 11 10
```

### Pistas

- Mantiene cuatro limites: `top`, `bottom`, `left`, `right`.
- Recorre en cuatro fases: izquierda-a-derecha por el top, arriba-a-abajo por el right, derecha-a-izquierda por el bottom, abajo-a-arriba por el left.
- Despues de cada fase, ajusta el limite correspondiente.
- Cuidado con matrices donde m != n y con matrices de una sola fila o columna.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn spiral_order(matrix: &Vec<Vec<i32>>) -> Vec<i32> {
    let m = matrix.len();
    if m == 0 { return vec![]; }
    let n = matrix[0].len();

    let mut result = Vec::with_capacity(m * n);
    let mut top = 0usize;
    let mut bottom = m;
    let mut left = 0usize;
    let mut right = n;

    while top < bottom && left < right {
        // Izquierda a derecha por la fila top
        for c in left..right {
            result.push(matrix[top][c]);
        }
        top += 1;

        // Arriba a abajo por la columna right-1
        for r in top..bottom {
            result.push(matrix[r][right - 1]);
        }
        right -= 1;

        // Derecha a izquierda por la fila bottom-1
        if top < bottom {
            for c in (left..right).rev() {
                result.push(matrix[bottom - 1][c]);
            }
            bottom -= 1;
        }

        // Abajo a arriba por la columna left
        if left < right {
            for r in (top..bottom).rev() {
                result.push(matrix[r][left]);
            }
            left += 1;
        }
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

    let m = next!(usize);
    let n = next!(usize);

    let mut matrix = vec![vec![0i32; n]; m];
    for r in 0..m {
        for c in 0..n {
            matrix[r][c] = next!(i32);
        }
    }

    let result = spiral_order(&matrix);
    let s: Vec<String> = result.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", s.join(" ")).unwrap();
}
```

**Complejidad:** O(m * n) en tiempo y espacio.

**Error comun:** Olvidar los chequeos `if top < bottom` y `if left < right` en las ultimas dos fases. Sin ellos, en matrices no cuadradas se duplican elementos.

</details>

---

## Problema 2: Rotate Image 90 Degrees

### Enunciado

Dada una matriz cuadrada de `n x n`, rotala 90 grados en sentido horario **in-place** (sin usar una matriz auxiliar). Imprime la matriz resultante.

### Formato de Entrada

- Primera linea: un entero `n` (1 <= n <= 100).
- Siguientes `n` lineas: `n` enteros separados por espacios.

### Formato de Salida

`n` lineas con la matriz rotada.

### Ejemplo

**Entrada:**
```
3
1 2 3
4 5 6
7 8 9
```

**Salida:**
```
7 4 1
8 5 2
9 6 3
```

**Entrada:**
```
4
1  2  3  4
5  6  7  8
9  10 11 12
13 14 15 16
```

**Salida:**
```
13 9  5  1
14 10 6  2
15 11 7  3
16 12 8  4
```

### Pistas

- Rotar 90 grados en sentido horario = transponer + invertir cada fila.
- Transponer: `swap(matrix[r][c], matrix[c][r])` para `r < c`.
- Invertir fila: `row.reverse()`.
- Otra forma: rotar capa por capa, moviendo 4 elementos a la vez en cada paso.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn rotate_90_clockwise(matrix: &mut Vec<Vec<i32>>) {
    let n = matrix.len();

    // Paso 1: Transponer
    for r in 0..n {
        for c in (r + 1)..n {
            let temp = matrix[r][c];
            matrix[r][c] = matrix[c][r];
            matrix[c][r] = temp;
        }
    }

    // Paso 2: Invertir cada fila
    for r in 0..n {
        matrix[r].reverse();
    }
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
    let mut matrix = vec![vec![0i32; n]; n];
    for r in 0..n {
        for c in 0..n {
            matrix[r][c] = next!(i32);
        }
    }

    rotate_90_clockwise(&mut matrix);

    for r in 0..n {
        let row: Vec<String> = matrix[r].iter().map(|x| x.to_string()).collect();
        writeln!(out, "{}", row.join(" ")).unwrap();
    }
}
```

**Solucion alternativa: rotacion capa por capa (mas elegante pero mas compleja):**

```rust
fn rotate_layer_by_layer(matrix: &mut Vec<Vec<i32>>) {
    let n = matrix.len();
    for layer in 0..n / 2 {
        let first = layer;
        let last = n - 1 - layer;
        for i in first..last {
            let offset = i - first;

            // Guardar top
            let top = matrix[first][i];

            // left -> top
            matrix[first][i] = matrix[last - offset][first];

            // bottom -> left
            matrix[last - offset][first] = matrix[last][last - offset];

            // right -> bottom
            matrix[last][last - offset] = matrix[i][last];

            // top -> right
            matrix[i][last] = top;
        }
    }
}
```

**Nota sobre Rust:** No podemos hacer `swap(&mut matrix[r][c], &mut matrix[c][r])` directamente porque ambas referencias provienen del mismo `Vec`. Usamos una variable temporal, o podemos usar indices con `split_at_mut` para obtener dos referencias mutables no superpuestas.

</details>

---

## Problema 3: Game of Life Step

### Enunciado

Implementa un paso del Juego de la Vida de Conway. Dada una matriz de `m x n` donde `1` representa una celula viva y `0` una muerta, calcula el siguiente estado segun las reglas:

1. Una celula viva con menos de 2 vecinos vivos muere (sub-poblacion).
2. Una celula viva con 2 o 3 vecinos vivos sobrevive.
3. Una celula viva con mas de 3 vecinos vivos muere (sobre-poblacion).
4. Una celula muerta con exactamente 3 vecinos vivos revive (reproduccion).

Los 8 vecinos (horizontal, vertical, diagonal) se consideran. Las actualizaciones son **simultaneas** (el estado nuevo se calcula a partir del estado anterior, no del parcialmente actualizado).

### Formato de Entrada

- Primera linea: dos enteros `m` y `n` (1 <= m, n <= 50).
- Siguientes `m` lineas: `n` enteros (0 o 1) separados por espacios.

### Formato de Salida

`m` lineas con el siguiente estado del tablero.

### Ejemplo

**Entrada:**
```
4 4
0 1 0 0
0 0 1 0
1 1 1 0
0 0 0 0
```

**Salida:**
```
0 0 0 0
1 0 1 0
0 1 1 0
0 1 0 0
```

### Pistas

- La clave es que las actualizaciones son simultaneas. Necesitas una copia del estado anterior, o un truco de codificacion in-place.
- Truco in-place: usa bits adicionales. Bit 0 = estado actual, Bit 1 = estado siguiente. Al final, shift right.
- Para contar vecinos, itera sobre las 8 direcciones y verifica limites.
- Con `clone()` de la matriz es mas simple y para m,n <= 50 no hay problema de memoria.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

const DIRS8: [(i32, i32); 8] = [
    (-1, -1), (-1, 0), (-1, 1),
    (0, -1),           (0, 1),
    (1, -1),  (1, 0),  (1, 1),
];

fn count_neighbors(grid: &Vec<Vec<i32>>, r: usize, c: usize, m: usize, n: usize) -> i32 {
    let mut count = 0;
    for &(dr, dc) in &DIRS8 {
        let nr = r as i32 + dr;
        let nc = c as i32 + dc;
        if nr >= 0 && nr < m as i32 && nc >= 0 && nc < n as i32 {
            count += grid[nr as usize][nc as usize] & 1; // solo bit 0
        }
    }
    count
}

fn game_of_life_step(grid: &mut Vec<Vec<i32>>, m: usize, n: usize) {
    // Fase 1: calcular siguiente estado y guardarlo en bit 1
    for r in 0..m {
        for c in 0..n {
            let neighbors = count_neighbors(grid, r, c, m, n);
            let alive = grid[r][c] & 1;

            let next_alive = if alive == 1 {
                // Reglas para celulas vivas
                if neighbors == 2 || neighbors == 3 { 1 } else { 0 }
            } else {
                // Regla para celulas muertas
                if neighbors == 3 { 1 } else { 0 }
            };

            // Guardar siguiente estado en bit 1
            grid[r][c] |= next_alive << 1;
        }
    }

    // Fase 2: extraer bit 1 como nuevo estado
    for r in 0..m {
        for c in 0..n {
            grid[r][c] >>= 1;
        }
    }
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

    let m = next!(usize);
    let n = next!(usize);

    let mut grid = vec![vec![0i32; n]; m];
    for r in 0..m {
        for c in 0..n {
            grid[r][c] = next!(i32);
        }
    }

    game_of_life_step(&mut grid, m, n);

    for r in 0..m {
        let row: Vec<String> = grid[r].iter().map(|x| x.to_string()).collect();
        writeln!(out, "{}", row.join(" ")).unwrap();
    }
}
```

**Solucion alternativa con clone (mas simple):**

```rust
fn game_of_life_clone(grid: &mut Vec<Vec<i32>>, m: usize, n: usize) {
    let old = grid.clone();

    for r in 0..m {
        for c in 0..n {
            let mut neighbors = 0;
            for &(dr, dc) in &DIRS8 {
                let nr = r as i32 + dr;
                let nc = c as i32 + dc;
                if nr >= 0 && nr < m as i32 && nc >= 0 && nc < n as i32 {
                    neighbors += old[nr as usize][nc as usize];
                }
            }

            grid[r][c] = match (old[r][c], neighbors) {
                (1, 2) | (1, 3) => 1,
                (0, 3) => 1,
                _ => 0,
            };
        }
    }
}
```

**El uso de `match` con tuplas** es un patron muy limpio en Rust para expresar reglas complejas de forma legible.

</details>

---

## Problema 4: Robot on Grid with Commands

### Enunciado

Un robot esta en una grilla infinita en la posicion `(0, 0)` mirando al norte. Recibe una secuencia de comandos:

- `G`: avanzar una unidad en la direccion actual.
- `L`: girar 90 grados a la izquierda.
- `R`: girar 90 grados a la derecha.

La secuencia de comandos se repite **infinitamente**. Determina si el robot esta **acotado** (eventualmente regresa al origen o queda en un ciclo que lo mantiene en una region finita).

El robot esta acotado si y solo si:
- Despues de una ejecucion de la secuencia, regresa al origen, **O**
- Despues de una ejecucion, no mira al norte (lo que garantiza que despues de como maximo 4 repeticiones regresa al origen).

### Formato de Entrada

Una sola linea con la secuencia de comandos (longitud <= 100, solo caracteres G, L, R).

### Formato de Salida

"Bounded" o "Unbounded".

### Ejemplo

**Entrada:**
```
GGLLGG
```

**Salida:**
```
Bounded
```

**Explicacion:** Despues de ejecutar GGLLGG, el robot esta en (0,0) mirando al sur. Despues de otra ejecucion, vuelve a (0,0) mirando al norte.

**Entrada:**
```
GG
```

**Salida:**
```
Unbounded
```

**Explicacion:** El robot siempre avanza al norte. Se aleja infinitamente.

**Entrada:**
```
GL
```

**Salida:**
```
Bounded
```

**Explicacion:** Despues de GL, el robot esta en (0,1) mirando al oeste. Despues de 4 repeticiones, regresa al origen.

### Pistas

- Simula una sola ejecucion de la secuencia completa.
- Despues de la simulacion, verifica la posicion y la direccion.
- Si la posicion es `(0, 0)`, esta acotado.
- Si la direccion no es norte, esta acotado (gira, asi que en 4 ciclos completa el cuadrado).
- Si la posicion no es `(0, 0)` y la direccion es norte, se aleja infinitamente.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let commands: Vec<char> = input.trim().chars().collect();

    // Direcciones: 0=Norte, 1=Este, 2=Sur, 3=Oeste
    // Movimiento por direccion: (dy, dx) -- Norte es +y
    let dy = [1, 0, -1, 0]; // norte, este, sur, oeste
    let dx = [0, 1, 0, -1];

    let mut x: i64 = 0;
    let mut y: i64 = 0;
    let mut dir: usize = 0; // norte

    for &cmd in &commands {
        match cmd {
            'G' => {
                x += dx[dir];
                y += dy[dir];
            }
            'L' => {
                dir = (dir + 3) % 4; // girar izquierda = -1 mod 4
            }
            'R' => {
                dir = (dir + 1) % 4; // girar derecha = +1 mod 4
            }
            _ => {} // ignorar caracteres invalidos
        }
    }

    let bounded = (x == 0 && y == 0) || dir != 0;

    writeln!(out, "{}", if bounded { "Bounded" } else { "Unbounded" }).unwrap();
}
```

**Por que funciona la condicion de direccion:**

Si despues de un ciclo el robot mira al este, despues de 4 ciclos habra mirado norte, este, sur, oeste, completando una rotacion completa. Los desplazamientos en cada ciclo se rotaran 90 grados cada vez, y la suma de 4 vectores rotados 0, 90, 180, 270 grados siempre da (0, 0).

```
Ciclo 1: desplazamiento (dx, dy), direccion final = Este
Ciclo 2: desplazamiento (dy, -dx), direccion final = Sur
Ciclo 3: desplazamiento (-dx, -dy), direccion final = Oeste
Ciclo 4: desplazamiento (-dy, dx), direccion final = Norte
Total: (dx + dy - dx - dy, dy - dx - dy + dx) = (0, 0)
```

</details>

---

## Problema 5: Josephus Problem

### Enunciado

`n` personas estan sentadas en un circulo, numeradas del `1` al `n`. Comenzando desde la persona 1, se cuenta en sentido horario y cada `k`-esima persona es eliminada. Determina el orden de eliminacion y la ultima persona que queda.

### Formato de Entrada

Una sola linea con dos enteros `n` y `k` (1 <= n <= 10^5, 1 <= k <= 10^5).

### Formato de Salida

- Primera linea: el orden de eliminacion (numeros separados por espacios).
- Segunda linea: el numero del sobreviviente.

### Ejemplo

**Entrada:**
```
7 3
```

**Salida:**
```
3 6 2 7 5 1 4
4
```

**Explicacion:** Circulo: 1 2 3 4 5 6 7. Contando de 3 en 3: se elimina 3, luego 6, luego 2, luego 7, luego 5, luego 1. Sobrevive 4.

### Pistas

- **Simulacion directa** con un `Vec`: mantiene la lista de personas y remueve la k-esima ciclicamente. Complejidad O(n^2) por las remociones.
- **Formula de Josephus** (solo para el sobreviviente): `J(n, k) = (J(n-1, k) + k) % n` con `J(1, k) = 0`. O(n) sin simulacion.
- Para n <= 10^5, la simulacion O(n^2) puede ser lenta. La formula recursiva da el sobreviviente en O(n).
- Si necesitas el orden completo de eliminacion, la simulacion es necesaria (o usa un BIT/Fenwick tree para O(n log n)).

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
    let n: usize = iter.next().unwrap().parse().unwrap();
    let k: usize = iter.next().unwrap().parse().unwrap();

    // Simulacion para orden de eliminacion
    let mut circle: Vec<usize> = (1..=n).collect();
    let mut elimination_order = Vec::with_capacity(n);
    let mut idx = 0;

    while !circle.is_empty() {
        idx = (idx + k - 1) % circle.len();
        elimination_order.push(circle.remove(idx));
        // Despues de remove, el siguiente elemento ya esta en idx
        // Pero si idx == circle.len(), necesitamos volver a 0
        if !circle.is_empty() && idx == circle.len() {
            idx = 0;
        }
    }

    let order_str: Vec<String> = elimination_order.iter().map(|x| x.to_string()).collect();
    writeln!(out, "{}", order_str.join(" ")).unwrap();
    writeln!(out, "{}", elimination_order.last().unwrap()).unwrap();
}
```

**Solucion eficiente solo para el sobreviviente (formula de Josephus):**

```rust
fn josephus_survivor(n: usize, k: usize) -> usize {
    // Iterativa para evitar stack overflow con n grande
    let mut survivor = 0; // base: J(1, k) = 0 (0-indexado)
    for i in 2..=n {
        survivor = (survivor + k) % i;
    }
    survivor + 1 // convertir a 1-indexado
}
```

**Solucion O(n log n) para orden completo usando BIT (Binary Indexed Tree):**

```rust
struct BIT {
    tree: Vec<usize>,
    n: usize,
}

impl BIT {
    fn new(n: usize) -> Self {
        BIT { tree: vec![0; n + 1], n }
    }

    fn update(&mut self, mut i: usize, val: i64) {
        while i <= self.n {
            self.tree[i] = (self.tree[i] as i64 + val) as usize;
            i += i & i.wrapping_neg();
        }
    }

    fn query(&self, mut i: usize) -> usize {
        let mut sum = 0;
        while i > 0 {
            sum += self.tree[i];
            i -= i & i.wrapping_neg();
        }
        sum
    }

    // Encontrar el k-esimo 1 en el BIT (binary lifting)
    fn find_kth(&self, mut k: usize) -> usize {
        let mut pos = 0;
        let mut bit_mask = 1;
        while bit_mask <= self.n { bit_mask <<= 1; }
        bit_mask >>= 1;

        while bit_mask > 0 {
            let next = pos + bit_mask;
            if next <= self.n && self.tree[next] < k {
                k -= self.tree[next];
                pos = next;
            }
            bit_mask >>= 1;
        }
        pos + 1
    }
}

fn josephus_full_order(n: usize, k: usize) -> Vec<usize> {
    let mut bit = BIT::new(n);
    for i in 1..=n {
        bit.update(i, 1);
    }

    let mut order = Vec::with_capacity(n);
    let mut current_rank = 0;
    let mut remaining = n;

    for _ in 0..n {
        current_rank = (current_rank + k - 1) % remaining;
        let person = bit.find_kth(current_rank + 1);
        order.push(person);
        bit.update(person, -1);
        remaining -= 1;
        if remaining > 0 {
            current_rank %= remaining;
        }
    }
    order
}
```

**Complejidad:**
- Simulacion con Vec::remove: O(n^2)
- Formula de Josephus (solo sobreviviente): O(n)
- Con BIT (orden completo): O(n log n)

</details>

---

## Conceptos Clave

### Patrones de Implementacion en Rust

#### Manejo seguro de coordenadas en grillas

```rust
// Verificar limites antes de acceder
fn in_bounds(r: i32, c: i32, rows: usize, cols: usize) -> bool {
    r >= 0 && r < rows as i32 && c >= 0 && c < cols as i32
}

// Vecinos validos de una celda
fn neighbors(r: usize, c: usize, rows: usize, cols: usize) -> Vec<(usize, usize)> {
    const DIRS: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];
    DIRS.iter()
        .map(|&(dr, dc)| (r as i32 + dr, c as i32 + dc))
        .filter(|&(nr, nc)| in_bounds(nr, nc, rows, cols))
        .map(|(nr, nc)| (nr as usize, nc as usize))
        .collect()
}
```

#### Movimiento circular

```rust
// Indice circular (siempre positivo)
fn circular_index(i: i64, n: usize) -> usize {
    ((i % n as i64 + n as i64) % n as i64) as usize
}

// Avanzar k posiciones en un circulo de n elementos
fn advance(pos: usize, k: usize, n: usize) -> usize {
    (pos + k) % n
}
```

#### Simulacion de estados con enum

```rust
#[derive(Clone, Copy, PartialEq)]
enum Direction {
    North, East, South, West,
}

impl Direction {
    fn turn_left(self) -> Self {
        match self {
            Direction::North => Direction::West,
            Direction::West  => Direction::South,
            Direction::South => Direction::East,
            Direction::East  => Direction::North,
        }
    }

    fn turn_right(self) -> Self {
        match self {
            Direction::North => Direction::East,
            Direction::East  => Direction::South,
            Direction::South => Direction::West,
            Direction::West  => Direction::North,
        }
    }

    fn delta(self) -> (i32, i32) {
        match self {
            Direction::North => (-1, 0),
            Direction::East  => (0, 1),
            Direction::South => (1, 0),
            Direction::West  => (0, -1),
        }
    }
}
```

### Errores Comunes en Implementacion

1. **Off-by-one**: rangos inclusivos vs exclusivos, 0-indexado vs 1-indexado.
2. **Modulo negativo**: `-1 % 5` es `-1` en Rust, no `4`.
3. **Actualizaciones simultaneas**: usar el estado viejo para calcular el nuevo (Game of Life).
4. **Overflow de indices**: usar `i32` para coordenadas cuando se hace aritmetica con offsets negativos, luego convertir a `usize` despues de verificar limites.
5. **Vec::remove es O(n)**: en simulaciones con muchas remociones, considerar `LinkedList` o `BIT`.

---

## Ejercicios Adicionales

1. Implementa "Zigzag Traversal" de una matriz (recorrer diagonales alternando direccion).
2. Simula un tablero de "Minesweeper": dada una matriz con minas ('*') y celdas vacias ('.'), calcula los numeros.
3. Implementa "Pascal's Triangle" hasta la fila n-esima.
4. Simula el movimiento de una serpiente (Snake game) dada una secuencia de comandos y posiciones de comida.
5. Dado un reloj analogico con horas y minutos, calcula el angulo menor entre las manecillas.
