defmodule JsonValidator do
  @spec main([String.t()]) :: :ok
  def main(argv) do
    argv
    |> parse_args()
    |> run()
    |> exit_with_code()
  end

  @spec parse_args([String.t()]) :: {:validate, String.t(), keyword()} | :help
  def parse_args(argv) do
    switches = [strict: :boolean, help: :boolean, keys: :string]
    aliases = [s: :string, h: :help, k: :keys]

  end

  defp run(:help) do
  end

  defp run({:validate, file_path, opts}) do
  end

  defp exit_with_code(:ok) do
  end

  defp exit_with_code({:error, reason}) do
  end
end
