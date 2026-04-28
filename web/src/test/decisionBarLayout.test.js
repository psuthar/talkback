import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const __dirname = dirname(fileURLToPath(import.meta.url))
const css = readFileSync(resolve(__dirname, '../components/DecisionBar.module.css'), 'utf8')

function getRuleBody(source, selector) {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const re = new RegExp(`${esc}\\s*\\{([\\s\\S]*?)\\}`, 'm')
  const match = source.match(re)
  return match ? match[1] : ''
}

function hasDecl(ruleBody, prop, value) {
  const re = new RegExp(`${prop}\\s*:\\s*${value}\\s*;`)
  return re.test(ruleBody)
}

describe('DecisionBar rationale layout contract (SCRUM-163)', () => {
  it('keeps rationale block aligned with stance controls and wraps on constrained widths', () => {
    const rationaleBlock = getRuleBody(css, '.rationaleBlock')
    expect(rationaleBlock).toBeTruthy()
    expect(hasDecl(rationaleBlock, 'display', 'flex')).toBe(true)
    expect(hasDecl(rationaleBlock, 'align-items', 'flex-start')).toBe(true)
    expect(hasDecl(rationaleBlock, 'flex-wrap', 'wrap')).toBe(true)
  })

  it('uses a textarea footprint that aligns with stance control height and shrinks before wrapping', () => {
    const rationaleInput = getRuleBody(css, '.rationaleInput')
    expect(rationaleInput).toBeTruthy()
    expect(hasDecl(rationaleInput, 'flex', '1 1 260px')).toBe(true)
    expect(hasDecl(rationaleInput, 'min-width', '180px')).toBe(true)
    expect(hasDecl(rationaleInput, 'min-height', '34px')).toBe(true)
    expect(hasDecl(rationaleInput, 'resize', 'vertical')).toBe(true)
  })
})
