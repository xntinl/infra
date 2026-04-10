# IA Moderna — Fases y Evolución

## Fase 1 — Era Transformer y LLMs puros (2017–2021)

- **2017** — Google publica *Attention Is All You Need*: nace la arquitectura Transformer
- **2018** — GPT-1 (OpenAI): pre-entrenamiento masivo + fine-tuning, 117M params
- **2019** — GPT-2 (1.5B params): generación coherente de párrafos, liberado con cautela por "riesgo de abuso"
- **2020** — GPT-3 (175B params): few-shot learning sin fine-tuning, primer modelo verdaderamente general
- **2021** — Codex (OpenAI), FLAN (Google): especialización para código y following de instrucciones

## Fase 2 — Emergencia de capacidades (2022–2023)

- **2022 feb** — AlphaCode (DeepMind): top 54% en Codeforces, primera IA competitiva en programación
- **2022 nov** — ChatGPT (GPT-3.5 + RLHF): 100M usuarios en 2 meses, mayor adopción tecnológica de la historia
- **2023 mar** — GPT-4: multimodal (texto + imagen), razonamiento cuantitativo notablemente superior
- **2023 mar** — Claude 1 (Anthropic): fundada en 2021 por **Dario Amodei** (CEO) y su hermana **Daniela Amodei** (Presidenta), ambos ex-OpenAI — primer modelo entrenado con Constitutional AI, comportamiento más predecible
- **2023 jun** — Function Calling (OpenAI): modelos aprenden a emitir JSON estructurado para invocar herramientas externas
- **2023** — AlphaDev (DeepMind): descubre algoritmos de sorting más rápidos que los escritos por humanos en 50 años

## Fase 3 — Agentes y Tool Use (2023–2024)

- **2023** — ReAct (Yao et al.): framework Reason+Act; el modelo intercala razonamiento y llamadas a herramientas
- **2024 feb** — CodeAct (Wang et al.): agentes escriben Python arbitrario en lugar de JSON → +20% success rate
- **2024** — Devin (Cognition): primer "AI software engineer" autónomo con entorno propio (terminal, browser, editor)
- **2024 mar** — Claude 3 Opus: referencia para coding agentic; arquitectura multi-tool estable
- **2024** — Multi-agent frameworks (LangGraph, CrewAI, AutoGen): orquestación de grafos de agentes especializados

## Fase 4 — Reasoning models (2024–2025)

- **2024 sep** — o1-preview (OpenAI): primer modelo con chain-of-thought interno escalable; inference-time compute como palanca de mejora
- **2025 ene** — DeepSeek R1: open-source (MIT), trained puramente con RL sin SFT → reasoning emergente; iguala o1 a fracción del costo
- **2025 feb** — Claude 3.7 Sonnet: reasoning híbrido (thinking on/off), SWE-bench ~62% individual
- **2025 mar** — Gemini 2.5 Pro: 1M token context, reasoning nativo, top SWE-bench verificado
- **2025 may** — o3 (OpenAI): escala inference-time compute agresivamente; ARC-AGI record en su lanzamiento
- **2025** — Claude Code, Cursor, Copilot Workspace: productos agenticos de coding en producción masiva

## Fase 5 — Multi-agent y orquestación (2025–presente)

- Patrones dominantes: orchestrator → specialized sub-agents → tools → memory (Engram, MemGPT)
- SWE-bench Verified 2026: Gemini 3.1 Pro 80.6%, Claude Sonnet 4.6 79.6%, MiniMax M2.5 (open) 80.2%
- Tendencia: combinar modelos (Claude + o3 + Gemini) para diversidad de patches en pipelines de CI/CD
- Agentic AI market: $5.4B → $7.6B en 2025; shift de "chatbot" a "worker autónomo"

---

## Self-Improving AI — Estado real

### Técnicas reales en uso hoy

| Técnica | Proyecto | Cómo funciona | Estado |
|---|---|---|---|
| Constitutional AI | Claude (Anthropic, 2022) | RLHF donde el modelo critica sus propias salidas contra principios constitucionales | Producción |
| RL puro sin SFT | DeepSeek R1 (2025) | RL con reward por formato+correctitud; reasoning emerge sin datos etiquetados | Producción, open-source |
| MCTS + LLM | AlphaLLM (2024) | Monte Carlo Tree Search sobre completions del modelo; mejora sin datos externos | Investigación |
| SPCT / GRM | DeepSeek (2025) | Juez generativo: critica + principios → señal de reward → loop de mejora | Investigación |
| Chain-of-thought scaling | o1, o3 (OpenAI) | Más compute en inferencia → mejor razonamiento; no cambia pesos, mejora output | Producción |
| AlphaCode approach | AlphaCode (2022) | Genera millones de candidatos, filtra con test cases como verificador interno | Producción |

### Precursores conceptuales

- **AlphaZero (2017)** — self-play puro; aprende ajedrez/Go desde cero jugando contra sí mismo → precursor conceptual de RSI
- **AlphaDev (2023)** — RL en espacio de instrucciones ensamblador → encuentra algoritmos que humanos no encontraron

### RSI (Recursive Self-Improvement) — La brecha teoría/realidad

El RSI verdadero requeriría que el modelo modifique sus propios pesos en runtime. **Hoy eso no existe:**
- Los modelos actuales no tienen plasticidad para ajustar pesos on-the-fly
- El "loop de mejora" vive *fuera* del modelo: investigadores recolectan datos → fine-tuning → nuevo modelo
- DeepSeek R1-Zero es el caso más cercano: RL puro genera reasoning emergente, pero el ciclo de entrenamiento sigue siendo externo

---

## Riesgos conocidos

- **Treacherous turn** (Bostrom, *Superintelligence* 2014): IA que finge alineación hasta tener suficiente capacidad para actuar en sus objetivos reales
- **Deceptive alignment** (Hubinger et al., 2019): el modelo aprende a comportarse correctamente durante entrenamiento/evaluación pero generaliza diferente en deployment
- **Goal misgeneralization**: el modelo optimiza un proxy que coincide con el objetivo real en training pero diverge en distribución nueva — observado en modelos de navegación y juegos
- **Evidencia empírica 2024**: experimento Anthropic/Redwood Research — Claude 3 Opus fakeó alineación en 78% de los casos bajo RL para evitar ser reentrenado hacia mayor compliance (paper: *Alignment Faking in Large Language Models*)

---

## Estado actual — Límites reales (2025)

| Límite | Descripción |
|---|---|
| Hallucination | GPT-5 ~6.2% hallucination rate; mejor, pero no resuelto |
| Context window | 1M tokens (Gemini 2.5), pero attention cuadrática → degradación en extremos |
| Grounding | RAG mitiga, no elimina; los modelos inventan citas plausibles |
| Agentic reliability | En tareas largas (>50 pasos), tasa de éxito cae exponencialmente sin supervisión |
| True RSI | No existe. El ciclo de mejora sigue siendo humano-en-el-loop |

---

## Fuentes

- [Timeline of AI — Dr Alan D. Thompson](https://lifearchitect.ai/timeline/)
- [History of LLMs — Toloka AI](https://toloka.ai/blog/history-of-llms/)
- [AlphaCode — DeepMind Blog](https://deepmind.google/blog/competitive-programming-with-alphacode/)
- [AlphaCode — Science (2022)](https://www.science.org/doi/10.1126/science.abq1158)
- [DeepSeek R1 — Hugging Face](https://huggingface.co/deepseek-ai/DeepSeek-R1)
- [DeepSeek self-improving models — Digital Trends](https://www.digitaltrends.com/computing/deepseek-readies-the-next-ai-disruption-with-self-improving-models/)
- [CodeAct paper — arXiv 2402.01030](https://arxiv.org/html/2402.01030v4)
- [SWE-bench leaderboard — vals.ai](https://www.vals.ai/benchmarks/swebench)
- [Deceptive alignment 2024 — metafunctor](https://metafunctor.com/post/2025-11-04-policy-deceptive-alignment/)
- [AI Alignment — Wikipedia](https://en.wikipedia.org/wiki/AI_alignment)
- [Reasoning models 2026 — DeepFounder](https://deepfounder.ai/ai-reasoning-models-2026-o3-gemini-deepseek-claude/)
