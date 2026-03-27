# Ejemplo 1: Variables y Backticks
version := "1.0.0"
git_hash := `git rev-parse --short HEAD`
node_env := env_var_or_default("NODE_ENV", "development")

build:
	@echo "Construyendo v{{version}} ({{git_hash}}) en modo {{node_env}}"
