#!/usr/bin/env node
import { spawnSync } from 'node:child_process'
import { existsSync } from 'node:fs'
import { resolve } from 'node:path'

const root = resolve(new URL('..', import.meta.url).pathname)
const desktop = resolve(root, 'packages/host/desktop')
const target = process.argv[2] ?? 'mac'
const args = process.argv.slice(3)

function run(command, commandArgs, cwd = root) {
  const result = spawnSync(command, commandArgs, { cwd, stdio: 'inherit', env: process.env })
  if (result.error) throw result.error
  if (result.status !== 0) process.exit(result.status ?? 1)
}

if (!existsSync(desktop)) throw new Error(`desktop package not found: ${desktop}`)
if (target === 'mac') {
  if (process.platform !== 'darwin') throw new Error('Mac packaging must run on a native macOS host')
  run('corepack', ['yarn', 'workspace', 'dsh-plugin-desktop', 'dist:mac-smoke', ...args])
} else if (target === 'win') {
  if (process.platform !== 'win32') throw new Error('Windows packaging must run on a native Windows host')
  run('corepack', ['yarn', 'workspace', 'dsh-plugin-desktop', 'dist:win', ...args])
} else if (target === 'both') {
  throw new Error('Cannot build native Mac and Windows artifacts in one host process; run this command once on each native host')
} else {
  throw new Error(`unknown target ${target}; use mac or win`)
}
