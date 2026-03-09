# How to create high-value technical tutorials

This is the foundational section. Everything below — the best practices, the difficulty levels, the exercise structure — is derived from these frameworks and principles. Understand these first.

**Diataxis** separates documentation into four types — tutorial, how-to, reference, explanation — based on two axes: learning vs. working, and practical vs. theoretical. Most bad documentation fails because it mixes these types. A tutorial that stops to explain every API field becomes a reference. A reference that tries to teach becomes a bad tutorial. Keep them separate.

**Bloom's Taxonomy** provides action verbs to write measurable learning objectives instead of vague goals. Six cognitive levels — remember, understand, apply, analyze, evaluate, create — map directly to difficulty levels: Basic targets Remember/Understand/Apply, Intermediate targets Apply/Analyze, Advanced targets Analyze/Evaluate, Insane targets Evaluate/Create.

The **Dreyfus Model** explains why novices need step-by-step rules while experts need autonomy. Learners progress through five stages (novice, advanced beginner, competent, proficient, expert), and each stage changes HOW they process information. The critical insight: the same detailed instructions that help a beginner actively hurt an advanced learner — this is the expertise reversal effect. Difficulty levels exist not as labels, but as fundamentally different instructional strategies.

**Cognitive Load Theory** limits working memory to ~4 items. Three types of load compete for that space: intrinsic (topic difficulty), extraneous (bad presentation), and germane (building mental models). Tutorials must minimize extraneous load through worked examples, adjacent explanations, no redundancy, and isolating one variable at a time between scenarios.

**Vygotsky's Zone of Proximal Development** defines scaffolding as temporary support that fades as the learner progresses. Effective tutorials operate inside the ZPD — not too easy (boring), not too hard (frustrating). Full worked examples for Basic, partial for Intermediate, hints only for Advanced, problem statement only for Insane.

**Active Recall** research shows that retrieval practice — predicting output before running a command — produces 80% retention versus 34% for passive re-reading (Roediger & Karpicke, 2006). This makes the verification section the most important part of any exercise. Later exercises should reference earlier concepts without re-explaining, forcing retrieval.

Treat tutorials as code: store in Git, write in Markdown, review through PRs, test code examples in CI. Study the style guides from Google, Microsoft, DigitalOcean, and Write the Docs for tone and structure patterns.

## Frameworks

### Diataxis — four types of documentation

Not everything is a tutorial. Diataxis separates documentation into four quadrants based on two axes: learning vs. working, and practical vs. theoretical.

- **Tutorial** — learning-oriented, practical: take the reader by the hand through a complete experience. The author is responsible for the reader's success.
- **How-to guide** — work-oriented, practical: help an already-competent user accomplish a specific goal. Assumes prior knowledge.
- **Reference** — work-oriented, theoretical: accurate, complete, factual descriptions. No interpretation, no guidance.
- **Explanation** — learning-oriented, theoretical: context, background, design decisions, "why it works this way."

Most bad documentation fails because it mixes these four types. A tutorial that stops to explain every API field becomes a reference. A reference that tries to teach becomes a bad tutorial. Keep them separate.

- [Diataxis — Start here](https://diataxis.fr/start-here/)
- [Diataxis — Full framework](https://diataxis.fr/)
- [What is Diataxis and should you use it?](https://idratherbewriting.com/blog/what-is-diataxis-documentation-framework)

### Bloom's Taxonomy — writing learning objectives

Learning objectives are not "understand X." They are measurable actions. Bloom defines six cognitive levels, each with specific verbs:

1. **Remember** — list, name, identify, recall
2. **Understand** — explain, summarize, compare, describe
3. **Apply** — use, implement, execute, solve
4. **Analyze** — differentiate, distinguish, compare, debug
5. **Evaluate** — judge, justify, choose between trade-offs
6. **Create** — design, build, compose, architect

Map difficulty levels to Bloom: Basic exercises target Remember/Understand/Apply. Intermediate targets Apply/Analyze. Advanced targets Analyze/Evaluate. Insane targets Evaluate/Create.

- [Using Bloom's Taxonomy to Write Learning Objectives](https://tips.uark.edu/using-blooms-taxonomy/)
- [Bloom's Taxonomy — Wikipedia](https://en.wikipedia.org/wiki/Bloom's_taxonomy)
- [Bloom's Taxonomy of Measurable Verbs (PDF)](https://www.utica.edu/academic/Assessment/new/Blooms%20Taxonomy%20-%20Best.pdf)

### Dreyfus Model — skill acquisition stages

Learners progress through five stages. Each stage changes HOW they process information, not just how much they know:

1. **Novice** — follows rules rigidly, needs step-by-step instructions, no judgment
2. **Advanced beginner** — recognizes patterns from experience, still needs structure
3. **Competent** — plans deliberately, handles complexity, makes conscious decisions
4. **Proficient** — grasps situations intuitively, consciously decides responses
5. **Expert** — acts from intuition, no longer relies on rules

The critical insight: what works for novices hurts experts. Detailed step-by-step instructions that help a novice create cognitive overload for an expert (the expertise reversal effect). This is why difficulty levels exist — not as labels, but as fundamentally different instructional strategies.

- [Dreyfus Model — Wikipedia](https://en.wikipedia.org/wiki/Dreyfus_model_of_skill_acquisition)
- [Dreyfus Model — CABEM Technologies](https://www.cabem.com/dreyfus-model-of-skill-acquisition/)
- [The Expertise Reversal Effect (Kalyuga et al.)](https://www.tandfonline.com/doi/abs/10.1207/S15326985EP3801_4)

### Cognitive Load Theory — why tutorials fail

Working memory can hold approximately 4 items at once. Three types of cognitive load compete for that space:

- **Intrinsic load** — the inherent difficulty of the topic itself
- **Extraneous load** — load created by bad presentation (cluttered layout, unclear instructions, missing context)
- **Germane load** — the productive effort of building mental models

The goal: minimize extraneous load so the reader can spend working memory on germane load. Practical applications:

- **Worked examples** — show the complete solution first, then fade steps gradually. Research shows worked examples outperform problem-solving for novices.
- **Split attention** — keep related information together. Code and its explanation should be adjacent, not separated by paragraphs.
- **Redundancy** — do not repeat the same information in text and diagram. One or the other.
- **Isolation** — change one variable at a time between scenarios. Multiple changes make it impossible to identify cause and effect.

- [Cognitive Load Theory — Instructional Design](https://elearningindustry.com/cognitive-load-theory-and-instructional-design)
- [Cognitive Load Theory — Structural Learning](https://www.structural-learning.com/post/cognitive-load-theory-a-teachers-guide)
- [Worked-example effect — Wikipedia](https://en.wikipedia.org/wiki/Worked-example_effect)
- [Cognitive Load Theory (John Sweller)](https://www.instructionaldesign.org/theories/cognitive-load/)

### Zone of Proximal Development — scaffolding and fading

Vygotsky's ZPD: the gap between what a learner can do alone and what they can do with guidance. Effective tutorials operate inside this zone — not too easy (boring), not too hard (frustrating).

Scaffolding is the temporary support that bridges the gap. Fading is the gradual removal of that support as the learner gains competence. In tutorial terms:

- Basic: full scaffolding (complete worked examples)
- Intermediate: partial scaffolding (some steps removed)
- Advanced: minimal scaffolding (hints only)
- Insane: no scaffolding (problem statement only)

- [Zone of Proximal Development — Simply Psychology](https://www.simplypsychology.org/zone-of-proximal-development.html)
- [Vygotsky Scaffolding — PrepScholar](https://blog.prepscholar.com/vygotsky-scaffolding-zone-of-proximal-development)
- [Guide to Vygotsky's ZPD and Scaffolding](https://elearningindustry.com/guide-to-vygotskys-zone-of-proximal-development-and-scaffolding)

### Active Recall and Spaced Repetition — retention

Reading is not learning. Retrieval practice (actively recalling information from memory) produces 80% retention after one week, compared to 34% for re-reading (Roediger & Karpicke, 2006).

Tutorial implications:

- End every exercise with verification commands that force the reader to predict output before running
- Section summaries should ask "what did you learn" not "here is what you learned"
- Later exercises should reference concepts from earlier ones without re-explaining — this forces retrieval
- Challenges at higher difficulty levels require applying concepts from previous sections in new contexts

- [Active Recall and Spaced Repetition — Recallify](https://recallify.ai/boost-memory-with-active-recall-and-spaced-repetition/)
- [Spaced Repetition — Wikipedia](https://en.wikipedia.org/wiki/Spaced_repetition)

## Documentation as Code

Tutorials are code. Treat them with the same rigor:

- Store in Git, version controlled alongside the project
- Use Markdown (portable, diffable, reviewable)
- Review changes through pull requests
- Automate validation: lint Markdown, test code examples in CI
- Modular structure: one README per exercise, sections as directories

- [Docs as Code — Write the Docs](https://www.writethedocs.org/guide/docs-as-code/)
- [What is Docs as Code — Kong](https://konghq.com/blog/learning-center/what-is-docs-as-code)
- [Adopt Docs as Code — Mintlify](https://www.mintlify.com/blog/adopt-docs-as-code)

## Style guides worth studying

- [Google Developer Documentation Style Guide](https://developers.google.com/style) — the most comprehensive, covers accessibility, API docs, naming
- [Microsoft Writing Style Guide](https://learn.microsoft.com/en-us/style-guide/welcome/) — industry standard, conversational tone
- [DigitalOcean Technical Writing Guidelines](https://www.digitalocean.com/community/tutorials/digitalocean-s-technical-writing-guidelines) — best tutorial framework with templates
- [Write the Docs — Documentation Principles](https://www.writethedocs.org/guide/writing/docs-principles/) — community-driven, open
- [Documentation Done Right — GitHub Blog](https://github.blog/developer-skills/documentation-done-right-a-developers-guide/)

## Free courses and learning resources

- [Google Technical Writing One](https://developers.google.com/tech-writing/one) — fundamentals: clarity, grammar, structure
- [Google Technical Writing Two](https://developers.google.com/tech-writing/two) — intermediate: large docs, tutorials, illustrations
- [Google Technical Writing for Accessibility](https://developers.google.com/tech-writing/accessibility) — inclusive documentation
- [Write the Docs — Software Documentation Guide](https://www.writethedocs.org/guide/index.html) — community guide for open-source projects
- [5 Free Courses to Master Technical Writing (2026)](https://hackmamba.io/technical-writing/5-free-courses-to-master-technical-writing/) — curated list
- [7 Best Technical Writing Courses — Class Central](https://www.classcentral.com/report/best-technical-writing-courses/) — ranked reviews
- [How to Write Technical Tutorials That Developers Love](https://draft.dev/learn/technical-tutorials) — practical guide from Draft.dev
- [How To Structure a Perfect Technical Tutorial](https://dev.to/dunithd/how-to-structure-a-perfect-technical-tutorial-21h9) — exercise structure breakdown

---

# Best practices for technical tutorials

- Each exercise lives in a single self-contained README.md
- All code goes in named file blocks, never inline in terminal commands
- Everything in English: prose, code, variables, filenames
- Explain WHY before showing HOW
- State prerequisites and required tools at the beginning of each exercise
- State learning objectives: what the reader will be able to do after completing it
- First exercise of each section short and motivating, last one with real-world complexity
- Use worked examples with gradual fading: full solution first, then partial, then independent
- Change one variable at a time between scenarios to isolate cause and effect
- Include multiple scenarios: success, failure, edge case
- Deliberately introduce and resolve realistic mistakes (logic bugs, environmental gotchas), not just typos
- Provide intermediate verification after each major step, not just at the end
- End with 3-5 verification commands with exact expected output
- Each step has a brief transition: what was accomplished and where it leads next
- Use analogies from the same technical context, never from everyday life
- Separate configuration from logic, never hardcode values
- Descriptive error messages, not just booleans
- Introduce testing early in the tutorial
- Organize sections by real-world domain, not by abstract level
- Exercises must be independent so readers can jump to their domain
- Directories with numeric prefix, kebab-case
- Conversational and direct tone, no formalities or emojis
- Use consistent terminology throughout all exercises
- Use accessible formatting: clear headers, whitespace, no walls of text
- Include a "what's next" section pointing to the next learning path
- Links to official documentation in each exercise
- Include additional links with solved exercises from various sources
- Test all code examples periodically to ensure they stay current
- End each section with a summary covering: key concepts learned, exercises completed, and important notes to remember

## Exercise difficulty levels

Each exercise within a section should be tagged with a difficulty level. This lets readers self-select based on experience and skip what they already know.

### Basic

- Covers: what the tool/concept IS and its general use cases
- Concepts: explain every term when introduced, assume zero prior knowledge of the tool
- Exercises: single-concept, minimal configuration, one input, one output
- Scaffolding: full worked examples with every step explained
- Verification: simple commands with obvious pass/fail output
- Goal: the reader gets a working result in minutes and builds confidence

### Intermediate

- Covers: HOW to use the tool to achieve common real-world tasks
- Concepts: reference basics without re-explaining, introduce patterns and combinations
- Exercises: multiple concepts combined, several inputs, configuration options
- Scaffolding: partial worked examples — some steps left for the reader to complete
- Verification: commands that require the reader to interpret output, not just match it
- Goal: the reader can plan, design, and execute standard tasks independently

### Advanced

- Covers: trade-offs, competing constraints, integration with other systems
- Concepts: discuss internals, performance implications, and architectural decisions
- Exercises: multi-variable scenarios, performance considerations, production-like conditions
- Scaffolding: problem statement and hints only — no worked solution provided
- Verification: the reader designs their own verification strategy
- Goal: the reader can analyze a situation, weigh options, and make informed decisions

### Insane

- Covers: edge cases at scale, unconventional uses, pushing the tool to its limits
- Concepts: no theory provided — the reader is expected to research, read source code, and discover on their own
- Exercises: open-ended challenges with no single correct answer, adversarial inputs, failure recovery
- Scaffolding: none — only the problem statement is given
- Verification: the reader defines success criteria and validates against them
- Goal: the reader can innovate, debug the unexpected, and operate under ambiguity
