defmodule Test do
  @moduledoc """
  Ejemplos.
  """

  @doc "pattern matching con tuplas"
  def tupla_example do
    {x, y} = {10, 20}
    x + y
  end

  def dividir(a, b) do
    if b == 0 do
      {:error, "division por cero"}
    else
      {:ok, a / b}
    end
  end
end
