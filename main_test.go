package main

// End-to-end tests: build the CLI, run it in a pseudo-terminal against a local metadata server,
// press keys, and check what ends up on screen. Set SPRING_INIT_BIN to test another binary.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	pty "github.com/aymanbagabas/go-pty"
	"github.com/hinshun/vt10x"
)

const (
	cols = 400
	rows = 50

	keyUp        = "\x1b[A"
	keyDown      = "\x1b[B"
	keyEnter     = "\r"
	keyEsc       = "\x1b"
	keySpace     = " "
	keyBackspace = "\x7f"
	keyCtrlC     = "\x03"
)

var (
	bin    string
	target string
)

var titles = []string{"Project type", "language", "bootVersion", "packaging", "javaVersion", "groupId", "artifactId",
	"name", "description", "packageName", "version", "Dependencies", "Extract project?", "Output location"}

func TestMain(m *testing.M) {
	os.Exit(func() int {
		bin = os.Getenv("SPRING_INIT_BIN")
		if bin == "" {
			dir, err := os.MkdirTemp("", "spring-init-test-")
			if err != nil {
				panic(err)
			}
			defer os.RemoveAll(dir)
			bin = filepath.Join(dir, "spring-init"+exeSuffix())
			if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
				panic("go build failed: " + err.Error() + "\n" + string(out))
			}
		}
		bin, _ = filepath.Abs(bin)

		metadata, err := os.ReadFile(filepath.Join("testdata", "metadata.json"))
		if err != nil {
			panic(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.initializr.v2.2+json")
			w.Write(metadata)
		}))
		defer server.Close()
		target = server.URL
		return m.Run()
	}())
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// cli is the CLI running in a pseudo-terminal, mirrored into a terminal emulator so assertions
// see the rendered screen rather than raw escape sequences.
type cli struct {
	t      *testing.T
	pty    pty.Pty
	term   vt10x.Terminal
	exited chan int
	mu     sync.Mutex
}

func start(t *testing.T, env []string, args ...string) *cli {
	t.Helper()
	p, err := pty.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Resize(cols, rows); err != nil {
		t.Fatal(err)
	}
	c := &cli{t: t, pty: p, exited: make(chan int, 1)}
	c.term = vt10x.New(vt10x.WithSize(cols, rows), vt10x.WithWriter(p)) // the writer answers terminal queries

	cmd := p.Command(bin, append([]string{"--target", target}, args...)...)
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				c.mu.Lock()
				c.term.Write(buf[:n])
				c.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		cmd.Wait()
		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		c.exited <- code
	}()
	t.Cleanup(func() {
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		p.Close()
	})
	return c
}

func (c *cli) screen() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines := strings.Split(c.term.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func (c *cli) send(keys string) {
	if _, err := c.pty.Write([]byte(keys)); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cli) waitFor(text string) string {
	c.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if s := c.screen(); strings.Contains(s, text) {
			return s
		}
		time.Sleep(25 * time.Millisecond)
	}
	c.t.Fatalf("timed out waiting for %q\n--- screen ---\n%s", text, c.screen())
	return ""
}

// answer waits for the prompt titled title, then presses keys (Enter by default).
func (c *cli) answer(title string, keys ...string) {
	c.t.Helper()
	c.waitFor("? " + title)
	if len(keys) == 0 {
		keys = []string{keyEnter}
	}
	c.send(strings.Join(keys, ""))
}

// acceptRest answers each remaining step with Enter (its default), starting at fromTitle.
func (c *cli) acceptRest(fromTitle string) {
	c.t.Helper()
	for i, title := range titles {
		if title == fromTitle {
			for _, t := range titles[i:] {
				c.answer(t)
			}
			return
		}
	}
	c.t.Fatalf("unknown step %q", fromTitle)
}

func (c *cli) exit() int {
	c.t.Helper()
	select {
	case code := <-c.exited:
		time.Sleep(150 * time.Millisecond) // let the last output reach the emulator
		return code
	case <-time.After(10 * time.Second):
		c.t.Fatalf("CLI did not exit\n--- screen ---\n%s", c.screen())
		return 0
	}
}

func (c *cli) command() string {
	c.t.Helper()
	for _, l := range strings.Split(c.screen(), "\n") {
		if strings.HasPrefix(l, "spring init") {
			return strings.TrimSpace(l)
		}
	}
	c.t.Fatalf("no spring init command on screen:\n%s", c.screen())
	return ""
}

func defaultCommand() string {
	return "spring init --target " + target + " --type maven-project --language java --boot-version 3.5.0 --packaging jar " +
		`--java-version 21 --group-id com.example --artifact-id demo --name demo --description "Demo project for Spring Boot" ` +
		"--package-name com.example.demo --version 0.0.1-SNAPSHOT --extract demo"
}

func expectExit(t *testing.T, c *cli, want int) {
	t.Helper()
	if got := c.exit(); got != want {
		t.Fatalf("exit code = %d, want %d\n--- screen ---\n%s", got, want, c.screen())
	}
}

func expectCommand(t *testing.T, c *cli, want string) {
	t.Helper()
	if got := c.command(); got != want {
		t.Fatalf("command mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestDefaults(t *testing.T) {
	c := start(t, nil, "--dry-run")
	c.acceptRest("Project type")
	expectExit(t, c, 0)
	expectCommand(t, c, defaultCommand())
}

func TestAnsweredStepsCollapseIntoSummaries(t *testing.T) {
	c := start(t, nil, "--dry-run")
	c.acceptRest("Project type")
	c.exit()
	s := c.screen()
	for _, line := range []string{"✔ Project type: Maven", "✔ language: Java", "✔ javaVersion: 21", "✔ groupId: com.example",
		"✔ packageName: com.example.demo", "✔ Dependencies: -", "✔ Extract project?: Yes (--extract)", "✔ Output location: demo"} {
		if !strings.Contains(s, line) {
			t.Errorf("missing %q in:\n%s", line, s)
		}
	}
	if strings.Contains(s, "? ") {
		t.Errorf("a prompt was left on screen:\n%s", s)
	}
}

func TestCustomSelections(t *testing.T) {
	c := start(t, nil, "--dry-run")
	c.answer("Project type", keyDown, keyEnter)
	c.answer("language", keyDown, keyEnter)
	c.answer("bootVersion")
	c.answer("packaging", keyDown, keyEnter)
	c.answer("javaVersion", keyUp, keyEnter)
	c.answer("groupId", "io.acme", keyEnter)
	c.answer("artifactId", "shop-api", keyEnter)
	c.waitFor("? name (shop-api)") // name defaults to the artifactId
	c.answer("name")
	c.answer("description", "Shop API", keyEnter)
	c.waitFor("? packageName (io.acme.shopapi)") // derived from groupId + artifactId
	c.answer("packageName")
	c.answer("version")

	c.waitFor("? Dependencies")
	c.send("web")
	c.waitFor("filter: web")
	c.send(keySpace)
	c.waitFor("selected: web")
	c.send(strings.Repeat(keyBackspace, 3) + "lombok")
	c.waitFor("filter: lombok")
	c.send(keySpace)
	c.waitFor("selected: web, lombok")
	c.answer("Dependencies")

	c.answer("Extract project?", keyDown, keyEnter)
	c.answer("Output location")
	expectExit(t, c, 0)
	expectCommand(t, c, "spring init --target "+target+" --type gradle-project --language kotlin --boot-version 3.5.0 --packaging war "+
		`--java-version 25 --group-id io.acme --artifact-id shop-api --name shop-api --description "Shop API" `+
		"--package-name io.acme.shopapi --version 0.0.1-SNAPSHOT --dependencies web,lombok shop-api")
}

func TestDependencyFilterNoMatch(t *testing.T) {
	c := start(t, nil, "--dry-run")
	for _, title := range titles[:11] {
		c.answer(title)
	}
	c.waitFor("? Dependencies")
	c.send("zzz")
	c.waitFor("no match")
	c.send(keySpace + keyEnter) // toggling with nothing visible selects nothing
	c.acceptRest("Extract project?")
	expectExit(t, c, 0)
	expectCommand(t, c, defaultCommand())
}

func TestEscOnFirstStepIgnored(t *testing.T) {
	c := start(t, nil, "--dry-run")
	if s := c.waitFor("? Project type"); strings.Contains(s, "esc: previous step") {
		t.Fatalf("first step shows a back hint:\n%s", s)
	}
	c.send(keyEsc)
	time.Sleep(800 * time.Millisecond)
	if !strings.Contains(c.screen(), "? Project type") {
		t.Fatalf("esc on the first step left it:\n%s", c.screen())
	}
	c.acceptRest("Project type")
	expectExit(t, c, 0)
	expectCommand(t, c, defaultCommand())
}

func TestEscGoesBack(t *testing.T) {
	c := start(t, nil, "--dry-run")
	c.answer("Project type", keyDown, keyEnter)
	c.waitFor("✔ Project type: Gradle - Groovy")
	if s := c.waitFor("? language"); !strings.Contains(s, "esc: previous step") {
		t.Fatalf("second step shows no back hint:\n%s", s)
	}
	c.send(keyEsc)
	c.waitFor("❯ Gradle - Groovy") // earlier answer is preselected
	if strings.Contains(c.screen(), "✔ Project type") {
		t.Fatalf("summary of the revisited step should be erased:\n%s", c.screen())
	}
	c.answer("Project type")

	for _, title := range titles[1:5] {
		c.answer(title)
	}
	c.answer("groupId", "org.x", keyEnter)
	c.waitFor("? artifactId")
	c.send(keyEsc)
	c.waitFor("> org.x") // text step is prefilled with the earlier answer
	c.answer("groupId")
	c.acceptRest("artifactId")

	expectExit(t, c, 0)
	cmd := c.command()
	for _, want := range []string{" --type gradle-project ", " --group-id org.x ", " --package-name org.x.demo "} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command %q lacks %q", cmd, want)
		}
	}
}

func TestCtrlC(t *testing.T) {
	c := start(t, nil, "--dry-run")
	c.answer("Project type")
	c.waitFor("? language")
	c.send(keyCtrlC)
	expectExit(t, c, 130)
	if strings.Contains(c.screen(), "spring init") {
		t.Fatalf("a command was printed after ctrl+c:\n%s", c.screen())
	}
}

func TestRunsSpringInit(t *testing.T) {
	fakeBin := t.TempDir()
	os.WriteFile(filepath.Join(fakeBin, "spring.cmd"), []byte("@echo FAKE-SPRING %*\r\n@exit /b 7\r\n"), 0o644)
	os.WriteFile(filepath.Join(fakeBin, "spring"), []byte("#!/bin/sh\necho FAKE-SPRING \"$@\"\nexit 7\n"), 0o755)
	env := withPath(fakeBin)

	c := start(t, env)
	c.acceptRest("Project type")
	expectExit(t, c, 7)
	c.waitFor("FAKE-SPRING init --target")
}

// withPath returns the environment with dir prepended to PATH (matched case-insensitively for Windows).
func withPath(dir string) []string {
	var env []string
	path := ""
	for _, kv := range os.Environ() {
		if k, v, _ := strings.Cut(kv, "="); strings.EqualFold(k, "PATH") {
			path = v
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+dir+string(os.PathListSeparator)+path)
}

// runPiped runs the CLI without a terminal (piped stdio) and returns its exit code and stderr.
func runPiped(t *testing.T, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader("")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatal(err)
	}
	return cmd.ProcessState.ExitCode(), stderr.String()
}

func TestRequiresTerminal(t *testing.T) {
	code, stderr := runPiped(t, "--target", target, "--dry-run")
	if code != 1 || !strings.Contains(stderr, "interactive terminal") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestFetchFailure(t *testing.T) {
	code, stderr := runPiped(t, "--target", "http://127.0.0.1:1", "--dry-run")
	if code != 1 || !regexp.MustCompile(`\S`).MatchString(stderr) {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}
