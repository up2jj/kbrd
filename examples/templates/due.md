---
name: Task with due date
filename: "{{slug .title}}"
steps:
  - title: Task
    fields:
      - key: title
        type: input
        title: Title
        required: true
      - key: when
        type: input
        title: Due (natural language)
        placeholder: e.g. next friday, in 2 weeks, za 2 tygodnie
        required: true
---
# {{.title}}

- Due: {{date .when}}
- Created: {{now "2006-01-02"}}

<!--
  {{date .when}} parses the phrase you typed (English or Polish) into a date.
  Pass a Go layout as a second argument for a different format, e.g.
  {{date .when "Mon, 02 Jan 2006"}}. An unparseable phrase fails creation.
-->
