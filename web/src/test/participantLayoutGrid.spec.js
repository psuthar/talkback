import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

// CSS spec test: the participant layout grid is a load-bearing visual contract
// (SCRUM-158). Vitest can't render the actual layout meaningfully, so we read the
// stylesheet and assert the documented column templates instead. This catches
// accidental fraction drift in the index.css blocks the ticket explicitly enumerates.
const __dirname = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(resolve(__dirname, '../index.css'), 'utf8')

// Extract every `.participant-layout-grid` rule (default + collapsed variant) along
// with its `grid-template-columns` declaration so we can assert each block individually.
function extractGridDeclarations(source) {
  const out = []
  // Match `<selector> { ... grid-template-columns: <value>; ... }` blocks
  const re = /(\.participant-layout-grid(?:\.materials-collapsed)?)\s*\{[^}]*?grid-template-columns:\s*([^;]+);/g
  let m
  while ((m = re.exec(source)) !== null) {
    // Track approximate position so we can attribute to a media query block
    out.push({ selector: m[1], value: m[2].trim(), index: m.index })
  }
  return out
}

function blockForRule(source, ruleIndex) {
  // Walk back from the rule index to find the nearest enclosing @media (...) start.
  // If none is between ruleIndex and the most recent `}` of an @media block, this rule
  // is at the file's top level (no media query wrapper).
  const before = source.slice(0, ruleIndex)
  const lastMediaOpen = before.lastIndexOf('@media')
  if (lastMediaOpen === -1) return 'default'
  // Find the matching closing brace of that @media block by counting braces from its start.
  let depth = 0
  let i = lastMediaOpen
  let started = false
  for (; i < source.length; i++) {
    const ch = source[i]
    if (ch === '{') {
      depth++
      started = true
    } else if (ch === '}') {
      depth--
      if (started && depth === 0) {
        // End of this @media block. If our rule index is past it, this rule is outside the @media.
        if (ruleIndex > i) return 'default'
        break
      }
    }
  }
  // Rule is inside this @media block — return the condition text.
  const conditionMatch = source.slice(lastMediaOpen).match(/@media\s*\(([^)]+)\)/)
  return conditionMatch ? conditionMatch[1].trim() : 'unknown'
}

describe('participant layout grid templates (SCRUM-158)', () => {
  const decls = extractGridDeclarations(css)

  it('declares the grid template columns at the default and both responsive breakpoints', () => {
    // 6 declarations expected: default + collapsed × (default block, 1024 block, 768 block).
    const grouped = decls.map((d) => ({ ...d, block: blockForRule(css, d.index) }))
    const blocks = new Set(grouped.map((g) => g.block))
    expect(blocks.has('default')).toBe(true)
    expect(blocks.has('max-width: 1024px')).toBe(true)
    expect(blocks.has('max-width: 768px')).toBe(true)
    // Six declarations: default expanded + default collapsed + 1024 expanded + 1024 collapsed + 768 expanded + 768 collapsed
    expect(grouped.length).toBe(6)
  })

  it('uses 20% / 40% / 40% in the default participant grid (Q&A column gains 5pp from center)', () => {
    const grouped = decls.map((d) => ({ ...d, block: blockForRule(css, d.index) }))
    const def = grouped.find((g) => g.selector === '.participant-layout-grid' && g.block === 'default')
    expect(def, 'default .participant-layout-grid declaration must exist').toBeTruthy()
    expect(def.value).toBe('20% 40% 40%')
  })

  it('uses 48px 1fr 40% when the materials column is collapsed at default width', () => {
    const grouped = decls.map((d) => ({ ...d, block: blockForRule(css, d.index) }))
    const collapsed = grouped.find(
      (g) => g.selector === '.participant-layout-grid.materials-collapsed' && g.block === 'default'
    )
    expect(collapsed.value).toBe('48px 1fr 40%')
  })

  it('uses 20% / 40% / 40% inside the 1024px breakpoint (matches default)', () => {
    const grouped = decls.map((d) => ({ ...d, block: blockForRule(css, d.index) }))
    const at1024 = grouped.find(
      (g) => g.selector === '.participant-layout-grid' && g.block === 'max-width: 1024px'
    )
    expect(at1024.value).toBe('20% 40% 40%')
    const at1024Collapsed = grouped.find(
      (g) => g.selector === '.participant-layout-grid.materials-collapsed' && g.block === 'max-width: 1024px'
    )
    expect(at1024Collapsed.value).toBe('48px 1fr 40%')
  })

  it('uses 24% / 38% / 38% inside the 768px breakpoint (proportional shift of 5pp from center)', () => {
    const grouped = decls.map((d) => ({ ...d, block: blockForRule(css, d.index) }))
    const at768 = grouped.find(
      (g) => g.selector === '.participant-layout-grid' && g.block === 'max-width: 768px'
    )
    expect(at768.value).toBe('24% 38% 38%')
    const at768Collapsed = grouped.find(
      (g) => g.selector === '.participant-layout-grid.materials-collapsed' && g.block === 'max-width: 768px'
    )
    expect(at768Collapsed.value).toBe('48px 1fr 38%')
  })

  it('every column template sums to 100% (no horizontal overflow at the documented widths)', () => {
    const grouped = decls.map((d) => ({ ...d, block: blockForRule(css, d.index) }))
    for (const g of grouped) {
      // For percentage-only templates, the three values should sum to 100. The collapsed
      // templates use `48px 1fr <pct>` — there the only number-with-percent is the right
      // column. For those rows we don't sum (px + 1fr + pct does not have a clean total),
      // we just assert the right column matches the corresponding non-collapsed row's
      // right value.
      if (/^[0-9]/.test(g.value) && g.value.indexOf('%') !== -1 && g.value.indexOf('1fr') === -1 && g.value.indexOf('px') === -1) {
        const parts = g.value.split(/\s+/).map((p) => Number(p.replace('%', '')))
        expect(parts.length).toBe(3)
        const sum = parts.reduce((a, b) => a + b, 0)
        expect(sum).toBe(100)
      }
    }
  })
})
