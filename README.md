# spring-initializr-cli

Interactive terminal tool that builds a `spring init` command from [Spring Initializr](https://start.spring.io) options and runs it.

Pick project type, language, Boot/Java versions, coordinates and dependencies with arrow keys; `Esc` goes back a step.

## Tech stack

- **Go 1.27+** (with `golang.org/x/term`)
- **Spring Boot CLI** (`spring`), needed at runtime unless you pass `--dry-run`
- **GitHub Actions** for build, test and release

## Usage

Download `spring-init.exe` from [Releases](https://github.com/justswb/spring-initializr-cli/releases), then:

```
spring-init                # choose options, then run `spring init ...`
spring-init --dry-run      # only print the command
spring-init --target URL   # use another Initializr service
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
- **Actions → Release → Run workflow**: choose a patch, minor or major bump. Tick *Dry run* to check the version without releasing.
- Push to any other branch: dry run only.
