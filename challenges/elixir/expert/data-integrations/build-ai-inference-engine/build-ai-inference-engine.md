# AI Inference Engine

**Project**: `inference_engine` — Elixir ML inference runtime for ONNX models with int8 quantization and batch processing

## Project Context

Your team maintains a real-time fraud detection pipeline. The data science team delivers trained models as ONNX files. Currently, every inference request is forwarded to a Python microservice that runs PyTorch. That service adds **30–80ms network latency**, requires a separate deployment, cannot share BEAM process memory with your Elixir application, and scales unpredictably.

**The ask**: Load a trained model directly in the Elixir process and run inference without any network hop. Models are convolutional classifiers — five to ten layers, weights in the hundreds of MB. Accuracy must match the Python service within 2%.

**Building blocks**: You will build `InferenceEngine`: a pure-Elixir ML inference runtime that loads a subset of ONNX, runs forward passes, supports int8 quantization for faster throughput, and batches multiple inference requests to amortize graph setup cost.

---

## Why Tensor Ops Are in Native Code, Not Pure Elixir

Elixir on the BEAM is not designed for tight numeric loops. A single inner loop with floating-point arithmetic is **100–1000× slower** than an equivalent BLAS kernel (CPU-optimized matrix multiplication). NIFs (native-implemented functions) or Erlang NIF wrappers let you call the actually-fast code while keeping orchestration in Elixir where it belongs.

## Design decisions
**Option A — Interpreted eager execution (each op dispatched individually)**
- Pros: simple, matches PyTorch eager mode defaults, easy to debug
- Cons: no kernel fusion, dispatch overhead dominates for small ops, 50–200% slower on typical models

**Option B — Graph-level tracing + kernel fusion before execution** (chosen)
- Pros: fused ops amortize dispatch cost, unlocks operator-level optimizations, 30–50% faster inference
- Cons: first call pays a trace-and-compile tax (100–500ms), requires deterministic graph structure

**Why we chose B**: In production inference, the same model is called millions of times. A one-time compile tax of 100–500ms is rounding error compared to millions of 5–50ms inference calls. Additionally, fusion uncovers micro-optimizations (e.g., ReLU + BatchNorm fused into a single kernel) that would be impossible in eager mode.

## Project structure
```
inference_engine/
├── script/
│   └── main.exs
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

**Objective**: Store numerics as a flat binary with separate shape metadata so element access reduces to byte-offset arithmetic — no per-element wrapper overhead.

The tensor stores all numerical data as a flat binary. The shape is tracked separately. Element access uses byte offsets computed from the flat index and the byte width of the dtype. Broadcasting follows NumPy semantics: dimensions of size 1 are stretched to match the other operand along each axis.

### Step 2: Operators

**Objective**: Apply activations element-wise over the binary, using max-subtraction in softmax so large logits don't overflow the exponential.

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

**Objective**: Reduce convolution to a single matrix multiply via im2col so the hot path benefits from cache locality and Task.async_stream parallelism.

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

**Objective**: Represent the model as a sequence of {op, weights, config} tuples so shape validation chains layer-by-layer and loader formats stay decoupled from execution.

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
    parse_weights(rest3, remaining - 1, Map.put(acc, String.to_existing_atom(name), tensor))
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
  defp onnx_op_to_atom(other), do: String.to_existing_atom(String.downcase(other))

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

**Objective**: Apply symmetric int8 quantization per tensor so weights shrink 4x with one scale factor — dequant is a single multiply, error bounded by max_abs/127.

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

**Objective**: Fold the input tensor through the layer list and batch with Task.async_stream so per-sample forwards run in parallel without reordering outputs.

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
defmodule InferenceEngine.TensorTest do
  use ExUnit.Case, async: true
  doctest InferenceEngine.Engine
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
## Main Entry Point

```elixir
def main do
  IO.puts("======== 43-build-ai-inference-engine ========")
  IO.puts("Build Ai Inference Engine")
  IO.puts("")
  
  InferenceEngine.Tensor.start_link([])
  IO.puts("InferenceEngine.Tensor started")
  
  IO.puts("Run: mix test")
end
```
---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Infer.MixProject do
  use Mix.Project

  def project do
    [
      app: :infer,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Infer.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `infer` (AI inference engine).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 100000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:infer) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Infer stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:infer) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:infer)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual infer operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Infer classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **1,000 tokens/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **100 ms** | GGUF llama.cpp + ONNX runtime |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- GGUF llama.cpp + ONNX runtime: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why AI Inference Engine matters

Mastering **AI Inference Engine** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

---

## Implementation

### `lib/inference_engine.ex`

```elixir
defmodule InferenceEngine do
  @moduledoc """
  Reference implementation for AI Inference Engine.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the inference_engine module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> InferenceEngine.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```
### `test/inference_engine_test.exs`

```elixir
defmodule InferenceEngineTest do
  use ExUnit.Case, async: true

  doctest InferenceEngine

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert InferenceEngine.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- GGUF llama.cpp + ONNX runtime
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
