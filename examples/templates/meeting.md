---
name: Meeting notes
filename: '{{now "2006-01-02"}}-{{slug .topic}}'
steps:
  - title: Meeting
    fields:
      - key: topic
        type: input
        title: Topic
        required: true
      - key: attendees
        type: input
        title: Attendees
        placeholder: comma-separated names
  - title: Notes
    fields:
      - key: agenda
        type: text
        title: Agenda
      - key: followup
        type: confirm
        title: Needs a follow-up meeting?
---
# Meeting: {{title .topic}}

- Date: {{now "Jan 2, 2006 15:04"}}
- Attendees: {{default "(none listed)" .attendees}}
- Follow-up needed: {{.followup}}

## Agenda
{{.agenda}}

## Decisions

## Action items
