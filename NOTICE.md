# Notice

gositter is MIT licensed, see `LICENSE`. It derives from the projects below.
Each license text is in the repository, unchanged, at the path given.

| Package | Upstream module | Version | License | Copyright | License text |
|---|---|---|---|---|---|
| `.` (runtime) | github.com/tree-sitter/tree-sitter | v0.25.0 | MIT | Copyright (c) 2018-2024 Max Brunsfeld | `LICENSE-tree-sitter` |
| `grammars/bash` | github.com/tree-sitter/tree-sitter-bash | v0.25.1 | MIT | Copyright (c) 2017 Max Brunsfeld | `grammars/bash/LICENSE` |
| `grammars/c` | github.com/tree-sitter/tree-sitter-c | v0.24.2 | MIT | Copyright (c) 2014 Max Brunsfeld | `grammars/c/LICENSE` |
| `grammars/cpp` | github.com/tree-sitter/tree-sitter-cpp | v0.23.4 | MIT | Copyright (c) 2014 Max Brunsfeld | `grammars/cpp/LICENSE` |
| `grammars/csharp` | github.com/tree-sitter/tree-sitter-c-sharp | v0.23.5 | MIT | Copyright (c) 2014-2023 Max Brunsfeld, Damien Guard, Amaan Qureshi, and contributors. | `grammars/csharp/LICENSE` |
| `grammars/golang` | github.com/tree-sitter/tree-sitter-go | v0.25.0 | MIT | Copyright (c) 2014 Max Brunsfeld | `grammars/golang/LICENSE` |
| `grammars/hcl` | github.com/tree-sitter-grammars/tree-sitter-hcl | v1.2.0 | Apache-2.0 | none stated | `grammars/hcl/LICENSE` |
| `grammars/java` | github.com/tree-sitter/tree-sitter-java | v0.23.5 | MIT | Copyright (c) 2017 Ayman Nadeem | `grammars/java/LICENSE` |
| `grammars/javascript` | github.com/tree-sitter/tree-sitter-javascript | v0.25.0 | MIT | Copyright (c) 2014 Max Brunsfeld | `grammars/javascript/LICENSE` |
| `grammars/json` | github.com/tree-sitter/tree-sitter-json | v0.24.8 | MIT | Copyright (c) 2014 Max Brunsfeld | `grammars/json/LICENSE` |
| `grammars/julia` | github.com/tree-sitter/tree-sitter-julia | v0.25.0 | MIT | Copyright (c) 2018 Max Brunsfeld, GitHub | `grammars/julia/LICENSE` |
| `grammars/kotlin` | github.com/tree-sitter-grammars/tree-sitter-kotlin | v1.1.0 | MIT | Copyright (c) 2024 Amaan Qureshi | `grammars/kotlin/LICENSE` |
| `grammars/lua` | github.com/tree-sitter-grammars/tree-sitter-lua | v0.5.0 | MIT | Copyright (c) 2021 Munif Tanjim | `grammars/lua/LICENSE` |
| `grammars/php` | github.com/tree-sitter/tree-sitter-php | v0.24.2 | MIT | Copyright (c) 2017 Josh Vera, GitHub | `grammars/php/LICENSE` |
| `grammars/python` | github.com/tree-sitter/tree-sitter-python | v0.25.0 | MIT | Copyright (c) 2016 Max Brunsfeld | `grammars/python/LICENSE` |
| `grammars/ruby` | github.com/tree-sitter/tree-sitter-ruby | v0.23.1 | MIT | Copyright (c) 2016 Rob Rix | `grammars/ruby/LICENSE` |
| `grammars/rust` | github.com/tree-sitter/tree-sitter-rust | v0.24.2 | MIT | Copyright (c) 2017 Maxim Sokolov | `grammars/rust/LICENSE` |
| `grammars/scala` | github.com/tree-sitter/tree-sitter-scala | v0.26.0 | MIT | Copyright (c) 2018 Max Brunsfeld and GitHub | `grammars/scala/LICENSE` |
| `grammars/tsx` | github.com/tree-sitter/tree-sitter-typescript | v0.23.2 | MIT | Copyright (c) 2017 Max Brunsfeld | `grammars/tsx/LICENSE` |
| `grammars/typescript` | github.com/tree-sitter/tree-sitter-typescript | v0.23.2 | MIT | Copyright (c) 2017 Max Brunsfeld | `grammars/typescript/LICENSE` |
| `grammars/verilog` | github.com/tree-sitter/tree-sitter-verilog | v1.0.3 | MIT | Copyright (c) 2018-2023 Aliaksei Chapyzhenka | `grammars/verilog/LICENSE` |
| `grammars/zig` | github.com/tree-sitter-grammars/tree-sitter-zig | v1.1.2 | MIT | Copyright (c) 2024 Amaan Qureshi | `grammars/zig/LICENSE` |

The runtime is a hand port of the C sources in `lib/src` of tree-sitter.

In each grammar package, `tables.go`, `lex.go` and `language.go` are generated
from the upstream `parser.c` by `cmd/gengrammar`, and `scanner.go`, where it
exists, is a hand port of the upstream external scanner. The versions are the
ones pinned in `grammars/grammars.lock`.

## Changes to Apache-2.0 code

`grammars/hcl` derives from tree-sitter-hcl, which is under the Apache License
2.0. Every file in that package is changed from the upstream: `tables.go`,
`lex.go` and `language.go` are generated from `src/parser.c`, and `scanner.go`
is ported to Go from `src/scanner.c`. The upstream ships no NOTICE file and
its LICENSE names no copyright holder.
