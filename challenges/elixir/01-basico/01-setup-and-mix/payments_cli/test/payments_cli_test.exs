defmodule PaymentsCliTest do
  use ExUnit.Case
  doctest PaymentsCli

  test "greets the world" do
    assert PaymentsCli.hello() == :world
  end
end
