// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import path from 'node:path'

// SCRUM-450: regression guard against the "frozen Generating-slides spinner"
// bug. The InlineSpinner used in the materials list relies on a keyframe
// (tb-spin) that is injected globally into <style id="tb-spinner-keyframes">
// at runtime by ensureSpinnerStyle(). The original implementation used a
// .module.css class with `animation: tb-spin ...`, which CSS Modules scopes
// (rewrites the keyframe name to _tb-spin_<hash>_), permanently breaking
// the link to the global keyframe and leaving the icon as a static
// partial-arc.
//
// The fix moves the `animation:` declaration to an inline style on the React
// element so it bypasses the CSS-Modules transform entirely. Visual
// properties stay in the .module.css class.
//
// We assert on the source files via fs because:
//   (1) jsdom does not run the CSS-Modules transform, so a runtime
//       getComputedStyle check would not reproduce the prod bug; and
//   (2) the fix surface is two specific files — the regression bar is the
//       file-level intent, not the rendered output in jsdom.

const here = path.dirname(fileURLToPath(import.meta.url))
const cssPath = path.resolve(here, '../components/MaterialsTreePanel.module.css')
const jsxPath = path.resolve(here, '../components/MaterialsTreePanel.jsx')

// Strip /* … */ comments before regex-checking so explanatory comments that
// quote the broken form don't trip the negative assertions.
const cssNoComments = readFileSync(cssPath, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '')
const jsxSource = readFileSync(jsxPath, 'utf8')

describe('SCRUM-450: MaterialsTreePanel inline spinner keyframe scoping', () => {
  it('CSS module declares NO animation property (so CSS Modules cannot scope the keyframe name)', () => {
    expect(cssNoComments).not.toMatch(/animation\s*:/)
    expect(cssNoComments).not.toMatch(/animation-name\s*:/)
  })

  it('JSX applies the rotation via an inline style that references tb-spin literally', () => {
    // Inline styles are not subject to the CSS-Modules transform, so the
    // bare keyframe name resolves to the globally-injected @keyframes.
    expect(jsxSource).toContain("animation: 'tb-spin 0.8s linear infinite'")
  })

  it('JSX still injects the global @keyframes via ensureSpinnerStyle so the inline reference resolves', () => {
    expect(jsxSource).toContain('@keyframes tb-spin')
    expect(jsxSource).toContain('ensureSpinnerStyle()')
  })
})
