---
worth: yes
where: _examples/lua_plugins/README.md:43
added: 2026-08-30
---
# lua examples README omits has_external_reply from the meta field list

`lib/tgspam/plugin/checker.go:211` exposes `has_external_reply` to plugins, and the main README's
equivalent list carries it. The `_examples/lua_plugins/README.md` list stops at `has_contact` (:43), so a
plugin author working from the examples cannot know the field exists without reading Go source.

Missed when `--meta.external-reply` was added (d26b9e91); the examples copy was never updated alongside
the main README.

Fix: add this line after the `has_contact` entry, matching `checker.go`'s ordering:

```
  - `has_external_reply`: Boolean indicating if the message replies to a message in another chat
```

Surfaced reviewing PR #447, which touches this list to add its own field.
