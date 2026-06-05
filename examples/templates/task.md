---
name: Task
filename: "{{slug .title}}"
steps:
  - title: Task
    fields:
      - key: title
        type: input
        title: Title
        placeholder: short description of the task
        required: true
      - key: priority
        type: select
        title: Priority
        options: [low, normal, high]
        default: normal
      - key: notes
        type: text
        title: Notes
        placeholder: optional context, links, acceptance criteria
---
# {{.title}}

- Priority: {{.priority}}

{{.notes}}
