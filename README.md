<div align="center">

# Plain

A small programming language without the usual surprises. Plain compiles to readable JavaScript and runs on Node or Bun.

[Pains it removes](#the-pains-and-how-plain-removes-them) · [Quick start](#quick-start) · [A taste](#a-taste) · [Status](#status) · [Contributing](#contributing)

![Status](https://img.shields.io/badge/status-early%20work%20in%20progress-e17055) ![Go](https://img.shields.io/badge/compiler-Go-00ADD8) ![Target](https://img.shields.io/badge/output-JavaScript-f7df1e) ![License](https://img.shields.io/badge/license-MIT-2d3436)

</div>

Plain was built by collecting common complaints about programming languages and removing them one by one. Files end in `.pln`. The compiler is written in Go with only the standard library.

## The pains and how Plain removes them

| # | Pain | Plain's fix |
|---|---|---|
| 1 | Null and undefined crashes | No null. A missing value is `Option[T]` (`Some(x)` or `None`), and the compiler won't let you use it without checking. |
| 2 | Error-handling noise | `Result[T, E]` plus a `?` operator for early return. An unused `Result` is a compile error. |
| 3 | Hidden throws | No exceptions. A function that can fail says so in its return type. `panic` is only for real bugs. |
| 4 | Type coercion (`"1" + 1 == "11"`) | No implicit conversions. Text is joined with `++` or `"Hi {name}"`. |
| 5 | `==` vs `===`, comparing by reference | One `==`. It compares contents and only accepts two values of the same type. |
| 6 | Shared mutable state | Values can't change by default. `var` is for local variables you want to change. |
| 7 | Number surprises | `Int` and `Float` are separate types. `/` always gives a Float, `div` and `mod` are for Int, and an Int past the safe range stops with a clear message. |
| 8 | Forgotten cases | `match` must cover every case, and the compiler names the missing one. |
| 9 | Cryptic errors | Messages show the file and line, the code with a caret, a plain-English explanation and a hint. |
| 10 | Tool sprawl | One `plain` command for `new`, `run`, `build`, `check`, `test` and `fmt`. |

Each pain has a folder in [`pains/`](pains) with the bug in another language (`before.*`), the same mistake in Plain (`mistake.pln`, caught by the compiler) and the fixed version (`after.pln`).

## Quick start

You need Go 1.27 or newer, plus Bun or Node to run programs.

```sh
git clone https://github.com/Gitguy268/plain.git
cd plain
go build -o plain ./cmd/plain
./plain run pains/01-null/after.pln
./plain check pains/01-null/mistake.pln    # shows the friendly error
```

| Command | What it does |
|---|---|
| `plain new <name>` | make a project folder with a starter program |
| `plain run <file.pln> [args]` | compile and run (needs Bun or Node) |
| `plain build <file.pln> [-o out.js]` | compile to one JavaScript file |
| `plain check <files...>` | find problems without running anything |
| `plain test [files or dirs]` | run the `test` blocks |
| `plain fmt [--check] [files or dirs]` | format code the one Plain way |

## A taste

```plain
// list.find gives an Option, and match makes you say what happens when nothing is found.
fn main() {
  let users = [{id: 1, name: "Ann"}, {id: 2, name: "Bo"}]
  for id in [1, 3] {
    match list.find(users, fn(u) u.id == id) {
      Some(user) -> print(string.upper(user.name))
      None -> print("No user with id {id}")
    }
  }
}
```

<details>
<summary>Language overview</summary>

- **Types:** `Int`, `Float`, `Bool`, `String`, `List[T]`, `Dict[K, V]`, records `{name: String}`, tagged unions (`type Shape = Circle(radius: Float) | Rect(w: Float, h: Float)`), and the built-in `Option` and `Result`.
- **Inference:** Hindley–Milner style, which gives generics. Top-level `fn` parameters and return types are written out so errors stay local.
- **Expressions:** `let`, `var`, `if` as an expression, exhaustive `match` with nested patterns, lambdas `fn(x) x + 1`, the pipe `|>`, string interpolation and `?`.
- **Modules:** one file is one module. `import math` reads `math.pln` next to it, and `pub` controls exports.
- **Standard library:** `internal/std/lib` (core, int, string, list, dict, option, result, math, fs, os).

</details>

<details>
<summary>How the compiler is laid out</summary>

| Path | What it does |
|---|---|
| `cmd/plain` | the CLI |
| `internal/syntax` | lexer, AST and parser |
| `internal/types` | the checker: inference, exhaustiveness, must-use `Result`, no-coercion rules |
| `internal/codegen` | emits JavaScript |
| `internal/runtime` | the embedded JavaScript prelude |
| `internal/diag` | the friendly error renderer |
| `internal/format` | the formatter |
| `internal/compiler` | ties the stages together |

</details>

## Status

Plain is early work. The compiler, CLI, standard library and `pains/` examples are in this repository. These parts are **not done yet**:

- automated tests (`go test` currently finds no test files);
- the browser playground and docs site that the CLI help mentions;
- the `pains/10-tool-sprawl` example, which is still a placeholder;
- a Homebrew formula and releases;
- traits, async, a package registry and editor support.

## Contributing

Issues and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Report security problems privately as [SECURITY.md](SECURITY.md) describes.

## License

[MIT](LICENSE). Language ideas come from Elm, Rust, Gleam, Swift and OCaml. No code was copied from them. See [NOTICE](NOTICE).
