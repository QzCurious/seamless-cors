# Diagram authoring rules

## Scope

These rules apply to files under `diagrams/`.

## Diagram intent

- Diagrams are thread/presentation-first explanations of how seamless-cors evolved across versions.
- Optimize for a speaker to explain the big picture, not for exhaustive standalone specification.
- Prefer high-level product concepts over implementation internals.

## Version files

- Keep all diagrams for a version in that version file, such as `v1.md` and `v2.md`.
- Keep version files diagram-only: use `##` section headers and Mermaid diagram blocks, not explanatory prose.

## Recommended diagrams per version

- `Overview`: high-level system shape and major responsibilities.
- `Setup`: what the user must wire up before browser traffic works. Prefer user-visible steps and do not infer automation unless it is explicit for that version.
- `Runtime Request Flow`: browser traffic routing and CORS repair behavior.

## Diagram types

- Use `flowchart` for overview, setup, lifecycle, ownership, and decision trees.
- Use `sequenceDiagram` for runtime request flows and multi-party interactions over time.
- Use `stateDiagram-v2` only when lifecycle states are the main point.

## Diagram writing

- Draw diagrams at the product level unless the user explicitly asks for implementation detail.
- For setup flowcharts, prefer stable roles/entities as boxes and user actions as edge labels.
- Prefer names from root `CONTEXT.md` domain terms.
- Keep command names literal, such as `start`, `stop`, and `check`.
- Use participants and nodes from `CONTEXT.md` when needed, but keep the visible set small enough for a slide.
- Ask before introducing a new user-facing concept that is not already in `CONTEXT.md`.
- Show both positive and negative branches when a decision is relevant.
- Keep messages in 台灣繁體中文.
- Keep messages short and action-oriented.
- Avoid startup, validation, reload, cleanup, listener, port, token, and state-cache details unless they are the point of the diagram.
