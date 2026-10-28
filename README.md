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
