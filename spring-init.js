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

// Runs a prompt: `render()` draws it, `onKey(str, key)` returns a value to resolve or undefined to keep going.
function prompt(render, onKey) {
  return new Promise((resolve) => {
    const handler = (str, key = {}) => {
      if (key.ctrl && key.name === 'c') {
        process.stdin.setRawMode(false);
        process.stdout.write('\n');
        process.exit(130);
      }
      const result = onKey(str, key);
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

function select(title, items, defaultId) {
  let cursor = Math.max(0, items.findIndex((i) => i.id === defaultId));
  return prompt(
    () => {
      console.log(cyan(`? ${title}`) + dim('  (↑/↓, enter)'));
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

function text(title, def = '') {
  let value = '';
  return prompt(
    () => process.stdout.write(cyan(`? ${title}`) + (def ? dim(` (${def})`) : '') + ': ' + value),
    (str, key) => {
      if (key.name === 'return') return value.trim() || def;
      if (key.name === 'backspace') value = value.slice(0, -1);
      else if (str && !key.ctrl && str >= ' ') value += str;
    },
  );
}

function multiSelect(title, items) {
  let filter = '';
  let cursor = 0;
  const chosen = new Set();
  const visible = () => {
    const f = filter.toLowerCase();
    return items.filter((i) => !f || `${i.id} ${i.name} ${i.group}`.toLowerCase().includes(f));
  };
  return prompt(
    () => {
      const list = visible();
      console.log(cyan(`? ${title}`) + dim('  (type to filter, ↑/↓, space toggle, enter done)'));
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
  opts.type = await select('Project type', types, meta.type.default);
  for (const key of ['language', 'bootVersion', 'packaging', 'javaVersion']) {
    opts[key] = await select(key, meta[key].values, meta[key].default);
  }
  opts.groupId = await text('groupId', meta.groupId.default);
  opts.artifactId = await text('artifactId', meta.artifactId.default);
  opts.name = await text('name', opts.artifactId);
  opts.description = await text('description', meta.description.default || 'Demo project for Spring Boot');
  opts.packageName = await text('packageName', `${opts.groupId}.${opts.artifactId}`.replace(/[^\w.]/g, ''));
  opts.version = await text('version', meta.version.default);

  const deps = meta.dependencies.values.flatMap((g) => g.values.map((d) => ({ ...d, group: g.name })));
  const dependencies = await multiSelect('Dependencies', deps);
  const extract = (await select('Extract project?', [{ id: 'yes', name: 'Yes (--extract)' }, { id: 'no', name: 'No (zip)' }], 'yes')) === 'yes';
  const location = await text('Output location', opts.artifactId);

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
