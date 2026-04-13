#!/bin/bash
# Script maestro para procesar 03-avanzado
# Combina validación inicial, corrección automática y validación final

set -euo pipefail

BASE_DIR="/Users/consulting/Documents/consulting/infra/challenges/elixir"
TARGET_DIR="${BASE_DIR}/03-avanzado"
SCRIPT_DIR="${BASE_DIR}"

# Colores para output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[!]${NC} $1"
}

log_error() {
    echo -e "${RED}[✗]${NC} $1"
}

echo ""
echo "================================================================================"
echo "PROCESAMIENTO DE 03-AVANZADO: FIX EXTRA END + VALIDACIÓN"
echo "================================================================================"
echo ""

# Verificar que el directorio existe
if [ ! -d "$TARGET_DIR" ]; then
    log_error "Directorio no encontrado: $TARGET_DIR"
    exit 1
fi

# Contar archivos
TOTAL_MD=$(find "$TARGET_DIR" -name "*.md" -type f | wc -l)
if [ "$TOTAL_MD" -eq 0 ]; then
    log_warn "No se encontraron archivos .md en $TARGET_DIR"
    log_info "Esperando a que tarea #7 complete la generación de 280 ejercicios..."
    exit 0
fi

log_success "Se encontraron $TOTAL_MD archivos .md"
echo ""

# Fase 1: Validación inicial
log_info "FASE 1: Validación inicial de bloques..."
if [ -f "$SCRIPT_DIR/validate_all_blocks.exs" ]; then
    elixir "$SCRIPT_DIR/validate_all_blocks.exs" "$TARGET_DIR" || true
    echo ""
else
    log_warn "Script validate_all_blocks.exs no encontrado"
fi

# Fase 2: Corrección automática
log_info "FASE 2: Corrección automática de 'end' extra..."
if [ -f "$SCRIPT_DIR/fix_extra_end.exs" ]; then
    elixir "$SCRIPT_DIR/fix_extra_end.exs" "$TARGET_DIR" || true
    echo ""
else
    log_error "Script fix_extra_end.exs no encontrado"
    exit 1
fi

# Fase 3: Validación final
log_info "FASE 3: Validación final de bloques..."
if [ -f "$SCRIPT_DIR/validate_all_blocks.exs" ]; then
    elixir "$SCRIPT_DIR/validate_all_blocks.exs" "$TARGET_DIR" || true
    echo ""
else
    log_warn "Script validate_all_blocks.exs no encontrado"
fi

echo "================================================================================"
log_success "Procesamiento completado"
echo "================================================================================"
echo ""
log_info "Próximos pasos:"
echo "  1. Revisar cualquier archivo reportado con errores en la validación final"
echo "  2. Si hay errores, revisar y corregir manualmente"
echo "  3. Ejecutar validación nuevamente si fue necesario hacer correcciones"
echo ""
