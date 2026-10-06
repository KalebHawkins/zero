# zero

`zero` is the command-line tool for Zero Series practice.
It gets an exercise, runs its tests on your computer, and reports the result to the site.

You write Go code in your own editor.
`zero` does the rest: sign-in, files, tests, hints and submitting.

## Install

`zero` needs Go 1.27 or newer. Get Go from <https://go.dev/dl/>.

```
go install github.com/KalebHawkins/zero/cmd/zero@latest
```

`go install` puts the program in `$(go env GOPATH)/bin`.
That folder must be on your `PATH`.
Check the install:

```
zero version
```

## The first five minutes

### 1. Sign in

```
zero login
```

The command prints a code and a web address.
It also opens the address in a browser.
Approve the code on that page.
The command then prints `Signed in as <your name>`.

### 2. Check your computer

```
zero doctor
```

`doctor` runs seven checks.
Each failed check prints one sentence that says how to fix it.

| Check | What it looks for | Needed |
|---|---|---|
| `go` | Go 1.27 or newer | required |
| `git` | the `git` command | required |
| `podman` | the `podman` command | optional |
| `cc` | a C compiler: `cc`, `gcc` or `clang` | optional |
| `editor` | the `editor` setting, then `$EDITOR`, then the `code` command | optional |
| `workspace` | the workspace folder exists and you can write in it | required |
| `api` | the site answers | required |

A required check that fails shows ✗, and `zero doctor` exits with code 1.
`podman`, `cc` and `editor` are optional checks.
A missing optional tool shows `-` and the word "optional".
It is a note, not a failure: `zero doctor` still says the computer is ready.
You need podman only when an exercise asks for containers.
You need a C compiler only for window exercises on Linux.

To report a problem, run `zero doctor --report` and paste the output into the issue.

### 3. Start the first exercise

```
zero start hello-world
```

This writes the exercise into `~/zero/hello-world`.
The command prints the folder and the next command.
`README.txt` in that folder lists the tasks.

### 4. Run the tests

```
cd ~/zero/hello-world
zero test
```

The first run fails. That is expected.

```
Hello, World

✗ Task 1: Say hello
    ✗ TestHello
        Hello() = "Goodbye, World", want "Hello, World"

Tasks passed: 0 of 1. Fix the first failing task, then run: zero test
```

The indented line is the message of the failing test.
It shows what the code returned and what the test wanted.
Open `hello.go`, fix it, and run `zero test` again.

```
Hello, World

✓ Task 1: Say hello

Tasks passed: 1 of 1. Every test passes. Run: zero submit
```

You can run `zero test` as often as you like.
`zero hint` prints a hint. `zero run` runs the exercise's program.

### 5. Submit

```
zero submit
```

`submit` runs the tests one more time.
When every test passes, it sends your files and marks the exercise finished.
Then it prints the next exercise.

## Commands

| Command | What it does |
|---|---|
| `zero login` | Signs in. Prints a code, opens the approval page, and waits until you approve. |
| `zero logout` | Ends the sign-in for the current site: asks the site to end the token, then removes it from this computer. |
| `zero whoami` | Shows who is signed in. |
| `zero doctor` | Checks that this computer is ready. `--report` prints a plain block for an issue. |
| `zero start <id>` | Gets an exercise or a project stage and writes it into the workspace. `--force` also replaces the files you edited. `--reference` first lays down the previous stage's reference solution. |
| `zero test` | Runs the tests and prints each task with ✓ or ✗. Exits with code 1 when a task fails. |
| `zero run` | Runs the exercise's program. |
| `zero hint` | Prints the next hint. One hint per call. |
| `zero submit` | Runs the tests and, when they pass, finishes the exercise. |
| `zero next` | Shows what comes next on your path. |
| `zero use <id>` | Copies files from a finished exercise into the current project. With no id, lists what the stage can use. `--force` replaces files you already have. |
| `zero config list` | Shows every setting, its value, and where the value comes from. |
| `zero config get <key>` | Prints one setting. |
| `zero config set <key> <value>` | Saves one setting in the config file. |
| `zero config unset <key>` | Removes one setting from the config file, so it uses its default again. |
| `zero config path` | Prints where the config file is. |
| `zero version` | Prints the version. |
| `zero help` | Prints the command list. |

`test`, `run`, `hint` and `submit` work anywhere inside an exercise folder.
They find the exercise by looking for `.zero/exercise.json` in the current folder and the folders above it.

`zero start` never replaces a file you edit unless you add `--force`.
It always refreshes the other files, such as the tests.

## Projects

A project is one program built in stages.
Each stage is an exercise with its own tasks.

### One folder for every stage

All stages of a project share one folder: `~/zero/<project>/`.

```
zero start game-of-life-1
zero start game-of-life-2
```

Both commands write into `~/zero/game-of-life/`.
`zero start` writes the stage's new tests and new files.
It never replaces a file you already wrote.
Your code from stage 1 stays in place for stage 2.

A stage opens when you pass the stage before it.
Until then, `zero start` prints which stage to finish first.

On a new computer, or after you lose the folder, add `--reference`:

```
zero start game-of-life-4 --reference
```

This first lays down the reference solution of stage 3, then the files of stage 4.
It refuses to replace files that hold your work.
Add `--force` to replace them anyway.

### Use a finished exercise

Some stages use code from an earlier exercise.
`zero use` copies those files into the project:

```
zero use the-game-loop
```

```
✓ Copied the-game-loop/loop.go to cmd/life-window/loop.go
```

Run `zero use` with no id to see what the current stage can use.
The exercise folder must exist. If it does not, run `zero start <exercise>` and finish it first.
If the files are in the project already (restored by `--reference`, say), `zero use` says so and copies nothing.
`zero use` does not replace a file you already have unless you add `--force`.

Some stages only build on what an earlier exercise taught, with no files to copy.
`zero use` lists them as "builds on" and has nothing to copy for them.

### Regressions

`zero test` runs every test in the project, not only the tests of the current stage.
The current stage's tests decide its tasks.
A failing test from an earlier stage is a regression: a change broke code that worked.

```
✗ Task 1: Count with wrapping edges
    ✗ TestCount
        Count(grid, 1, 1) = 0, want 2

An earlier stage broke:
    ✗ TestNext
        Next(false, 3) = false, want true

Tasks passed: 0 of 1. An earlier stage broke. Fix the earlier stage first, then run: zero test
```

Fix the earlier stage first.
`zero submit` refuses while any regression is left.
When every test passes, `zero submit` sends every file you edit in the project, from every stage.

## Configuration

A setting can come from four places.
`zero` uses the first one that has a value:

1. a flag on the command line
2. an environment variable
3. the config file
4. the default

| Setting | Config key | Flag | Environment | Default |
|---|---|---|---|---|
| Site address | `api_url` | `--api-url <url>` | `ZERO_API_URL` | `https://kryolabs.duckdns.org/zero` |
| Workspace folder | `workspace` | `--workspace <dir>` | `ZERO_WORKSPACE` | `~/zero` |
| Open a browser on login | `browser` (`true` or `false`) | `--no-browser` | `ZERO_NO_BROWSER=1` | `true` |
| Color | `color` (`auto`, `always` or `never`) | `--no-color` | `NO_COLOR` | `auto` |
| Editor | `editor` | none | `EDITOR` | `code`, when it is on `PATH` |
| Token | none; `zero login` saves one for each site | none | `ZERO_TOKEN` | the token saved for the current `api_url` |
| Config file | none | `--config <file>` | `ZERO_CONFIG` | `<user config dir>/zero/config.json` |

Flags work before or after the command name.
These two lines do the same thing:

```
zero --workspace ~/practice start hello-world
zero start hello-world --workspace ~/practice
```

### See the settings

```
zero config list
```

```
api_url      https://kryolabs.duckdns.org/zero                from default
workspace    /home/you/practice                    from file
browser      false                                 from env
color        auto                                  from default
editor       (not set)                             from default
token        (set, hidden)                         from file
config file  /home/you/.config/zero/config.json    from default
```

The last column says which place won: `flag`, `env`, `file` or `default`.

### Change a setting

```
zero config set workspace ~/practice
zero config set browser false
zero config set color never
zero config set editor "code --wait"
zero config set api_url http://localhost:8090
```

`zero config get workspace` prints one value.

### Restore a default

```
zero config unset workspace
```

`unset` removes the key from the config file.
The setting then uses its default again.
`zero config set workspace ""` does the same thing.
`unset` works on the five setting keys. It does not touch your sign-in; use `zero logout` for that.

### Sign-ins and sites

A sign-in belongs to one site address.
`zero login` saves the token under the current `api_url`.
`zero` sends a token only to the site it came from.

When you change `api_url`, you are not signed in to the new site until you run `zero login` there.
The sign-in for the first site stays saved, and it works again when you switch back.
`zero config list` shows the token row for the current site only.

`zero logout` ends the sign-in for the current site and leaves the others.
It asks the site to end the token, then removes the token from the config file.
The token is removed from the file even when the site cannot be reached.

`ZERO_TOKEN` overrides the saved token for every site.
`zero logout` does not end or remove a token that comes from `ZERO_TOKEN`.

The tokens are in the config file like this:

```json
{
  "workspace": "~/practice",
  "tokens": {
    "https://kryolabs.duckdns.org/zero": { "token": "...", "login": "kryo" },
    "http://localhost:8090": { "token": "...", "login": "kryo" }
  }
}
```

An older config file has one `token` field and no site.
`zero` reads it as the token for the saved `api_url`, or for `https://kryolabs.duckdns.org/zero` when no `api_url` is saved.
The next write to the file stores it under `tokens`.

### Notes on the settings

- `api_url` is the address of the site, not of the API. It can include a path, for example `https://example.org/zero-next`.
- `~` at the start of `workspace` means your home folder.
- `color` set to `auto` uses color only when the output goes to a terminal.
- The `editor` key wins over `EDITOR`, because `EDITOR` is shared with other programs. `zero doctor` uses the editor setting to check that an editor is installed.
- The config file is JSON. On Linux it is `~/.config/zero/config.json`. Run `zero config path` to see yours.
- The config file holds your tokens. `zero` writes it with mode `0600`, so only your user can read it. Do not share the file.

## The exercise folder

A plain exercise has a folder of its own:

```
~/zero/
  hello-world/
    .zero/exercise.json    the exercise as the site sent it; zero reads this
    .zero/hints            how many hints zero has shown
    README.txt             the tasks in plain text
    hello.go               the file you edit
    hello_test.go          the tests; read them, do not edit them
    go.mod
    cmd/hello/main.go      the program that zero run starts
```

A project has one folder for all of its stages.
There `.zero/exercise.json` and `README.txt` describe the current stage.

## Development

The module uses only the Go standard library.

```
go vet ./...
gofmt -l .
go test ./...
```

`go test` includes an end-to-end test.
It starts a fake site in the test, then runs login, start, test and submit against it.
That test runs the real `go` command on the hello-world exercise.
A second test does the same for a fake two-stage project: the stage lock, a regression, `zero use`, submit and `--reference`.

The package `spec` holds the JSON types that the command and the site share.

## License

Apache License 2.0. See `LICENSE`.
