import { useState, useEffect } from 'react'
import { getMaterialSlides } from '../api/materials'

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
 * Displays one slide at a time from a slides material (e.g. derived PPTX PNGs).
 * Fetches slide list from GET .../materials/{materialId}/slides and shows prev/next navigation.
 * When slides are not ready yet, shows "Processing slides…" and refetches when session_updated
 * arrives via WebSocket (slidesRefreshTrigger bumps). Also polls periodically while empty so
 * slide preview still appears if WebSocket misses (e.g. multi-instance API behind a load balancer).
 * @param {string} [artifactId] - Optional; when set, empty state shows "Open original file" link.
 * @param {number} [slidesRefreshTrigger] - Bump when session_updated (e.g. slides ready); triggers one refetch.
 */
export function SlideDeckViewer({ apiBaseUrl, sessionId, materialId, initialSlide, artifactId, slidesRefreshTrigger }) {
  const [slides, setSlides] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [currentIndex, setCurrentIndex] = useState(0)

  useEffect(() => {
    if (!sessionId || !materialId) {
      setSlides([])
      setLoading(false)
      setError(null)
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)
    setSlides([])

    const run = async () => {
      try {
        const data = await getMaterialSlides(apiBaseUrl, sessionId, materialId)
        if (cancelled) return
        const list = Array.isArray(data?.slides) ? data.slides : []
        if (list.length > 0) {
          setSlides(list)
          const safeInitial = Math.max(0, Math.min(list.length - 1, (initialSlide ?? 1) - 1))
          setCurrentIndex(safeInitial)
        }
        setLoading(false)
      } catch (err) {
        if (cancelled) return
        setError(err?.message || 'Unable to load slides.')
        setSlides([])
        setLoading(false)
      }
    }

    run()

    return () => {
      cancelled = true
    }
  }, [apiBaseUrl, sessionId, materialId, slidesRefreshTrigger])

  // Poll while slides are still empty (processing). WebSocket session_updated is best-effort only
  // when the hub is in-memory per process (e.g. Render with multiple instances).
  useEffect(() => {
    if (!sessionId || !materialId || loading || error) return
    if (slides.length > 0) return

    let cancelled = false
    let attempts = 0
    const maxAttempts = 90
    const intervalMs = 2000

    const tick = async () => {
      if (cancelled) return
      attempts += 1
      if (attempts > maxAttempts) return
      try {
        const data = await getMaterialSlides(apiBaseUrl, sessionId, materialId)
        if (cancelled) return
        const list = Array.isArray(data?.slides) ? data.slides : []
        if (list.length > 0) {
          setSlides(list)
          const safeInitial = Math.max(0, Math.min(list.length - 1, (initialSlide ?? 1) - 1))
          setCurrentIndex(safeInitial)
        }
      } catch {
        /* keep polling until maxAttempts */
      }
    }

    const id = setInterval(tick, intervalMs)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [apiBaseUrl, sessionId, materialId, slides.length, loading, error, slidesRefreshTrigger, initialSlide])

  useEffect(() => {
    if (slides.length === 0) return
    const nextIndex = Math.max(0, Math.min(slides.length - 1, (initialSlide ?? 1) - 1))
    setCurrentIndex(nextIndex)
  }, [slides.length, initialSlide])

  const handlePrev = () => setCurrentIndex((i) => Math.max(0, i - 1))
  const handleNext = () => setCurrentIndex((i) => Math.min(slides.length - 1, i + 1))

  if (loading) {
    return (
      <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ padding: '24px', color: '#666' }}>
        Loading slides…
      </div>
    )
  }
  if (error) {
    return (
      <div data-testid="slide-deck-viewer" className="slide-deck-viewer" style={{ padding: '24px', color: '#c62828' }}>
        Unable to load slides.
      </div>
    )
  }
  if (slides.length === 0) {
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
            style={{ fontSize: '14px', color: '#1976d2', fontWeight: 500 }}
          >
            Open original file in the meantime
          </a>
        )}
      </div>
    )
  }

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
