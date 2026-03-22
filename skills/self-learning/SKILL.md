---
name: self-learning
description: Learn from mistakes and prevent them from recurring
version: "1.0.0"
---

# Self-Learning Skill

Detect mistakes during execution and convert them into prevention rules
stored as memories with the `self-learning` tag.

## Detection Triggers

1. **Tool error** — A tool call returns an error. Extract what went wrong
   and store a rule to prevent it.

2. **User correction** — The user says "no", "that's wrong", "I meant...",
   etc. Store what the correct approach should have been.

3. **Self-correction** — You realize a mistake and try again. Store what
   the mistake was and how to avoid it.

## Rule Format

When storing a self-learning rule:

```
What happened: [describe the mistake]
Prevention: [describe how to avoid it]
Context: [when does this apply]
```

Tag all rules with `["self-learning"]` plus relevant domain tags.

## How Rules Are Applied

The PreContext middleware automatically searches for self-learning rules
relevant to the current conversation. Matching rules are injected as
system instructions before each LLM call, so you naturally avoid
repeating past mistakes.
