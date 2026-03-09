# 12. Terraform + AWS Workflow

## Metadata
| Campo | Valor |
|-------|-------|
| **Concepto** | Terraform init/plan/apply con workspaces, Lambda deploy, SSM, ECR |
| **Dificultad** | Avanzado |

## Que vas a aprender

- **`[confirm]`** — Atributo que solicita confirmacion interactiva antes de ejecutar recetas destructivas como `tf-apply` o `tf-destroy`. Acepta un mensaje personalizado que se muestra al usuario.
  [Documentacion: confirm attribute](https://just.systems/man/en/chapter_32.html)

- **`env_var_or_default`** — Funcion que lee variables de entorno con valores por defecto, permitiendo que el mismo justfile funcione en distintos entornos sin modificacion. Equivalente a `env("VAR", "default")`.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **Workspace-based environments** — Patron donde cada entorno (dev, staging, prod) es un workspace de Terraform, seleccionado automaticamente al hacer `tf-init` segun la variable `env`.
  [Documentacion: Just Manual](https://just.systems/man/en/)

- **`set positional-arguments`** — Configuracion que pasa los argumentos de las recetas como argumentos posicionales al shell (`$1`, `$2`), en lugar de interpolarlos con `{{}}`.
  [Documentacion: Just Manual](https://just.systems/man/en/)

## Descripcion

Vas a crear un justfile completo para gestionar infraestructura en AWS con Terraform. El archivo centraliza todas las operaciones de infraestructura: inicializacion con backend S3, planificacion y aplicacion con workspaces por entorno, despliegue de funciones Lambda compiladas con cargo-lambda, gestion de parametros en SSM, y comandos informativos. Las operaciones destructivas requieren confirmacion explicita, y las variables de entorno permiten cambiar de entorno con un simple `ENV=staging`.

## Codigo

```justfile
set dotenv-load
set shell := ["bash", "-euo", "pipefail", "-c"]

project := env("PROJECT", "myproject")
env := env("ENV", "dev")
region := env("AWS_REGION", "us-east-1")
tf_dir := "terraform"

default:
    @just --list

# ── Terraform ─────────────────────────────────────────

[group('terraform')]
tf-init:
    terraform -chdir={{tf_dir}} init \
        -backend-config="bucket={{project}}-terraform-state"
    terraform -chdir={{tf_dir}} workspace select {{env}} 2>/dev/null \
        || terraform -chdir={{tf_dir}} workspace new {{env}}

[group('terraform')]
tf-plan: tf-init
    terraform -chdir={{tf_dir}} plan \
        -var="project_name={{project}}" \
        -var="region={{region}}"

[group('terraform')]
[confirm("Aplicar cambios Terraform en {{env}}?")]
tf-apply: tf-init
    terraform -chdir={{tf_dir}} apply \
        -var="project_name={{project}}" \
        -var="region={{region}}" \
        -auto-approve

[group('terraform')]
[confirm("DESTRUIR toda la infra en {{env}}? No se puede deshacer!")]
tf-destroy:
    terraform -chdir={{tf_dir}} destroy \
        -var="project_name={{project}}" \
        -var="region={{region}}"

[group('terraform')]
tf-fmt:
    terraform -chdir={{tf_dir}} fmt -recursive

[group('terraform')]
tf-validate: tf-init
    terraform -chdir={{tf_dir}} validate

[group('terraform')]
tf-output *args:
    terraform -chdir={{tf_dir}} output {{args}}

# ── Lambda ────────────────────────────────────────────

[group('lambda')]
lambda-build name:
    cargo lambda build --release --arm64 --bin {{name}}

[group('lambda')]
lambda-deploy name: (lambda-build name)
    aws lambda update-function-code \
        --function-name {{project}}-{{env}}-{{name}} \
        --zip-file fileb://target/lambda/{{name}}/bootstrap.zip \
        --region {{region}}

[group('lambda')]
lambda-logs name:
    aws logs tail /aws/lambda/{{project}}-{{env}}-{{name}} \
        --follow --region {{region}}

# ── SSM ───────────────────────────────────────────────

[group('ssm')]
ssm-get key:
    aws ssm get-parameter --name "/{{project}}/{{env}}/{{key}}" \
        --with-decryption --query "Parameter.Value" --output text

[group('ssm')]
ssm-set key value:
    aws ssm put-parameter --name "/{{project}}/{{env}}/{{key}}" \
        --value "{{value}}" --type SecureString --overwrite

# ── Info ──────────────────────────────────────────────

[group('info')]
whoami:
    aws sts get-caller-identity

[group('info')]
info:
    @echo "Project: {{project}}"
    @echo "Env:     {{env}}"
    @echo "Region:  {{region}}"
```

## Verificacion

1. Ejecuta `just` y verifica que las recetas aparecen agrupadas por terraform, lambda, ssm e info.
2. Ejecuta `just tf-plan` y confirma que inicializa el backend y ejecuta el plan de Terraform.
3. Ejecuta `just tf-apply` y verifica que solicita confirmacion con el mensaje "Aplicar cambios Terraform en dev?".
4. Ejecuta `just tf-destroy` y observa la confirmacion con el mensaje de advertencia sobre destruccion.
5. Ejecuta `ENV=staging just tf-plan` y confirma que apunta al workspace de staging.
6. Ejecuta `just lambda-deploy hello-api` y verifica que primero compila y luego despliega la Lambda.
7. Ejecuta `just whoami` y confirma que muestra la identidad de AWS.

## Solucion y Aprendizaje

- [Just Manual - confirm attribute](https://just.systems/man/en/chapter_32.html) — Documentacion del atributo `[confirm]` para solicitar confirmacion antes de ejecutar recetas peligrosas.
- [Tour de Just](https://rodarmor.com/blog/tour-de-just/) — Recorrido general por las funcionalidades de just, incluyendo variables de entorno y settings.
- [Frank Wiles - Just Do It](https://frankwiles.com/posts/just-do-it/) — Articulo practico sobre como reemplazar Makefiles con just en proyectos reales.

## Recursos

- [Just GitHub](https://github.com/casey/just)
- [Just Manual](https://just.systems/man/en/)

## Notas

_Espacio para tus notas personales._
