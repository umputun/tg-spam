---
worth: later
where: app/webapi/config.go:267
added: 2026-08-20
---
# CAS gets disabled by any PUT /config that omits casEnabled

`updateSettingsFromForm` writes `settings.CAS.API = ""` whenever `casEnabled` is not `"on"`, with no
check that the form carried the field at all. A partial submit that never mentions CAS therefore turns
CAS off. Today this is masked: `ApplyDefaults` refills `CAS.API` on the next load, which is the bug in
issue #443. Fixing #443 removes the mask and makes the clear permanent.

Two things make this bigger than a one-line gate:

- The obvious fix is wrong. Gating on `r.Form["casEnabled"]` presence would make *disabling* CAS
  impossible, because an unchecked HTML checkbox is simply absent from the submission. It needs a
  hidden sentinel field alongside the checkbox.
- CAS is not special. About 28 booleans in the same function are assigned straight from
  `r.FormValue(...) == "on"` with no presence check. CAS only stands out because absence there clears a
  string rather than setting a bool to false. `Message.Startup` has the same shape at `:585`.

So the real question is whether `PUT /config` is a full-form-only endpoint or is meant to support partial
updates. `superUsers` (`:277`) and the 15 meta fields (`:305`) are explicitly gated on `r.Form` presence
with comments saying partial saves must preserve them, so the handler currently answers both ways. Pick
one and make the whole endpoint consistent, rather than adding a sentinel for CAS and leaving the rest.

Surfaced while investigating #443. Deliberately kept out of that fix.
