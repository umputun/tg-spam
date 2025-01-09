# GoMoji 🔥

![Tests](https://github.com/forPelevin/gomoji/actions/workflows/tests.yml/badge.svg) [![Go Report](https://goreportcard.com/badge/github.com/forPelevin/gomoji)](https://goreportcard.com/report/github.com/forPelevin/gomoji) [![codecov](https://codecov.io/gh/forPelevin/gomoji/branch/github-actions/graph/badge.svg?token=34X68AXAMS)](https://codecov.io/gh/forPelevin/gomoji)

GoMoji is a Go package that provides a 🚀[fast](#performance) and ✨[simple](#check-string-contains-emoji) way to work with emojis in strings.
It has features such as:
 * [check whether string contains emoji](#check-string-contains-emoji) 🔎
 * [find all emojis in string](#find-all) 👪
 * [get all emojis](#get-all) 🌐
 * [remove all emojis from string](#remove-all-emojis) 🧹
 * [replace all emojis in a string with a specified rune](#replace-all-emojis-with-a-specified-rune) ↔️
* [custom emojis replacements in a string](#replace-all-emojis-with-the-result-of-custom-replacer-function) ↕️
 * [get emoji description](#get-emoji-info) 🧐

Getting Started
===============

## Installing

To start using GoMoji, install Go and run `go get`:

```sh
$ go get -u github.com/forPelevin/gomoji
```

This will retrieve the package.

## Check string contains emoji
```go
package main

import (
    "github.com/forPelevin/gomoji"
)

func main() {
    res := gomoji.ContainsEmoji("hello world")
    println(res) // false
    
    res = gomoji.ContainsEmoji("hello world 🤗")
    println(res) // true
}
```

## Find all
The function searches for all emoji occurrences in a string. It returns a nil slice if there are no emojis.
```go
package main

import (
    "github.com/forPelevin/gomoji"
)

func main() {
    res := gomoji.FindAll("🧖 hello 🦋 world")
    println(res)
}
```

Result:

```go
[]gomoji.Emoji{
    {
        Slug:        "person-in-steamy-room",
        Character:   "🧖",
        UnicodeName: "E5.0 person in steamy room",
        CodePoint:   "1F9D6",
        Group:       "People & Body",
        SubGroup:    "person-activity",
    },
    {
        Slug:        "butterfly",
        Character:   "🦋",
        UnicodeName: "E3.0 butterfly",
        CodePoint:   "1F98B",
        Group:       "Animals & Nature",
        SubGroup:    "animal-bug",
    },
}
```

## Get all
The function returns all existing emojis. You can do whatever you need with the list.
 ```go
 package main
 
 import (
     "github.com/forPelevin/gomoji"
 )
 
 func main() {
     emojis := gomoji.AllEmojis()
     println(emojis)
 }
 ```

## Remove all emojis

The function removes all emojis from given string:

```go
res := gomoji.RemoveEmojis("🧖 hello 🦋world")
println(res) // "hello world"
```

## Replace all emojis with a specified rune

The function replaces all emojis in a string with a given rune:

```go
res := gomoji.ReplaceEmojisWith("🧖 hello 🦋world", '_')
println(res) // "_ hello _world"
```

## Replace all emojis with the result of custom replacer function

The function replaces all emojis in a string with the result of given replacer function:

```go
res := gomoji.ReplaceEmojisWithFunc("🧖 hello 🦋 world", func(em Emoji) string {
    return em.Slug
})
println(res) // "person-in-steamy-room hello butterfly world"
```

## Get emoji info

The function returns info about provided emoji:

```go
info, err := gomoji.GetInfo("1") // error: the string is not emoji
info, err := gomoji.GetInfo("1️⃣")
println(info)
```

Result:

```go
gomoji.Entity{
    Slug:        "keycap-1",
    Character:   "1️⃣",
    UnicodeName: "E0.6 keycap: 1",
    CodePoint:   "0031 FE0F 20E3",
    Group:       "Symbols",
    SubGroup:    "keycap",
}
```

## Emoji entity
All searching methods return the Emoji entity which contains comprehensive info about emoji.
```go
type Emoji struct {
    Slug        string `json:"slug"`
    Character   string `json:"character"`
    UnicodeName string `json:"unicode_name"`
    CodePoint   string `json:"code_point"`
    Group       string `json:"group"`
    SubGroup    string `json:"sub_group"`
}
 ```
Example:
```go
[]gomoji.Emoji{
    {
        Slug:        "butterfly",
        Character:   "🦋",
        UnicodeName: "E3.0 butterfly",
        CodePoint:   "1F98B",
        Group:       "Animals & Nature",
        SubGroup:    "animal-bug",
    },
    {
        Slug:        "roll-of-paper",
        Character:   "🧻",
        UnicodeName: "E11.0 roll of paper",
        CodePoint:   "1F9FB",
        Group:       "Objects",
        SubGroup:    "household",
    },
}
 ```

## Performance

GoMoji Benchmarks

```
go test -bench=. -benchmem -v -run Benchmark ./...
goos: darwin
goarch: arm64
pkg: github.com/forPelevin/gomoji
BenchmarkContainsEmojiParallel
BenchmarkContainsEmojiParallel-10       150872365                7.207 ns/op           0 B/op          0 allocs/op
BenchmarkContainsEmoji
BenchmarkContainsEmoji-10               20428110                58.86 ns/op            0 B/op          0 allocs/op
BenchmarkRemoveEmojisParallel
BenchmarkRemoveEmojisParallel-10         6463779               193.1 ns/op            68 B/op          2 allocs/op
BenchmarkRemoveEmojis
BenchmarkRemoveEmojis-10                  882298              1341 ns/op              68 B/op          2 allocs/op
BenchmarkGetInfoParallel
BenchmarkGetInfoParallel-10             447353414                2.428 ns/op           0 B/op          0 allocs/op
BenchmarkGetInfo
BenchmarkGetInfo-10                     57733017                20.32 ns/op            0 B/op          0 allocs/op
BenchmarkFindAllParallel
BenchmarkFindAllParallel-10              4001587               305.8 ns/op           296 B/op          4 allocs/op
BenchmarkFindAll
BenchmarkFindAll-10                       694861              1675 ns/op             296 B/op          4 allocs/op
PASS
ok      github.com/forPelevin/gomoji    11.192s
```

## Contact
Vlad Gukasov [@vgukasov](https://www.facebook.com/vgukasov)

## License

GoMoji source code is available under the MIT [License](/LICENSE).