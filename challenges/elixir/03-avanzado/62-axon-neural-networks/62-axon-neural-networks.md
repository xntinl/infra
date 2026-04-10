# Ejercicio 62: Axon — Redes Neuronales en Elixir

## Objetivo

Construir, entrenar y evaluar redes neuronales con `Axon`: desde un clasificador
simple hasta un custom training loop con gradient accumulation.

## Conceptos Clave

- `Axon.input/2`, `Axon.dense/3`, `Axon.relu/1`, `Axon.softmax/1`, `Axon.dropout/2`
- `Axon.Loop.trainer/3`: orquesta optimizer, loss function y métricas
- `Axon.Loop.run/3`: ejecuta el training loop sobre batches de datos
- `Axon.serialize/1` / `Axon.deserialize/1`: persistencia de modelos
- Transfer learning: `Axon.freeze/2` para congelar capas
- Custom layers: `Axon.layer/3` para definir operaciones arbitrarias

---

## Setup

```elixir
# mix.exs
defp deps do
  [
    {:nx,   "~> 0.7"},
    {:axon, "~> 0.6"},
    {:exla, "~> 0.7"}   # aceleración CPU/GPU
  ]
end
```

```elixir
# config/config.exs
config :nx, default_backend: EXLA.Backend
```

---

## Conceptos Previos: Anatomía de un Modelo Axon

En Axon, un modelo es un `%Axon{}` struct que describe el grafo computacional.
No es un proceso — es una descripción que se compila cuando se entrena o se predice.

```
Input → Dense(128) → ReLU → Dropout(0.5) → Dense(10) → Softmax
  ↑                                                         ↑
{nil, 784}                                             {nil, 10}
(batch, features)                                  (batch, clases)
```

- `nil` en el shape indica dimensión dinámica (el batch size varía)
- Axon infiere shapes automáticamente propagando a través del grafo
- `Axon.Loop.trainer` construye el training loop con optimizer y loss

---

## Parte 1: Conceptos de Capas

```elixir
# Dense: y = activation(xW + b)
Axon.dense(3, activation: :relu)   # 3 unidades de salida
Axon.dense(10)                     # sin activación (lineal)

# Activaciones independientes
Axon.relu(x)
Axon.sigmoid(x)
Axon.softmax(x)                    # para clasificación multiclase
Axon.tanh(x)

# Regularización
Axon.dropout(x, rate: 0.5)        # desactiva 50% en training, off en inference

# Normalización
Axon.batch_norm(x)

# Inspeccionar el modelo (sin datos aún)
Axon.Display.as_table(model, Nx.template({1, 784}, :f32))
```

---

## Parte 2: Optimizadores y Funciones de Loss

```elixir
# Optimizadores (de Polaris, incluido con Axon)
optimizer = Axon.Optimizers.adam(0.001)
optimizer = Axon.Optimizers.sgd(0.01, momentum: 0.9)
optimizer = Axon.Optimizers.rmsprop(0.001)

# Loss functions
loss = :categorical_cross_entropy   # clasificación multiclase (con softmax)
loss = :binary_cross_entropy        # clasificación binaria (con sigmoid)
loss = :mean_squared_error          # regresión
loss = :mean_absolute_error         # regresión robusta a outliers

# Loss customizada
custom_loss = fn y_pred, y_true ->
  Nx.mean(Nx.power(y_pred - y_true, 2)) * 0.5
end
```

---

## Ejercicio 1: Clasificador MNIST

**Contexto**: MNIST tiene 60.000 imágenes de dígitos escritos a mano (28×28 px,
grayscale). La tarea es clasificar cada imagen en una de 10 clases (0-9).

**Tu tarea**: Completa el módulo `MnistClassifier`.

```elixir
defmodule MnistClassifier do
  @doc """
  Define la arquitectura del modelo.
  Input: imágenes aplanadas de 784 píxeles.
  Output: probabilidades sobre 10 clases.
  """
  def build_model do
    # TODO: Axon.input con shape {nil, 784} y name "images"
    # TODO: Dense(128) con relu
    # TODO: Dropout(0.5) para regularización
    # TODO: Dense(64) con relu
    # TODO: Dense(10) con softmax
  end

  @doc """
  Configura y devuelve el training loop.
  """
  def build_loop(model) do
    # TODO: crear loop con Axon.Loop.trainer/3
    #   - model: el modelo de build_model/0
    #   - loss: :categorical_cross_entropy
    #   - optimizer: adam con lr 0.001
    # TODO: agregar métrica de accuracy con Axon.Loop.metric/3
    # TODO: devolver el loop configurado
  end

  @doc """
  Entrena el modelo y devuelve {model_state, history}.
  data debe ser un stream de {%{"images" => x, "labels" => y}}.
  """
  def train(model, loop, data, epochs \\ 10) do
    # TODO: usar Axon.Loop.run/4 con epochs: epochs
    # TODO: capturar y devolver el estado entrenado
  end

  @doc """
  Evalúa accuracy sobre un batch de test.
  Devuelve accuracy como float entre 0.0 y 1.0.
  """
  def evaluate(model, model_state, x_test, y_test) do
    # TODO: obtener predicciones con Axon.predict/4
    # TODO: y_pred tiene shape {batch, 10}: tomar argmax de axis 1
    # TODO: y_test también debe convertirse a labels (argmax si one-hot)
    # TODO: calcular fracción correctas
  end
end
```

**Preparar datos** (sin descargar MNIST real, usamos datos sintéticos para el ejercicio):

```elixir
defmodule MnistData do
  @doc """
  Genera datos sintéticos con la misma forma que MNIST.
  En producción usarías Scidata.MNIST o similar.
  """
  def synthetic_dataset(n_samples) do
    x = Nx.random_uniform({n_samples, 784}, 0.0, 1.0, type: :f32)
    # Labels one-hot: {n_samples, 10}
    labels = Enum.map(1..n_samples, fn _ -> :rand.uniform(10) - 1 end)
    y = Nx.tensor(labels) |> one_hot(num_classes: 10)
    {x, y}
  end

  defp one_hot(labels, num_classes: n) do
    # Convierte [0,3,7,...] a [[1,0,...,0],[0,0,0,1,...],...]
    batch = Nx.size(labels)
    eye   = Nx.eye(n, type: :f32)
    Nx.take(eye, labels)
    |> Nx.reshape({batch, n})
  end

  @doc """
  Convierte tensores x,y a stream de batches para Axon.Loop.
  """
  def to_batches(x, y, batch_size \\ 32) do
    n = Nx.axis_size(x, 0)
    Enum.map(Enum.chunk_every(0..(n-1), batch_size), fn idx ->
      batch_idx = Nx.tensor(idx)
      %{
        "images" => Nx.take(x, batch_idx),
        "labels" => Nx.take(y, batch_idx)
      }
    end)
  end
end
```

**Uso esperado**:

```elixir
{x_train, y_train} = MnistData.synthetic_dataset(5000)
{x_test,  y_test}  = MnistData.synthetic_dataset(1000)

model  = MnistClassifier.build_model()
loop   = MnistClassifier.build_loop(model)
train_data = MnistData.to_batches(x_train, y_train)

{model_state, _} = MnistClassifier.train(model, loop, train_data, 5)

acc = MnistClassifier.evaluate(model, model_state, x_test, y_test)
IO.puts("Test accuracy: #{Float.round(acc * 100, 2)}%")
# Con datos sintéticos (aleatorios) → ~10% (aleatorio)
# Con MNIST real → >97% después de 10 epochs
```

---

## Ejercicio 2: Transfer Learning — Congelar Capas

**Contexto**: Transfer learning = tomar un modelo preentrenado, congelar sus
capas (no actualizar sus pesos) y añadir nuevas capas que sí se entrenan.
Útil cuando tienes pocos datos propios pero existe un modelo preentrenado en
un dominio similar.

```elixir
defmodule TransferLearning do
  @doc """
  Simula un modelo "preentrenado" con pesos inicializados.
  En producción cargarías desde disco con Axon.deserialize/1.
  """
  def load_pretrained do
    model =
      Axon.input("features", shape: {nil, 256})
      |> Axon.dense(128, activation: :relu, name: "pretrained_dense_1")
      |> Axon.dense(64, activation: :relu, name: "pretrained_dense_2")

    # Simular pesos "preentrenados"
    {init_fn, _predict_fn} = Axon.build(model)
    params = init_fn.(Nx.template({1, 256}, :f32), %{})

    {model, params}
  end

  @doc """
  Construye modelo con transfer learning:
  1. Toma el backbone preentrenado
  2. Congela todas sus capas
  3. Añade nueva cabeza de clasificación para 5 clases

  Devuelve {full_model, frozen_params}.
  """
  def build_transfer_model(backbone, pretrained_params) do
    # TODO: congelar el backbone con Axon.freeze/1
    #   Axon.freeze/1 acepta el modelo y devuelve modelo con capas congeladas

    # TODO: añadir nueva cabeza sobre el backbone congelado:
    #   - Dense(32) con relu
    #   - Dense(5) con softmax (5 clases nuevas)

    # TODO: devolver {full_model, pretrained_params}
    # Nota: los params del backbone se pasan como params iniciales.
    #   Axon.Loop.trainer los respeta y no los actualiza en las capas congeladas.
  end

  @doc """
  Entrena solo la cabeza nueva, manteniendo el backbone congelado.
  Devuelve model_state entrenado.
  """
  def fine_tune(model, initial_params, data, epochs \\ 5) do
    # TODO: configurar loop con Axon.Loop.trainer
    # TODO: pasar initial_params a Axon.Loop.run como params iniciales
    #   Axon.Loop.run(loop, data, initial_params, epochs: epochs)
  end
end
```

**Lo que demuestras**: con `Axon.freeze`, solo las capas de la "cabeza" reciben
gradientes — el backbone mantiene sus pesos intactos.

---

## Ejercicio 3: Custom Training Loop con Gradient Accumulation

**Contexto**: Cuando el batch es demasiado grande para caber en memoria,
gradient accumulation divide el batch en micro-batches, acumula gradientes
de cada uno y aplica el update una sola vez. Equivale matemáticamente a
un batch completo.

```elixir
defmodule CustomTrainer do
  import Nx.Defn

  @doc """
  Un paso de forward + backward sobre un micro-batch.
  Devuelve {loss, gradients}.
  """
  defn compute_gradients(model_predict_fn, params, x, y_true) do
    # TODO: usar Nx.Defn.value_and_grad/2 para obtener loss y grads en un paso
    # value_and_grad calcula tanto el valor como el gradiente de una función
    Nx.Defn.value_and_grad(params, fn p ->
      y_pred = model_predict_fn.(p, x)
      categorical_cross_entropy(y_pred, y_true)
    end)
  end

  defn categorical_cross_entropy(y_pred, y_true) do
    # TODO: implementar -mean(sum(y_true * log(y_pred + epsilon), axis: 1))
    # epsilon = 1.0e-7 para estabilidad numérica
  end

  @doc """
  Acumula gradientes sobre `accumulation_steps` micro-batches
  y aplica el update una sola vez.

  Simula un effective_batch_size = batch_size * accumulation_steps.
  """
  def accumulate_and_update(model, params, optimizer_state, micro_batches, optimizer) do
    # TODO: inicializar grads acumulados con Nx.zeros_like(params)
    # TODO: para cada micro-batch: llamar compute_gradients y sumar al acumulado
    # TODO: promediar grads dividiendo por número de micro-batches
    # TODO: aplicar optimizer update: optimizer.update(grads_avg, optimizer_state, params)
    # TODO: devolver {new_params, new_optimizer_state, avg_loss}
  end

  @doc """
  Training loop manual completo con gradient accumulation.
  """
  def train(model, data, opts \\ []) do
    epochs             = Keyword.get(opts, :epochs, 10)
    accumulation_steps = Keyword.get(opts, :accumulation_steps, 4)
    learning_rate      = Keyword.get(opts, :learning_rate, 0.001)

    # TODO: inicializar params con Axon.build
    # TODO: inicializar optimizer (adam)
    # TODO: dividir data en chunks de accumulation_steps
    # TODO: loop principal: para cada chunk de micro-batches:
    #   - llamar accumulate_and_update/5
    #   - loggear loss cada N pasos
    # TODO: devolver params finales
  end
end
```

**Lo que demuestras vs Axon.Loop.trainer**:

| Aspecto                    | `Axon.Loop.trainer`     | Custom Loop              |
|----------------------------|-------------------------|--------------------------|
| Simplicidad                | Alta                    | Media                    |
| Control sobre gradientes   | Limitado                | Total                    |
| Gradient accumulation      | No incluido             | Implementable            |
| Gradient clipping manual   | Posible con callbacks   | Directo                  |
| Mixed precision custom     | Difícil                 | Controlado               |

---

## Solución de Referencia

<details>
<summary>Ver solución (intenta resolver primero)</summary>

### Ejercicio 1

```elixir
defmodule MnistClassifier do
  def build_model do
    Axon.input("images", shape: {nil, 784})
    |> Axon.dense(128, activation: :relu)
    |> Axon.dropout(rate: 0.5)
    |> Axon.dense(64, activation: :relu)
    |> Axon.dense(10, activation: :softmax)
  end

  def build_loop(model) do
    model
    |> Axon.Loop.trainer(:categorical_cross_entropy, Axon.Optimizers.adam(0.001))
    |> Axon.Loop.metric(:accuracy)
  end

  def train(model, loop, data, epochs \\ 10) do
    Axon.Loop.run(loop, data, %{}, epochs: epochs)
  end

  def evaluate(model, model_state, x_test, y_test) do
    y_pred   = Axon.predict(model, model_state, %{"images" => x_test})
    pred_cls = Nx.argmax(y_pred, axis: 1)

    # Si y_test es one-hot, convertir a labels
    true_cls =
      if Nx.rank(y_test) == 2,
        do: Nx.argmax(y_test, axis: 1),
        else: y_test

    correct = Nx.sum(Nx.equal(pred_cls, true_cls)) |> Nx.to_number()
    total   = Nx.axis_size(x_test, 0)
    correct / total
  end
end
```

### Ejercicio 2

```elixir
defmodule TransferLearning do
  def load_pretrained do
    model =
      Axon.input("features", shape: {nil, 256})
      |> Axon.dense(128, activation: :relu, name: "pretrained_dense_1")
      |> Axon.dense(64,  activation: :relu, name: "pretrained_dense_2")

    {init_fn, _} = Axon.build(model)
    params = init_fn.(Nx.template({1, 256}, :f32), %{})
    {model, params}
  end

  def build_transfer_model(backbone, pretrained_params) do
    full_model =
      backbone
      |> Axon.freeze()
      |> Axon.dense(32, activation: :relu, name: "head_dense")
      |> Axon.dense(5,  activation: :softmax, name: "head_output")

    {full_model, pretrained_params}
  end

  def fine_tune(model, initial_params, data, epochs \\ 5) do
    loop =
      model
      |> Axon.Loop.trainer(:categorical_cross_entropy, Axon.Optimizers.adam(0.0001))
      |> Axon.Loop.metric(:accuracy)

    # Pasamos los params preentrenados como estado inicial
    Axon.Loop.run(loop, data, initial_params, epochs: epochs)
  end
end
```

### Ejercicio 3

```elixir
defmodule CustomTrainer do
  import Nx.Defn

  defn compute_gradients(model_predict_fn, params, x, y_true) do
    Nx.Defn.value_and_grad(params, fn p ->
      y_pred = model_predict_fn.(p, x)
      categorical_cross_entropy(y_pred, y_true)
    end)
  end

  defn categorical_cross_entropy(y_pred, y_true) do
    eps = Nx.tensor(1.0e-7, type: :f32)
    -Nx.mean(Nx.sum(y_true * Nx.log(y_pred + eps), axes: [1]))
  end

  def accumulate_and_update(predict_fn, params, optimizer_state, micro_batches, optimizer) do
    n_micro = length(micro_batches)

    {accumulated_grads, total_loss} =
      Enum.reduce(micro_batches, {nil, 0.0}, fn {x, y}, {acc_grads, acc_loss} ->
        {loss, grads} = compute_gradients(predict_fn, params, x, y)
        loss_val = Nx.to_number(loss)

        new_acc =
          if acc_grads == nil,
            do: grads,
            else: deep_add(acc_grads, grads)

        {new_acc, acc_loss + loss_val}
      end)

    avg_grads = deep_scale(accumulated_grads, 1.0 / n_micro)
    avg_loss  = total_loss / n_micro

    {updates, new_opt_state} = optimizer.update.(avg_grads, optimizer_state, params)
    new_params = Axon.Updates.apply_updates(params, updates)

    {new_params, new_opt_state, avg_loss}
  end

  # Suma recursiva de nested maps de tensores
  defp deep_add(a, b) when is_map(a) do
    Map.merge(a, b, fn _k, va, vb -> deep_add(va, vb) end)
  end
  defp deep_add(a, b), do: Nx.add(a, b)

  defp deep_scale(grads, factor) when is_map(grads) do
    Map.new(grads, fn {k, v} -> {k, deep_scale(v, factor)} end)
  end
  defp deep_scale(tensor, factor), do: Nx.multiply(tensor, factor)

  def train(model, data, opts \\ []) do
    epochs             = Keyword.get(opts, :epochs, 10)
    accumulation_steps = Keyword.get(opts, :accumulation_steps, 4)
    learning_rate      = Keyword.get(opts, :learning_rate, 0.001)

    {init_fn, predict_fn} = Axon.build(model, mode: :train)
    params = init_fn.(Nx.template({1, 784}, :f32), %{})

    {optimizer_init, _optimizer_update} = Axon.Optimizers.adam(learning_rate)
    opt_state = optimizer_init.(params)
    optimizer = %{update: fn g, s, p -> Axon.Optimizers.adam(learning_rate) |> elem(1).(g, s, p) end}

    Enum.reduce(1..epochs, {params, opt_state}, fn epoch, {p, os} ->
      chunks = Enum.chunk_every(data, accumulation_steps)

      {final_p, final_os} =
        Enum.reduce(chunks, {p, os}, fn chunk, {cp, cos} ->
          {new_p, new_os, loss} =
            accumulate_and_update(predict_fn, cp, cos, chunk, optimizer)
          IO.puts("epoch #{epoch} | loss: #{Float.round(loss, 4)}")
          {new_p, new_os}
        end)

      {final_p, final_os}
    end)
    |> elem(0)
  end
end
```

</details>

---

## Serializar y Cargar Modelos

```elixir
# Serializar (guardar)
model_state = Axon.Loop.run(loop, data, %{}, epochs: 10)

serialized = Axon.serialize(model, model_state)
File.write!("mnist_model.axon", serialized)

# Deserializar (cargar)
{model, loaded_state} =
  File.read!("mnist_model.axon")
  |> Axon.deserialize()

# Inferencia con modelo cargado
Axon.predict(model, loaded_state, %{"images" => x_new})
```

---

## Custom Layer con Axon.layer

```elixir
# Capa personalizada: Scaled Dot-Product Attention simplificada
defmodule Layers do
  import Nx.Defn

  defn scaled_dot_attention(query, key, value) do
    d_k    = Nx.axis_size(query, -1) |> Nx.as_type(:f32)
    scores = Nx.dot(query, Nx.transpose(key)) / Nx.sqrt(d_k)
    weights = Nx.exp(scores) / Nx.sum(Nx.exp(scores), axes: [-1], keep_axes: true)
    Nx.dot(weights, value)
  end

  def attention_layer(query, key, value) do
    Axon.layer(
      &scaled_dot_attention/4,   # función con arity = inputs + 1 (opts)
      [query, key, value],
      name: "scaled_dot_attention"
    )
  end
end
```

---

## Preguntas de Reflexión

1. ¿Por qué `Axon.dropout` se desactiva automáticamente durante inference?
   ¿Cómo sabe Axon si está en modo `:train` o `:inference`?

2. `Axon.freeze/1` evita que los gradientes fluyan hacia atrás a través de
   las capas congeladas. ¿Qué operación matemática hace esto? ¿Es equivalente
   a `stop_gradient` en TensorFlow/JAX?

3. En gradient accumulation, dividimos los gradientes acumulados por el número
   de micro-batches. ¿Es esto matemáticamente equivalente al gradiente de la
   loss sobre el batch completo? ¿Bajo qué condiciones?

4. ¿Qué ventaja tiene usar `Nx.Defn.value_and_grad/2` respecto a calcular
   la loss y los gradientes en dos llamadas separadas?

---

## Recursos

- [Axon HexDocs](https://hexdocs.pm/axon/Axon.html)
- [Axon.Loop — Training Loops](https://hexdocs.pm/axon/Axon.Loop.html)
- [Axon GitHub examples](https://github.com/elixir-nx/axon/tree/main/examples)
- [Bumblebee — modelos preentrenados Hugging Face en Elixir](https://hexdocs.pm/bumblebee)
- [Sean Moriarity — Machine Learning in Elixir (PragProg)](https://pragprog.com/titles/smelixir/machine-learning-in-elixir/)
