#!/usr/bin/env node
// Interactively select Spring Initializr options and build a `spring init` command.
// Usage: node spring-init.js [--target URL] [--run]
const readline = require('readline');
const { spawnSync } = require('child_process');

const args = process.argv.slice(2);
const target = args.includes('--target') ? args[args.indexOf('--target') + 1] : 'https://start.spring.io';
const run = args.includes('--run');

const ESC = '\x1b[';
const dim = (s) => `${ESC}2m${s}${ESC}0m`;
const cyan = (s) => `${ESC}36m${s}${ESC}0m`;
const green = (s) => `${ESC}32m${s}${ESC}0m`;
const inverse = (s) => `${ESC}7m${s}${ESC}0m`;
const PAGE = Math.max(3, Math.min(10, (process.stdout.rows || 24) - 8));
const BACK = Symbol('back');
let canGoBack = false; // set per step by main(); the first step has no previous step
const backHint = () => (canGoBack ? dim('  esc: previous step') : '');

// Prompts are drawn in place below the current terminal output: each redraw moves the cursor
// back up over the rows drawn last time and clears from there down.
const strip = (s) => s.replace(/\x1b\[[0-9;]*m/g, '');
const rowsOf = (lines) => lines.reduce((n, l) => n + Math.max(1, Math.ceil(strip(l).length / (process.stdout.columns || 80))), 0);
let drawnRows = 0; // rows of the active prompt on screen
const answeredRows = []; // rows of each answered step's summary line, erased again on going back

function eraseUp(rows) {
  if (rows > 0) process.stdout.write(`${ESC}${rows}A`);
  process.stdout.write(`\r${ESC}0J`);
}

function draw(lines) {
  eraseUp(Math.max(0, drawnRows - 1));
  process.stdout.write(lines.join('\n'));
  drawnRows = rowsOf(lines);
}

function exit(code) {
  process.stdout.write(`${ESC}?25h`);
  process.stdin.setRawMode(false);
  process.exit(code);
}

// Runs a prompt: `render()` returns its lines, `onKey(str, key)` returns a value to resolve or undefined
// to keep going, `summary(value)` is the line left on screen once answered.
// Esc resolves with BACK, erasing the previous step's summary so it can be asked again.
function prompt(title, render, onKey, summary) {
  return new Promise((resolve) => {
    const handler = (str, key = {}) => {
      if (key.ctrl && key.name === 'c') {
        draw([]);
        exit(130);
      }
      if (key.name === 'escape' && !canGoBack) return;
      const result = key.name === 'escape' ? BACK : onKey(str, key);
      if (result === undefined) return draw(render());
      process.stdin.off('keypress', handler);
      draw([]);
      drawnRows = 0;
      if (result === BACK) {
        eraseUp(answeredRows.pop() || 0);
      } else {
        const line = `${green('✔')} ${title}: ${cyan(summary(result))}`;
        process.stdout.write(line + '\n');
        answeredRows.push(rowsOf([line]));
      }
      resolve(result);
    };
    draw(render());
    process.stdin.on('keypress', handler);
  });
}

function window(items, cursor) {
  const start = Math.min(Math.max(0, cursor - Math.floor(PAGE / 2)), Math.max(0, items.length - PAGE));
  return { start, slice: items.slice(start, start + PAGE) };
}

function select(title, items, defaultId, current) {
  let cursor = Math.max(0, items.findIndex((i) => i.id === (current ?? defaultId)));
  return prompt(
    title,
    () => {
      const { start, slice } = window(items, cursor);
      return [
        cyan(`? ${title}`) + dim('  (↑/↓, enter)') + backHint(),
        ...slice.map((item, i) => {
          const mark = item.id === defaultId ? dim(' (default)') : '';
          return start + i === cursor ? green(`❯ ${item.name}`) + mark : `  ${item.name}${mark}`;
        }),
      ];
    },
    (str, key) => {
      if (key.name === 'up') cursor = (cursor - 1 + items.length) % items.length;
      else if (key.name === 'down') cursor = (cursor + 1) % items.length;
      else if (key.name === 'return') return items[cursor].id;
    },
    (id) => items.find((i) => i.id === id).name,
  );
}

function text(title, def = '', current = '') {
  let value = current;
  return prompt(
    title,
    () => [cyan(`? ${title}`) + (def ? dim(` (${def})`) : '') + backHint(), `> ${value}${inverse(' ')}`],
    (str, key) => {
      if (key.name === 'return') return value.trim() || def;
      if (key.name === 'backspace') value = value.slice(0, -1);
      else if (str && !key.ctrl && str >= ' ') value += str;
    },
    (v) => v || '-',
  );
}

function multiSelect(title, items, current = []) {
  let filter = '';
  let cursor = 0;
  const chosen = new Set(current);
  const visible = () => {
    const f = filter.toLowerCase();
    return items.filter((i) => !f || `${i.id} ${i.name} ${i.group}`.toLowerCase().includes(f));
  };
  return prompt(
    title,
    () => {
      const list = visible();
      const { start, slice } = window(list, cursor);
      return [
        cyan(`? ${title}`) + dim('  (type to filter, ↑/↓, space toggle, enter done)') + backHint(),
        `  filter: ${filter}${inverse(' ')}`,
        `  selected: ${green([...chosen].join(', ') || '-')}`,
        ...slice.map((item, i) => {
          const box = chosen.has(item.id) ? green('◉') : '◯';
          const line = `${box} ${item.name} ${dim(`[${item.id}] ${item.group}`)}`;
          return start + i === cursor ? green('❯ ') + line : '  ' + line;
        }),
        ...(list.length ? [] : [dim('  no match')]),
      ];
    },
    (str, key) => {
      const list = visible();
      if (key.name === 'return') return [...chosen];
      if (key.name === 'up') cursor = Math.max(0, cursor - 1);
      else if (key.name === 'down') cursor = Math.min(list.length - 1, cursor + 1);
      else if (key.name === 'space') {
        const item = list[cursor];
        if (item) chosen.has(item.id) ? chosen.delete(item.id) : chosen.add(item.id);
      } else if (key.name === 'backspace') {
        filter = filter.slice(0, -1);
        cursor = 0;
      } else if (str && !key.ctrl && str > ' ') {
        filter += str;
        cursor = 0;
      }
    },
    (ids) => ids.join(', ') || '-',
  );
}

function quote(v) {
  return /^[\w.,:\/@=-]+$/.test(v) ? v : `"${v.replace(/"/g, '\\"')}"`;
}

async function main() {
  const res = await fetch(target, { headers: { Accept: 'application/vnd.initializr.v2.2+json' } });
  if (!res.ok) throw new Error(`Failed to fetch ${target}: ${res.status}`);
  const meta = await res.json();

  if (!process.stdin.isTTY) throw new Error('An interactive terminal is required.');
  readline.emitKeypressEvents(process.stdin);
  process.stdin.setRawMode(true);
  process.stdout.write(`${ESC}?25l`);

  const opts = {};
  const types = meta.type.values.filter((v) => v.action === '/starter.zip');
  const deps = meta.dependencies.values.flatMap((g) => g.values.map((d) => ({ ...d, group: g.name })));
  const selectStep = (key, title, values, def) => () => select(title, values, def, opts[key]);
  const textStep = (key, def, title = key) => () => text(title, def(), opts[key]);
  const steps = [
    ['type', selectStep('type', 'Project type', types, meta.type.default)],
    ...['language', 'bootVersion', 'packaging', 'javaVersion'].map((k) => [k, selectStep(k, k, meta[k].values, meta[k].default)]),
    ['groupId', textStep('groupId', () => meta.groupId.default)],
    ['artifactId', textStep('artifactId', () => meta.artifactId.default)],
    ['name', textStep('name', () => opts.artifactId)],
    ['description', textStep('description', () => meta.description.default || 'Demo project for Spring Boot')],
    ['packageName', textStep('packageName', () => `${opts.groupId}.${opts.artifactId}`.replace(/[^\w.]/g, ''))],
    ['version', textStep('version', () => meta.version.default)],
    ['dependencies', () => multiSelect('Dependencies', deps, opts.dependencies)],
    ['extract', selectStep('extract', 'Extract project?', [{ id: 'yes', name: 'Yes (--extract)' }, { id: 'no', name: 'No (zip)' }], 'yes')],
    ['location', textStep('location', () => opts.artifactId, 'Output location')],
  ];
  for (let i = 0; i < steps.length; ) {
    const [key, ask] = steps[i];
    canGoBack = i > 0;
    const answer = await ask();
    if (answer === BACK) {
      i = Math.max(0, i - 1);
    } else {
      opts[key] = answer;
      i++;
    }
  }
  const { dependencies, location } = opts;
  const extract = opts.extract === 'yes';

  process.stdout.write(`${ESC}?25h\n`);
  process.stdin.setRawMode(false);
  process.stdin.pause();

  const flags = {
    type: '--type', language: '--language', bootVersion: '--boot-version', packaging: '--packaging',
    javaVersion: '--java-version', groupId: '--group-id', artifactId: '--artifact-id', name: '--name',
    description: '--description', packageName: '--package-name', version: '--version',
  };
  const cmd = ['spring', 'init'];
  if (target !== 'https://start.spring.io') cmd.push('--target', target);
  for (const [key, flag] of Object.entries(flags)) if (opts[key]) cmd.push(flag, opts[key]);
  if (dependencies.length) cmd.push('--dependencies', dependencies.join(','));
  if (extract) cmd.push('--extract');
  if (location) cmd.push(location);

  const line = cmd.map(quote).join(' ');
  console.log(green(line));
  if (run) spawnSync(line, { stdio: 'inherit', shell: true });
}

main().catch((e) => {
  console.error(e.message);
  process.exit(1);
});
