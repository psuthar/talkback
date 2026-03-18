import { useState, useEffect } from 'react'
import { getMaterialSlides } from '../api/materials'

/**
 * Displays one slide at a time from a slides material (e.g. derived PPTX PNGs).
 * Fetches slide list from GET .../materials/{materialId}/slides and shows prev/next navigation.
 * @param {string} [artifactId] - Optional; when set, empty state shows "Open original file" link.
 */
export function SlideDeckViewer({ apiBaseUrl, sessionId, materialId, initialSlide, artifactId }) {
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
    getMaterialSlides(apiBaseUrl, sessionId, materialId)
      .then((data) => {
        if (cancelled) return
        const list = Array.isArray(data?.slides) ? data.slides : []
        setSlides(list)
        setLoading(false)
        if (list.length > 0) {
          const safeInitial = Math.max(0, Math.min(list.length - 1, (initialSlide ?? 1) - 1))
          setCurrentIndex(safeInitial)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err?.message || 'Unable to load slides.')
          setSlides([])
          setLoading(false)
        }
      })
    return () => { cancelled = true }
  }, [apiBaseUrl, sessionId, materialId])

  useEffect(() => {
    if (slides.length === 0) return
    const nextIndex = Math.max(0, Math.min(slides.length - 1, (initialSlide ?? 1) - 1))
    setCurrentIndex(nextIndex)
  }, [slides.length, initialSlide])

  const handlePrev = () => setCurrentIndex((i) => Math.max(0, i - 1))
  const handleNext = () => setCurrentIndex((i) => Math.min(slides.length - 1, i + 1))

  if (loading) {
    return (
      <div className="slide-deck-viewer" style={{ padding: '24px', color: '#666' }}>
        Loading slides…
      </div>
    )
  }
  if (error) {
    return (
      <div className="slide-deck-viewer" style={{ padding: '24px', color: '#c62828' }}>
        Unable to load slides.
      </div>
    )
  }
  if (slides.length === 0) {
    const originalFileUrl = (apiBaseUrl != null && artifactId && materialId)
      ? `${apiBaseUrl.replace(/\/$/, '')}/artifacts/${artifactId}/materials/${materialId}/file`
      : null
    return (
      <div className="slide-deck-viewer" style={{ padding: '24px', color: '#666', display: 'flex', flexDirection: 'column', gap: '12px' }}>
        <p style={{ margin: 0 }}>No slide preview is available for this deck yet.</p>
        <p style={{ margin: 0, fontSize: '13px', color: '#888' }}>
          The original file may still be available even if preview generation failed.
        </p>
        {originalFileUrl && (
          <a
            href={originalFileUrl}
            target="_blank"
            rel="noopener noreferrer"
            style={{ fontSize: '14px', color: '#1976d2', fontWeight: 500 }}
          >
            Open original file
          </a>
        )}
      </div>
    )
  }

  const current = slides[currentIndex]
  return (
    <div className="slide-deck-viewer" style={{ display: 'flex', flexDirection: 'column', height: '100%', minHeight: 0 }}>
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
