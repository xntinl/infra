# AI Inference Engine

**Project**: `inference_engine` — Elixir ML inference runtime for ONNX models with int8 quantization

## Project context

Your team maintains a real-time fraud detection pipeline. The data science team delivers trained models as ONNX files. Currently, every inference request is forwarded to a Python microservice that runs PyTorch. That service adds 30–80ms of network latency, requires a separate deployment, and cannot share BEAM process memory with your Elixir application.

The ask is clear: load a trained model directly in the Elixir process and run inference without any network hop. The models are convolutional classifiers — five to ten layers, weights in the hundreds of MB range. Accuracy must match the Python service to within 2%.

You will build `InferenceEngine`: a pure-Elixir ML inference runtime that loads a subset of ONNX, runs forward passes, and supports int8 quantization for faster throughput.

## Project Structure

```
inference_engine/
├── mix.exs
├── lib/
│   ├── inference_engine/
│   │   ├── tensor.ex          # N-dimensional tensor with binary storage
│   │   ├── ops.ex             # Element-wise ops: relu, sigmoid, softmax, batch_norm
│   │   ├── conv2d.ex          # im2col-based Conv2D in NHWC format
│   │   ├── matmul.ex          # Matrix multiplication with optional parallelism
│   │   ├── model.ex           # Graph representation: list of {op, weights, config}
│   │   ├── loader.ex          # ONNX-subset binary parser
│   │   ├── quantize.ex        # Symmetric int8 quantization and dequantization
│   │   └── engine.ex          # infer/2 entry point with Task.async_stream batching
│   └── inference_engine.ex
├── test/
│   ├── tensor_test.exs
│   ├── conv2d_test.exs
│   ├── ops_test.exs
│   ├── quantize_test.exs
│   └── engine_test.exs
└── bench/
    └── forward_pass.exs       # Benchee benchmark: 224×224×3 forward pass
```

## Why build tensors on top of Elixir binaries

A `%Tensor{}` struct stores data as a raw Elixir binary: `<<f1::float-32-native, f2::float-32-native, ...>>`. This is the only zero-copy representation available in Elixir. Binaries larger than 64 bytes live on the heap and are reference-counted — sub-binaries share the parent's storage via `:binary.part/3`. This means `reshape/2` and `slice/3` are O(1) pointer operations, not copies.

The alternative — a list of floats — requires pointer-chasing per element and cannot be passed efficiently to NIFs or `:erlang.nif_error/1` wrappers.

## Why im2col for convolution

A naive Conv2D is four nested loops: batch, output row, output column, kernel. For a 224×224×3 input with 64 3×3 filters this is ~27M multiply-accumulate operations per image. In Elixir's immutable-value model, each accumulation creates a new float on the heap.

`im2col` transforms the convolution into a single matrix multiplication. For each output position, it copies the corresponding input patch into a row of a "column matrix." The convolution then becomes `output = im2col_matrix × kernel_matrix`. Matrix multiplication benefits from cache locality and can be parallelized across rows with `Task.async_stream`. The trade-off: im2col uses 3–5× more memory during forward pass (the column matrix is large), but this is acceptable for batch sizes of one or a small number of images.

## Why int8 quantization

Float32 arithmetic requires 4 bytes per weight and uses the FPU. Int8 uses 1 byte, fits 4× more weights in cache, and integer multiply is cheaper than float multiply on many CPUs. Symmetric quantization maps the float range `[-max_abs, max_abs]` to `[-127, 127]` with a single scale factor per tensor. This means dequantization is one multiplication, and the quantization error is bounded by `max_abs / 127`.

For fraud detection classifiers, empirical accuracy loss is typically under 1% on calibration data. For safety-critical models, post-training quantization of this type is not appropriate without careful per-layer calibration data.

### Step 1: Tensor type

```elixir
defmodule InferenceEngine.Tensor do
  @enforce_keys [:data, :shape, :dtype]
  defstruct [:data, :shape, :dtype]

  @type dtype :: :f32 | :i8
  @type t :: %__MODULE__{
    data: binary(),
    shape: [non_neg_integer()],
    dtype: dtype()
  }

  @doc "Create a tensor from a list of floats with the given shape"
  def from_list(list, shape, dtype \\ :f32) do
    # TODO: validate that length(list) == product of shape dimensions
    # TODO: encode each element as float-32-native or signed-8 depending on dtype
    # HINT: for f32: Enum.reduce(list, <<>>, fn x, acc -> acc <> <<x::float-32-native>> end)
    # HINT: for i8: <<round(x)::signed-8>>
  end

  @doc "Return the element at the given flat index"
  def at(%__MODULE__{data: data, dtype: :f32}, index) do
    # TODO: skip index * 4 bytes, read one float-32-native
    # HINT: <<_::binary-size(offset), value::float-32-native, _::binary>> = data
  end

  @doc "Element-wise add with NumPy-style broadcasting"
  def add(%__MODULE__{shape: sa} = a, %__MODULE__{shape: sb} = b) do
    # TODO: compute broadcast shape; raise if shapes are incompatible
    # TODO: iterate over output indices, compute a_index and b_index using broadcast strides
    # HINT: broadcast strides: if a dim == 1, the stride for that axis is 0
  end

  @doc "Element-wise multiply with broadcasting"
  def mul(a, b) do
    # TODO: same pattern as add/2 but multiply instead of add
  end

  @doc "Matrix multiply: both tensors must be rank-2"
  def matmul(%__MODULE__{shape: [m, k], dtype: :f32} = a, %__MODULE__{shape: [k2, n], dtype: :f32} = b)
      when k == k2 do
    # TODO: produce a [m, n] tensor
    # TODO: for each (i, j): sum over k of a[i,k] * b[k,j]
    # HINT: inner loop can be expressed as Enum.reduce(0..(k-1), 0.0, fn ...)
    # HINT: then encode results row-major into binary
  end

  def matmul(%__MODULE__{shape: sa}, %__MODULE__{shape: sb}) do
    raise ArgumentError, "matmul shape mismatch: #{inspect(sa)} × #{inspect(sb)}"
  end

  @doc "Reshape without copying data; total elements must be unchanged"
  def reshape(%__MODULE__{shape: old_shape} = t, new_shape) do
    # TODO: validate that product(old_shape) == product(new_shape)
    # TODO: return struct with same data binary and new shape
  end

  @doc "Transpose a 2D tensor (swap axes 0 and 1)"
  def transpose(%__MODULE__{shape: [rows, cols], data: data, dtype: :f32}) do
    # TODO: produce a [cols, rows] tensor
    # TODO: for each (r, c) in new layout: value = old[c, r]
    # HINT: new_index = r * rows + c, old_offset = (c * cols + r) * 4
  end

  # Internal helpers
  def num_elements(shape), do: Enum.product(shape)
  def bytes_per_element(:f32), do: 4
  def bytes_per_element(:i8), do: 1
end
```

### Step 2: Operators

```elixir
defmodule InferenceEngine.Ops do
  alias InferenceEngine.Tensor

  @doc "ReLU: max(0, x) element-wise"
  def relu(%Tensor{data: data, shape: shape, dtype: :f32}) do
    # TODO: iterate bytes in chunks of 4, decode float, apply max(0.0, x), re-encode
    # HINT: for bin << x::float-32-native, rest::binary >> -> recurse
  end

  @doc "Sigmoid: 1 / (1 + exp(-x))"
  def sigmoid(%Tensor{} = t) do
    # TODO: apply sigmoid element-wise using :math.exp/1
  end

  @doc "Softmax over the last axis; numerically stable via max subtraction"
  def softmax(%Tensor{shape: shape} = t) do
    # TODO: for each row (last axis), subtract max, compute exp, divide by sum
    # HINT: a 2D [batch, classes] tensor → apply softmax per row
    # HINT: numeric stability: subtract max(row) before exp
  end

  @doc "Batch normalization: (x - mean) / sqrt(var + eps) * gamma + beta"
  def batch_norm(%Tensor{} = x, %Tensor{} = gamma, %Tensor{} = beta, %Tensor{} = running_mean, %Tensor{} = running_var, eps \\ 1.0e-5) do
    # TODO: element-wise: normalized = (x - mean) / sqrt(var + eps)
    # TODO: then scale and shift: normalized * gamma + beta
    # HINT: gamma, beta, mean, var are all shape [channels]; broadcast over [N, H, W, C]
  end
end
```

### Step 3: Conv2D

```elixir
defmodule InferenceEngine.Conv2D do
  alias InferenceEngine.Tensor

  @doc """
  2D convolution in NHWC format.
  input: [N, H, W, C_in]
  kernel: [kH, kW, C_in, C_out]
  Returns: [N, out_H, out_W, C_out]
  """
  def forward(%Tensor{shape: [n, h, w, c_in]} = input,
              %Tensor{shape: [kh, kw, c_in2, c_out]} = kernel,
              stride: stride,
              padding: padding)
      when c_in == c_in2 do
    # TODO: compute padded input if padding == :same or padding > 0
    # TODO: compute out_H = div(H + 2*padding - kH, stride) + 1
    # TODO: build im2col matrix: shape [out_H * out_W, kH * kW * C_in]
    # TODO: reshape kernel to [kH * kW * C_in, C_out]
    # TODO: matmul(im2col, kernel_2d) → reshape to [N, out_H, out_W, C_out]
  end

  @doc "Extract im2col matrix for one sample"
  def im2col(%Tensor{shape: [h, w, c]} = input, kh, kw, out_h, out_w, stride) do
    # TODO: for each (oh, ow) output position:
    #   extract patch input[oh*stride .. oh*stride+kH, ow*stride .. ow*stride+kW, :]
    #   flatten patch to a row of length kH * kW * C
    # TODO: stack all rows → binary of shape [out_h * out_w, kh * kw * c]
    # HINT: use :binary.part(data, offset, length) to extract sub-binaries
  end
end
```

### Step 4: Model and loader

```elixir
defmodule InferenceEngine.Model do
  @type op :: :conv2d | :batch_norm | :relu | :sigmoid | :softmax | :flatten | :dense
  @type layer :: {op(), weights :: map(), config :: map()}

  defstruct layers: [], input_shape: nil

  @doc "Validate that layer output shapes chain correctly"
  def validate(%__MODULE__{layers: layers, input_shape: shape}) do
    # TODO: walk layers, compute output shape of each, raise on mismatch
    # TODO: return {:ok, output_shape} or {:error, reason, layer_index}
  end
end

defmodule InferenceEngine.Loader do
  alias InferenceEngine.{Model, Tensor}

  @doc """
  Load a model from a custom binary format.
  Header: <<magic::32, version::8, num_layers::32>>
  Each layer: <<op_type::8, config_len::32, config_json::binary-size(config_len),
                num_weights::8, [weight_header, weight_data]...>>
  """
  def load_binary(path) do
    # TODO: File.read!(path) → parse header → parse layers
    # TODO: for each layer, decode op_type byte to atom
    # TODO: decode config JSON → map
    # TODO: decode weight tensors from binary
    # TODO: return {:ok, %Model{}} or {:error, reason}
  end

  @doc "Load a minimal ONNX protobuf (Conv, Relu, Flatten, Gemm, Softmax only)"
  def load_onnx(path) do
    # TODO: parse protobuf binary manually (no protobuf library)
    # HINT: ONNX field IDs: model.graph=7, node.op_type=4, node.input=1, initializer=5
    # HINT: protobuf wire type 2 = length-delimited; wire type 5 = 32-bit
    # TODO: build %Model{} from parsed nodes and initializer tensors
  end
end
```

### Step 5: Quantization

```elixir
defmodule InferenceEngine.Quantize do
  alias InferenceEngine.Tensor

  @max_int8 127

  @doc """
  Symmetric per-tensor quantization.
  scale = max(abs(weights)) / 127
  quantized = clamp(round(x / scale), -127, 127)
  """
  def quantize_tensor(%Tensor{dtype: :f32} = t) do
    # TODO: find max abs value in tensor
    # TODO: compute scale
    # TODO: re-encode each float as i8 after rounding and clamping
    # TODO: return {%Tensor{dtype: :i8}, scale}
  end

  @doc "Dequantize int8 tensor back to float32 using stored scale"
  def dequantize_tensor(%Tensor{dtype: :i8} = t, scale) do
    # TODO: decode each signed-8 integer, multiply by scale, encode as float-32-native
  end

  @doc "Quantize all weight tensors in a model; return model with int8 weights and scale map"
  def quantize_model(%InferenceEngine.Model{} = model) do
    # TODO: walk model.layers, quantize each weight tensor
    # TODO: store scales in a map keyed by layer index and weight name
    # TODO: return {quantized_model, scales}
  end
end
```

### Step 6: Inference engine

```elixir
defmodule InferenceEngine.Engine do
  alias InferenceEngine.{Model, Tensor, Ops, Conv2D, Quantize}

  @doc """
  Run inference on a batch of input tensors.
  Returns results in the same order as inputs.
  """
  def infer(%Model{} = model, inputs, opts \\ []) when is_list(inputs) do
    concurrency = Keyword.get(opts, :concurrency, System.schedulers_online())

    # TODO: use Task.async_stream(inputs, fn input -> forward_pass(model, input) end,
    #       max_concurrency: concurrency, ordered: true)
    # TODO: collect results, preserving order
    # TODO: return {:ok, [%Tensor{}, ...]} or {:error, reason}
  end

  @doc "Run a single forward pass through the model"
  def forward_pass(%Model{layers: layers}, %Tensor{} = input) do
    Enum.reduce(layers, input, fn layer, acc ->
      apply_layer(layer, acc)
    end)
  end

  defp apply_layer({:conv2d, weights, config}, input) do
    # TODO: call Conv2D.forward/4 with kernel from weights, stride and padding from config
    # TODO: if config.activation == :relu, fuse: apply Ops.relu/1 after conv
  end

  defp apply_layer({:batch_norm, weights, _config}, input) do
    # TODO: call Ops.batch_norm/6 with gamma, beta, mean, var from weights
  end

  defp apply_layer({:relu, _weights, _config}, input), do: Ops.relu(input)

  defp apply_layer({:flatten, _weights, _config}, %Tensor{shape: [n | rest]} = input) do
    # TODO: reshape to [n, product(rest)]
  end

  defp apply_layer({:dense, weights, _config}, input) do
    # TODO: matmul(input, weights.kernel) then add weights.bias
    # TODO: apply activation if specified in config
  end

  defp apply_layer({:softmax, _weights, _config}, input), do: Ops.softmax(input)
end
```

## Given tests

```elixir
# test/tensor_test.exs
defmodule InferenceEngine.TensorTest do
  use ExUnit.Case, async: true
  alias InferenceEngine.Tensor

  test "from_list encodes and at/2 decodes correctly" do
    t = Tensor.from_list([1.0, 2.0, 3.0, 4.0], [2, 2])
    assert Tensor.at(t, 0) == 1.0
    assert Tensor.at(t, 3) == 4.0
  end

  test "add with broadcasting [3,1] + [3,4]" do
    a = Tensor.from_list([1.0, 2.0, 3.0], [3, 1])
    b = Tensor.from_list(List.duplicate(1.0, 12), [3, 4])
    result = Tensor.add(a, b)
    assert result.shape == [3, 4]
    # first row: 1+1, 1+1, 1+1, 1+1
    assert_in_delta Tensor.at(result, 0), 2.0, 1.0e-5
    assert_in_delta Tensor.at(result, 4), 3.0, 1.0e-5
  end

  test "matmul [2,3] x [3,2] = [2,2]" do
    a = Tensor.from_list([1.0, 2.0, 3.0, 4.0, 5.0, 6.0], [2, 3])
    b = Tensor.from_list([7.0, 8.0, 9.0, 10.0, 11.0, 12.0], [3, 2])
    result = Tensor.matmul(a, b)
    assert result.shape == [2, 2]
    # [1,2,3]·[7,9,11] = 58
    assert_in_delta Tensor.at(result, 0), 58.0, 1.0e-4
    # [1,2,3]·[8,10,12] = 64
    assert_in_delta Tensor.at(result, 1), 64.0, 1.0e-4
  end

  test "reshape preserves data, changes shape" do
    t = Tensor.from_list(Enum.map(1..6, &(&1 * 1.0)), [2, 3])
    r = Tensor.reshape(t, [3, 2])
    assert r.shape == [3, 2]
    assert r.data == t.data
  end

  test "matmul raises on incompatible shapes" do
    a = Tensor.from_list([1.0, 2.0], [1, 2])
    b = Tensor.from_list([1.0, 2.0], [1, 2])
    assert_raise ArgumentError, ~r/shape mismatch/, fn -> Tensor.matmul(a, b) end
  end
end

# test/ops_test.exs
defmodule InferenceEngine.OpsTest do
  use ExUnit.Case, async: true
  alias InferenceEngine.{Tensor, Ops}

  test "relu zeroes negatives" do
    t = Tensor.from_list([-1.0, 0.0, 1.0, 2.0], [4])
    result = Ops.relu(t)
    assert_in_delta Tensor.at(result, 0), 0.0, 1.0e-5
    assert_in_delta Tensor.at(result, 2), 1.0, 1.0e-5
  end

  test "softmax sums to 1.0 per row" do
    t = Tensor.from_list([1.0, 2.0, 3.0, 1.0, 1.0, 1.0], [2, 3])
    result = Ops.softmax(t)
    row0_sum = Tensor.at(result, 0) + Tensor.at(result, 1) + Tensor.at(result, 2)
    assert_in_delta row0_sum, 1.0, 1.0e-5
  end

  test "softmax is numerically stable with large values" do
    t = Tensor.from_list([1000.0, 1001.0, 1002.0], [1, 3])
    result = Ops.softmax(t)
    sum = Tensor.at(result, 0) + Tensor.at(result, 1) + Tensor.at(result, 2)
    assert_in_delta sum, 1.0, 1.0e-5
    # result should not contain NaN or Inf
    for i <- 0..2 do
      v = Tensor.at(result, i)
      assert v >= 0.0 and v <= 1.0
    end
  end
end

# test/conv2d_test.exs
defmodule InferenceEngine.Conv2DTest do
  use ExUnit.Case, async: true
  alias InferenceEngine.{Tensor, Conv2D}

  test "3x3 conv on 4x4x1 input with stride 1 no padding produces 2x2x1 output" do
    # Single channel 4x4 input filled with 1.0
    input = Tensor.from_list(List.duplicate(1.0, 16), [1, 4, 4, 1])
    # Single 3x3x1x1 kernel filled with 1.0 (sum of patch = 9.0)
    kernel = Tensor.from_list(List.duplicate(1.0, 9), [3, 3, 1, 1])
    result = Conv2D.forward(input, kernel, stride: 1, padding: 0)
    assert result.shape == [1, 2, 2, 1]
    assert_in_delta Tensor.at(result, 0), 9.0, 1.0e-3
  end
end

# test/quantize_test.exs
defmodule InferenceEngine.QuantizeTest do
  use ExUnit.Case, async: true
  alias InferenceEngine.{Tensor, Quantize}

  test "quantize then dequantize preserves values within 2% error" do
    floats = Enum.map(1..100, fn i -> (i - 50) * 0.1 end)
    t = Tensor.from_list(floats, [100])
    {qt, scale} = Quantize.quantize_tensor(t)
    assert qt.dtype == :i8
    recovered = Quantize.dequantize_tensor(qt, scale)
    # Check a few values
    for i <- [0, 25, 50, 75, 99] do
      original = Tensor.at(t, i)
      reconstructed = Tensor.at(recovered, i)
      if original != 0.0 do
        error = abs(original - reconstructed) / abs(original)
        assert error < 0.02, "error #{error} at index #{i}: #{original} vs #{reconstructed}"
      end
    end
  end
end

# test/engine_test.exs
defmodule InferenceEngine.EngineTest do
  use ExUnit.Case, async: true
  alias InferenceEngine.{Model, Tensor, Engine}

  defp tiny_model do
    # A minimal model: flatten → dense(4→2) → softmax
    # Input: [1, 2, 2] → flatten → [4] → dense → [2] → softmax
    kernel = Tensor.from_list([0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8], [4, 2])
    bias = Tensor.from_list([0.0, 0.0], [2])
    %Model{
      input_shape: [1, 2, 2],
      layers: [
        {:flatten, %{}, %{}},
        {:dense, %{kernel: kernel, bias: bias}, %{activation: nil}},
        {:softmax, %{}, %{}}
      ]
    }
  end

  test "forward pass produces output with correct shape" do
    model = tiny_model()
    input = Tensor.from_list([1.0, 2.0, 3.0, 4.0], [1, 2, 2])
    output = Engine.forward_pass(model, input)
    assert output.shape == [1, 2]
  end

  test "output of softmax layer sums to 1.0" do
    model = tiny_model()
    input = Tensor.from_list([1.0, 2.0, 3.0, 4.0], [1, 2, 2])
    output = Engine.forward_pass(model, input)
    sum = Tensor.at(output, 0) + Tensor.at(output, 1)
    assert_in_delta sum, 1.0, 1.0e-5
  end

  test "infer/2 returns results in input order" do
    model = tiny_model()
    inputs = Enum.map(1..5, fn i ->
      Tensor.from_list(Enum.map(1..4, fn j -> i * j * 1.0 end), [1, 2, 2])
    end)
    {:ok, results} = Engine.infer(model, inputs)
    assert length(results) == 5
    Enum.each(results, fn r -> assert r.shape == [1, 2] end)
  end

  test "batching overhead is under 20% for single-element vs direct forward_pass" do
    model = tiny_model()
    input = Tensor.from_list([1.0, 2.0, 3.0, 4.0], [1, 2, 2])

    direct_time =
      Enum.reduce(1..50, 0, fn _, acc ->
        {us, _} = :timer.tc(fn -> Engine.forward_pass(model, input) end)
        acc + us
      end) / 50

    batch_time =
      Enum.reduce(1..50, 0, fn _, acc ->
        {us, _} = :timer.tc(fn -> Engine.infer(model, [input]) end)
        acc + us
      end) / 50

    overhead = (batch_time - direct_time) / direct_time
    assert overhead < 0.20, "Task overhead #{Float.round(overhead * 100, 1)}% exceeds 20%"
  end
end
```

## Benchmark

```elixir
# bench/forward_pass.exs
# Run with: mix run bench/forward_pass.exs
alias InferenceEngine.{Tensor, Model, Engine}

# Build a 5-layer convolutional model:
# Conv(3,64,stride=1,pad=1) → BN → ReLU → Conv(64,128,stride=2,pad=1) → BN → ReLU
# → Flatten → Dense(128*56*56→256) → Dense(256→10) → Softmax
# Using random weights; correct shapes matter, correct values do not
random_kernel = fn shape ->
  n = Enum.product(shape)
  Tensor.from_list(Enum.map(1..n, fn _ -> :rand.normal() * 0.01 end), shape)
end

const_tensor = fn val, shape ->
  Tensor.from_list(List.duplicate(val, Enum.product(shape)), shape)
end

layers = [
  {:conv2d,
   %{kernel: random_kernel.([3, 3, 3, 32])},
   %{stride: 1, padding: 1, activation: :relu}},
  {:batch_norm,
   %{gamma: const_tensor.(1.0, [32]), beta: const_tensor.(0.0, [32]),
     mean: const_tensor.(0.0, [32]), var: const_tensor.(1.0, [32])},
   %{}},
  {:relu, %{}, %{}},
  {:conv2d,
   %{kernel: random_kernel.([3, 3, 32, 64])},
   %{stride: 2, padding: 1, activation: :relu}},
  {:flatten, %{}, %{}},
  {:dense,
   %{kernel: random_kernel.([64 * 112 * 112, 10]),
     bias: const_tensor.(0.0, [10])},
   %{activation: nil}},
  {:softmax, %{}, %{}}
]

model = %Model{input_shape: [1, 224, 224, 3], layers: layers}
input = Tensor.from_list(List.duplicate(0.5, 224 * 224 * 3), [1, 224, 224, 3])

# Warmup
Engine.forward_pass(model, input)

# Measure 10 iterations
times =
  Enum.map(1..10, fn _ ->
    {us, _} = :timer.tc(fn -> Engine.forward_pass(model, input) end)
    us / 1000.0
  end)

sorted = Enum.sort(times)
median = Enum.at(sorted, 4)
p95 = Enum.at(sorted, 9)  # with only 10 samples, p95 ≈ max

IO.puts("Median: #{Float.round(median, 1)} ms")
IO.puts("P95:    #{Float.round(p95, 1)} ms")
IO.puts("Target: < 100 ms")
IO.puts("Pass:   #{if median < 100, do: "YES", else: "NO — consider NIF for matmul"}")
```

## Trade-off analysis

| Approach | Throughput | Memory | Accuracy | Complexity |
|---|---|---|---|---|
| Pure Elixir floats | Low (baseline) | Moderate | Exact float32 | Low |
| im2col + matmul | 3–5× over naive | 3–5× higher (column matrix) | Exact float32 | Medium |
| Task.async_stream batching | ~N× for batch N | Linear in batch size | Same as single | Low |
| int8 symmetric quantization | ~2× matmul speed | 4× smaller weights | 98–99% of float32 | Medium |
| Rustler NIF for matmul | 10–50× over pure Elixir | Same | Exact float32 | High |
| Full Nx/EXLA | Maximum | CUDA-dependent | Same | Low (but external dep) |

## Common production mistakes

**Returning tensors with stale data references.** A common bug is returning a sub-binary view that holds a reference to a large parent binary, preventing GC. Use `:binary.copy/1` when the parent binary is a large loaded model file and you only need a small slice.

**Ignoring NHWC vs NCHW layout mismatches.** PyTorch defaults to NCHW; TensorFlow and ONNX often use NHWC. Feeding an NCHW tensor to an NHWC kernel silently produces wrong results with no error. Encode layout as part of the tensor struct and validate on every operator boundary.

**Not fusing Conv + BN + ReLU.** Running three separate passes over the output of a convolutional layer is 3× memory bandwidth. At inference time, batch norm parameters are constant and can be folded into the conv kernel (absorbed into weights and bias). This fusion eliminates the BN pass entirely.

**Quantizing activations with the wrong range.** Symmetric quantization assumes the distribution is centered at zero. For activations after ReLU (range [0, ∞)), asymmetric quantization (zero-point + scale) gives half the error for the same bit width. Using symmetric int8 for ReLU outputs wastes half the representable range.

**Using `Task.async_stream` with `ordered: true` when you don't need order.** `ordered: true` buffers completed tasks until all preceding tasks in the stream finish. For large batches with uneven compute times, this serializes collection. Use `ordered: false` and sort by a tag if order matters.

## Resources

- ONNX Operator Specification — https://onnx.ai/onnx/operators/ (reference for Conv, Gemm, BatchNormalization exact semantics)
- Goodfellow, Bengio, Courville — "Deep Learning" Chapter 9: Convolutional Networks (im2col derivation)
- Nagel et al. — "A White Paper on Neural Network Quantization" (2021) — Qualcomm Research (symmetric vs asymmetric, per-channel vs per-tensor)
- Chellapilla et al. — "High Performance Convolutional Neural Networks for Document Processing" (2006) — original im2col paper
- Brendan Gregg — "Systems Performance" Chapter 6: CPUs (cache locality and why matmul layout matters)
- Elixir `<<>>` binary syntax — https://hexdocs.pm/elixir/Kernel.SpecialForms.html#%3C%3C%3E%3E/1
