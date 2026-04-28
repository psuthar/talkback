import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const __dirname = dirname(fileURLToPath(import.meta.url))
const participantModeCss = readFileSync(resolve(__dirname, '../modes/ParticipantMode.module.css'), 'utf8')
const sessionMenuCss = readFileSync(resolve(__dirname, '../components/ParticipantSessionMenu.module.css'), 'utf8')

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

describe('participant topbar alignment contract (SCRUM-160)', () => {
  it('keeps topbar inner row single-line and stretchable for title/menu alignment', () => {
    const rule = getRuleBody(participantModeCss, '.topbarInner')
    expect(rule).toBeTruthy()
    expect(hasDecl(rule, 'display', 'flex')).toBe(true)
    expect(hasDecl(rule, 'align-items', 'center')).toBe(true)
    expect(hasDecl(rule, 'flex-wrap', 'nowrap')).toBe(true)
    expect(hasDecl(rule, 'flex', '1 1 auto')).toBe(true)
  })

  it('ensures long session titles truncate instead of wrapping rows', () => {
    const rule = getRuleBody(participantModeCss, '.sessionTitle')
    expect(rule).toBeTruthy()
    expect(hasDecl(rule, 'white-space', 'nowrap')).toBe(true)
    expect(hasDecl(rule, 'overflow', 'hidden')).toBe(true)
    expect(hasDecl(rule, 'text-overflow', 'ellipsis')).toBe(true)
  })

  it('pins session menu root to centered inline alignment in topbar', () => {
    const rule = getRuleBody(sessionMenuCss, '.menuRoot')
    expect(rule).toBeTruthy()
    expect(hasDecl(rule, 'display', 'inline-flex')).toBe(true)
    expect(hasDecl(rule, 'flex', '0 0 auto')).toBe(true)
    expect(hasDecl(rule, 'align-self', 'center')).toBe(true)
  })
})
