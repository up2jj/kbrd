---
name: Bug report
filename: "bug-{{slug .title}}"
steps:
  - title: Basics
    fields:
      - key: title
        type: input
        title: Bug title
        required: true
        min_len: 3
      - key: ticket
        type: input
        title: Ticket ID (optional)
        pattern: '^KB-[0-9]+$'
        pattern_hint: must look like KB-123
      - key: severity
        type: select
        title: Severity
        options: [low, medium, high, critical]
        default: medium
      - key: areas
        type: multiselect
        title: Affected areas
        options: [UI, backend, data, build, docs]
  - title: Details
    fields:
      - key: repro
        type: text
        title: Repro steps
        placeholder: how to trigger the bug
      - key: regression
        type: confirm
        title: Is this a regression?
---
# Bug: {{.title}}

- Ticket: {{.ticket}}
- Severity: {{.severity}}
- Areas: {{join .areas ", "}}

## Affected checklist
{{checklist .areas}}
- Regression: {{.regression}}

## Repro
{{.repro}}

## Expected

## Actual
