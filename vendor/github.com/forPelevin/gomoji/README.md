# GoMoji 🔥

![Tests](https://github.com/forPelevin/gomoji/actions/workflows/tests.yml/badge.svg) [![Go Report](https://goreportcard.com/badge/github.com/forPelevin/gomoji)](https://goreportcard.com/report/github.com/forPelevin/gomoji) [![codecov](https://codecov.io/gh/forPelevin/gomoji/branch/github-actions/graph/badge.svg?token=34X68AXAMS)](https://codecov.io/gh/forPelevin/gomoji)

A high-performance Go library for working with emojis in strings, offering fast emoji detection, manipulation, and information retrieval.

## Table of Contents

- [Installation](#installation)
- [Features](#features)
- [Usage Examples](#usage-examples)
  - [Check for Emojis](#check-for-emojis)
  - [Find All Emojis](#find-all-emojis)
  - [Remove Emojis](#remove-emojis)
  - [Replace Emojis](#replace-emojis)
  - [Get Emoji Information](#get-emoji-information)
- [API Documentation](#api-documentation)
- [Performance](#performance)
- [Contributing](#contributing)
- [License](#license)

## Installation

### Prerequisites

- Go 1.11 or higher (for module support)

### Installing

```sh
go get -u github.com/forPelevin/gomoji
```

## Features

- 🔎 Fast emoji detection in strings
- 👪 Find all emoji occurrences with detailed information
- 🌐 Access comprehensive emoji database
- 🧹 Remove emojis from strings
- ↔️ Replace emojis with custom characters
- ↕️ Custom emoji replacement functions
- 🧐 Detailed emoji information lookup

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
- `RemoveEmojis(s string) string` - Removes all emojis from a string
- `ReplaceEmojisWith(s string, c rune) string` - Replaces emojis with a specified character
- `GetInfo(emoji string) (Emoji, error)` - Gets detailed information about an emoji
- `AllEmojis() []Emoji` - Returns all available emojis

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

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Contact

Vlad Gukasov [@vgukasov](https://www.facebook.com/vgukasov)

## License

GoMoji is available under the MIT [License](/LICENSE).
GoMoji source code is available under the MIT [License](/LICENSE).
