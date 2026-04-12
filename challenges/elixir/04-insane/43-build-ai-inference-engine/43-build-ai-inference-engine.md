# AI Inference Engine

**Project**: `inference_engine` — Elixir ML inference runtime for ONNX models with int8 quantization

## Project context

Your team maintains a real-time fraud detection pipeline. The data science team delivers trained models as ONNX files. Currently, every inference request is forwarded to a Python microservice that runs PyTorch. That service adds 30–80ms of network latency, requires a separate deployment, and cannot share BEAM process memory with your Elixir application.

The ask is clear: load a trained model directly in the Elixir process and run inference without any network hop. The models are convolutional classifiers — five to ten layers, weights in the hundreds of MB range. Accuracy must match the Python service to within 2%.

You will build `InferenceEngine`: a pure-Elixir ML inference runtime that loads a subset of ONNX, runs forward passes, and supports int8 quantization for faster throughput.

## Why NIFs via Nx and EXLA for tensor ops and not pure-Elixir tensor implementations

Elixir on the BEAM is not designed for tight numeric loops; it's 100-1000x slower than a BLAS kernel. NIFs let us call the actually-fast code while keeping the orchestration in Elixir where it belongs.

## Design decisions

**Option A — interpreted eager execution (each op dispatched individually)**
- Pros: simple, matches PyTorch defaults
- Cons: no kernel fusion, dispatch overhead dominates for small ops

**Option B — graph-level tracing + kernel fusion before execution** (chosen)
- Pros: fused ops amortize dispatch cost, unlocks operator-level optimizations
- Cons: first call pays a trace-and-compile tax

→ Chose **B** because for production inference the same model is called millions of times; a one-time trace cost is rounding error.

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

The tensor stores all numerical data as a flat binary. The shape is tracked separately. Element access uses byte offsets computed from the flat index and the byte width of the dtype. Broadcasting follows NumPy semantics: dimensions of size 1 are stretched to match the other operand along each axis.

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

  @doc """
  Create a tensor from a flat list of numbers with the given shape.
  Validates that the list length matches the product of shape dimensions,
  then encodes each element according to the dtype (:f32 or :i8).
  """
  @spec from_list([number()], [non_neg_integer()], dtype()) :: t()
  def from_list(list, shape, dtype \\ :f32) do
    expected = num_elements(shape)

    if length(list) != expected do
      raise ArgumentError,
        "list length #{length(list)} does not match shape #{inspect(shape)} (#{expected} elements)"
    end

    data =
      case dtype do
        :f32 ->
          Enum.reduce(list, <<>>, fn x, acc ->
            acc <> <<x * 1.0::float-32-native>>
          end)

        :i8 ->
          Enum.reduce(list, <<>>, fn x, acc ->
            acc <> <<round(x)::signed-8>>
          end)
      end

    %__MODULE__{data: data, shape: shape, dtype: dtype}
  end

  @doc """
  Return the element at the given flat index.
  For :f32 tensors, skips index * 4 bytes and reads one float-32-native.
  For :i8 tensors, skips index bytes and reads one signed-8.
  """
  @spec at(t(), non_neg_integer()) :: number()
  def at(%__MODULE__{data: data, dtype: :f32}, index) do
    offset = index * 4
    <<_::binary-size(offset), value::float-32-native, _::binary>> = data
    value
  end

  def at(%__MODULE__{data: data, dtype: :i8}, index) do
    <<_::binary-size(index), value::signed-8, _::binary>> = data
    value
  end

  @doc """
  Element-wise addition with NumPy-style broadcasting.
  When a dimension is 1 in one tensor, its stride for that axis is 0
  so the single value is reused across all positions in that dimension.
  """
  @spec add(t(), t()) :: t()
  def add(%__MODULE__{shape: sa, dtype: :f32} = a, %__MODULE__{shape: sb, dtype: :f32} = b) do
    out_shape = broadcast_shape(sa, sb)
    strides_a = broadcast_strides(sa, out_shape)
    strides_b = broadcast_strides(sb, out_shape)
    total = num_elements(out_shape)

    data =
      Enum.reduce((total - 1)..0//-1, <<>>, fn flat_idx, acc ->
        coords = flat_to_coords(flat_idx, out_shape)
        a_idx = dot_strides(coords, strides_a)
        b_idx = dot_strides(coords, strides_b)
        val = at(a, a_idx) + at(b, b_idx)
        <<val::float-32-native, acc::binary>>
      end)

    %__MODULE__{data: data, shape: out_shape, dtype: :f32}
  end

  @doc """
  Element-wise multiplication with NumPy-style broadcasting.
  Same broadcasting logic as add/2.
  """
  @spec mul(t(), t()) :: t()
  def mul(%__MODULE__{shape: sa, dtype: :f32} = a, %__MODULE__{shape: sb, dtype: :f32} = b) do
    out_shape = broadcast_shape(sa, sb)
    strides_a = broadcast_strides(sa, out_shape)
    strides_b = broadcast_strides(sb, out_shape)
    total = num_elements(out_shape)

    data =
      Enum.reduce((total - 1)..0//-1, <<>>, fn flat_idx, acc ->
        coords = flat_to_coords(flat_idx, out_shape)
        a_idx = dot_strides(coords, strides_a)
        b_idx = dot_strides(coords, strides_b)
        val = at(a, a_idx) * at(b, b_idx)
        <<val::float-32-native, acc::binary>>
      end)

    %__MODULE__{data: data, shape: out_shape, dtype: :f32}
  end

  @doc """
  Matrix multiplication for rank-2 tensors.
  For each output position (i, j), computes the dot product of
  row i of a with column j of b.
  """
  @spec matmul(t(), t()) :: t()
  def matmul(
        %__MODULE__{shape: [m, k], dtype: :f32} = a,
        %__MODULE__{shape: [k2, n], dtype: :f32} = b
      )
      when k == k2 do
    data =
      for i <- 0..(m - 1), j <- 0..(n - 1), reduce: <<>> do
        acc ->
          sum =
            Enum.reduce(0..(k - 1), 0.0, fn kk, s ->
              a_val = at(a, i * k + kk)
              b_val = at(b, kk * n + j)
              s + a_val * b_val
            end)

          acc <> <<sum::float-32-native>>
      end

    %__MODULE__{data: data, shape: [m, n], dtype: :f32}
  end

  def matmul(%__MODULE__{shape: sa}, %__MODULE__{shape: sb}) do
    raise ArgumentError, "matmul shape mismatch: #{inspect(sa)} × #{inspect(sb)}"
  end

  @doc """
  Reshape without copying data. The total number of elements
  must remain unchanged. Returns the same binary with a new shape.
  """
  @spec reshape(t(), [non_neg_integer()]) :: t()
  def reshape(%__MODULE__{shape: old_shape} = t, new_shape) do
    if num_elements(old_shape) != num_elements(new_shape) do
      raise ArgumentError,
        "cannot reshape #{inspect(old_shape)} (#{num_elements(old_shape)} elements) " <>
          "to #{inspect(new_shape)} (#{num_elements(new_shape)} elements)"
    end

    %{t | shape: new_shape}
  end

  @doc """
  Transpose a 2D tensor by swapping axes 0 and 1.
  Produces a [cols, rows] tensor where each element at (r, c) comes
  from position (c, r) in the original.
  """
  @spec transpose(t()) :: t()
  def transpose(%__MODULE__{shape: [rows, cols], data: _data, dtype: :f32} = t) do
    data =
      for r <- 0..(cols - 1), c <- 0..(rows - 1), reduce: <<>> do
        acc ->
          val = at(t, c * cols + r)
          acc <> <<val::float-32-native>>
      end

    %__MODULE__{data: data, shape: [cols, rows], dtype: :f32}
  end

  # --- Internal helpers ---

  @doc false
  def num_elements(shape), do: Enum.product(shape)

  @doc false
  def bytes_per_element(:f32), do: 4
  def bytes_per_element(:i8), do: 1

  # Compute broadcast output shape following NumPy rules.
  # Pads shorter shape with leading 1s, then for each dimension
  # picks the max of the two sizes (they must be equal or one must be 1).
  defp broadcast_shape(sa, sb) do
    max_rank = max(length(sa), length(sb))
    pa = List.duplicate(1, max_rank - length(sa)) ++ sa
    pb = List.duplicate(1, max_rank - length(sb)) ++ sb

    Enum.zip(pa, pb)
    |> Enum.map(fn
      {a, b} when a == b -> a
      {1, b} -> b
      {a, 1} -> a
      {a, b} -> raise ArgumentError, "incompatible broadcast dimensions: #{a} vs #{b}"
    end)
  end

  # Compute the effective strides for a tensor being broadcast to out_shape.
  # If a dimension is 1 in the original, its stride is 0 (the value is reused).
  defp broadcast_strides(shape, out_shape) do
    padded = List.duplicate(1, length(out_shape) - length(shape)) ++ shape
    suffix_products = suffix_strides(padded)

    Enum.zip(padded, suffix_products)
    |> Enum.map(fn
      {1, _stride} -> 0
      {_dim, stride} -> stride
    end)
  end

  # Compute row-major strides from a shape: stride_i = product of dims after i.
  defp suffix_strides(shape) do
    shape
    |> Enum.reverse()
    |> Enum.reduce({[], 1}, fn dim, {strides, acc} ->
      {[acc | strides], acc * dim}
    end)
    |> elem(0)
  end

  # Convert a flat index to multi-dimensional coordinates.
  defp flat_to_coords(flat_idx, shape) do
    {coords, _} =
      shape
      |> Enum.reverse()
      |> Enum.reduce({[], flat_idx}, fn dim, {coords, remaining} ->
        {[rem(remaining, dim) | coords], div(remaining, dim)}
      end)

    coords
  end

  # Compute a flat index from coordinates and strides via dot product.
  defp dot_strides(coords, strides) do
    Enum.zip(coords, strides)
    |> Enum.reduce(0, fn {c, s}, acc -> acc + c * s end)
  end
end
```

### Step 2: Operators

Operators are applied element-wise. ReLU zeros out negatives. Sigmoid maps values through the logistic function. Softmax is computed per row with max-subtraction for numerical stability to prevent overflow from large exponentials. Batch normalization normalizes channels using running statistics, then scales and shifts.

```elixir
defmodule InferenceEngine.Ops do
  alias InferenceEngine.Tensor

  @doc """
  ReLU activation: max(0, x) applied element-wise.
  Iterates the binary in 4-byte chunks, decodes each float,
  clamps negatives to 0, and re-encodes.
  """
  @spec relu(Tensor.t()) :: Tensor.t()
  def relu(%Tensor{data: data, shape: shape, dtype: :f32}) do
    new_data = apply_elementwise_f32(data, fn x -> max(0.0, x) end)
    %Tensor{data: new_data, shape: shape, dtype: :f32}
  end

  @doc """
  Sigmoid activation: 1 / (1 + exp(-x)) applied element-wise.
  Uses :math.exp/1 for the exponential.
  """
  @spec sigmoid(Tensor.t()) :: Tensor.t()
  def sigmoid(%Tensor{data: data, shape: shape, dtype: :f32}) do
    new_data =
      apply_elementwise_f32(data, fn x ->
        1.0 / (1.0 + :math.exp(-x))
      end)

    %Tensor{data: new_data, shape: shape, dtype: :f32}
  end

  @doc """
  Softmax over the last axis with numerical stability.
  For a 2D tensor [batch, classes], applies softmax per row:
  1. Subtract the row maximum from each element (prevents exp overflow)
  2. Compute exp of each shifted element
  3. Divide each exp by the row sum

  For a 1D tensor, treats the entire tensor as one row.
  """
  @spec softmax(Tensor.t()) :: Tensor.t()
  def softmax(%Tensor{shape: shape, dtype: :f32} = t) do
    {num_rows, row_len} =
      case shape do
        [n, c] -> {n, c}
        [c] -> {1, c}
      end

    data =
      for row <- 0..(num_rows - 1), reduce: <<>> do
        acc ->
          row_vals = for j <- 0..(row_len - 1), do: Tensor.at(t, row * row_len + j)
          row_max = Enum.max(row_vals)
          exps = Enum.map(row_vals, fn v -> :math.exp(v - row_max) end)
          sum_exps = Enum.sum(exps)

          Enum.reduce(exps, acc, fn e, bin ->
            bin <> <<(e / sum_exps)::float-32-native>>
          end)
      end

    %Tensor{data: data, shape: shape, dtype: :f32}
  end

  @doc """
  Batch normalization: (x - mean) / sqrt(var + eps) * gamma + beta.
  gamma, beta, running_mean, and running_var are 1D tensors of shape [channels].
  The input x can be any shape where the last dimension is channels (NHWC layout).
  Broadcasting is handled by iterating the flat data and indexing the channel
  parameters by `flat_index mod num_channels`.
  """
  @spec batch_norm(Tensor.t(), Tensor.t(), Tensor.t(), Tensor.t(), Tensor.t(), float()) ::
          Tensor.t()
  def batch_norm(
        %Tensor{data: data, shape: shape, dtype: :f32} = _x,
        %Tensor{} = gamma,
        %Tensor{} = beta,
        %Tensor{} = running_mean,
        %Tensor{} = running_var,
        eps \\ 1.0e-5
      ) do
    num_channels = List.last(shape)
    total = Tensor.num_elements(shape)

    new_data =
      for i <- 0..(total - 1), reduce: <<>> do
        acc ->
          c = rem(i, num_channels)
          offset = i * 4
          <<_::binary-size(offset), x_val::float-32-native, _::binary>> = data

          mean_c = Tensor.at(running_mean, c)
          var_c = Tensor.at(running_var, c)
          gamma_c = Tensor.at(gamma, c)
          beta_c = Tensor.at(beta, c)

          normalized = (x_val - mean_c) / :math.sqrt(var_c + eps)
          result = normalized * gamma_c + beta_c
          acc <> <<result::float-32-native>>
      end

    %Tensor{data: new_data, shape: shape, dtype: :f32}
  end

  # Iterate a float-32 binary, applying fun to each element and rebuilding the binary.
  defp apply_elementwise_f32(data, fun) do
    apply_elementwise_f32_acc(data, <<>>, fun)
  end

  defp apply_elementwise_f32_acc(<<>>, acc, _fun), do: acc

  defp apply_elementwise_f32_acc(<<x::float-32-native, rest::binary>>, acc, fun) do
    val = fun.(x)
    apply_elementwise_f32_acc(rest, acc <> <<val::float-32-native>>, fun)
  end
end
```

### Step 3: Conv2D

The convolution uses im2col to reshape the problem into a single matrix multiply. For each output position, im2col extracts the corresponding input patch and flattens it into a row of the column matrix. The kernel is reshaped from `[kH, kW, C_in, C_out]` to `[kH*kW*C_in, C_out]`. The output is `im2col_matrix × kernel_2d`, reshaped to `[N, out_H, out_W, C_out]`.

```elixir
defmodule InferenceEngine.Conv2D do
  alias InferenceEngine.Tensor

  @doc """
  2D convolution in NHWC format.
  input:  [N, H, W, C_in]
  kernel: [kH, kW, C_in, C_out]
  Returns: [N, out_H, out_W, C_out]

  Supports integer padding (number of pixels) or :same for auto-padding.
  Uses im2col to convert the convolution into a matrix multiplication.
  """
  @spec forward(Tensor.t(), Tensor.t(), keyword()) :: Tensor.t()
  def forward(
        %Tensor{shape: [n, h, w, c_in]} = input,
        %Tensor{shape: [kh, kw, c_in2, c_out]} = kernel,
        stride: stride,
        padding: padding
      )
      when c_in == c_in2 do
    pad =
      case padding do
        :same -> div(kh - 1, 2)
        p when is_integer(p) -> p
      end

    padded_input = if pad > 0, do: pad_nhwc(input, pad), else: input
    {_n2, ph, pw, _c} = shape_tuple(padded_input.shape)

    out_h = div(ph - kh, stride) + 1
    out_w = div(pw - kw, stride) + 1

    kernel_2d = Tensor.reshape(kernel, [kh * kw * c_in, c_out])

    data =
      for batch <- 0..(n - 1), reduce: <<>> do
        acc ->
          sample = extract_sample(padded_input, batch, ph, pw, c_in)
          col_matrix = im2col(sample, kh, kw, out_h, out_w, stride)
          result = Tensor.matmul(col_matrix, kernel_2d)
          acc <> result.data
      end

    %Tensor{data: data, shape: [n, out_h, out_w, c_out], dtype: :f32}
  end

  @doc """
  Extract the im2col matrix for a single HWC sample.
  For each output position (oh, ow), extracts the input patch of size
  [kh, kw, c] starting at (oh*stride, ow*stride) and flattens it
  into a single row. Stacks all rows into a matrix of shape
  [out_h * out_w, kh * kw * c].
  """
  @spec im2col(Tensor.t(), pos_integer(), pos_integer(), pos_integer(), pos_integer(), pos_integer()) ::
          Tensor.t()
  def im2col(%Tensor{shape: [h, w, c], dtype: :f32} = input, kh, kw, out_h, out_w, stride) do
    row_len = kh * kw * c

    data =
      for oh <- 0..(out_h - 1), ow <- 0..(out_w - 1), reduce: <<>> do
        acc ->
          patch =
            for kr <- 0..(kh - 1), kc <- 0..(kw - 1), ch <- 0..(c - 1), reduce: <<>> do
              patch_acc ->
                r = oh * stride + kr
                col = ow * stride + kc
                flat_idx = r * w * c + col * c + ch
                val = Tensor.at(input, flat_idx)
                patch_acc <> <<val::float-32-native>>
            end

          acc <> patch
      end

    %Tensor{data: data, shape: [out_h * out_w, row_len], dtype: :f32}
  end

  # Pad an NHWC tensor with zeros around the H and W dimensions.
  defp pad_nhwc(%Tensor{shape: [n, h, w, c], dtype: :f32} = input, pad) do
    new_h = h + 2 * pad
    new_w = w + 2 * pad

    data =
      for batch <- 0..(n - 1), r <- 0..(new_h - 1), col <- 0..(new_w - 1), ch <- 0..(c - 1),
          reduce: <<>> do
        acc ->
          if r >= pad and r < h + pad and col >= pad and col < w + pad do
            orig_idx = batch * h * w * c + (r - pad) * w * c + (col - pad) * c + ch
            val = Tensor.at(input, orig_idx)
            acc <> <<val::float-32-native>>
          else
            acc <> <<0.0::float-32-native>>
          end
      end

    %Tensor{data: data, shape: [n, new_h, new_w, c], dtype: :f32}
  end

  # Extract one sample from a batch NHWC tensor, returning an [H, W, C] tensor.
  defp extract_sample(%Tensor{shape: [_n, h, w, c], data: data, dtype: :f32}, batch, _ph, _pw, _c_in) do
    sample_size = h * w * c * 4
    offset = batch * sample_size
    sample_data = :binary.part(data, offset, sample_size)
    %Tensor{data: sample_data, shape: [h, w, c], dtype: :f32}
  end

  defp shape_tuple([n, h, w, c]), do: {n, h, w, c}
end
```

### Step 4: Model and loader

The model is a linear sequence of layers, each described as `{op_atom, weights_map, config_map}`. The loader parses a custom binary format. The ONNX loader handles a minimal subset of ops (Conv, Relu, Flatten, Gemm, Softmax) by manually parsing protobuf wire format.

```elixir
defmodule InferenceEngine.Model do
  @type op :: :conv2d | :batch_norm | :relu | :sigmoid | :softmax | :flatten | :dense
  @type layer :: {op(), weights :: map(), config :: map()}

  defstruct layers: [], input_shape: nil

  @doc """
  Validate that layer output shapes chain correctly.
  Walks the layer list, computing the output shape of each layer
  given its input shape. Returns {:ok, final_shape} if valid,
  or {:error, reason, layer_index} on the first mismatch.
  """
  @spec validate(t()) :: {:ok, [non_neg_integer()]} | {:error, String.t(), non_neg_integer()}
  def validate(%__MODULE__{layers: layers, input_shape: shape}) do
    layers
    |> Enum.with_index()
    |> Enum.reduce_while({:ok, shape}, fn {{op, weights, config}, idx}, {:ok, current_shape} ->
      case output_shape(op, weights, config, current_shape) do
        {:ok, next_shape} -> {:cont, {:ok, next_shape}}
        {:error, reason} -> {:halt, {:error, reason, idx}}
      end
    end)
  end

  defp output_shape(:conv2d, %{kernel: %{shape: [kh, _kw, _ci, c_out]}}, config, [_n, h, w, _c]) do
    stride = Map.get(config, :stride, 1)
    pad = Map.get(config, :padding, 0)
    pad = if pad == :same, do: div(kh - 1, 2), else: pad
    out_h = div(h + 2 * pad - kh, stride) + 1
    out_w = div(w + 2 * pad - kh, stride) + 1
    {:ok, [1, out_h, out_w, c_out]}
  end

  defp output_shape(:batch_norm, _w, _c, shape), do: {:ok, shape}
  defp output_shape(:relu, _w, _c, shape), do: {:ok, shape}
  defp output_shape(:sigmoid, _w, _c, shape), do: {:ok, shape}
  defp output_shape(:softmax, _w, _c, shape), do: {:ok, shape}

  defp output_shape(:flatten, _w, _c, [n | rest]) do
    {:ok, [n, Enum.product(rest)]}
  end

  defp output_shape(:dense, %{kernel: %{shape: [_in, out]}}, _c, [n, _in2]) do
    {:ok, [n, out]}
  end

  defp output_shape(op, _w, _c, shape) do
    {:error, "cannot compute output shape for #{inspect(op)} with input #{inspect(shape)}"}
  end
end

defmodule InferenceEngine.Loader do
  alias InferenceEngine.{Model, Tensor}

  @op_map %{
    0 => :conv2d,
    1 => :batch_norm,
    2 => :relu,
    3 => :sigmoid,
    4 => :softmax,
    5 => :flatten,
    6 => :dense
  }

  @doc """
  Load a model from a custom binary format.
  Header: <<magic::32, version::8, num_layers::32>>
  Each layer: <<op_type::8, config_len::32, config_json::binary-size(config_len),
                num_weights::8, [weight_header, weight_data]...>>
  Weight header: <<name_len::8, name::binary-size(name_len),
                   dtype::8, ndims::8, [dim::32]...>>
  Weight data follows immediately after shape.
  """
  @spec load_binary(String.t()) :: {:ok, Model.t()} | {:error, term()}
  def load_binary(path) do
    case File.read(path) do
      {:ok, binary} -> parse_binary(binary)
      {:error, reason} -> {:error, reason}
    end
  end

  defp parse_binary(<<"INFE"::binary, version::8, num_layers::32, rest::binary>>) do
    case parse_layers(rest, num_layers, []) do
      {:ok, layers} ->
        {:ok, %Model{layers: layers, input_shape: nil}}

      {:error, _} = err ->
        err
    end
  end

  defp parse_binary(_), do: {:error, :invalid_magic}

  defp parse_layers(_rest, 0, acc), do: {:ok, Enum.reverse(acc)}

  defp parse_layers(
         <<op_type::8, config_len::32, config_json::binary-size(config_len), num_weights::8,
           rest::binary>>,
         remaining,
         acc
       ) do
    op = Map.get(@op_map, op_type, :unknown)
    config = Jason.decode!(config_json, keys: :atoms)

    case parse_weights(rest, num_weights, %{}) do
      {:ok, weights, rest2} ->
        parse_layers(rest2, remaining - 1, [{op, weights, config} | acc])

      {:error, _} = err ->
        err
    end
  end

  defp parse_layers(_, _, _), do: {:error, :truncated_layer}

  defp parse_weights(rest, 0, acc), do: {:ok, acc, rest}

  defp parse_weights(<<name_len::8, name::binary-size(name_len), dtype_byte::8, ndims::8, rest::binary>>, remaining, acc) do
    {shape, rest2} = parse_dims(rest, ndims, [])
    dtype = if dtype_byte == 0, do: :f32, else: :i8
    elem_size = if dtype == :f32, do: 4, else: 1
    data_size = Enum.product(shape) * elem_size
    <<data::binary-size(data_size), rest3::binary>> = rest2
    tensor = %Tensor{data: data, shape: shape, dtype: dtype}
    parse_weights(rest3, remaining - 1, Map.put(acc, String.to_atom(name), tensor))
  end

  defp parse_weights(_, _, _), do: {:error, :truncated_weight}

  defp parse_dims(rest, 0, acc), do: {Enum.reverse(acc), rest}

  defp parse_dims(<<dim::32, rest::binary>>, remaining, acc) do
    parse_dims(rest, remaining - 1, [dim | acc])
  end

  @doc """
  Load a minimal ONNX protobuf (Conv, Relu, Flatten, Gemm, Softmax only).
  Parses protobuf wire format manually without a protobuf library.
  This is a simplified parser that handles the subset needed for
  convolutional classifiers.
  """
  @spec load_onnx(String.t()) :: {:ok, Model.t()} | {:error, term()}
  def load_onnx(path) do
    case File.read(path) do
      {:ok, binary} ->
        case parse_onnx_model(binary) do
          {:ok, _} = result -> result
          {:error, _} = err -> err
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  # Minimal protobuf parser for ONNX ModelProto.
  # Field 7 is GraphProto. Inside GraphProto, field 1 = nodes, field 5 = initializers.
  defp parse_onnx_model(binary) do
    fields = parse_protobuf_fields(binary)
    graph_bin = Map.get(fields, 7, <<>>)
    graph_fields = parse_protobuf_fields(graph_bin)

    nodes = Map.get(graph_fields, 1, [])
    nodes = if is_list(nodes), do: nodes, else: [nodes]

    initializers = Map.get(graph_fields, 5, [])
    initializers = if is_list(initializers), do: initializers, else: [initializers]

    weight_map = parse_initializers(initializers)

    layers =
      Enum.map(nodes, fn node_bin ->
        node_fields = parse_protobuf_fields(node_bin)
        op_type_str = Map.get(node_fields, 4, "")
        inputs = Map.get(node_fields, 1, [])
        inputs = if is_list(inputs), do: inputs, else: [inputs]

        op = onnx_op_to_atom(op_type_str)
        weights = collect_node_weights(inputs, weight_map)
        {op, weights, %{}}
      end)

    {:ok, %Model{layers: layers, input_shape: nil}}
  end

  defp onnx_op_to_atom("Conv"), do: :conv2d
  defp onnx_op_to_atom("Relu"), do: :relu
  defp onnx_op_to_atom("Flatten"), do: :flatten
  defp onnx_op_to_atom("Gemm"), do: :dense
  defp onnx_op_to_atom("Softmax"), do: :softmax
  defp onnx_op_to_atom("BatchNormalization"), do: :batch_norm
  defp onnx_op_to_atom(other), do: String.to_atom(String.downcase(other))

  defp collect_node_weights(input_names, weight_map) do
    input_names
    |> Enum.with_index()
    |> Enum.reduce(%{}, fn {name, idx}, acc ->
      case Map.get(weight_map, name) do
        nil -> acc
        tensor ->
          key = if idx == 1, do: :kernel, else: :"weight_#{idx}"
          Map.put(acc, key, tensor)
      end
    end)
  end

  defp parse_initializers(initializer_bins) do
    Enum.reduce(initializer_bins, %{}, fn init_bin, acc ->
      init_fields = parse_protobuf_fields(init_bin)
      name = Map.get(init_fields, 1, "unknown")
      dims = Map.get(init_fields, 2, [])
      dims = if is_list(dims), do: dims, else: [dims]
      shape = Enum.map(dims, &parse_varint_value/1)
      raw_data = Map.get(init_fields, 13, <<>>)

      if byte_size(raw_data) > 0 do
        tensor = %Tensor{data: raw_data, shape: shape, dtype: :f32}
        Map.put(acc, name, tensor)
      else
        acc
      end
    end)
  end

  defp parse_varint_value(v) when is_integer(v), do: v
  defp parse_varint_value(v) when is_binary(v), do: :binary.decode_unsigned(v, :little)

  # Minimal protobuf field parser. Groups repeated fields into lists.
  defp parse_protobuf_fields(binary) do
    parse_protobuf_fields(binary, %{})
  end

  defp parse_protobuf_fields(<<>>, acc), do: acc

  defp parse_protobuf_fields(binary, acc) do
    {tag, rest} = decode_varint(binary)
    field_number = Bitwise.bsr(tag, 3)
    wire_type = Bitwise.band(tag, 0x07)

    {value, rest2} =
      case wire_type do
        0 -> decode_varint(rest)
        1 -> <<v::little-64, r::binary>> = rest; {v, r}
        2 ->
          {len, r} = decode_varint(rest)
          <<v::binary-size(len), r2::binary>> = r
          {v, r2}
        5 -> <<v::little-32, r::binary>> = rest; {v, r}
        _ -> {nil, <<>>}
      end

    updated =
      case Map.get(acc, field_number) do
        nil -> Map.put(acc, field_number, value)
        existing when is_list(existing) -> Map.put(acc, field_number, existing ++ [value])
        existing -> Map.put(acc, field_number, [existing, value])
      end

    parse_protobuf_fields(rest2, updated)
  end

  defp decode_varint(binary, acc \\ 0, shift \\ 0)

  defp decode_varint(<<1::1, byte::7, rest::binary>>, acc, shift) do
    decode_varint(rest, acc + Bitwise.bsl(byte, shift), shift + 7)
  end

  defp decode_varint(<<0::1, byte::7, rest::binary>>, acc, shift) do
    {acc + Bitwise.bsl(byte, shift), rest}
  end
end
```

### Step 5: Quantization

Symmetric int8 quantization maps the float range `[-max_abs, max_abs]` to `[-127, 127]` using a single scale factor. The scale is `max_abs / 127`. Quantization rounds each float to the nearest int8 after dividing by scale. Dequantization multiplies each int8 by the scale to recover an approximate float.

```elixir
defmodule InferenceEngine.Quantize do
  alias InferenceEngine.Tensor

  @max_int8 127

  @doc """
  Symmetric per-tensor quantization.
  Computes scale = max(abs(weights)) / 127, then maps each float to
  clamp(round(x / scale), -127, 127) stored as signed-8.
  Returns {quantized_tensor, scale}.
  """
  @spec quantize_tensor(Tensor.t()) :: {Tensor.t(), float()}
  def quantize_tensor(%Tensor{data: data, shape: shape, dtype: :f32}) do
    floats = decode_all_f32(data)
    max_abs = floats |> Enum.map(&abs/1) |> Enum.max()
    scale = if max_abs == 0.0, do: 1.0, else: max_abs / @max_int8

    quantized_data =
      Enum.reduce(floats, <<>>, fn x, acc ->
        q = round(x / scale)
        q = q |> max(-@max_int8) |> min(@max_int8)
        acc <> <<q::signed-8>>
      end)

    {%Tensor{data: quantized_data, shape: shape, dtype: :i8}, scale}
  end

  @doc """
  Dequantize int8 tensor back to float32.
  Each signed-8 integer is multiplied by the scale and encoded as float-32-native.
  """
  @spec dequantize_tensor(Tensor.t(), float()) :: Tensor.t()
  def dequantize_tensor(%Tensor{data: data, shape: shape, dtype: :i8}, scale) do
    new_data = dequantize_binary(data, scale, <<>>)
    %Tensor{data: new_data, shape: shape, dtype: :f32}
  end

  @doc """
  Quantize all weight tensors in a model.
  Returns {quantized_model, scales} where scales is a map of
  {layer_index, weight_name} => scale_float.
  """
  @spec quantize_model(InferenceEngine.Model.t()) ::
          {InferenceEngine.Model.t(), map()}
  def quantize_model(%InferenceEngine.Model{layers: layers} = model) do
    {new_layers, scales} =
      layers
      |> Enum.with_index()
      |> Enum.map_reduce(%{}, fn {{op, weights, config}, idx}, scale_acc ->
        {new_weights, layer_scales} =
          Enum.reduce(weights, {%{}, %{}}, fn {name, tensor}, {w_acc, s_acc} ->
            case tensor do
              %Tensor{dtype: :f32} ->
                {qt, scale} = quantize_tensor(tensor)
                {Map.put(w_acc, name, qt), Map.put(s_acc, name, scale)}

              _ ->
                {Map.put(w_acc, name, tensor), s_acc}
            end
          end)

        new_scale_acc =
          Enum.reduce(layer_scales, scale_acc, fn {wname, scale}, acc ->
            Map.put(acc, {idx, wname}, scale)
          end)

        {{op, new_weights, config}, new_scale_acc}
      end)

    {%{model | layers: new_layers}, scales}
  end

  # Decode all float-32-native values from a binary into a list.
  defp decode_all_f32(data), do: decode_all_f32(data, [])
  defp decode_all_f32(<<>>, acc), do: Enum.reverse(acc)

  defp decode_all_f32(<<v::float-32-native, rest::binary>>, acc) do
    decode_all_f32(rest, [v | acc])
  end

  # Dequantize a binary of signed-8 integers to float-32-native.
  defp dequantize_binary(<<>>, _scale, acc), do: acc

  defp dequantize_binary(<<q::signed-8, rest::binary>>, scale, acc) do
    val = q * scale
    dequantize_binary(rest, scale, acc <> <<val::float-32-native>>)
  end
end
```

### Step 6: Inference engine

The engine runs a forward pass through the model's layer list. Each layer is dispatched to the appropriate operator. Batch inference uses `Task.async_stream` to parallelize across inputs while preserving ordering.

```elixir
defmodule InferenceEngine.Engine do
  alias InferenceEngine.{Model, Tensor, Ops, Conv2D, Quantize}

  @doc """
  Run inference on a batch of input tensors.
  Uses Task.async_stream for parallelism with ordered results.
  Returns {:ok, [%Tensor{}, ...]} or {:error, reason}.
  """
  @spec infer(Model.t(), [Tensor.t()], keyword()) :: {:ok, [Tensor.t()]} | {:error, term()}
  def infer(%Model{} = model, inputs, opts \\ []) when is_list(inputs) do
    concurrency = Keyword.get(opts, :concurrency, System.schedulers_online())

    results =
      inputs
      |> Task.async_stream(
        fn input -> forward_pass(model, input) end,
        max_concurrency: concurrency,
        ordered: true
      )
      |> Enum.map(fn {:ok, result} -> result end)

    {:ok, results}
  end

  @doc """
  Run a single forward pass through the model.
  Reduces over the layer list, threading the tensor through each layer.
  """
  @spec forward_pass(Model.t(), Tensor.t()) :: Tensor.t()
  def forward_pass(%Model{layers: layers}, %Tensor{} = input) do
    Enum.reduce(layers, input, fn layer, acc ->
      apply_layer(layer, acc)
    end)
  end

  defp apply_layer({:conv2d, weights, config}, input) do
    stride = Map.get(config, :stride, 1)
    padding = Map.get(config, :padding, 0)
    result = Conv2D.forward(input, weights.kernel, stride: stride, padding: padding)

    case Map.get(config, :activation) do
      :relu -> Ops.relu(result)
      :sigmoid -> Ops.sigmoid(result)
      _ -> result
    end
  end

  defp apply_layer({:batch_norm, weights, config}, input) do
    eps = Map.get(config, :eps, 1.0e-5)
    Ops.batch_norm(input, weights.gamma, weights.beta, weights.mean, weights.var, eps)
  end

  defp apply_layer({:relu, _weights, _config}, input), do: Ops.relu(input)

  defp apply_layer({:sigmoid, _weights, _config}, input), do: Ops.sigmoid(input)

  defp apply_layer({:flatten, _weights, _config}, %Tensor{shape: [n | rest]} = input) do
    Tensor.reshape(input, [n, Enum.product(rest)])
  end

  defp apply_layer({:dense, weights, config}, input) do
    result = Tensor.matmul(input, weights.kernel)

    result =
      if Map.has_key?(weights, :bias) do
        Tensor.add(result, weights.bias)
      else
        result
      end

    case Map.get(config, :activation) do
      :relu -> Ops.relu(result)
      :sigmoid -> Ops.sigmoid(result)
      _ -> result
    end
  end

  defp apply_layer({:softmax, _weights, _config}, input), do: Ops.softmax(input)
end
```

### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
# test/tensor_test.exs
defmodule InferenceEngine.TensorTest do
  use ExUnit.Case, async: true
  alias InferenceEngine.Tensor


  describe "Tensor" do

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

## Reflection

You can run a 7B-parameter model on one GPU. A single request takes 500ms. At 100 req/s, what's your queuing strategy — batch, dedicated-replica-per-request, or speculative decoding? Pick one and defend.

## Resources

- ONNX Operator Specification — https://onnx.ai/onnx/operators/ (reference for Conv, Gemm, BatchNormalization exact semantics)
- Goodfellow, Bengio, Courville — "Deep Learning" Chapter 9: Convolutional Networks (im2col derivation)
- Nagel et al. — "A White Paper on Neural Network Quantization" (2021) — Qualcomm Research (symmetric vs asymmetric, per-channel vs per-tensor)
- Chellapilla et al. — "High Performance Convolutional Neural Networks for Document Processing" (2006) — original im2col paper
- Brendan Gregg — "Systems Performance" Chapter 6: CPUs (cache locality and why matmul layout matters)
- Elixir `<<>>` binary syntax — https://hexdocs.pm/elixir/Kernel.SpecialForms.html#%3C%3C%3E%3E/1
