# spring-initializr-cli

Interactive terminal tool that builds a `spring init` command from [Spring Initializr](https://start.spring.io) options and runs it.

Pick project type, language, Boot/Java versions, coordinates and dependencies with arrow keys; `Esc` goes back a step.

## Tech stack

- **Go 1.27+** (with `golang.org/x/term`)
- **Spring Boot CLI** (`spring`) **2.6 or later**, needed at runtime unless you pass `--dry-run`
- **GitHub Actions** for build, test and release

## Supported Spring Boot CLI versions

| Spring Boot CLI | Supported |
|---|---|
| 4.x | ✅ |
| 3.x | ✅ |
| 2.6 – 2.7 | ✅ |
| 2.5 and older | ❌ `spring init` there uses `--groupId` / `--artifactId` instead of `--group-id` / `--artifact-id` |

Install: `sdk install springboot`, or see the [installation guide](https://docs.spring.io/spring-boot/installing.html).

## Usage

Install, or update by running it again. It needs no admin rights: it puts `spring-init.exe` in `%LOCALAPPDATA%\Programs\spring-init` and adds that folder to your user `PATH`.

```powershell
irm https://raw.githubusercontent.com/justswb/spring-initializr-cli/master/install.ps1 | iex
```

Uninstall:

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/justswb/spring-initializr-cli/master/install.ps1))) -Uninstall
```

Pass `-Version v2.0.3` the same way to install a specific version. You can read [install.ps1](install.ps1) first, or download `spring-init.exe` from [Releases](https://github.com/justswb/spring-initializr-cli/releases) and put it on `PATH` yourself.

Then, in a new terminal:

```
spring-init                # choose options, then run `spring init ...`
spring-init --dry-run      # only print the command
spring-init --target URL   # use another Initializr service
spring-init --version      # print the version (also -v)
spring-init --help         # print usage (also -h)
```

## Build

```
go build -trimpath -ldflags="-s -w" -o dist/spring-init.exe .
```

## Test

```
go test ./...
```

## Release

- Push to `master`: tags the next patch version and publishes a release with the exe.
- CI also tests `install.ps1` with `install_test.ps1` (install, update, uninstall, user `PATH`). That script really edits your user `PATH`, so don't run it on your own machine.
- **Actions → Release → Run workflow**: choose a patch, minor or major bump. Tick *Dry run* to check the version without releasing.
- Push to any other branch: dry run only.
