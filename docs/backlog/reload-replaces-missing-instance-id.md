---
worth: yes
where: app/webapi/config.go:loadConfigHandler
added: 2026-09-25
---
# /config/reload replaces a missing instance_id with the default

`loadConfigFromDB` keeps the CLI `--instance-id` when the stored blob has no `instance_id`, which is
what externally written blobs look like. `loadConfigHandler` does not: after `Load` the field is empty,
`ReloadNormalize` runs `ApplyDefaults`, and `ApplyDefaults` skips only `Transient` and `zeroAwarePaths`,
so `InstanceID` becomes the template's `tg-spam`.

The wrong value then gets persisted. The next settings save (`updateConfigHandler`, `saveConfigHandler`)
writes `instance_id: "tg-spam"` into the blob, and on the next start `loadConfigFromDB` trusts a
non-empty persisted `instance_id` over the CLI value, only logging a warning. Every per-instance store
then binds to gid `tg-spam`, and the Argon2 salt for encrypted fields changes with it.

Traced by reading the code, not reproduced. Surfaced by codex while reviewing the PR for #460. Fix
direction: keep the in-memory `InstanceID` across reload when the loaded one is empty, the same rule
startup applies.
