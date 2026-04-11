

case Test.dividir(10,2) do
  {:ok,resultado} -> IO.puts("resultado: #{resultado}")
  {:error,error} -> IO.puts("error: #{error}")
end
