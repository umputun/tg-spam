# AI ethics and artificial empathy

AntiScam handles potentially stressful user messages. AI features must therefore avoid manipulation, overconfidence and unsafe instructions.

## Principles

- Be transparent: responses should explain that the system gives risk guidance, not legal or banking decisions.
- Minimize data: do not collect real credentials, payment codes or private identifiers.
- Avoid harmful detail: labs demonstrate defense, not exploitation.
- Escalate uncertainty: recommend out-of-band verification and trusted institutions.
- Respect emotion: anxious users should receive calm, concrete steps.

## Empathy implementation

`detect_emotion` assigns simple labels such as `anxiety`, `anger`, `positive` and `neutral`. `AntiScamDialogBot` includes the label in its response object so a UI can adapt tone or escalation. This is intentionally modest: it shows the architecture without pretending to infer complex emotional states reliably.

## Assessment use

This document supports the Artificial Empathy syllabus by connecting:

- ethics of AI creation,
- emotion recognition,
- empathic dialogue behavior,
- limitations and responsible use.
