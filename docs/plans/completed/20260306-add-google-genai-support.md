## Plan: Add Gemini Support for Spam Detection

Add Google Gemini as an alternative LLM spam checker alongside OpenAI, using the `google.golang.org/genai` library. Users can configure `--openai.*`, `--gemini.*`, or both. The implementation mirrors the existing OpenAI pattern with a separate `gemini.go` file, and the detector routes to both LLMs if they are active.

**Decisions**
- Coexistence: allow both `openai.token` and `gemini.token` to be set. If both are present, both will check the message.
- Separate files: `gemini.go` / `gemini_test.go` parallel to openai.go / openai_test.go
- Response name: `"gemini"` (distinct from `"openai"` in check results/UI filters)
- Detector Config: add specific `GeminiVeto` and `GeminiHistorySize` fields to avoid overlap with OpenAI settings.

---

### Phase 1: Gemini checker implementation

1. **Add go-genai dependency** — `go get google.golang.org/genai` + vendor
2. **Create lib/tgspam/gemini.go** — mirrors openai.go:
   - `geminiClient` interface wrapping `GenerateContent` method (from `client.Models`)
   - `GeminiConfig` struct (Model, SystemPrompt, CustomPrompts, MaxOutputTokens, MaxSymbolsRequest, RetryCount, CheckShortMessages)
   - `geminiChecker` struct + `newGeminiChecker()` constructor (default model: `gemma-4-31b-it`)
   - `check(msg, history)` method — same prompt format, same JSON response (`{"spam":bool,"reason":"...","confidence":1-100}`), returns `spamcheck.Response{Name: "gemini"}`
   - Uses `ResponseMIMEType: "application/json"` for structured output, `SystemInstruction` for system prompt
   - `//go:generate moq` directive for mock generation
   - Sets safety settings to `BLOCK_NONE` for all categories (Harassment, Hate Speech, Sexually Explicit, Dangerous Content) to ensure consistent spam detection for controversial or explicit spam.
3. **Create lib/tgspam/gemini_test.go** — mirror openai_test.go test patterns with mock client

### Phase 2: Detector integration

4. **Modify detector.go**:
   - Add `geminiChecker *geminiChecker` field to `Detector`
   - Add `WithGeminiChecker(client, config)` method
   - Update `Check` method to call both OpenAI and Gemini if configured
   - Update results aggregation to handle multiple LLM responses

### Phase 3: CLI and wiring

5. **Modify main.go**:
   - Add `Gemini` options struct with flags: `--gemini.token`, `--gemini.prompt`, `--gemini.custom-prompt`, `--gemini.model` (default: `gemma-4-31b-it`), `--gemini.max-tokens-response`, `--gemini.max-symbols-request`, `--gemini.retry-count`, `--gemini.history-size`, `--gemini.check-short-messages`, `--gemini.veto`
   - Update validation: allow both OpenAI & Gemini tokens to be set
   - Wire up `genai.NewClient(ctx, &genai.ClientConfig{APIKey:..., Backend: genai.BackendGeminiAPI})` → `detector.WithGeminiChecker()`
   - Map Gemini veto/history to `detectorConfig.GeminiVeto` / `GeminiHistorySize`
   - Mask Gemini token in logs
6. **Generate mock** — `go generate lib.`

### Phase 4: Web UI and documentation

7. **Modify detected_spam.html** — add `<option value="gemini">Gemini</option>` to filter dropdown
8. **Modify webapi.go** — add `case "gemini":` filter logic (~line 782)
9. **Update README.md** — add Gemini section near OpenAI, add `--gemini.*` flags to "All Application Options", note mutual exclusivity

### Phase 5: Vendor and validate

10. **Vendor** — `go mod tidy && go mod vendor`
11. **Run tests, linter, normalize comments**

---

**Relevant reference files**
- openai.go — primary implementation template (`openAIChecker`, `check()`, `sendRequest()`, `buildSystemPrompt()`)
- openai_test.go — test patterns with mock client
- openai_client.go — mock generation pattern

**Verification**
1. `go generate lib.` — generates gemini client mock
2. `go build -o tg-spam ./app` — compiles
3. `go test -race ./...` — all tests pass
4. `golangci-lint run` — clean
5. `./tg-spam --help` — shows `--gemini.*` flags
6. Verify concurrent operation when both `--openai.token` and `--gemini.token` are set
7. `unfuck-ai-comments run --fmt --skip=mocks ./...` — normalize comments

**Further Considerations**
1. **Thought tag stripping** — do not use existing `stripThoughtTags()` from openai.go (same package, so direct call works).
2. **No tokenizer needed** — Gemini checker uses simple symbol-based request truncation (no `gpt3-encoder` dependency), keeping `MaxSymbolsRequest` only.