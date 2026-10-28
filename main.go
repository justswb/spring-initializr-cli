// Command spring-init interactively selects Spring Initializr options and builds a `spring init` command.
//
// Usage: spring-init [--target URL] [--dry-run]
//
// Build: go build -trimpath -ldflags="-s -w" -o dist/spring-init.exe .
// Test:  go test ./...
//
// Runs the built command by default; --dry-run only prints it.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

const defaultTarget = "https://start.spring.io"

const esc = "\x1b["

func dim(s string) string     { return esc + "2m" + s + esc + "0m" }
func cyan(s string) string    { return esc + "36m" + s + esc + "0m" }
func green(s string) string   { return esc + "32m" + s + esc + "0m" }
func inverse(s string) string { return esc + "7m" + s + esc + "0m" }

type option struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Action string `json:"action"`
}

type singleSelect struct {
	Default string   `json:"default"`
	Values  []option `json:"values"`
}

type textField struct {
	Default string `json:"default"`
}

type metadata struct {
	Type         singleSelect `json:"type"`
	Packaging    singleSelect `json:"packaging"`
	JavaVersion  singleSelect `json:"javaVersion"`
	Language     singleSelect `json:"language"`
	BootVersion  singleSelect `json:"bootVersion"`
	GroupID      textField    `json:"groupId"`
	ArtifactID   textField    `json:"artifactId"`
	Version      textField    `json:"version"`
	Description  textField    `json:"description"`
	Dependencies struct {
		Values []struct {
			Name   string   `json:"name"`
			Values []option `json:"values"`
		} `json:"values"`
	} `json:"dependencies"`
}

type dependency struct {
	option
	group string
}

// errBack is returned by a prompt when Esc asks to return to the previous step.
var errBack = errors.New("back")

// ui draws prompts in place below the current terminal output: each redraw moves the cursor
// back up over the rows drawn last time and clears from there down.
type ui struct {
	keys         *keyReader
	restore      func()
	page         int
	canGoBack    bool  // set per step; the first step has no previous step
	drawnRows    int   // rows of the active prompt on screen
	answeredRows []int // rows of each answered step's summary line, erased again on going back
}

// visibleWidth counts runes, skipping SGR color sequences (ESC [ digits/; m).
func visibleWidth(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], esc) {
			j := i + len(esc)
			for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				i = j + 1
				continue
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
	}
	return n
}

func (u *ui) rowsOf(lines []string) int {
	cols, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 {
		cols = 80
	}
	n := 0
	for _, l := range lines {
		n += max(1, (visibleWidth(l)+cols-1)/cols)
	}
	return n
}

func eraseUp(rows int) string {
	s := ""
	if rows > 0 {
		s = fmt.Sprintf("%s%dA", esc, rows)
	}
	return s + "\r" + esc + "0J"
}

func (u *ui) draw(lines []string) {
	os.Stdout.WriteString(eraseUp(max(0, u.drawnRows-1)) + strings.Join(lines, "\n"))
	u.drawnRows = u.rowsOf(lines)
}

func (u *ui) exit(code int) {
	os.Stdout.WriteString(esc + "?25h")
	u.restore()
	os.Exit(code)
}

func (u *ui) backHint() string {
	if u.canGoBack {
		return dim("  esc: previous step")
	}
	return ""
}

// prompt runs one step: render returns its lines, onKey returns done=true with the answer,
// and summary is the line left on screen once answered.
// Esc returns errBack, erasing the previous step's summary so it can be asked again.
func prompt[T any](u *ui, title string, render func() []string, onKey func(k key) (T, bool), summary func(T) string) (T, error) {
	u.draw(render())
	for {
		k, err := u.keys.next()
		if err != nil {
			var zero T
			return zero, err
		}
		if k.name == "ctrl-c" {
			u.draw(nil)
			u.exit(130)
		}
		if k.name == "escape" {
			if !u.canGoBack {
				continue
			}
			u.draw(nil)
			u.drawnRows = 0
			if n := len(u.answeredRows); n > 0 {
				os.Stdout.WriteString(eraseUp(u.answeredRows[n-1]))
				u.answeredRows = u.answeredRows[:n-1]
			}
			var zero T
			return zero, errBack
		}
		result, done := onKey(k)
		if !done {
			u.draw(render())
			continue
		}
		u.draw(nil)
		u.drawnRows = 0
		line := green("✔") + " " + title + ": " + cyan(summary(result))
		os.Stdout.WriteString(line + "\n")
		u.answeredRows = append(u.answeredRows, u.rowsOf([]string{line}))
		return result, nil
	}
}

func (u *ui) window(n, cursor int) (start, end int) {
	start = min(max(0, cursor-u.page/2), max(0, n-u.page))
	return start, min(n, start+u.page)
}

func (u *ui) selectOne(title string, items []option, defaultID, current string) (string, error) {
	want := current
	if want == "" {
		want = defaultID
	}
	cursor := 0
	for i, it := range items {
		if it.ID == want {
			cursor = i
			break
		}
	}
	return prompt(u, title,
		func() []string {
			lines := []string{cyan("? "+title) + dim("  (↑/↓, enter)") + u.backHint()}
			start, end := u.window(len(items), cursor)
			for i := start; i < end; i++ {
				mark := ""
				if items[i].ID == defaultID {
					mark = dim(" (default)")
				}
				if i == cursor {
					lines = append(lines, green("❯ "+items[i].Name)+mark)
				} else {
					lines = append(lines, "  "+items[i].Name+mark)
				}
			}
			return lines
		},
		func(k key) (string, bool) {
			switch k.name {
			case "up":
				cursor = (cursor - 1 + len(items)) % len(items)
			case "down":
				cursor = (cursor + 1) % len(items)
			case "return":
				return items[cursor].ID, true
			}
			return "", false
		},
		func(id string) string {
			for _, it := range items {
				if it.ID == id {
					return it.Name
				}
			}
			return id
		})
}

func (u *ui) text(title, def, current string) (string, error) {
	value := []rune(current)
	return prompt(u, title,
		func() []string {
			head := cyan("? " + title)
			if def != "" {
				head += dim(" (" + def + ")")
			}
			return []string{head + u.backHint(), "> " + string(value) + inverse(" ")}
		},
		func(k key) (string, bool) {
			switch k.name {
			case "return":
				if v := strings.TrimSpace(string(value)); v != "" {
					return v, true
				}
				return def, true
			case "backspace":
				if len(value) > 0 {
					value = value[:len(value)-1]
				}
			case "space", "char":
				value = append(value, k.r)
			}
			return "", false
		},
		func(v string) string {
			if v == "" {
				return "-"
			}
			return v
		})
}

func (u *ui) multiSelect(title string, items []dependency, current []string) ([]string, error) {
	var filter []rune
	cursor := 0
	chosen := append([]string(nil), current...)
	isChosen := func(id string) int {
		for i, c := range chosen {
			if c == id {
				return i
			}
		}
		return -1
	}
	visible := func() []dependency {
		f := strings.ToLower(string(filter))
		var list []dependency
		for _, it := range items {
			if f == "" || strings.Contains(strings.ToLower(it.ID+" "+it.Name+" "+it.group), f) {
				list = append(list, it)
			}
		}
		return list
	}
	joined := func(ids []string) string {
		if len(ids) == 0 {
			return "-"
		}
		return strings.Join(ids, ", ")
	}
	return prompt(u, title,
		func() []string {
			list := visible()
			lines := []string{
				cyan("? "+title) + dim("  (type to filter, ↑/↓, space toggle, enter done)") + u.backHint(),
				"  filter: " + string(filter) + inverse(" "),
				"  selected: " + green(joined(chosen)),
			}
			start, end := u.window(len(list), cursor)
			for i := start; i < end; i++ {
				box := "◯"
				if isChosen(list[i].ID) >= 0 {
					box = green("◉")
				}
				line := box + " " + list[i].Name + " " + dim("["+list[i].ID+"] "+list[i].group)
				if i == cursor {
					lines = append(lines, green("❯ ")+line)
				} else {
					lines = append(lines, "  "+line)
				}
			}
			if len(list) == 0 {
				lines = append(lines, dim("  no match"))
			}
			return lines
		},
		func(k key) ([]string, bool) {
			list := visible()
			switch k.name {
			case "return":
				return append([]string{}, chosen...), true
			case "up":
				cursor = max(0, cursor-1)
			case "down":
				cursor = max(0, min(len(list)-1, cursor+1))
			case "space":
				if cursor < len(list) {
					if i := isChosen(list[cursor].ID); i >= 0 {
						chosen = append(chosen[:i], chosen[i+1:]...)
					} else {
						chosen = append(chosen, list[cursor].ID)
					}
				}
			case "backspace":
				if len(filter) > 0 {
					filter = filter[:len(filter)-1]
				}
				cursor = 0
			case "char":
				filter = append(filter, k.r)
				cursor = 0
			}
			return nil, false
		},
		joined)
}

// quote leaves arguments made only of [\w.,:/@=-] bare and double-quotes anything else.
func quote(v string) string {
	safe := v != "" && strings.IndexFunc(v, func(r rune) bool { return !isWordChar(r) && !strings.ContainsRune(".,:/@=-", r) }) < 0
	if safe {
		return v
	}
	return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"`
}

func fetchMetadata(target string) (*metadata, error) {
	body, status, err := httpGet(target, "application/vnd.initializr.v2.2+json")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("Failed to fetch %s: %d", target, status)
	}
	var meta metadata
	return &meta, json.Unmarshal(body, &meta)
}

// isWordChar matches the regexp class \w.
func isWordChar(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// packageNameOf keeps only [\w.] characters, e.g. com.example + my-app -> com.example.myapp.
func packageNameOf(s string) string {
	return strings.Map(func(r rune) rune {
		if isWordChar(r) || r == '.' {
			return r
		}
		return -1
	}, s)
}

func run() (int, error) {
	target, dryRun := defaultTarget, false
	args := os.Args[1:]
	for i, a := range args {
		switch a {
		case "--target":
			if i+1 < len(args) {
				target = args[i+1]
			}
		case "--dry-run":
			dryRun = true
		}
	}

	// The built command is run at the end, so fail before any prompts if the Spring CLI is missing.
	// LookPath also finds spring.cmd / spring.bat on Windows (via PATHEXT).
	if !dryRun {
		if _, err := exec.LookPath("spring"); err != nil {
			return 1, errors.New("Spring Boot CLI (spring) was not found on PATH.\n" +
				"Install it, e.g. `sdk install springboot` or see https://docs.spring.io/spring-boot/installing.html,\n" +
				"or run with --dry-run to only print the command.")
		}
	}

	meta, err := fetchMetadata(target)
	if err != nil {
		return 1, err
	}

	stdin := int(os.Stdin.Fd())
	if !term.IsTerminal(stdin) {
		return 1, errors.New("An interactive terminal is required.")
	}
	enableVirtualTerminal()
	state, err := term.MakeRaw(stdin)
	if err != nil {
		return 1, err
	}
	_, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || rows <= 0 {
		rows = 24
	}
	u := &ui{keys: newKeyReader(os.Stdin), restore: func() { term.Restore(stdin, state) }, page: max(3, min(10, rows-8))}
	os.Stdout.WriteString(esc + "?25l")

	opts := map[string]string{}
	var dependencies []string
	var types []option
	for _, t := range meta.Type.Values {
		if t.Action == "/starter.zip" {
			types = append(types, t)
		}
	}
	var deps []dependency
	for _, g := range meta.Dependencies.Values {
		for _, d := range g.Values {
			deps = append(deps, dependency{d, g.Name})
		}
	}
	selectStep := func(key, title string, s singleSelect) func() (string, error) {
		return func() (string, error) { return u.selectOne(title, s.Values, s.Default, opts[key]) }
	}
	textStep := func(key, title string, def func() string) func() (string, error) {
		return func() (string, error) { return u.text(title, def(), opts[key]) }
	}
	description := meta.Description.Default
	if description == "" {
		description = "Demo project for Spring Boot"
	}
	type step struct {
		key string
		ask func() (string, error)
	}
	steps := []step{
		{"type", selectStep("type", "Project type", singleSelect{meta.Type.Default, types})},
		{"language", selectStep("language", "language", meta.Language)},
		{"bootVersion", selectStep("bootVersion", "bootVersion", meta.BootVersion)},
		{"packaging", selectStep("packaging", "packaging", meta.Packaging)},
		{"javaVersion", selectStep("javaVersion", "javaVersion", meta.JavaVersion)},
		{"groupId", textStep("groupId", "groupId", func() string { return meta.GroupID.Default })},
		{"artifactId", textStep("artifactId", "artifactId", func() string { return meta.ArtifactID.Default })},
		{"name", textStep("name", "name", func() string { return opts["artifactId"] })},
		{"description", textStep("description", "description", func() string { return description })},
		{"packageName", textStep("packageName", "packageName", func() string {
			return packageNameOf(opts["groupId"] + "." + opts["artifactId"])
		})},
		{"version", textStep("version", "version", func() string { return meta.Version.Default })},
		{"dependencies", func() (string, error) {
			ids, err := u.multiSelect("Dependencies", deps, dependencies)
			if err == nil {
				dependencies = ids
			}
			return "", err
		}},
		{"extract", selectStep("extract", "Extract project?", singleSelect{"yes", []option{
			{ID: "yes", Name: "Yes (--extract)"}, {ID: "no", Name: "No (zip)"},
		}})},
		{"location", textStep("location", "Output location", func() string { return opts["artifactId"] })},
	}
	for i := 0; i < len(steps); {
		u.canGoBack = i > 0
		answer, err := steps[i].ask()
		switch {
		case errors.Is(err, errBack):
			i = max(0, i-1)
		case err != nil:
			u.restore()
			return 1, err
		default:
			opts[steps[i].key] = answer
			i++
		}
	}

	os.Stdout.WriteString(esc + "?25h\n")
	u.restore()

	cmd := []string{"spring", "init"}
	if target != defaultTarget {
		cmd = append(cmd, "--target", target)
	}
	flags := [][2]string{
		{"type", "--type"}, {"language", "--language"}, {"bootVersion", "--boot-version"}, {"packaging", "--packaging"},
		{"javaVersion", "--java-version"}, {"groupId", "--group-id"}, {"artifactId", "--artifact-id"}, {"name", "--name"},
		{"description", "--description"}, {"packageName", "--package-name"}, {"version", "--version"},
	}
	for _, f := range flags {
		if v := opts[f[0]]; v != "" {
			cmd = append(cmd, f[1], v)
		}
	}
	if len(dependencies) > 0 {
		cmd = append(cmd, "--dependencies", strings.Join(dependencies, ","))
	}
	if opts["extract"] == "yes" {
		cmd = append(cmd, "--extract")
	}
	if loc := opts["location"]; loc != "" {
		cmd = append(cmd, loc)
	}

	quoted := make([]string, len(cmd))
	for i, c := range cmd {
		quoted[i] = quote(c)
	}
	line := strings.Join(quoted, " ")
	fmt.Println(green(line))
	if dryRun {
		return 0, nil
	}
	c := shellCommand(line)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func main() {
	code, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}
