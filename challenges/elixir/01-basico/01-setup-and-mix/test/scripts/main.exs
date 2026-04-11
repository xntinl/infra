defmodule Test do
  def tupla_ejemplo do
    {x,y}={10,20}
    x+y
  end

end

IO.puts("tupla_ejemplo #{Test.tupla_ejemplo()}")
