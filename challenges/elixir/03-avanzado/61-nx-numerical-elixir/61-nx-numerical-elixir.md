# Ejercicio 61: Nx — Computación Numérica de Alta Performance

## Objetivo

Dominar `Nx` (Numerical Elixir) para procesamiento tensorial: desde operaciones
elementwise hasta compilación con `defn` y manejo de backends (BinaryBackend vs EXLA).

## Conceptos Clave

- `Nx.tensor/2` con tipos explícitos (`f32`, `u8`, `s64`, `f64`)
- Operaciones elementwise: `add`, `multiply`, `exp`, `log`, `power`
- Broadcasting: tensores de shapes distintos operando juntos
- `Nx.dot/2` para multiplicación de matrices
- `defn/2`: compilación a XLA/EXLA para vectorización real
- Backends: `Nx.BinaryBackend` (default, portable) vs `EXLA` (CPU/GPU acelerado)

---

## Setup

```elixir
# mix.exs
defp deps do
  [
    {:nx, "~> 0.7"},
    {:exla, "~> 0.7"}   # opcional, requiere XLA compilado
  ]
end
```

```elixir
# Configurar backend globalmente (en config/config.exs)
config :nx, default_backend: EXLA.Backend

# O por sesión en iex
Nx.default_backend(EXLA.Backend)
```

---

## Parte 1: Tensores y Tipos

### Creación de tensores

```elixir
# Escalar
Nx.tensor(3.14)
#=> #Nx.Tensor<f32 3.1400001049041748>

# Vector de enteros
Nx.tensor([1, 2, 3, 4, 5])
#=> #Nx.Tensor<s64[5] [1, 2, 3, 4, 5]>

# Matriz f32 explícita
Nx.tensor([[1.0, 2.0], [3.0, 4.0]], type: :f32)
#=> #Nx.Tensor<f32[2][2] [[1.0, 2.0], [3.0, 4.0]]>

# Tensor 3D (batch de imágenes grayscale: batch x H x W)
Nx.tensor([[[0, 128, 255], [64, 192, 32]]], type: :u8)
#=> #Nx.Tensor<u8[1][2][3] ...>
```

### Tipos disponibles

| Tipo  | Descripción              | Rango                     |
|-------|--------------------------|---------------------------|
| `:f32` | float 32 bits           | ±3.4×10³⁸                |
| `:f64` | float 64 bits           | ±1.8×10³⁰⁸               |
| `:s8`  | signed int 8 bits       | -128 a 127                |
| `:s64` | signed int 64 bits      | -2⁶³ a 2⁶³-1             |
| `:u8`  | unsigned int 8 bits     | 0 a 255                   |
| `:u32` | unsigned int 32 bits    | 0 a 4294967295            |
| `:bf16`| bfloat16 (ML)           | menor precisión, más rango|

```elixir
# Conversión de tipos
t = Nx.tensor([0, 128, 255], type: :u8)
Nx.as_type(t, :f32)
#=> #Nx.Tensor<f32[3] [0.0, 128.0, 255.0]>
```

---

## Parte 2: Operaciones Elementwise

```elixir
a = Nx.tensor([1.0, 2.0, 3.0, 4.0])
b = Nx.tensor([10.0, 20.0, 30.0, 40.0])

Nx.add(a, b)        #=> [11.0, 22.0, 33.0, 44.0]
Nx.subtract(b, a)   #=> [9.0, 18.0, 27.0, 36.0]
Nx.multiply(a, b)   #=> [10.0, 40.0, 90.0, 160.0]
Nx.divide(b, a)     #=> [10.0, 10.0, 10.0, 10.0]

# Funciones matemáticas
Nx.exp(Nx.tensor([0.0, 1.0, 2.0]))
#=> [1.0, 2.718..., 7.389...]

Nx.log(Nx.tensor([1.0, Math.e, 10.0]))
Nx.sqrt(Nx.tensor([4.0, 9.0, 16.0]))
Nx.power(Nx.tensor([2.0, 3.0, 4.0]), 3)
```

### Broadcasting

Broadcasting permite operar tensores de shapes distintos siguiendo reglas
similares a NumPy: las dimensiones se alinean por la derecha y se expanden
automáticamente donde una dimensión es 1.

```elixir
# Vector + escalar
vector = Nx.tensor([1.0, 2.0, 3.0])
scalar = Nx.tensor(10.0)
Nx.add(vector, scalar)
#=> [11.0, 12.0, 13.0]

# Matriz + vector (broadcast sobre filas)
matrix = Nx.tensor([[1.0, 2.0, 3.0],
                    [4.0, 5.0, 6.0]])
bias   = Nx.tensor([100.0, 200.0, 300.0])

Nx.add(matrix, bias)
#=> [[101.0, 202.0, 303.0],
#    [104.0, 205.0, 306.0]]

# Shape check antes de operar
Nx.shape(matrix)  #=> {2, 3}
Nx.shape(bias)    #=> {3}
# broadcast: {2,3} + {3} → {2,3} ✓
```

---

## Parte 3: Estadísticas y Reducciones

```elixir
t = Nx.tensor([[1.0, 2.0, 3.0],
               [4.0, 5.0, 6.0],
               [7.0, 8.0, 9.0]])

Nx.sum(t)                    #=> 45.0
Nx.mean(t)                   #=> 5.0
Nx.variance(t)               #=> 6.666...
Nx.standard_deviation(t)     #=> 2.581...

# Reducción por eje
Nx.sum(t, axes: [0])         #=> [12.0, 15.0, 18.0]  (suma columnas)
Nx.sum(t, axes: [1])         #=> [6.0, 15.0, 24.0]   (suma filas)
Nx.mean(t, axes: [0])        #=> [4.0, 5.0, 6.0]

# Min/max
Nx.reduce_min(t)             #=> 1.0
Nx.reduce_max(t, axes: [1])  #=> [3.0, 6.0, 9.0]
```

---

## Parte 4: Multiplicación Matricial

```elixir
# Nx.dot/2 = matrix multiplication cuando ambos son 2D
a = Nx.tensor([[1.0, 2.0],
               [3.0, 4.0]])

b = Nx.tensor([[5.0, 6.0],
               [7.0, 8.0]])

Nx.dot(a, b)
#=> [[19.0, 22.0],
#    [43.0, 50.0]]
# (1*5+2*7, 1*6+2*8) = (19, 22)
# (3*5+4*7, 3*6+4*8) = (43, 50)

# Producto punto entre vectores
v1 = Nx.tensor([1.0, 2.0, 3.0])
v2 = Nx.tensor([4.0, 5.0, 6.0])
Nx.dot(v1, v2)
#=> 32.0  (1*4 + 2*5 + 3*6)

# Batch matmul: {batch, m, k} x {batch, k, n}
Nx.LinAlg.matmul(batch_a, batch_b)
```

---

## Ejercicio 1: Normalización de Imagen

**Contexto**: En computer vision, normalizar los píxeles a media 0 y desviación 1
es un paso estándar de preprocesamiento antes de pasar imágenes a una red neuronal.

**Tu tarea**: Implementa el módulo `ImageProcessor` con las funciones indicadas.

```elixir
defmodule ImageProcessor do
  import Nx

  @doc """
  Normaliza un tensor de imagen: (x - mean) / std
  Soporta tensores de cualquier shape (HxW, HxWxC, BxHxWxC).
  Devuelve {:ok, normalized} o {:error, reason}.
  """
  def normalize(image) do
    # TODO: validar que image es un tensor Nx
    # TODO: calcular mean y std sobre todos los elementos
    # TODO: manejar std == 0 (imagen constante)
    # TODO: aplicar la fórmula y devolver {:ok, result}
  end

  @doc """
  Normaliza cada canal por separado (último eje = canales).
  Útil para imágenes RGB donde cada canal tiene su propia distribución.
  """
  def normalize_per_channel(image) do
    # TODO: image shape debe ser {H, W, C} o {B, H, W, C}
    # TODO: calcular mean y std por canal (reducir todos los ejes excepto el último)
    # TODO: expandir dimensiones para broadcasting correcto
    # TODO: aplicar normalización y devolver {:ok, result}
  end

  @doc """
  Convierte imagen u8 [0,255] a f32 [0.0,1.0]
  """
  def to_float(image) do
    # TODO: verificar type == :u8
    # TODO: convertir a f32 y dividir entre 255.0
  end
end
```

**Datos de prueba**:

```elixir
# Imagen grayscale 3x3 (pixeles u8)
gray = Nx.tensor([
  [10,  20,  30],
  [40,  50,  60],
  [70,  80,  90]
], type: :u8)

# Imagen RGB 2x2x3
rgb = Nx.tensor([
  [[255, 0, 0], [0, 255, 0]],
  [[0,   0, 255], [128, 128, 128]]
], type: :u8)
```

**Resultado esperado** (normalize sobre gray como f32):

```
mean = 50.0
std  = 25.819...
normalized[0][0] = (10 - 50) / 25.819 = -1.549...
normalized[1][1] = (50 - 50) / 25.819 =  0.0
normalized[2][2] = (90 - 50) / 25.819 =  1.549...
```

---

## Ejercicio 2: Regresión Lineal con defn

**Contexto**: `defn/2` es una macro de Nx que compila el código a XLA,
permitiendo ejecución vectorizada en CPU/GPU. Las funciones `defn` solo
pueden llamar a otras `defn` y a operaciones Nx — no a código Elixir arbitrario.

```elixir
defmodule LinearRegression do
  import Nx.Defn

  @doc """
  Predicción: y_hat = X @ W + b
  X: {batch, features}
  W: {features, outputs}
  b: {outputs}
  """
  defn predict(x, weights, bias) do
    Nx.dot(x, weights) + bias
  end

  @doc """
  Mean Squared Error: mean((y_pred - y_true)^2)
  """
  defn mse_loss(y_pred, y_true) do
    # TODO: implementar con Nx.mean y Nx.power
  end

  @doc """
  Un paso de gradient descent manual.
  Devuelve {new_weights, new_bias, loss}.
  """
  defn train_step(x, y_true, weights, bias, learning_rate) do
    # TODO: calcular y_pred con predict/3
    # TODO: calcular loss con mse_loss/2
    # TODO: calcular gradientes:
    #   - grad_w = 2/n * X^T @ (y_pred - y_true)
    #   - grad_b = 2/n * mean(y_pred - y_true)
    # TODO: actualizar pesos: w = w - lr * grad_w
    # TODO: devolver {new_w, new_b, loss}
  end

  @doc """
  Entrena por `epochs` iteraciones.
  Devuelve {weights, bias, history_losses}.
  """
  def train(x, y_true, epochs \\ 1000, learning_rate \\ 0.01) do
    # TODO: inicializar weights y bias con Nx.zeros
    # TODO: iterar epochs veces llamando train_step/5
    # TODO: acumular losses en lista
    # TODO: devolver {final_w, final_b, losses}
  end
end
```

**Datos de prueba — relación lineal simple** `y = 3x + 7`:

```elixir
x_data = Nx.tensor([[1.0], [2.0], [3.0], [4.0], [5.0]])
y_data = Nx.tensor([[10.0], [13.0], [16.0], [19.0], [22.0]])

{w, b, losses} = LinearRegression.train(x_data, y_data, 2000, 0.05)

# Esperado: w ≈ [[3.0]], b ≈ [7.0]
# La loss debe decrecer monótonamente
```

---

## Ejercicio 3: Batch Processing con Medición de Throughput

**Contexto**: Procesar datos en batches reduce overhead de llamadas al backend
y permite al hardware (especialmente GPU) operar de forma óptima.

```elixir
defmodule BatchProcessor do
  import Nx.Defn

  @doc """
  Aplica una transformación a un tensor con la función dada,
  procesando en batches de `batch_size`.

  Devuelve {resultados, tiempo_ms, throughput_items_per_sec}.
  """
  def process_batches(data, batch_size, transform_fn) do
    # TODO: dividir data en chunks de batch_size con Enum.chunk_every
    # TODO: medir tiempo total con :timer.tc
    # TODO: aplicar transform_fn a cada batch
    # TODO: concatenar resultados con Nx.concatenate
    # TODO: calcular throughput = n_items / tiempo_segundos
  end

  @doc """
  Simula preprocesamiento de imágenes: normalización + resize (crop central).
  Compilada con defn para máximo rendimiento.
  """
  defn preprocess_batch(batch) do
    # batch shape: {B, H, W, C}
    # TODO: convertir a f32
    # TODO: normalizar a [0, 1]
    # TODO: aplicar mean=[0.485,0.456,0.406], std=[0.229,0.224,0.225] (ImageNet stats)
    # Retorna batch normalizado
  end

  @doc """
  Genera N imágenes sintéticas para benchmark.
  """
  def generate_images(n, height \\ 224, width \\ 224, channels \\ 3) do
    # TODO: usar Nx.random_uniform con type: :u8, min: 0, max: 255
    # shape: {n, height, width, channels}
  end
end
```

**Benchmark esperado**:

```elixir
images = BatchProcessor.generate_images(1000)

{results, time_ms, throughput} =
  BatchProcessor.process_batches(images, 32, &BatchProcessor.preprocess_batch/1)

IO.puts("Procesadas: #{Nx.shape(results) |> elem(0)} imágenes")
IO.puts("Tiempo total: #{time_ms} ms")
IO.puts("Throughput: #{Float.round(throughput, 1)} imgs/seg")
```

---

## Solución de Referencia

<details>
<summary>Ver solución (intenta resolver primero)</summary>

### Ejercicio 1

```elixir
defmodule ImageProcessor do
  import Nx

  def normalize(image) when is_struct(image, Nx.Tensor) do
    image_f = as_type(image, :f32)
    mean    = mean(image_f)
    std     = standard_deviation(image_f)

    # std == 0 significa imagen constante (todos mismos píxeles)
    if Nx.to_number(std) < 1.0e-7 do
      {:error, :constant_image}
    else
      {:ok, divide(subtract(image_f, mean), std)}
    end
  end

  def normalize(_), do: {:error, :not_a_tensor}

  def normalize_per_channel(image) when is_struct(image, Nx.Tensor) do
    rank = Nx.rank(image)
    unless rank in [3, 4], do: raise ArgumentError, "expected rank 3 or 4"

    image_f = as_type(image, :f32)

    # Reducir todos los ejes excepto el último (canales)
    reduce_axes = Enum.to_list(0..(rank - 2))

    channel_mean = mean(image_f, axes: reduce_axes, keep_axes: true)
    channel_std  = standard_deviation(image_f, axes: reduce_axes, keep_axes: true)

    # Broadcasting automático porque keep_axes: true preserva shape con 1s
    {:ok, divide(subtract(image_f, channel_mean), channel_std)}
  end

  def to_float(image) when is_struct(image, Nx.Tensor) do
    unless Nx.type(image) == {:u, 8},
      do: raise ArgumentError, "expected u8 tensor"

    image
    |> as_type(:f32)
    |> divide(255.0)
  end
end
```

### Ejercicio 2

```elixir
defmodule LinearRegression do
  import Nx.Defn

  defn predict(x, weights, bias) do
    Nx.dot(x, weights) + bias
  end

  defn mse_loss(y_pred, y_true) do
    Nx.mean(Nx.power(y_pred - y_true, 2))
  end

  defn train_step(x, y_true, weights, bias, learning_rate) do
    y_pred = predict(x, weights, bias)
    loss   = mse_loss(y_pred, y_true)

    n        = Nx.size(y_true) |> Nx.as_type(:f32)
    error    = y_pred - y_true

    grad_w = Nx.dot(Nx.transpose(x), error) * (2.0 / n)
    grad_b = Nx.mean(error) * 2.0

    new_w = weights - learning_rate * grad_w
    new_b = bias    - learning_rate * grad_b

    {new_w, new_b, loss}
  end

  def train(x, y_true, epochs \\ 1000, learning_rate \\ 0.01) do
    {_batch, features} = Nx.shape(x)
    {_batch, outputs}  = Nx.shape(y_true)

    weights = Nx.zeros({features, outputs})
    bias    = Nx.zeros({outputs})
    lr      = Nx.tensor(learning_rate, type: :f32)

    Enum.reduce(1..epochs, {weights, bias, []}, fn _epoch, {w, b, losses} ->
      {new_w, new_b, loss} = train_step(x, y_true, w, b, lr)
      {new_w, new_b, [Nx.to_number(loss) | losses]}
    end)
    |> then(fn {w, b, losses} -> {w, b, Enum.reverse(losses)} end)
  end
end
```

### Ejercicio 3

```elixir
defmodule BatchProcessor do
  import Nx.Defn

  def process_batches(data, batch_size, transform_fn) do
    n_items  = Nx.axis_size(data, 0)
    indices  = Enum.to_list(0..(n_items - 1))
    batches  = Enum.chunk_every(indices, batch_size)

    {time_us, results} = :timer.tc(fn ->
      batches
      |> Enum.map(fn idx ->
        batch = Nx.take(data, Nx.tensor(idx))
        transform_fn.(batch)
      end)
      |> Nx.concatenate(axis: 0)
    end)

    time_ms    = time_us / 1000
    throughput = n_items / (time_us / 1_000_000)

    {results, time_ms, throughput}
  end

  defn preprocess_batch(batch) do
    imagenet_mean = Nx.tensor([0.485, 0.456, 0.406], type: :f32)
    imagenet_std  = Nx.tensor([0.229, 0.224, 0.225], type: :f32)

    # {B, H, W, C} → normalizar a [0,1]
    normalized = Nx.as_type(batch, :f32) / 255.0

    # Reshape stats para broadcasting: {1, 1, 1, 3}
    mean = Nx.reshape(imagenet_mean, {1, 1, 1, 3})
    std  = Nx.reshape(imagenet_std,  {1, 1, 1, 3})

    (normalized - mean) / std
  end

  def generate_images(n, height \\ 224, width \\ 224, channels \\ 3) do
    Nx.random_uniform({n, height, width, channels}, 0, 255, type: :u8)
  end
end
```

</details>

---

## Preguntas de Reflexión

1. ¿Por qué `defn` mejora el rendimiento respecto a llamar funciones Nx directamente
   desde código Elixir normal? ¿Qué hace XLA bajo el capó?

2. En el ejercicio de batch processing, ¿por qué `Nx.take` es preferible a
   construir sub-tensores con slicing manual para índices no contiguos?

3. ¿Cuándo usarías `:bf16` en lugar de `:f32`? ¿Qué se pierde y qué se gana?

4. El gradiente de MSE que calculamos es el gradiente analítico manual.
   ¿Cómo lo haría Nx/Axon automáticamente con `Nx.Defn.grad/2`?

---

## Recursos

- [Nx HexDocs](https://hexdocs.pm/nx/Nx.html)
- [Nx.Defn — Numerical Definitions](https://hexdocs.pm/nx/Nx.Defn.html)
- [EXLA Backend](https://hexdocs.pm/exla/EXLA.html)
- [Sean Moriarity — Machine Learning in Elixir (PragProg)](https://pragprog.com/titles/smelixir/machine-learning-in-elixir/)
