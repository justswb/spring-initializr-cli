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
const clear = () => process.stdout.write(`${ESC}2J${ESC}H`);
const PAGE = Math.max(5, (process.stdout.rows || 24) - 8);
const BACK = Symbol('back');
const BACK_HINT = dim('  esc: previous step');

// Runs a prompt: `render()` draws it, `onKey(str, key)` returns a value to resolve or undefined to keep going.
// Esc resolves with BACK so the caller can return to the previous step.
function prompt(render, onKey) {
  return new Promise((resolve) => {
    const handler = (str, key = {}) => {
      if (key.ctrl && key.name === 'c') {
        process.stdin.setRawMode(false);
        process.stdout.write('\n');
        process.exit(130);
      }
      const result = key.name === 'escape' ? BACK : onKey(str, key);
      if (result !== undefined) {
        process.stdin.off('keypress', handler);
        resolve(result);
      } else {
        clear();
        render();
      }
    };
    clear();
    render();
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
    () => {
      console.log(cyan(`? ${title}`) + dim('  (↑/↓, enter)') + BACK_HINT);
      const { start, slice } = window(items, cursor);
      slice.forEach((item, i) => {
        const idx = start + i;
        const mark = item.id === defaultId ? dim(' (default)') : '';
        console.log(idx === cursor ? green(`❯ ${item.name}`) + mark : `  ${item.name}${mark}`);
      });
    },
    (str, key) => {
      if (key.name === 'up') cursor = (cursor - 1 + items.length) % items.length;
      else if (key.name === 'down') cursor = (cursor + 1) % items.length;
      else if (key.name === 'return') return items[cursor].id;
    },
  );
}

function text(title, def = '', current = '') {
  let value = current;
  return prompt(
    () => process.stdout.write(cyan(`? ${title}`) + (def ? dim(` (${def})`) : '') + BACK_HINT + '\n> ' + value),
    (str, key) => {
      if (key.name === 'return') return value.trim() || def;
      if (key.name === 'backspace') value = value.slice(0, -1);
      else if (str && !key.ctrl && str >= ' ') value += str;
    },
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
    () => {
      const list = visible();
      console.log(cyan(`? ${title}`) + dim('  (type to filter, ↑/↓, space toggle, enter done)') + BACK_HINT);
      console.log(`  filter: ${filter}${dim('_')}`);
      console.log(`  selected: ${green([...chosen].join(', ') || '-')}\n`);
      const { start, slice } = window(list, cursor);
      slice.forEach((item, i) => {
        const box = chosen.has(item.id) ? green('◉') : '◯';
        const line = `${box} ${item.name} ${dim(`[${item.id}] ${item.group}`)}`;
        console.log(start + i === cursor ? green('❯ ') + line : '  ' + line);
      });
      if (!list.length) console.log(dim('  no match'));
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

  process.stdin.setRawMode(false);
  process.stdin.pause();
  clear();

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
