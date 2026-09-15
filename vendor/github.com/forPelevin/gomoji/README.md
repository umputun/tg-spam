# GoMoji 🔥

![Tests](https://github.com/forPelevin/gomoji/actions/workflows/tests.yml/badge.svg) [![Go Report](https://goreportcard.com/badge/github.com/forPelevin/gomoji)](https://goreportcard.com/report/github.com/forPelevin/gomoji) [![codecov](https://codecov.io/gh/forPelevin/gomoji/branch/github-actions/graph/badge.svg?token=34X68AXAMS)](https://codecov.io/gh/forPelevin/gomoji) [![OpenSSF Best Practices](https://www.bestpractices.dev/projects/7105/badge)](https://www.bestpractices.dev/projects/7105)

GoMoji makes it easy to detect, remove, replace, and look up emojis in Go strings.

It solves the common problem that emojis aren't always a single "character"—GoMoji handles them correctly so your string logic just works.

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Features](#features)
- [Usage Examples](#usage-examples)
  - [Check for Emojis](#check-for-emojis)
  - [Find All Emojis](#find-all-emojis)
  - [Remove Emojis](#remove-emojis)
  - [Replace Emojis](#replace-emojis)
  - [Get Emoji Information](#get-emoji-information)
- [API Documentation](#api-documentation)
- [Automated Updates](#automated-updates)
- [Performance](#performance)
- [Security](#security)
- [Feedback](#feedback)
- [Contributing](#contributing)
- [License](#license)

## Installation

### Prerequisites

- Go 1.11 or higher (for module support)

### Installing

```sh
go get -u github.com/forPelevin/gomoji
```

## Quick Start

Create a minimal program that detects and removes emojis:

```go
package main

import (
    "fmt"
    "github.com/forPelevin/gomoji"
)

func main() {
    s := "Hello 👋 world 🦋"
    fmt.Println(gomoji.ContainsEmoji(s)) // true
    fmt.Println(gomoji.RemoveEmojis(s))  // "Hello  world "
}
```

Then run:

```sh
go mod init example.com/hello
go get github.com/forPelevin/gomoji@latest
go run .
```

## Features

- 🔎 Fast emoji detection in strings
- 👪 Find all emoji occurrences with detailed information
- 🌐 Access comprehensive emoji database
- 🧹 Remove emojis from strings
- ↔️ Replace emojis with custom characters
- ↕️ Custom emoji replacement functions
- 🧐 Detailed emoji information lookup
- 🔄 Automated Unicode updates via [gomoji-updater](https://github.com/forPelevin/gomoji-updater)

## Usage Examples

### Check for Emojis

```go
package main

import "github.com/forPelevin/gomoji"

func main() {
    hasEmoji := gomoji.ContainsEmoji("hello world 🤗")
    println(hasEmoji) // true
}
```

### Find All Emojis

```go
emojis := gomoji.FindAll("🧖 hello 🦋 world")
// Returns slice of Emoji structs with detailed information
```

### Remove Emojis

```go
cleaned := gomoji.RemoveEmojis("🧖 hello 🦋 world")
println(cleaned) // "hello world"
```

### Replace Emojis

```go
// Replace with a specific character
replaced := gomoji.ReplaceEmojisWith("🧖 hello 🦋 world", '_')
println(replaced) // "_ hello _ world"

// Replace with custom function
customReplaced := gomoji.ReplaceEmojisWithFunc("🧖 hello 🦋 world", func(em Emoji) string {
    return em.Slug
})
println(customReplaced) // "person-in-steamy-room hello butterfly world"
```

### Get Emoji Information

```go
info, err := gomoji.GetInfo("🦋")
if err == nil {
    println(info.Slug)        // "butterfly"
    println(info.UnicodeName) // "E3.0 butterfly"
    println(info.Group)       // "Animals & Nature"
}
```

## API Documentation

### Emoji Structure

```go
type Emoji struct {
    Slug        string `json:"slug"`        // Unique identifier
    Character   string `json:"character"`   // The emoji character
    UnicodeName string `json:"unicode_name"` // Official Unicode name
    CodePoint   string `json:"code_point"`  // Unicode code point
    Group       string `json:"group"`       // Emoji group category
    SubGroup    string `json:"sub_group"`   // Emoji subgroup category
}
```

### Core Functions

- `ContainsEmoji(s string) bool` - Checks if a string contains any emoji
- `FindAll(s string) []Emoji` - Finds all unique emojis in a string
- `CollectAll(s string) []Emoji` - Finds all emojis including repeats (preserves order)
- `RemoveEmojis(s string) string` - Removes all emojis from a string
- `ReplaceEmojisWith(s string, c rune) string` - Replaces emojis with a specified character
- `ReplaceEmojisWithSlug(s string) string` - Replaces emojis with their slugs
- `ReplaceEmojisWithFunc(s string, replacer func(Emoji) string) string` - Replaces emojis via a custom function
- `GetInfo(emoji string) (Emoji, error)` - Gets detailed information about an emoji; returns `ErrStrNotEmoji` if not found
- `AllEmojis() []Emoji` - Returns all available emojis

For full reference documentation generated from source, see the package docs on `pkg.go.dev`: [github.com/forPelevin/gomoji](https://pkg.go.dev/github.com/forPelevin/gomoji).

## Automated Updates

GoMoji automatically stays up-to-date with the latest Unicode emoji standards through our automated update system:

- **Daily Updates**: Our GitHub Actions workflow runs daily to check for new Unicode emoji releases
- **Automated PRs**: When new emoji data is available, automated pull requests are created for review
- **Source**: Emoji data is fetched from the official Unicode Consortium: https://unicode.org/Public/emoji/latest/
- **Tool**: Updates are generated using [gomoji-updater](https://github.com/forPelevin/gomoji-updater), a specialized tool for processing Unicode emoji data

## Performance

GoMoji is designed for high performance, with parallel processing capabilities for optimal speed. Here are the key benchmarks:

| Operation         | Sequential Performance | Parallel Performance | Allocations           |
| ----------------- | ---------------------- | -------------------- | --------------------- |
| Contains Emoji    | 57.49 ns/op            | 7.167 ns/op          | 0 B/op, 0 allocs/op   |
| Remove Emojis     | 1454 ns/op             | 201.4 ns/op          | 68 B/op, 2 allocs/op  |
| Find All          | 1494 ns/op             | 313.8 ns/op          | 288 B/op, 2 allocs/op |
| Get Info          | 8.591 ns/op            | 1.139 ns/op          | 0 B/op, 0 allocs/op   |
| Replace With Slug | 3822 ns/op             | 602.4 ns/op          | 160 B/op, 3 allocs/op |

Full benchmark details:

```
go test -bench=. -benchmem -v -run Benchmark ./...
goos: darwin
goarch: arm64
pkg: github.com/forPelevin/gomoji
cpu: Apple M1 Pro
BenchmarkContainsEmojiParallel-10               164229604                7.167 ns/op           0 B/op             0 allocs/op
BenchmarkContainsEmoji-10                       20967183                57.49 ns/op            0 B/op             0 allocs/op
BenchmarkReplaceEmojisWithSlugParallel-10        2160638               602.4 ns/op           160 B/op             3 allocs/op
BenchmarkReplaceEmojisWithSlug-10                 309879              3822 ns/op             160 B/op             3 allocs/op
BenchmarkRemoveEmojisParallel-10                 5794255               201.4 ns/op            68 B/op             2 allocs/op
BenchmarkRemoveEmojis-10                          830334              1454 ns/op              68 B/op             2 allocs/op
BenchmarkGetInfoParallel-10                     989043939                1.139 ns/op           0 B/op             0 allocs/op
BenchmarkGetInfo-10                             139558108                8.591 ns/op           0 B/op             0 allocs/op
BenchmarkFindAllParallel-10                      4029028               313.8 ns/op           288 B/op             2 allocs/op
BenchmarkFindAll-10                               751990              1494 ns/op             288 B/op             2 allocs/op
```

> Note: Benchmarks were performed on Apple M1 Pro processor. Your results may vary depending on hardware.

## Security

- This is a string-processing library, not a general-purpose sanitizer. Do not rely on it to prevent XSS or other injection attacks; use a proper HTML/markup sanitizer where needed.
- Emoji detection and replacement operate on Unicode grapheme clusters. Do not assume 1 code point == 1 visible symbol.
- Variation selectors are stripped during processing to normalize output. If your application depends on preserving exact variation selectors, avoid `RemoveEmojis`/`Replace*` or handle this explicitly.
- When replacing with slugs, treat the resulting text as untrusted like any other user-controlled string and escape/encode as appropriate for the output context.
- If you need normalization against visually confusable characters, use additional tooling (e.g., `golang.org/x/text` packages) alongside this library.

## Feedback

- Bug report: please [open an issue](https://github.com/forPelevin/gomoji/issues/new/choose) with a minimal repro and environment details.
- Feature request: [suggest an enhancement](https://github.com/forPelevin/gomoji/issues/new/choose) describing the use case and expected behavior.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### Requirements

- Follow Go’s style guidelines:
  - [Effective Go](https://go.dev/doc/effective_go)
  - [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- Keep code formatted and linted: run `make lint-fix` (uses `goimports`, `gofmt`, and `golangci-lint`).
- Add tests for new functionality and fixes; all tests must pass: `make test` or `go test ./...`.
- For performance-sensitive changes, include relevant benchmarks or comparisons (`make bench`).

### How to contribute

1. Fork the repo and create a feature branch.
2. Make your changes, run linters and tests locally.
3. Open a PR describing the change and linking any related issue: [open a pull request](https://github.com/forPelevin/gomoji/compare).

## Contact

[Vlad Gukasov](https://github.com/forPelevin)

## License

GoMoji is available under the MIT [License](/LICENSE).
GoMoji source code is available under the MIT [License](/LICENSE).
