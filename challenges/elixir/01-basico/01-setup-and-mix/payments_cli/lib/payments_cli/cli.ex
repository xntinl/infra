defmodule PaymentsCli.CLI do
  @moduledoc """
    Entry  point
  """

  @doc """
    Main entry point
  """
  @spec main([String.t()]) :: :ok | {:error, String.t()}
  def main([file_path]) when is_binary(file_path) do
    IO.puts("processing: #{file_path}")
    :ok
  end

  def main([]) do
    print_error("no file path given")
  end

  def main(_other) do
    print_error("usage: payment_cli <file>")
  end

  @spec print_error(String.t()) :: {:error, String.t()}
  def print_error(message) do
    IO.puts(:stderr, message)
    {:error, message}
  end
end
