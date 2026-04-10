# 43. Build an AI Inference Engine

**Difficulty**: Insane

## Prerequisites

- Álgebra lineal: multiplicación de matrices, convoluciones, broadcasting
- Redes neuronales: forward pass, activaciones, batch normalization
- Manipulación eficiente de binarios y arrays en Elixir
- `:array` module de Erlang o NIF bindings para operaciones numéricas
- Comprensión del formato ONNX a nivel de protobuf
- Concurrencia: paralelización de operaciones con Task.async_stream
- Tipos numéricos: float32, int8, representación IEEE 754

## Problem Statement

Implementa un motor de inferencia de machine learning en Elixir puro, sin dependencias externas de ML (no Nx, no EXLA, no librerías de álgebra lineal). El motor debe ser capaz de cargar un modelo entrenado y ejecutar predicciones.

El núcleo del sistema es un tipo `Tensor`: un array N-dimensional de números de punto flotante con soporte para las operaciones fundamentales de redes neuronales. Sobre este tipo se construyen los operadores estándar de deep learning.

El motor carga modelos en un formato propio o en un subconjunto de ONNX (solo los operadores necesarios: Conv, Relu, BatchNormalization, Flatten, Gemm/MatMul, Softmax). Los pesos del modelo se cargan desde un archivo binario al inicio y permanecen inmutables durante la inferencia.

La optimización de rendimiento se logra mediante: (1) paralelización de operaciones independientes sobre un batch con `Task.async_stream`, (2) quantización int8 que reduce el tamaño del modelo 4× y acelera las multiplicaciones enteras, y (3) fusión de operadores (Conv + ReLU en un solo pase).

El benchmark objetivo es procesar una imagen 224×224×3 (ImageNet size) a través de una red tipo ResNet-18 simplificada (sin las conexiones residuales complejas si es necesario) en menos de 100ms en hardware de laptop moderno.

## Acceptance Criteria

- [ ] Tensor: estructura `%Tensor{data: binary, shape: [d1, d2, ...], dtype: :f32 | :i8}` con operaciones `add/2`, `mul/2` (element-wise con broadcasting), `matmul/2` (multiplicación matricial estándar), `reshape/2`, `transpose/2`; los errores de shape son detectados y reportados con las shapes involucradas
- [ ] Operators: `relu/1` (max(0, x) element-wise), `sigmoid/1`, `softmax/1` (sobre el último eje, numéricamente estable con substracción del máximo), `batch_norm/4` (con gamma, beta, mean, variance de los pesos del modelo)
- [ ] Conv2D: convolución 2D en formato NHWC (batch, height, width, channels) con kernel `[kH, kW, in_channels, out_channels]`, stride y padding configurables; la implementación puede ser `im2col` + matmul o el loop directo; debe producir resultados idénticos a numpy a 4 decimales de precisión
- [ ] Model loader: carga un archivo de pesos en formato propio (binary con header descriptivo: operator type, input/output shapes, weight data) o subconjunto de ONNX protobuf; construye un grafo de operadores como lista ordenada de `{op_type, weights, config}`; la topología del grafo es validada (shapes compatibles entre capas consecutivas)
- [ ] Batching: `infer(model, batch)` donde `batch` es una lista de tensores de entrada; procesa todos en paralelo con `Task.async_stream` con concurrency configurable; el resultado es una lista de tensores de salida en el mismo orden; el overhead de paralelización no penaliza lotes de un solo elemento más del 20%
- [ ] Quantization: `quantize_model/1` convierte los pesos float32 a int8 usando calibración simétrica (rango [-127, 127]); la inferencia int8 usa multiplicación entera con re-escalado al final de cada capa; el error de accuracy respecto al modelo float32 es menor del 2% en el dataset de validación de prueba
- [ ] Benchmark: una red convolucional de 5+ capas (Conv→BN→ReLU × 3, Flatten, Dense, Softmax) sobre input 224×224×3 completa forward pass en menos de 100ms en promedio sobre 10 iteraciones; el benchmark reporta: tiempo mediano, P95, P99, throughput en imágenes/segundo

## What You Will Learn

- Álgebra lineal implementada a mano: entender la convolución 2D a nivel de loops y por qué `im2col` la acelera
- Representación eficiente de tensores en Elixir: binarios, acceso por índice, broadcasting
- Quantization de modelos: por qué int8 es más rápido y cuánta precisión se sacrifica
- Grafos de cómputo: cómo un modelo de ML es un grafo de operaciones con dependencias
- Las limitaciones de Elixir para cómputo numérico intensivo y cómo mitigarlas
- Por qué proyectos como Nx/EXLA existen: la brecha entre Elixir idiomático y rendimiento numérico

## Hints

- Representa los datos del tensor como un binario Elixir: `<<f1::float-32-native, f2::float-32-native, ...>>`; acceso por índice requiere cálculo de offset según strides
- `im2col`: transforma la operación de convolución en una multiplicación de matrices; para cada posición de output, extrae el patch del input como una fila de la matriz; luego matmul con los pesos reshapeados
- Broadcasting: si `a.shape = [3, 1]` y `b.shape = [3, 4]`, expande `a` repitiendo a lo largo del eje con dimensión 1; implementa como paso de preprocessing antes de la operación element-wise
- Para el matmul eficiente: en Elixir puro, usa list comprehension con `Enum.zip`; para mejor rendimiento, considera un NIF mínimo solo para matmul (pero el ejercicio pide Elixir puro)
- Quantization simétrica: `scale = max(abs(weight)) / 127.0`; quantized = `round(weight / scale)` clipped a `[-127, 127]`; dequantize = `quantized * scale`
- El modelo de prueba puede ser definido manualmente en código Elixir como lista de capas con pesos aleatorios; no es necesario cargar un modelo entrenado real para verificar correctness

## Reference Material

- ONNX operator spec: https://onnx.ai/onnx/operators/
- "Neural Networks from Scratch" — Harrison Kinsley & Daniel Kukiela (implementación desde cero)
- "Deep Learning" — Goodfellow, Bengio, Courville (capítulo sobre convoluciones)
- "A Survey of Quantization Methods for Efficient Neural Network Inference" (paper de 2021)
- Numpy source code (implementación de referencia para comparar outputs)
- CppCon 2014: Mike Acton — "Data-Oriented Design and C++" (filosofía de layout de datos para rendimiento)

## Difficulty Rating ★★★★★★★

El mayor desafío es implementar operaciones numéricas eficientemente en un lenguaje diseñado para concurrencia, no para cómputo. La correctness es difícil de verificar sin una referencia externa. La quantización requiere entender el tradeoff precisión-rendimiento en profundidad. Alcanzar el benchmark de 100ms sin NIFs es el objetivo más ambicioso.

## Estimated Time

40–60 horas
