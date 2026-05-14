import { useState, useEffect, useRef, useCallback, lazy, Suspense } from 'react'
import { getMaterialSlides } from '../api/materials'

// SCRUM-444/446: lazy-load the PDF renderer + pdfjs-dist so legacy PNG decks
// (and users who never open a PPTX) don't pay the ~430 KB gzipped cost.
const SlideDeckViewerPDF = lazy(() => import('./SlideDeckViewerPDF'))

const SPINNER_STYLE_ID = 'tb-spinner-keyframes'
function ensureSpinnerStyle() {
  if (typeof document === 'undefined') return
  if (document.getElementById(SPINNER_STYLE_ID)) return
  const style = document.createElement('style')
  style.id = SPINNER_STYLE_ID
  style.textContent = '@keyframes tb-spin { from { transform: rotate(0deg); } to { transform: rotate(360deg); } }'
  document.head.appendChild(style)
}

/**
 * Displays one slide at a time from a slides material. SCRUM-444/446: the
 * response shape determines the renderer — legacy {slides:[]} → inline PNG
 * branch (back-compat), new {format:"pdf", pdf_url, slide_count} →
 * lazy-loaded PDF.js branch with selectable text. The dispatcher itself owns
 * the fetch + readiness polling so both renderers stay focused on rendering.
 * @param {string} [artifactId] - Optional; when set, empty state shows "Open original file" link.
 * @param {number} [slidesRefreshTrigger] - Bump when session_updated (e.g. slides ready); triggers one refetch.
 */
export function SlideDeckViewer({ apiBaseUrl, sessionId, materialId, initialSlide, artifactId, slidesRefreshTrigger }) {
  const [response, setResponse] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const refetchInFlightRef = useRef(null)

  // Single fetch helper used by both the initial load effect and the PDF
  // viewer's refetch-on-expiry callback. De-dupes concurrent calls.
  const loadOnce = async () => {
    if (refetchInFlightRef.current) return refetchInFlightRef.current
    const p = (async () => {
      try {
        return await getMaterialSlides(apiBaseUrl, sessionId, materialId)
      } finally {
        refetchInFlightRef.current = null
      }
    })()
    refetchInFlightRef.current = p
    return p
  }

  // Initial load: full reset + fetch when the deck identity changes.
  // SCRUM-448: deliberately excludes slidesRefreshTrigger so a sibling
  // material's session_updated does not unmount the loaded viewer (which would
  // reset SlideDeckViewerPDF's currentPage state back to slide 1). The next
  // effect below handles the legitimate "slides finished generating" bump
  // path with the same trigger, but only when no response has loaded yet.
  useEffect(() => {
    if (!sessionId || !materialId) {
      setResponse(null)
      setLoading(false)
      setError(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    setResponse(null)
    ;(async () => {
      try {
        const data = await loadOnce()
        if (cancelled) return
        setResponse(data)
        setLoading(false)
      } catch (err) {
        if (cancelled) return
        setError(err?.message || 'Unable to load slides.')
        setResponse(null)
        setLoading(false)
      }
    })()
    return () => { cancelled = true }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiBaseUrl, sessionId, materialId])

  // SCRUM-448: silent refetch on slidesRefreshTrigger ONLY when the response
  // isn't yet loaded. App.jsx bumps this trigger on every WebSocket
  // session_updated — including unrelated sibling uploads — so guarding on
  // response keeps the open viewer's page state intact while still catching
  // the original "slides finished generating" use case.
  useEffect(() => {
    if (!sessionId || !materialId) return
    const hasLoadedResponse =
      !!response &&
      ((response.format === 'pdf' && response.pdf_url) ||
        (Array.isArray(response.slides) && response.slides.length > 0))
    if (hasLoadedResponse) return
    let cancelled = false
    ;(async () => {
      try {
        const data = await loadOnce()
        if (cancelled) return
        if (data) setResponse(data)
      } catch {
        /* poll loop below will continue trying */
      }
    })()
    return () => { cancelled = true }
    // response is intentionally not a dep — we only react to the trigger;
    // reading the latest value via closure is fine because the effect
    // re-runs only when slidesRefreshTrigger changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slidesRefreshTrigger])

  const isPDFShape = response && response.format === 'pdf' && response.pdf_url
  const hasLegacySlides = response && Array.isArray(response.slides) && response.slides.length > 0
  const isEmpty = !loading && !error && !isPDFShape && !hasLegacySlides

  // Poll while the response is missing or has neither shape (legacy: empty
  // slides; new: 404 returning null). WebSocket session_updated is best-effort
  // only when the hub is in-memory per process (e.g. Render multi-instance).
  useEffect(() => {
    if (!sessionId || !materialId || loading || error) return
    if (!isEmpty) return

    let cancelled = false
    let attempts = 0
    const maxAttempts = 90
    const intervalMs = 2000

    const tick = async () => {
      if (cancelled) return
      attempts += 1
      if (attempts > maxAttempts) return
      try {
        const data = await loadOnce()
        if (cancelled) return
        if (data) setResponse(data)
      } catch {
        /* keep polling until maxAttempts */
      }
    }

    const id = setInterval(tick, intervalMs)
    return () => {
      cancelled = true
      clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [apiBaseUrl, sessionId, materialId, isEmpty, loading, error, slidesRefreshTrigger])

  // Refetch callback handed to the PDF viewer. PDF.js will call this when it
  // hits an expired-URL load/render failure; the next render uses the fresh
  // pdf_url from the new response.
  //
  // SCRUM-448: stable identity via a ref + useCallback so SlideDeckViewerPDF's
  // [pdfUrl, onRefetch] effect does NOT re-run on every parent re-render. A
  // fresh function each render was causing the viewer to re-load the document
  // (re-call getDocument) when slidesRefreshTrigger bumped, which compounded
  // the page-state reset bug this ticket addresses.
  const handlePDFRefetchRef = useRef(null)
  handlePDFRefetchRef.current = async () => {
    try {
      const data = await loadOnce()
      if (data && data.format === 'pdf' && data.pdf_url) {
        setResponse(data)
        return data.pdf_url
      }
    } catch {
      /* fall through to caller's error path */
    }
    return null
  }
  const handlePDFRefetch = useCallback(() => handlePDFRefetchRef.current(), [])

  if (loading) {
    return (
      <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ padding: '24px', color: '#666' }}>
        Loading slides…
      </div>
    )
  }
  if (error) {
    return (
      <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ padding: '24px', color: 'var(--color-danger-dark)' }}>
        Unable to load slides.
      </div>
    )
  }
  if (isPDFShape) {
    return (
      <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
        <Suspense fallback={<div style={{ padding: '24px', color: '#666' }}>Loading slides…</div>}>
          <SlideDeckViewerPDF
            pdfUrl={response.pdf_url}
            slideCount={response.slide_count}
            initialSlide={initialSlide}
            onRefetch={handlePDFRefetch}
          />
        </Suspense>
      </div>
    )
  }
  if (hasLegacySlides) {
    return (
      <SlideDeckViewerPNG slides={response.slides} initialSlide={initialSlide} />
    )
  }

  // Empty state — slides not generated yet (or generation failed silently).
  ensureSpinnerStyle()
  const originalFileUrl = (apiBaseUrl != null && artifactId && materialId)
    ? `${apiBaseUrl.replace(/\/$/, '')}/artifacts/${artifactId}/materials/${materialId}/file`
    : null
  return (
    <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ padding: '24px', color: '#666', display: 'flex', flexDirection: 'column', gap: '12px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
        <span
          aria-hidden
          style={{
            display: 'inline-block',
            width: '16px',
            height: '16px',
            border: '2px solid #e65100',
            borderTopColor: 'transparent',
            borderRadius: '50%',
            animation: 'tb-spin 0.8s linear infinite',
            flexShrink: 0,
          }}
        />
        <p style={{ margin: 0, fontWeight: 500 }}>Generating slide previews…</p>
      </div>
      <p style={{ margin: 0, fontSize: '13px', color: '#888' }}>
        This takes 20–60 seconds. Slide previews will appear here automatically when ready.
      </p>
      {originalFileUrl && (
        <a
          href={originalFileUrl}
          target="_blank"
          rel="noopener noreferrer"
          style={{ fontSize: '14px', color: 'var(--color-primary)', fontWeight: 500 }}
        >
          Open original file in the meantime
        </a>
      )}
    </div>
  )
}

// SlideDeckViewerPNG is the legacy PNG-per-slide branch, extracted from the
// original SlideDeckViewer body so the dispatcher above stays compact. Kept
// inline (not a separate file) because it is the back-compat path that runs
// for every existing PPTX upload and there is no bundle-cost benefit to
// lazy-loading it.
function SlideDeckViewerPNG({ slides, initialSlide }) {
  const [currentIndex, setCurrentIndex] = useState(() => {
    const safe = Math.max(0, Math.min(slides.length - 1, (initialSlide ?? 1) - 1))
    return safe
  })

  useEffect(() => {
    if (slides.length === 0) return
    const nextIndex = Math.max(0, Math.min(slides.length - 1, (initialSlide ?? 1) - 1))
    setCurrentIndex(nextIndex)
  }, [slides.length, initialSlide])

  const handlePrev = () => setCurrentIndex((i) => Math.max(0, i - 1))
  const handleNext = () => setCurrentIndex((i) => Math.min(slides.length - 1, i + 1))
  const current = slides[currentIndex]

  return (
    <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
      <div
        className="slide-deck-toolbar"
        style={{
          flexShrink: 0,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: '12px',
          padding: '8px 0 12px',
          borderBottom: '1px solid #e0e0e0',
          marginBottom: '12px'
        }}
      >
        <button
          type="button"
          onClick={handlePrev}
          disabled={currentIndex <= 0}
          style={{
            padding: '6px 14px',
            fontSize: '13px',
            cursor: currentIndex <= 0 ? 'not-allowed' : 'pointer',
            opacity: currentIndex <= 0 ? 0.6 : 1
          }}
        >
          Previous
        </button>
        <div style={{ fontSize: '13px', color: '#333' }}>
          Slide {currentIndex + 1} of {slides.length}
        </div>
        <button
          type="button"
          onClick={handleNext}
          disabled={currentIndex >= slides.length - 1}
          style={{
            padding: '6px 14px',
            fontSize: '13px',
            cursor: currentIndex >= slides.length - 1 ? 'not-allowed' : 'pointer',
            opacity: currentIndex >= slides.length - 1 ? 0.6 : 1
          }}
        >
          Next
        </button>
      </div>
      <div
        className="slide-deck-stage"
        style={{
          flex: 1,
          minHeight: 0,
          display: 'flex',
          justifyContent: 'center',
          alignItems: 'flex-start',
          overflow: 'auto'
        }}
      >
        {current ? (
          <img
            src={current.image_url}
            alt={`Slide ${current.index}`}
            style={{
              maxWidth: '100%',
              maxHeight: '100%',
              objectFit: 'contain',
              display: 'block'
            }}
          />
        ) : null}
      </div>
    </div>
  )
}
