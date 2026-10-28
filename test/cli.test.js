// Black-box tests: run the CLI in a pseudo-terminal against a local metadata server, press keys,
// and check what ends up on screen. Set SPRING_INIT_BIN to test a built binary (e.g. the .exe or a
// Go build) instead of `node spring-init.js`.
const { test, before, after } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const { spawn } = require('node:child_process');
const pty = require('@lydell/node-pty');
const { Terminal } = require('@xterm/headless');

const ROOT = path.join(__dirname, '..');
const BIN = process.env.SPRING_INIT_BIN
  ? [path.resolve(process.env.SPRING_INIT_BIN)]
  : [process.execPath, path.join(ROOT, 'spring-init.js')];
const COLS = 300;
const ROWS = 50;

const KEY = { up: '\x1b[A', down: '\x1b[B', enter: '\r', esc: '\x1b', space: ' ', backspace: '\x7f', ctrlC: '\x03' };

let server;
let target;

before(async () => {
  const metadata = fs.readFileSync(path.join(__dirname, 'fixtures', 'metadata.json'));
  server = http.createServer((req, res) => {
    res.writeHead(200, { 'Content-Type': 'application/vnd.initializr.v2.2+json' });
    res.end(metadata);
  });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  target = `http://127.0.0.1:${server.address().port}`;
});

after(() => server.close());

// Starts the CLI in a pseudo-terminal and mirrors its output into a headless terminal emulator,
// so assertions see the rendered screen rather than raw escape sequences.
function start(args = ['--dry-run'], env = process.env) {
  const term = new Terminal({ cols: COLS, rows: ROWS, allowProposedApi: true });
  const proc = pty.spawn(BIN[0], [...BIN.slice(1), '--target', target, ...args], { cols: COLS, rows: ROWS, cwd: ROOT, env });
  let raw = '';
  const exited = new Promise((resolve) => proc.onExit(({ exitCode }) => resolve(exitCode)));
  proc.onData((d) => {
    raw += d;
    term.write(d);
  });

  const screen = () => {
    const lines = [];
    const buf = term.buffer.active;
    for (let i = 0; i < buf.length; i++) {
      const line = buf.getLine(i);
      const text = line.translateToString(true);
      if (line.isWrapped && lines.length) lines[lines.length - 1] += text; // rejoin soft-wrapped rows
      else lines.push(text);
    }
    return lines.join('\n').replace(/\n+$/, '');
  };

  const cli = {
    screen,
    send: (keys) => proc.write(keys),
    async waitFor(pattern, timeout = 10000) {
      const deadline = Date.now() + timeout;
      while (Date.now() < deadline) {
        const s = screen();
        if (typeof pattern === 'string' ? s.includes(pattern) : pattern.test(s)) return s;
        await new Promise((r) => setTimeout(r, 25));
      }
      throw new Error(`Timed out waiting for ${pattern}\n--- screen ---\n${screen()}\n--- raw ---\n${JSON.stringify(raw.slice(-2000))}`);
    },
    // Waits for the prompt titled `title`, then presses `keys`.
    async answer(title, keys = KEY.enter) {
      await cli.waitFor(`? ${title}`);
      cli.send(keys);
    },
    async exit(timeout = 10000) {
      const code = await Promise.race([exited, new Promise((_, rej) => setTimeout(() => rej(new Error('CLI did not exit')), timeout))]);
      await new Promise((r) => setTimeout(r, 100)); // let the last output reach the emulator
      return code;
    },
    command() {
      const line = screen().split('\n').find((l) => l.startsWith('spring init'));
      assert.ok(line, `no spring init command on screen:\n${screen()}`);
      return line.trim();
    },
    kill: () => {
      try {
        proc.kill();
      } catch {}
      term.dispose();
    },
  };
  return cli;
}

// Runs the CLI without a terminal (piped stdio). Async so the in-process metadata server can still respond.
function runPiped(args) {
  return new Promise((resolve, reject) => {
    const child = spawn(BIN[0], [...BIN.slice(1), ...args], { stdio: ['pipe', 'pipe', 'pipe'] });
    let stderr = '';
    child.stderr.on('data', (d) => (stderr += d));
    child.on('error', reject);
    child.on('close', (status) => resolve({ status, stderr }));
    child.stdin.end();
  });
}

const TITLES = ['Project type', 'language', 'bootVersion', 'packaging', 'javaVersion', 'groupId', 'artifactId', 'name',
  'description', 'packageName', 'version', 'Dependencies', 'Extract project?', 'Output location'];
const defaultCommand = () =>
  `spring init --target ${target} --type maven-project --language java --boot-version 3.5.0 --packaging jar ` +
  '--java-version 21 --group-id com.example --artifact-id demo --name demo --description "Demo project for Spring Boot" ' +
  '--package-name com.example.demo --version 0.0.1-SNAPSHOT --extract demo';

// Answers each remaining step with Enter (its default), starting at `fromTitle`.
async function acceptRest(cli, fromTitle) {
  for (const title of TITLES.slice(TITLES.indexOf(fromTitle))) await cli.answer(title);
}

async function withCli(args, fn, env) {
  const cli = start(args, env);
  try {
    await fn(cli);
  } finally {
    cli.kill();
  }
}

test('accepting every default builds the default command', () =>
  withCli(['--dry-run'], async (cli) => {
    await acceptRest(cli, 'Project type');
    assert.equal(await cli.exit(), 0);
    assert.equal(cli.command(), defaultCommand());
  }));

test('answered steps collapse into one-line summaries', () =>
  withCli(['--dry-run'], async (cli) => {
    await acceptRest(cli, 'Project type');
    await cli.exit();
    const s = cli.screen();
    for (const line of ['✔ Project type: Maven', '✔ language: Java', '✔ javaVersion: 21', '✔ groupId: com.example',
      '✔ packageName: com.example.demo', '✔ Dependencies: -', '✔ Extract project?: Yes (--extract)', '✔ Output location: demo']) {
      assert.ok(s.includes(line), `missing "${line}" in:\n${s}`);
    }
    assert.ok(!s.includes('? '), `a prompt was left on screen:\n${s}`);
  }));

test('custom selections, text input and dependencies end up in the command', () =>
  withCli(['--dry-run'], async (cli) => {
    await cli.answer('Project type', KEY.down + KEY.enter);
    await cli.answer('language', KEY.down + KEY.enter);
    await cli.answer('bootVersion');
    await cli.answer('packaging', KEY.down + KEY.enter);
    await cli.answer('javaVersion', KEY.up + KEY.enter);
    await cli.answer('groupId', 'io.acme' + KEY.enter);
    await cli.answer('artifactId', 'shop-api' + KEY.enter);
    await cli.waitFor('? name (shop-api)'); // name defaults to the artifactId
    await cli.answer('name');
    await cli.answer('description', 'Shop API' + KEY.enter);
    await cli.waitFor('? packageName (io.acme.shopapi)'); // derived from groupId + artifactId
    await cli.answer('packageName');
    await cli.answer('version');

    await cli.waitFor('? Dependencies');
    cli.send('web');
    await cli.waitFor('filter: web');
    cli.send(KEY.space);
    await cli.waitFor('selected: web');
    cli.send(KEY.backspace.repeat(3) + 'lombok');
    await cli.waitFor('filter: lombok');
    cli.send(KEY.space);
    await cli.waitFor('selected: web, lombok');
    await cli.answer('Dependencies');

    await cli.answer('Extract project?', KEY.down + KEY.enter);
    await cli.answer('Output location');
    assert.equal(await cli.exit(), 0);
    assert.equal(
      cli.command(),
      `spring init --target ${target} --type gradle-project --language kotlin --boot-version 3.5.0 --packaging war ` +
        '--java-version 25 --group-id io.acme --artifact-id shop-api --name shop-api --description "Shop API" ' +
        '--package-name io.acme.shopapi --version 0.0.1-SNAPSHOT --dependencies web,lombok shop-api',
    );
  }));

test('dependency filter shows "no match" for unknown terms', () =>
  withCli(['--dry-run'], async (cli) => {
    for (const title of TITLES.slice(0, TITLES.indexOf('Dependencies'))) await cli.answer(title);
    await cli.waitFor('? Dependencies');
    cli.send('zzz');
    await cli.waitFor('no match');
    cli.send(KEY.space + KEY.enter); // toggling with nothing visible selects nothing
    await acceptRest(cli, 'Extract project?');
    assert.equal(await cli.exit(), 0);
    assert.equal(cli.command(), defaultCommand());
  }));

test('esc on the first step is ignored and shows no back hint', () =>
  withCli(['--dry-run'], async (cli) => {
    const s = await cli.waitFor('? Project type');
    assert.ok(!s.includes('esc: previous step'));
    cli.send(KEY.esc);
    await new Promise((r) => setTimeout(r, 800));
    assert.ok(cli.screen().includes('? Project type'));
    await acceptRest(cli, 'Project type');
    assert.equal(await cli.exit(), 0);
    assert.equal(cli.command(), defaultCommand());
  }));

test('esc goes back a step, erases its summary and keeps the earlier answer', () =>
  withCli(['--dry-run'], async (cli) => {
    await cli.answer('Project type', KEY.down + KEY.enter);
    await cli.waitFor('✔ Project type: Gradle - Groovy');
    const s = await cli.waitFor('? language');
    assert.ok(s.includes('esc: previous step'));
    cli.send(KEY.esc);
    await cli.waitFor((/❯ Gradle - Groovy/));
    assert.ok(!cli.screen().includes('✔ Project type'), 'summary of the revisited step should be erased');
    await cli.answer('Project type'); // Enter keeps the preselected earlier answer

    for (const title of ['language', 'bootVersion', 'packaging', 'javaVersion']) await cli.answer(title);
    await cli.answer('groupId', 'org.x' + KEY.enter);
    await cli.waitFor('? artifactId');
    cli.send(KEY.esc);
    await cli.waitFor('> org.x'); // text step is prefilled with the earlier answer
    await cli.answer('groupId');
    await acceptRest(cli, 'artifactId');

    assert.equal(await cli.exit(), 0);
    const cmd = cli.command();
    assert.match(cmd, / --type gradle-project /);
    assert.match(cmd, / --group-id org\.x /);
    assert.match(cmd, / --package-name org\.x\.demo /);
  }));

test('ctrl+c exits with code 130 without printing a command', () =>
  withCli(['--dry-run'], async (cli) => {
    await cli.answer('Project type');
    await cli.waitFor('? language');
    cli.send(KEY.ctrlC);
    assert.equal(await cli.exit(), 130);
    assert.ok(!cli.screen().includes('spring init'));
  }));

test('without --dry-run it runs spring init and exits with its status', () => {
  const fakeBin = fs.mkdtempSync(path.join(os.tmpdir(), 'fake-spring-'));
  fs.writeFileSync(path.join(fakeBin, 'spring.cmd'), '@echo FAKE-SPRING %*\r\n@exit /b 7\r\n');
  fs.writeFileSync(path.join(fakeBin, 'spring'), '#!/bin/sh\necho FAKE-SPRING "$@"\nexit 7\n', { mode: 0o755 });
  const env = { ...process.env, PATH: fakeBin + path.delimiter + process.env.PATH };
  return withCli([], async (cli) => {
    await acceptRest(cli, 'Project type');
    assert.equal(await cli.exit(), 7);
    await cli.waitFor('FAKE-SPRING init --target');
  }, env);
});

test('fails with a clear error when not attached to a terminal', async () => {
  const r = await runPiped(['--target', target, '--dry-run']);
  assert.equal(r.status, 1);
  assert.match(r.stderr, /interactive terminal/);
});

test('fails with a clear error when metadata cannot be fetched', async () => {
  const r = await runPiped(['--target', 'http://127.0.0.1:1', '--dry-run']);
  assert.equal(r.status, 1);
  assert.ok(r.stderr.trim().length > 0, 'expected an error message on stderr');
});
