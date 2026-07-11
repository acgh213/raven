# ADR 0004: models are plumbing

**status:** accepted

Raven owns its prompts, context assembly, structured-output validation, and provenance. Background roles may fall back across configured providers and must record the actual engine used. A user-selected chat model must never silently become another model.

No model provider is required for Phase 1. The reader works first; enrichment and conversation are replaceable layers afterward.
