// SCRUM-579 (Slice 2 of SCRUM-577): unit tests for AdminGuardrailStats.
// Mirrors the AdminUsers.test.jsx pattern: vi.stubGlobal('fetch', ...)
// returns canned payloads; assertions hit visible text, fetch URL, and
// computed bar widths.

import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import { AdminGuardrailStats } from '../components/AdminGuardrailStats'

afterEach(() => {
  vi.restoreAllMocks()
})

// Minimal mock payload matching the SCRUM-578 + SCRUM-568 contract.
// Tests override individual fields as needed.
const samplePayload = ({
  total_calls = 1000,
  by_decision = { allowed: 980, refused: 18, redacted: 2 },
  by_site = { qa_ask: 700, qa_grounding_judge: 250, action_items: 50 },
  top_refusal_codes = [
    { code: 'citation_missing', count: 12 },
    { code: 'grounding_failed', count: 4 },
    { code: 'input_off_scope', count: 2 },
  ],
  p95_latency_ms = 1840.5,
  dropped_telemetry_rows = 0,
  total_input_tokens = null,
  total_output_tokens = null,
  by_model = {},
} = {}) => ({
  days_window: 7,
  since: '2026-05-18T12:00:00Z',
  total_calls,
  by_decision,
  by_site,
  top_refusal_codes,
  p95_latency_ms,
  dropped_telemetry_rows,
  total_input_tokens,
  total_output_tokens,
  by_model,
})

function mockFetchReturning(body, { status = 200 } = {}) {
  return vi.fn(async () => ({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  }))
}

describe('AdminGuardrailStats', () => {
  it('does not fetch when collapsed', async () => {
    const fetchMock = mockFetchReturning(samplePayload())
    vi.stubGlobal('fetch', fetchMock)
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={false}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    // Give the effect a tick.
    await new Promise((r) => setTimeout(r, 10))
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('fetches /api/admin/llm-stats?days=7 on first expand', async () => {
    const fetchMock = mockFetchReturning(samplePayload())
    vi.stubGlobal('fetch', fetchMock)
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith('/api/admin/llm-stats?days=7', {
        credentials: 'include',
      })
    })
  })

  it('refetches with new window when [1d] or [30d] is clicked', async () => {
    const fetchMock = mockFetchReturning(samplePayload())
    vi.stubGlobal('fetch', fetchMock)
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/admin/llm-stats?days=7', expect.anything()),
    )
    fireEvent.click(screen.getByRole('button', { name: '1d' }))
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/admin/llm-stats?days=1', expect.anything()),
    )
    fireEvent.click(screen.getByRole('button', { name: '30d' }))
    await waitFor(() =>
      expect(fetchMock).toHaveBeenCalledWith('/api/admin/llm-stats?days=30', expect.anything()),
    )
  })

  it('refetches when Refresh is clicked', async () => {
    const fetchMock = mockFetchReturning(samplePayload())
    vi.stubGlobal('fetch', fetchMock)
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    fireEvent.click(screen.getByRole('button', { name: /Refresh/i }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
  })

  it('renders the four big-number cards with formatted values', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(
        samplePayload({
          total_calls: 12418,
          by_decision: { allowed: 12181, refused: 237, redacted: 0 },
          p95_latency_ms: 1840.5,
          dropped_telemetry_rows: 0,
        }),
      ),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Total calls')).toBeInTheDocument())
    expect(screen.getByText('12,418')).toBeInTheDocument() // total
    expect(screen.getByText('1,841 ms')).toBeInTheDocument() // p95 rounded
    // refused count "237" appears in BOTH the Refused big-number card
    // AND the Decisions table row — both are correct. Assert presence
    // via getAllByText to allow either count.
    expect(screen.getAllByText('237').length).toBeGreaterThanOrEqual(1)
    // refused % = 237/12418 = 1.9%
    expect(screen.getByText(/1\.9% of total/)).toBeInTheDocument()
  })

  it('renders the top refusal codes bar chart with correct widths', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(
        samplePayload({
          top_refusal_codes: [
            { code: 'citation_missing', count: 142 },
            { code: 'grounding_failed', count: 58 },
            { code: 'input_off_scope', count: 37 },
          ],
        }),
      ),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('citation_missing')).toBeInTheDocument())
    // Max count = 142, so 58 maps to 40.845% and 37 maps to 26.056%.
    expect(screen.getByTestId('bar-citation_missing').style.width).toBe('100%')
    expect(screen.getByTestId('bar-grounding_failed').style.width).toMatch(/^40\./)
    expect(screen.getByTestId('bar-input_off_scope').style.width).toMatch(/^26\./)
  })

  it('renders Decisions in fixed order; missing keys show 0', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(samplePayload({ by_decision: { allowed: 500 } })),
    )
    const { container } = render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Decisions')).toBeInTheDocument())
    // Pull the first table after the Decisions heading.
    const decisionsHeading = screen.getByText('Decisions')
    const table = decisionsHeading.parentElement.querySelector('table')
    const rowTexts = Array.from(table.querySelectorAll('tr')).map((r) => r.textContent)
    expect(rowTexts[0]).toContain('allowed')
    expect(rowTexts[0]).toContain('500')
    expect(rowTexts[1]).toContain('refused')
    expect(rowTexts[1]).toContain('0') // missing key → 0
    expect(rowTexts[2]).toContain('redacted')
    expect(rowTexts[2]).toContain('0')
    expect(container).toBeTruthy()
  })

  it('renders the fixed Sites enum; sites not in payload show 0', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(samplePayload({ by_site: { qa_ask: 100, obsworker: 25 } })),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Sites')).toBeInTheDocument())
    // All 8 sites should be present even if 6 of them are 0.
    for (const site of [
      'qa_ask',
      'qa_ask_retry_citation',
      'qa_ask_retry_grounding',
      'qa_grounding_judge',
      'action_items',
      'action_items_retry_schema',
      'question_polish',
      'obsworker',
    ]) {
      expect(screen.getByText(site)).toBeInTheDocument()
    }
  })

  it('shows the empty-state message when total_calls is 0', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(
        samplePayload({
          total_calls: 0,
          by_decision: {},
          by_site: {},
          top_refusal_codes: [],
          p95_latency_ms: null,
        }),
      ),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() =>
      expect(screen.getByText('No LLM calls recorded in this window.')).toBeInTheDocument(),
    )
    // p95 null and Token usage null both render "—" in their big-number
    // cards. Assert at least one is present.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(1)
    // The bar chart + tables don't render in empty state.
    expect(screen.queryByText('Top refusal codes (last 7d)')).not.toBeInTheDocument()
  })

  it('shows red warning subtitle on the Dropped card when dropped > 0', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(samplePayload({ dropped_telemetry_rows: 17 })),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() =>
      expect(screen.getByText(/17 dropped — buffer pressure/)).toBeInTheDocument(),
    )
  })

  it('renders "Forbidden: admin access required" on 403', async () => {
    vi.stubGlobal('fetch', mockFetchReturning({}, { status: 403 }))
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() =>
      expect(screen.getByText('Forbidden: admin access required')).toBeInTheDocument(),
    )
  })

  it('renders "Failed to load: 500" on a 500 error', async () => {
    vi.stubGlobal('fetch', mockFetchReturning({}, { status: 500 }))
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Failed to load: 500')).toBeInTheDocument())
  })

  it('renders the network error message when fetch throws', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => {
        throw new Error('connection refused')
      }),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('connection refused')).toBeInTheDocument())
  })

  // SCRUM-580 (Slice 3): cost surface tests — Token usage card +
  // Models table + defensive fallback when the API doesn't return the
  // cost fields (pre-SCRUM-578 backend or no token-bearing rows).

  it('renders the Token usage card when token totals are present', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(
        samplePayload({
          total_input_tokens: 12340,
          total_output_tokens: 5678,
          by_model: { 'gpt-4o-mini': 100, 'gpt-4o': 25 },
        }),
      ),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Token usage')).toBeInTheDocument())
    // Card value = sum formatted with thousands separators: 18,018.
    expect(screen.getByText('18,018')).toBeInTheDocument()
    // Subtitle: "12,340 in / 5,678 out".
    expect(screen.getByText('12,340 in / 5,678 out')).toBeInTheDocument()
  })

  it('Token usage card shows "—" when both totals are null', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(
        samplePayload({
          total_input_tokens: null,
          total_output_tokens: null,
          by_model: {},
        }),
      ),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Token usage')).toBeInTheDocument())
    // The Token usage card subtitle is "no calls in window".
    const subtitles = screen.getAllByText('no calls in window')
    // Both p95 latency (when null) and Token usage (when null) carry
    // the same subtitle string — assert at least one is present.
    expect(subtitles.length).toBeGreaterThanOrEqual(1)
  })

  it('renders the Models table in count-desc order', async () => {
    vi.stubGlobal(
      'fetch',
      mockFetchReturning(
        samplePayload({
          by_model: { 'gpt-4o': 7, 'gpt-4o-mini': 100, 'gpt-3.5-turbo': 22 },
        }),
      ),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Models')).toBeInTheDocument())
    const modelsHeading = screen.getByText('Models')
    const table = modelsHeading.parentElement.querySelector('table')
    const rowTexts = Array.from(table.querySelectorAll('tr')).map((r) => r.textContent)
    // Sorted count-desc: gpt-4o-mini (100), gpt-3.5-turbo (22), gpt-4o (7).
    expect(rowTexts[0]).toContain('gpt-4o-mini')
    expect(rowTexts[0]).toContain('100')
    expect(rowTexts[1]).toContain('gpt-3.5-turbo')
    expect(rowTexts[1]).toContain('22')
    expect(rowTexts[2]).toContain('gpt-4o')
    expect(rowTexts[2]).toContain('7')
  })

  it('hides the Models table when by_model is empty', async () => {
    vi.stubGlobal('fetch', mockFetchReturning(samplePayload({ by_model: {} })))
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Decisions')).toBeInTheDocument())
    expect(screen.queryByText('Models')).not.toBeInTheDocument()
  })

  it('gracefully handles a pre-SCRUM-578 payload missing the cost fields', async () => {
    // Defensive: if Slice 1 (SCRUM-578) hasn't deployed yet and the
    // API returns the old payload shape, the component should still
    // render cleanly. Token usage card falls back to "—"; Models
    // table is hidden.
    vi.stubGlobal(
      'fetch',
      mockFetchReturning({
        days_window: 7,
        since: '2026-05-18T12:00:00Z',
        total_calls: 100,
        by_decision: { allowed: 100 },
        by_site: { qa_ask: 100 },
        top_refusal_codes: [],
        p95_latency_ms: 500,
        dropped_telemetry_rows: 0,
        // total_input_tokens / total_output_tokens / by_model absent
      }),
    )
    render(
      <AdminGuardrailStats
        apiBaseUrl=""
        guardrailStatsExpanded={true}
        onGuardrailStatsExpandedChange={() => {}}
      />,
    )
    await waitFor(() => expect(screen.getByText('Token usage')).toBeInTheDocument())
    // Doesn't crash; renders the placeholder.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(1)
    expect(screen.queryByText('Models')).not.toBeInTheDocument()
  })

  it('works in uncontrolled mode (local expand state, no callback)', async () => {
    const fetchMock = mockFetchReturning(samplePayload())
    vi.stubGlobal('fetch', fetchMock)
    render(<AdminGuardrailStats apiBaseUrl="" />)
    // Collapsed by default; clicking the header expands.
    expect(fetchMock).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: /Guardrail telemetry/i }))
    await waitFor(() => expect(fetchMock).toHaveBeenCalled())
  })
})
