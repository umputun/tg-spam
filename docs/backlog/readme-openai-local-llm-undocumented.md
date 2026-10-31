---
worth: yes
where: README.md
added: 2026-10-31
---
# README misstates how OpenAI is enabled and omits local LLM use

The "OpenAI integration" section says the integration is enabled by setting `--openai.token`, but
`Settings.IsOpenAIEnabled()` (`app/config/settings.go`) returns true when either the token or
`--openai.apibase` is set, and `app/main.go` gates the provider on that predicate. The section also never
says that `--openai.apibase` pointed at an OpenAI-compatible server (Ollama, LM Studio) runs the check on
a local model. Users ask for it as a missing feature: https://github.com/umputun/tg-spam/discussions/273.
Fix is a few README lines: correct the enable sentence and add a short local-LLM example with
`--openai.apibase`, `--openai.model` and a dummy token.
