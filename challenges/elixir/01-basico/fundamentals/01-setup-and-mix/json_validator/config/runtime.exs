import Config

if config_env() == :prod do
  config :json_validator,
    required_keys:
      System.get_env("REQUIRED_KEYS", "name,version,type")
      |> String.split(",", trim: true)
end
