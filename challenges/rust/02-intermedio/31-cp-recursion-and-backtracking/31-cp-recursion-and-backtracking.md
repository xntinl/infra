# 31. CP: Recursion and Backtracking

**Difficulty**: Intermedio

## Introduccion

La recursion y el backtracking son tecnicas fundamentales en programacion competitiva. La recursion permite descomponer problemas en subproblemas mas pequenos, mientras que el backtracking explora sistematicamente todas las posibles soluciones, descartando ramas que no pueden llevar a una solucion valida.

### Recursion en Rust: Consideraciones Importantes

Rust **no garantiza la optimizacion de tail calls (TCO)**. Esto significa que funciones recursivas profundas pueden provocar un **stack overflow**. El stack por defecto de un thread en Rust es de ~8 MB (configurable), lo que limita la profundidad de recursion a aproximadamente 10,000-100,000 llamadas dependiendo del tamano del frame.

```rust
// PELIGRO: esto puede causar stack overflow para n grande
fn factorial(n: u64) -> u64 {
    if n <= 1 { 1 } else { n * factorial(n - 1) }
}

// SEGURO: version iterativa equivalente
fn factorial_iter(n: u64) -> u64 {
    (1..=n).product()
}
```

### Patron de Backtracking en Rust

```rust
fn backtrack(state: &mut State, result: &mut Vec<Solution>) {
    if es_solucion(state) {
        result.push(state.to_solution());
        return;
    }
    for candidato in generar_candidatos(state) {
        if es_valido(state, &candidato) {
            aplicar(state, &candidato);      // hacer
            backtrack(state, result);          // recurrir
            deshacer(state, &candidato);       // deshacer
        }
    }
}
```

### Plantilla de I/O para Competitive Programming

```rust
use std::io::{self, Read, Write, BufWriter};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    // macro para leer valores
    macro_rules! next {
        ($t:ty) => { iter.next().unwrap().parse::<$t>().unwrap() }
    }

    let n: usize = next!(usize);
    // ... resolver ...
    writeln!(out, "{}", resultado).unwrap();
}
```

---

## Problema 1: Generar Permutaciones

### Enunciado

Dado un numero entero `n`, genera todas las permutaciones de los numeros del `1` al `n` en orden lexicografico. Imprime cada permutacion en una linea separada.

### Formato de Entrada

Una sola linea con un entero `n` (1 <= n <= 8).

### Formato de Salida

Todas las permutaciones de `[1, 2, ..., n]` en orden lexicografico, una por linea, con los numeros separados por espacios.

### Ejemplo

**Entrada:**
```
3
```

**Salida:**
```
1 2 3
1 3 2
2 1 3
2 3 1
3 1 2
3 2 1
```

### Pistas

- Usa un vector `used` de booleanos para rastrear que numeros ya estan en la permutacion actual.
- La recursion construye la permutacion posicion por posicion.
- Al generar candidatos en orden 1..=n, las permutaciones salen en orden lexicografico naturalmente.
- Para n=8, hay 8! = 40,320 permutaciones. Esto es manejable.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn generate_permutations(
    n: usize,
    current: &mut Vec<usize>,
    used: &mut Vec<bool>,
    result: &mut Vec<Vec<usize>>,
) {
    if current.len() == n {
        result.push(current.clone());
        return;
    }
    for i in 1..=n {
        if !used[i] {
            used[i] = true;
            current.push(i);
            generate_permutations(n, current, used, result);
            current.pop();
            used[i] = false;
        }
    }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = input.trim().parse().unwrap();
    let mut result = Vec::new();
    let mut current = Vec::new();
    let mut used = vec![false; n + 1];

    generate_permutations(n, &mut current, &mut used, &mut result);

    for perm in &result {
        let s: Vec<String> = perm.iter().map(|x| x.to_string()).collect();
        writeln!(out, "{}", s.join(" ")).unwrap();
    }
}
```

**Complejidad:** O(n! * n) en tiempo, O(n! * n) en espacio para almacenar todas las permutaciones.

**Alternativa iterativa:** Se puede usar el algoritmo de Heap o el next_permutation para generar permutaciones sin recursion:

```rust
fn next_permutation(arr: &mut Vec<usize>) -> bool {
    let n = arr.len();
    if n <= 1 { return false; }
    let mut i = n - 1;
    while i > 0 && arr[i - 1] >= arr[i] {
        i -= 1;
    }
    if i == 0 { return false; }
    let mut j = n - 1;
    while arr[j] <= arr[i - 1] {
        j -= 1;
    }
    arr.swap(i - 1, j);
    arr[i..].reverse();
    true
}
```

</details>

---

## Problema 2: N-Queens (4x4 y 8x8)

### Enunciado

El problema de las N-Reinas consiste en colocar N reinas en un tablero de NxN de forma que ninguna reina ataque a otra. Dos reinas se atacan si estan en la misma fila, columna o diagonal.

**Parte A:** Para N=4, imprime **todas** las soluciones. Cada solucion es una linea con N numeros donde el i-esimo numero indica la columna de la reina en la fila i (1-indexado).

**Parte B:** Para N=8, imprime solo la **cantidad** de soluciones distintas.

### Formato de Entrada

Una sola linea con un entero `n` (4 <= n <= 12).

Si n <= 8, imprime todas las soluciones seguidas de la cantidad total. Si n > 8, imprime solo la cantidad.

### Formato de Salida

Para n <= 8: cada solucion en una linea (columnas separadas por espacios), seguida de una linea con el total.
Para n > 8: solo el total.

### Ejemplo

**Entrada:**
```
4
```

**Salida:**
```
2 4 1 3
3 1 4 2
2
```

### Pistas

- Coloca reinas fila por fila. Para cada fila, prueba cada columna.
- Necesitas verificar tres condiciones: columna libre, diagonal principal libre, diagonal secundaria libre.
- Usa tres arreglos booleanos: `col[c]`, `diag1[r+c]`, `diag2[r-c+n-1]`.
- Para N=8, hay exactamente 92 soluciones.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

struct NQueens {
    n: usize,
    col: Vec<bool>,
    diag1: Vec<bool>,  // r + c
    diag2: Vec<bool>,  // r - c + n - 1
    queens: Vec<usize>,
    solutions: Vec<Vec<usize>>,
}

impl NQueens {
    fn new(n: usize) -> Self {
        NQueens {
            n,
            col: vec![false; n],
            diag1: vec![false; 2 * n],
            diag2: vec![false; 2 * n],
            queens: Vec::with_capacity(n),
            solutions: Vec::new(),
        }
    }

    fn solve(&mut self, row: usize) {
        if row == self.n {
            self.solutions.push(self.queens.clone());
            return;
        }
        for c in 0..self.n {
            let d1 = row + c;
            let d2 = row + self.n - 1 - c;
            if !self.col[c] && !self.diag1[d1] && !self.diag2[d2] {
                self.col[c] = true;
                self.diag1[d1] = true;
                self.diag2[d2] = true;
                self.queens.push(c + 1);

                self.solve(row + 1);

                self.queens.pop();
                self.col[c] = false;
                self.diag1[d1] = false;
                self.diag2[d2] = false;
            }
        }
    }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let n: usize = input.trim().parse().unwrap();
    let mut solver = NQueens::new(n);
    solver.solve(0);

    if n <= 8 {
        for sol in &solver.solutions {
            let s: Vec<String> = sol.iter().map(|x| x.to_string()).collect();
            writeln!(out, "{}", s.join(" ")).unwrap();
        }
    }
    writeln!(out, "{}", solver.solutions.len()).unwrap();
}
```

**Complejidad:** O(n!) en el peor caso, pero la poda reduce significativamente el espacio de busqueda.

**Nota sobre stack:** Para n=12, la profundidad de recursion es solo 12, asi que no hay riesgo de stack overflow. El backtracking con poda es muy eficiente aqui.

</details>

---

## Problema 3: Subset Sum

### Enunciado

Dado un conjunto de `n` numeros enteros positivos y un valor objetivo `target`, determina si existe un subconjunto cuya suma sea exactamente `target`. Si existe, imprime "Yes" seguido de los elementos del subconjunto (el de menor tamano lexicograficamente). Si no existe, imprime "No".

### Formato de Entrada

- Primera linea: dos enteros `n` y `target` (1 <= n <= 20, 1 <= target <= 10^6).
- Segunda linea: `n` enteros positivos separados por espacios.

### Formato de Salida

- "Yes" seguido en la siguiente linea de los elementos del subconjunto en orden creciente, o "No".

### Ejemplo

**Entrada:**
```
5 9
3 34 4 12 5
```

**Salida:**
```
Yes
4 5
```

**Entrada:**
```
3 11
1 2 5
```

**Salida:**
```
No
```

### Pistas

- Con n <= 20, puedes usar backtracking o incluso fuerza bruta con bitmask (2^20 = ~10^6).
- Para encontrar el subconjunto de menor tamano, primero ordena los numeros y busca con preferencia por subconjuntos mas pequenos.
- La poda es crucial: si la suma parcial ya excede el target, no sigas explorando esa rama.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn find_subset(
    nums: &[i64],
    idx: usize,
    target: i64,
    current: &mut Vec<i64>,
    best: &mut Option<Vec<i64>>,
) {
    if target == 0 {
        match best {
            Some(ref b) if current.len() >= b.len() => {}
            _ => *best = Some(current.clone()),
        }
        return;
    }
    if target < 0 || idx == nums.len() {
        return;
    }
    // Poda: si ya tenemos una solucion mejor, no buscar subconjuntos mas grandes
    if let Some(ref b) = best {
        if current.len() >= b.len() {
            return;
        }
    }

    // Incluir nums[idx]
    current.push(nums[idx]);
    find_subset(nums, idx + 1, target - nums[idx], current, best);
    current.pop();

    // No incluir nums[idx]
    find_subset(nums, idx + 1, target, current, best);
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    let n: usize = iter.next().unwrap().parse().unwrap();
    let target: i64 = iter.next().unwrap().parse().unwrap();
    let mut nums: Vec<i64> = (0..n)
        .map(|_| iter.next().unwrap().parse().unwrap())
        .collect();

    nums.sort();

    let mut best = None;
    let mut current = Vec::new();
    find_subset(&nums, 0, target, &mut current, &mut best);

    match best {
        Some(subset) => {
            writeln!(out, "Yes").unwrap();
            let s: Vec<String> = subset.iter().map(|x| x.to_string()).collect();
            writeln!(out, "{}", s.join(" ")).unwrap();
        }
        None => {
            writeln!(out, "No").unwrap();
        }
    }
}
```

**Alternativa con bitmask (mas idiomatica para n <= 20):**

```rust
fn solve_bitmask(nums: &[i64], target: i64) -> Option<Vec<i64>> {
    let n = nums.len();
    let mut best: Option<Vec<i64>> = None;

    for mask in 0u32..(1 << n) {
        let mut sum = 0i64;
        let mut subset = Vec::new();
        for i in 0..n {
            if mask & (1 << i) != 0 {
                sum += nums[i];
                subset.push(nums[i]);
            }
        }
        if sum == target {
            match best {
                Some(ref b) if subset.len() >= b.len() => {}
                _ => {
                    subset.sort();
                    best = Some(subset);
                }
            }
        }
    }
    best
}
```

**Complejidad:** O(2^n) para ambas versiones. El backtracking con poda suele ser mas rapido en la practica.

</details>

---

## Problema 4: Word Search on Grid

### Enunciado

Dado un tablero de MxN con letras y una palabra, determina si la palabra puede encontrarse en el tablero. La palabra se construye a partir de celdas adyacentes (horizontal o verticalmente). Cada celda solo puede usarse una vez por camino.

### Formato de Entrada

- Primera linea: dos enteros `m` y `n` (1 <= m, n <= 10).
- Siguientes `m` lineas: cadena de `n` caracteres (letras minusculas).
- Ultima linea: la palabra a buscar (longitud <= m*n).

### Formato de Salida

"Yes" si la palabra existe en el tablero, "No" en caso contrario.

### Ejemplo

**Entrada:**
```
3 4
abce
sfcs
adee
```

**Salida para palabra "abcced":**
```
Yes
```

**Entrada completa:**
```
3 4
abce
sfcs
adee
abcced
```

**Salida:**
```
Yes
```

### Pistas

- Para cada celda del tablero que coincida con la primera letra de la palabra, inicia una busqueda DFS.
- Marca las celdas visitadas para evitar reusar (puedes cambiar temporalmente el caracter a un valor especial como `#`).
- Direcciones: arriba, abajo, izquierda, derecha.
- Peor caso: O(m * n * 4^L) donde L es la longitud de la palabra, pero la poda lo hace mucho mas rapido.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

fn dfs(
    board: &mut Vec<Vec<u8>>,
    word: &[u8],
    idx: usize,
    r: usize,
    c: usize,
    rows: usize,
    cols: usize,
) -> bool {
    if idx == word.len() {
        return true;
    }
    if r >= rows || c >= cols || board[r][c] != word[idx] {
        return false;
    }

    let original = board[r][c];
    board[r][c] = b'#'; // marcar como visitada

    let directions: [(i32, i32); 4] = [(-1, 0), (1, 0), (0, -1), (0, 1)];
    for (dr, dc) in &directions {
        let nr = r as i32 + dr;
        let nc = c as i32 + dc;
        if nr >= 0 && nc >= 0 {
            if dfs(board, word, idx + 1, nr as usize, nc as usize, rows, cols) {
                board[r][c] = original; // restaurar antes de retornar
                return true;
            }
        }
    }

    board[r][c] = original; // backtrack
    false
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut lines = input.lines();
    let first: Vec<usize> = lines.next().unwrap()
        .split_whitespace()
        .map(|x| x.parse().unwrap())
        .collect();
    let (rows, cols) = (first[0], first[1]);

    let mut board: Vec<Vec<u8>> = Vec::new();
    for _ in 0..rows {
        let line = lines.next().unwrap().trim();
        board.push(line.bytes().collect());
    }
    let word: Vec<u8> = lines.next().unwrap().trim().bytes().collect();

    let mut found = false;
    'outer: for r in 0..rows {
        for c in 0..cols {
            if dfs(&mut board, &word, 0, r, c, rows, cols) {
                found = true;
                break 'outer;
            }
        }
    }

    writeln!(out, "{}", if found { "Yes" } else { "No" }).unwrap();
}
```

**Nota sobre el patron:** Modificar el tablero in-place (`board[r][c] = b'#'`) es mas eficiente que mantener una matriz `visited` separada. Es un patron muy comun en CP con Rust.

**Nota sobre labeled breaks:** El `'outer:` label permite salir de loops anidados, una caracteristica muy util de Rust que evita variables booleanas auxiliares.

</details>

---

## Problema 5: Sudoku Solver (Simple)

### Enunciado

Dado un tablero de Sudoku 9x9 parcialmente lleno, completalo. El tablero usa digitos del 1 al 9, y los espacios vacios se representan con `0`. Se garantiza que el puzzle tiene exactamente una solucion.

### Formato de Entrada

9 lineas, cada una con 9 enteros separados por espacios (0 para celdas vacias).

### Formato de Salida

9 lineas con el tablero resuelto, 9 enteros separados por espacios.

### Ejemplo

**Entrada:**
```
5 3 0 0 7 0 0 0 0
6 0 0 1 9 5 0 0 0
0 9 8 0 0 0 0 6 0
8 0 0 0 6 0 0 0 3
4 0 0 8 0 3 0 0 1
7 0 0 0 2 0 0 0 6
0 6 0 0 0 0 2 8 0
0 0 0 4 1 9 0 0 5
0 0 0 0 8 0 0 7 9
```

**Salida:**
```
5 3 4 6 7 8 9 1 2
6 7 2 1 9 5 3 4 8
1 9 8 3 4 2 5 6 7
8 5 9 7 6 1 4 2 3
4 2 6 8 5 3 7 9 1
7 1 3 9 2 4 8 5 6
9 6 1 5 3 7 2 8 4
2 8 7 4 1 9 6 3 5
3 4 5 2 8 6 1 7 9
```

### Pistas

- Recorre las celdas de izquierda a derecha, arriba a abajo. Salta las celdas ya llenas.
- Para cada celda vacia, prueba digitos del 1 al 9. Verifica que el digito no este en la misma fila, columna, ni en el mismo bloque 3x3.
- Usa tres conjuntos (o arreglos de bits) para rastrear digitos usados en filas, columnas y bloques.
- Con buena poda, el solver es muy rapido incluso para los Sudokus mas dificiles.

<details>
<summary>Solucion</summary>

```rust
use std::io::{self, Read, Write, BufWriter};

struct Sudoku {
    board: [[u8; 9]; 9],
    row: [u16; 9],   // bitmask de digitos usados por fila
    col: [u16; 9],   // bitmask de digitos usados por columna
    blk: [u16; 9],   // bitmask de digitos usados por bloque 3x3
}

impl Sudoku {
    fn new(board: [[u8; 9]; 9]) -> Self {
        let mut row = [0u16; 9];
        let mut col = [0u16; 9];
        let mut blk = [0u16; 9];

        for r in 0..9 {
            for c in 0..9 {
                if board[r][c] != 0 {
                    let bit = 1u16 << board[r][c];
                    row[r] |= bit;
                    col[c] |= bit;
                    blk[(r / 3) * 3 + c / 3] |= bit;
                }
            }
        }

        Sudoku { board, row, col, blk }
    }

    fn solve(&mut self, pos: usize) -> bool {
        // Encontrar la siguiente celda vacia
        let mut p = pos;
        while p < 81 {
            let r = p / 9;
            let c = p % 9;
            if self.board[r][c] == 0 {
                break;
            }
            p += 1;
        }
        if p == 81 {
            return true; // todas las celdas llenas
        }

        let r = p / 9;
        let c = p % 9;
        let b = (r / 3) * 3 + c / 3;
        let used = self.row[r] | self.col[c] | self.blk[b];

        for d in 1u8..=9 {
            let bit = 1u16 << d;
            if used & bit == 0 {
                // Colocar digito
                self.board[r][c] = d;
                self.row[r] |= bit;
                self.col[c] |= bit;
                self.blk[b] |= bit;

                if self.solve(p + 1) {
                    return true;
                }

                // Backtrack
                self.board[r][c] = 0;
                self.row[r] &= !bit;
                self.col[c] &= !bit;
                self.blk[b] &= !bit;
            }
        }

        false
    }
}

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).unwrap();
    let stdout = io::stdout();
    let mut out = BufWriter::new(stdout.lock());

    let mut iter = input.split_whitespace();
    let mut board = [[0u8; 9]; 9];
    for r in 0..9 {
        for c in 0..9 {
            board[r][c] = iter.next().unwrap().parse().unwrap();
        }
    }

    let mut sudoku = Sudoku::new(board);
    sudoku.solve(0);

    for r in 0..9 {
        let row: Vec<String> = sudoku.board[r].iter().map(|x| x.to_string()).collect();
        writeln!(out, "{}", row.join(" ")).unwrap();
    }
}
```

**Optimizacion con bitmasks:** Usar `u16` como bitmask para rastrear digitos usados es mucho mas rapido que usar `HashSet` o `Vec<bool>`. La operacion `used & bit == 0` es O(1).

**Nota sobre profundidad de recursion:** El peor caso es 81 niveles de recursion (todas las celdas vacias), lo cual es totalmente seguro para el stack de Rust.

</details>

---

## Conceptos Clave

### Recursion vs Iteracion en Rust

| Aspecto | Recursion | Iteracion |
|---------|-----------|-----------|
| Legibilidad | Mas natural para arboles/grafos | Mejor para secuencias lineales |
| Stack | Limitado (~8 MB default) | Sin limite de profundidad |
| Performance | Overhead de llamadas | Generalmente mas rapido |
| TCO en Rust | **No garantizado** | N/A |
| Mutabilidad | `&mut` a traves de parametros | Natural con `let mut` |

### Aumentar el Stack en Rust

Si necesitas recursion profunda (>10,000 niveles):

```rust
fn main() {
    // Crear un thread con stack de 64 MB
    let builder = std::thread::Builder::new().stack_size(64 * 1024 * 1024);
    let handler = builder.spawn(|| {
        solve();
    }).unwrap();
    handler.join().unwrap();
}
```

### Evitar Stack Overflow: Conversion a Iterativo

Para DFS, puedes reemplazar la recursion con un stack explicito:

```rust
fn dfs_iterative(start: usize, adj: &[Vec<usize>]) -> Vec<usize> {
    let n = adj.len();
    let mut visited = vec![false; n];
    let mut stack = vec![start];
    let mut order = Vec::new();

    while let Some(node) = stack.pop() {
        if visited[node] { continue; }
        visited[node] = true;
        order.push(node);
        // Agregar vecinos en orden reverso para mantener orden
        for &next in adj[node].iter().rev() {
            if !visited[next] {
                stack.push(next);
            }
        }
    }
    order
}
```

### Patrones Comunes de Backtracking

1. **Permutaciones**: usado/no-usado con vector de booleanos
2. **Combinaciones**: indice de inicio para evitar duplicados
3. **Grid search**: marcar celda, explorar 4 direcciones, desmarcar
4. **Constraint satisfaction** (Sudoku, N-Queens): validar restricciones antes de colocar

---

## Ejercicios Adicionales

1. Genera todas las combinaciones de `k` elementos de `[1..n]`.
2. Resuelve el problema del "Knight's Tour" en un tablero 5x5.
3. Implementa un generador de parentesis balanceados para `n` pares.
4. Dado un laberinto (matriz con paredes), encuentra el camino mas corto de esquina a esquina usando backtracking con poda.
5. Implementa "Letter Combinations of a Phone Number" (mapeo T9).
