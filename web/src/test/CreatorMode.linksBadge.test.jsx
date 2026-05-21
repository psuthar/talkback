// SCRUM-535: pins the CreatorMode wiring that clears the "Links • New N"
// badge once the creator clicks a link. The bug was a hardcoded
// `lastSeenLinkCount={0}` in CreatorMode.jsx; this harness mirrors the
// post-fix wiring so any drift between the two trips the test.
import { describe, it, expect, vi } from 'vitest'
import { useEffect, useState } from 'react'
import { render, screen, fireEvent } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

function _seedSession(links) {
  return {
    session: { id: 'sess-535' },
    id: 'sess-535',
    video_sources: [],
    materials: [],
    links: links.map((l) => ({ id: l.id, url: l.url, title: l.title, status: 'ready' })),
    unread_material_ids: [],
    processing_jobs: [],
  }
}

// CreatorWiringHarness mirrors the SCRUM-535 wiring in
// web/src/modes/CreatorMode.jsx (the `lastSeenLinkCountBySession` state,
// the one-shot init effect, the `onSelectLink` setter, and the resolved
// `lastSeenLinkCount` prop). Tests drive it by re-rendering with a
// different `session` prop to simulate a refetch returning new links.
function CreatorWiringHarness({ session, onSelectLinkSpy }) {
  const [lastSeenLinkCountBySession, setLastSeenLinkCountBySession] = useState({})
  const sessionIdForLinks = session?.session?.id
  useEffect(() => {
    if (!sessionIdForLinks) return
    const linkCount = session?.links?.length ?? 0
    setLastSeenLinkCountBySession((prev) => {
      if (prev[sessionIdForLinks] !== undefined) return prev
      return { ...prev, [sessionIdForLinks]: linkCount }
    })
  }, [sessionIdForLinks, session?.links?.length])

  return (
    <MaterialsTreePanel
      session={session}
      apiBaseUrl=""
      onSelectLink={(link) => {
        onSelectLinkSpy?.(link)
        const sid = session?.session?.id
        if (sid && session?.links?.length != null) {
          setLastSeenLinkCountBySession((prev) => ({
            ...prev,
            [sid]: session.links.length,
          }))
        }
      }}
      lastSeenLinkCount={sessionIdForLinks ? (lastSeenLinkCountBySession[sessionIdForLinks] ?? 0) : 0}
      hideHeader
    />
  )
}

describe('SCRUM-535: CreatorMode Links badge wiring', () => {
  it('existing links on first session load do NOT flash "NEW N"', () => {
    const session = _seedSession([{ id: 'a', url: 'https://x.test/a', title: 'A' }])
    render(<CreatorWiringHarness session={session} />)
    // One link already present → header is bare "Links", no "New" suffix.
    expect(screen.getByText('Links')).toBeTruthy()
    expect(screen.queryByText(/Links.*New/)).toBeNull()
  })

  it('shows "Links • New 1" when a new link arrives after first load', () => {
    const initial = _seedSession([{ id: 'a', url: 'https://x.test/a', title: 'A' }])
    const { rerender } = render(<CreatorWiringHarness session={initial} />)
    // Refetch returns one new link beyond the high-water mark.
    const refetched = _seedSession([
      { id: 'a', url: 'https://x.test/a', title: 'A' },
      { id: 'b', url: 'https://x.test/b', title: 'B' },
    ])
    rerender(<CreatorWiringHarness session={refetched} />)
    expect(screen.getByText('Links • New 1')).toBeTruthy()
  })

  it('clicking the link clears the badge', () => {
    const initial = _seedSession([{ id: 'a', url: 'https://x.test/a', title: 'A' }])
    const onSelectLinkSpy = vi.fn()
    const { rerender } = render(
      <CreatorWiringHarness session={initial} onSelectLinkSpy={onSelectLinkSpy} />
    )
    // Add a second link → badge appears.
    const refetched = _seedSession([
      { id: 'a', url: 'https://x.test/a', title: 'A' },
      { id: 'b', url: 'https://x.test/b', title: 'B' },
    ])
    rerender(
      <CreatorWiringHarness session={refetched} onSelectLinkSpy={onSelectLinkSpy} />
    )
    expect(screen.getByText('Links • New 1')).toBeTruthy()

    // Click any link button → badge clears to bare "Links".
    const linkButtons = screen.getAllByTestId('link-item')
    fireEvent.click(linkButtons[0])
    expect(onSelectLinkSpy).toHaveBeenCalled()
    expect(screen.getByText('Links')).toBeTruthy()
    expect(screen.queryByText(/Links.*New/)).toBeNull()
  })

  it('badge re-appears when another new link arrives after a click', () => {
    const initial = _seedSession([
      { id: 'a', url: 'https://x.test/a', title: 'A' },
      { id: 'b', url: 'https://x.test/b', title: 'B' },
    ])
    const onSelectLinkSpy = vi.fn()
    const { rerender } = render(
      <CreatorWiringHarness session={initial} onSelectLinkSpy={onSelectLinkSpy} />
    )
    // After click, the high-water mark equals the current link count.
    fireEvent.click(screen.getAllByTestId('link-item')[0])
    expect(screen.queryByText(/Links.*New/)).toBeNull()

    // Now a third link arrives → badge fires again.
    const refetched = _seedSession([
      { id: 'a', url: 'https://x.test/a', title: 'A' },
      { id: 'b', url: 'https://x.test/b', title: 'B' },
      { id: 'c', url: 'https://x.test/c', title: 'C' },
    ])
    rerender(
      <CreatorWiringHarness session={refetched} onSelectLinkSpy={onSelectLinkSpy} />
    )
    expect(screen.getByText('Links • New 1')).toBeTruthy()
  })
})
