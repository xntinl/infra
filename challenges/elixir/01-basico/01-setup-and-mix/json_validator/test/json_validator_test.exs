defmodule JsonValidatorTest do
  use ExUnit.Case
  doctest JsonValidator

  test "greets the world" do
    assert JsonValidator.hello() == :world
  end
end
