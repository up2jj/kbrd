---
# Card template that calls an external command via {{shell}}.
#
# Requires opt-in: set `[template] exec = true` in kbrd.toml (off by default —
# a {{shell}} command runs with your full environment; see SECURITY.md). It
# also needs an LLM CLI on your PATH — this example uses `claude`, but any
# command that reads a prompt on stdin works (e.g. `llm`, `ollama run <model>`).
#
# The card is created immediately with a "⏳ running…" placeholder; the command
# runs in the background and its output replaces the placeholder when it
# finishes (the board live-reloads). Launch `kbrd --safe` to defuse all of this.
name: AI summary
filename: "summary-{{slug .topic}}"
steps:
  - title: Topic
    fields:
      - key: topic
        type: input
        title: What should I summarise?
        required: true
      - key: notes
        type: text
        title: Notes / source text
        placeholder: paste the material to summarise
---
# {{title .topic}}

- Created: {{now "2006-01-02 15:04"}}

## Source
{{default "_(none)_" .notes}}

## Summary
{{shell "claude -p 'Summarise the following into 3 terse bullet points.'" .topic "\n\n" .notes}}
