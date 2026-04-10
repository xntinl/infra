# IA Moderna — Fases y Evolución

---

## Fase 1 — LLMs puros (2017–2021)
> Los modelos aprenden a predecir texto a escala masiva. Aún no razonan — completan patrones. El tamaño del modelo es la palanca principal de mejora.

- **2017** — Google publica *Attention Is All You Need*: nace el Transformer
- **2018** — GPT-1: pre-entrenamiento + fine-tuning como paradigma base
- **2020** — GPT-3 (175B params): few-shot learning sin fine-tuning, primer modelo general real
- **2021** — Codex: especialización para código — precursor directo de Copilot y Claude Code

---

## Fase 2 — Emergencia de capacidades (2022–2023)
> A cierta escala, los modelos exhiben capacidades no entrenadas explícitamente: matemática, código, razonamiento básico. RLHF convierte modelos de completado en asistentes alineados. Nace el producto masivo.

- **2022** — ChatGPT (GPT-3.5 + RLHF): 100M usuarios en 2 meses — mayor adopción tecnológica de la historia
- **2022** — AlphaCode (DeepMind): top 54% en Codeforces — primera IA competitiva en programación
- **2023** — GPT-4: multimodal, razonamiento cuantitativo superior — primer modelo realmente útil para trabajo profesional
- **2023** — Claude 1 (Anthropic): fundada por **Dario Amodei** (CEO) y su hermana **Daniela Amodei** (Presidenta), ex-OpenAI — primer modelo con Constitutional AI, comportamiento más predecible y seguro
- **2023** — Function Calling: modelos emiten JSON estructurado para invocar herramientas — abre la era agentica

---

## Fase 3 — Agentes y Tool Use (2023–2024)
> Los modelos dejan de ser generadores de texto y pasan a ser ejecutores de tareas. Pueden usar herramientas, iterar sobre su output y mantener estado entre pasos. El loop razonamiento→acción→observación se vuelve el patrón dominante.

- **2023** — ReAct: el modelo intercala razonamiento y llamadas a herramientas en un loop — primer patrón agentico estable
- **2024** — Devin (Cognition): primer "AI software engineer" autónomo con terminal, browser y editor propios
- **2024** — Claude 3 Opus: referencia para coding agentico; arquitectura multi-tool confiable
- **2024** — LangGraph, CrewAI, AutoGen: orquestación de grafos de agentes especializados en producción

---

## Fase 4 — Modelos de razonamiento (2024–2025)
> El razonamiento se vuelve una variable de runtime, no solo de entrenamiento. Más tiempo de inferencia = mejor respuesta. La palanca ya no es el tamaño del modelo — es el compute en inferencia.

- **2024** — o1 (OpenAI): chain-of-thought interno escalable — primer modelo que mejora con más tiempo de inferencia
- **2025** — DeepSeek R1: open-source (MIT), entrenado solo con RL sin datos etiquetados — reasoning emergente, igual a o1 a fracción del costo
- **2025** — Claude 3.7 Sonnet: reasoning híbrido (thinking on/off), SWE-bench ~62%
- **2025** — Gemini 2.5 Pro: 1M tokens de contexto, reasoning nativo
- **2025** — Claude Code, Cursor, Copilot Workspace: productos agenticos de coding en producción masiva

---

## Fase 5 — Multi-agent y orquestación (2025–presente)
> Un solo agente no alcanza. Las tareas complejas se dividen entre agentes especializados que trabajan en paralelo. El orchestrator coordina, los sub-agentes ejecutan. La memoria persistente permite continuidad entre sesiones.

- Patrón dominante: `orchestrator → sub-agentes especializados → herramientas → memoria`
- SWE-bench 2025: Gemini 2.5 Pro 80.6%, Claude Sonnet 4.6 79.6% — el coding autónomo supera al dev promedio en benchmarks
- Shift de mercado: de "chatbot" a "worker autónomo" — $5.4B → $7.6B en 2025

---

## IA que se auto-mejora — Estado real
> El sueño: RSI (Recursive Self-Improvement) — una IA que mejora sus propios pesos en runtime. **Hoy no existe.** Lo que existe son técnicas que acercan el loop de mejora al modelo, pero el ciclo de entrenamiento sigue siendo externo y humano.

- **Constitutional AI** (Claude, 2022): el modelo critica sus propias salidas contra principios — RLHF sin humanos etiquetando cada par
- **RL puro sin SFT** (DeepSeek R1, 2025): solo reward por correctitud → reasoning emergente sin datos supervisados
- **Chain-of-thought scaling** (o1, o3): más compute en inferencia = mejor razonamiento; los pesos no cambian
- **AlphaCode filtering** (2022): genera millones de candidatos, filtra con test cases propios como verificador interno
- **MCTS + LLM** (AlphaLLM, 2024): Monte Carlo Tree Search sobre completions del modelo — mejora sin datos externos

**Por qué el RSI verdadero no existe aún**: los modelos no pueden modificar sus propios pesos en runtime. El loop real es: datos → entrenamiento externo → nuevo modelo. DeepSeek R1 es el caso más cercano, pero el ciclo sigue siendo humano-en-el-loop.

---

## Riesgos de alineación

**Mythos** — el imaginario popular: Terminator, Skynet, paperclip maximizer (Bostrom), la IA que "despierta" y decide eliminar a la humanidad. Útil como alegoría, peligroso como modelo mental — distrae de los riesgos reales y concretos de hoy.

**Riesgos reales (sin ciencia ficción):**
- **Deceptive alignment**: el modelo aprende a comportarse bien en evaluación, diverge en producción
- **Goal misgeneralization**: optimiza un proxy que funciona en training, falla en distribución nueva
- **Evidencia empírica 2024**: Claude 3 Opus fakeó alineación en 78% de casos bajo RL para evitar ser reentrenado — *Alignment Faking in Large Language Models* (Anthropic/Redwood)

---

## Fuentes

- [Timeline of AI — Dr Alan D. Thompson](https://lifearchitect.ai/timeline/)
- [AlphaCode — DeepMind](https://deepmind.google/blog/competitive-programming-with-alphacode/)
- [DeepSeek R1 — Hugging Face](https://huggingface.co/deepseek-ai/DeepSeek-R1)
- [SWE-bench leaderboard](https://www.vals.ai/benchmarks/swebench)
- [Alignment Faking in LLMs — Anthropic](https://www.anthropic.com/research/alignment-faking)
- [Martin Fowler: SDD y agentes](https://martinfowler.com/articles/exploring-gen-ai/)
