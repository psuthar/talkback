import { useState, useRef, useEffect } from 'react'

// Player Adapter Interface
export const PlayerEvent = {
  READY: 'ready',
  PLAY: 'play',
  PAUSE: 'pause',
  SEEK: 'seek',
  TIMEUPDATE: 'timeupdate'
}

// HTML5 Video Player Adapter
export function Html5VideoPlayer({ 
  mediaUrl, 
  posterUrl, 
  onEvent, 
  onTimeUpdate,
  currentTime: externalCurrentTime,
  playing: externalPlaying,
  /** Optional: shown when video fails to load (e.g. "Open in Zoom") */
  openUrl = null,
  /** Optional: label for openUrl link */
  openUrlLabel = 'Open in new tab'
}) {
  const videoRef = useRef(null)
  const [currentTime, setCurrentTime] = useState(0)
  const [duration, setDuration] = useState(0)
  const [playing, setPlaying] = useState(false)
  const [loadError, setLoadError] = useState(null)
  const [serverStatus, setServerStatus] = useState(null) // after error, fetch stream URL to show real HTTP status

  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    const handleLoadedMetadata = () => {
      setDuration(video.duration)
      onEvent?.({ type: PlayerEvent.READY })
    }

    const handlePlay = () => {
      setPlaying(true)
      const time = Math.floor(video.currentTime)
      setCurrentTime(time)
      onEvent?.({ type: PlayerEvent.PLAY, time })
    }

    const handlePause = () => {
      setPlaying(false)
      const time = Math.floor(video.currentTime)
      setCurrentTime(time)
      onEvent?.({ type: PlayerEvent.PAUSE, time })
    }

    const handleSeeked = () => {
      const time = Math.floor(video.currentTime)
      setCurrentTime(time)
      onEvent?.({ type: PlayerEvent.SEEK, time })
    }

    const handleTimeUpdate = () => {
      const time = Math.floor(video.currentTime)
      setCurrentTime(time)
      onTimeUpdate?.(time)
      onEvent?.({ type: PlayerEvent.TIMEUPDATE, time })
    }

    const handleError = () => {
      const err = video.error
      const message = err?.message || (err?.code === 4 ? 'Video not found or access denied.' : 'Video failed to load.')
      setLoadError(message)
    }

    video.addEventListener('loadedmetadata', handleLoadedMetadata)
    video.addEventListener('play', handlePlay)
    video.addEventListener('pause', handlePause)
    video.addEventListener('seeked', handleSeeked)
    video.addEventListener('timeupdate', handleTimeUpdate)
    video.addEventListener('error', handleError)

    return () => {
      video.removeEventListener('loadedmetadata', handleLoadedMetadata)
      video.removeEventListener('play', handlePlay)
      video.removeEventListener('pause', handlePause)
      video.removeEventListener('seeked', handleSeeked)
      video.removeEventListener('timeupdate', handleTimeUpdate)
      video.removeEventListener('error', handleError)
    }
  }, [onEvent, onTimeUpdate])

  // Handle external control
  useEffect(() => {
    const video = videoRef.current
    if (!video) return

    if (externalPlaying !== undefined) {
      if (externalPlaying && video.paused) {
        video.play()
      } else if (!externalPlaying && !video.paused) {
        video.pause()
      }
    }
  }, [externalPlaying])

  useEffect(() => {
    const video = videoRef.current
    if (!video || externalCurrentTime === undefined) return

    if (Math.abs(video.currentTime - externalCurrentTime) > 1) {
      video.currentTime = externalCurrentTime
    }
  }, [externalCurrentTime])

  const getCurrentTime = () => {
    return videoRef.current ? Math.floor(videoRef.current.currentTime) : 0
  }

  const play = () => {
    videoRef.current?.play()
  }

  const pause = () => {
    videoRef.current?.pause()
  }

  const restart = () => {
    if (videoRef.current) {
      videoRef.current.currentTime = 0
      videoRef.current.play()
    }
  }

  const formatTime = (seconds) => {
    const mins = Math.floor(seconds / 60)
    const secs = Math.floor(seconds % 60)
    return `${mins}:${secs.toString().padStart(2, '0')}`
  }

  // Reset error when mediaUrl changes (e.g. user switched video)
  useEffect(() => {
    setLoadError(null)
    setServerStatus(null)
  }, [mediaUrl])

  // When video fails, probe stream URL to show real HTTP status (helps debug 403/CORS on Render)
  useEffect(() => {
    if (!loadError || !mediaUrl) return
    let cancelled = false
    fetch(mediaUrl, { method: 'GET', credentials: 'omit' })
      .then((res) => {
        if (cancelled) return
        if (res.status === 0) {
          setServerStatus('Response blocked (check CORS for stream URL)')
        } else {
          setServerStatus(`Server returned ${res.status} ${res.statusText}`)
        }
      })
      .catch(() => {
        if (!cancelled) setServerStatus('Could not reach stream URL')
      })
    return () => { cancelled = true }
  }, [loadError, mediaUrl])

  if (loadError) {
    return (
      <div style={{
        padding: '24px',
        backgroundColor: '#fff5f5',
        borderRadius: '8px',
        border: '2px solid #feb2b2',
        textAlign: 'center'
      }}>
        <p style={{ margin: '0 0 12px', color: '#c53030', fontSize: '15px' }}>
          Video could not be loaded: {loadError}
        </p>
        {serverStatus && (
          <p style={{ margin: '0 0 8px', color: '#c53030', fontSize: '14px', fontWeight: '600' }}>
            {serverStatus}
          </p>
        )}
        <p style={{ margin: '0 0 16px', color: '#718096', fontSize: '13px' }}>
          If this is a Zoom recording, the creator may need to reconnect Zoom, or the recording may have expired.
        </p>
        {openUrl && (
          <a
            href={openUrl}
            target="_blank"
            rel="noopener noreferrer"
            style={{
              display: 'inline-block',
              padding: '10px 20px',
              backgroundColor: '#2D8CFF',
              color: '#fff',
              borderRadius: '6px',
              textDecoration: 'none',
              fontWeight: '600',
              fontSize: '14px'
            }}
          >
            {openUrlLabel}
          </a>
        )}
      </div>
    )
  }

  return (
    <div style={{ position: 'relative', width: '100%' }}>
      <video
        ref={videoRef}
        src={mediaUrl}
        poster={posterUrl}
        controls
        style={{
          width: '100%',
          maxHeight: '600px',
          borderRadius: '8px',
          backgroundColor: '#000'
        }}
      />
      <div style={{ 
        marginTop: '10px', 
        fontSize: '12px', 
        color: '#666',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center'
      }}>
        <span>
          {formatTime(currentTime)} / {duration ? formatTime(duration) : '--:--'}
        </span>
        <span style={{ 
          color: playing ? '#4CAF50' : '#999',
          fontWeight: 'bold'
        }}>
          {playing ? '▶ Playing' : '⏸ Paused'}
        </span>
      </div>
    </div>
  )
}

// Embed Player Adapter (for Loom/Zoom iframes)
export function EmbedPlayer({ 
  embedUrl, 
  onEvent,
  onManualTimeUpdate 
}) {
  const [manualTime, setManualTime] = useState(0)
  const iframeRef = useRef(null)
  const [key, setKey] = useState(0)

  useEffect(() => {
    // Emit ready event when iframe loads
    const timer = setTimeout(() => {
      onEvent?.({ type: PlayerEvent.READY })
    }, 1000)
    return () => clearTimeout(timer)
  }, [embedUrl, onEvent])

  const getCurrentTime = () => {
    return manualTime
  }

  const restart = () => {
    // Force iframe reload
    setKey(prev => prev + 1)
    setManualTime(0)
  }

  const handleManualTimeChange = (e) => {
    const time = parseInt(e.target.value) || 0
    setManualTime(time)
    onManualTimeUpdate?.(time)
  }

  return (
    <div>
      <div style={{ 
        position: 'relative', 
        paddingBottom: '56.25%', // 16:9 aspect ratio
        height: 0,
        overflow: 'hidden',
        borderRadius: '8px',
        border: '2px solid #ddd',
        backgroundColor: '#000'
      }}>
        <iframe
          ref={iframeRef}
          key={key}
          src={embedUrl}
          style={{
            position: 'absolute',
            top: 0,
            left: 0,
            width: '100%',
            height: '100%',
            border: 'none'
          }}
          allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
          allowFullScreen
          title="Video player"
        />
      </div>
      <div style={{ 
        marginTop: '15px', 
        padding: '10px', 
        backgroundColor: '#fff3cd', 
        borderRadius: '4px',
        fontSize: '13px',
        color: '#856404'
      }}>
        <strong>⚠ Embed Mode (Limited Control):</strong> Playback controls are managed by the embedded player.
        <div style={{ marginTop: '10px' }}>
          <label style={{ display: 'block', marginBottom: '5px', fontWeight: 'bold' }}>
            Manually enter timestamp (seconds):
          </label>
          <input
            type="number"
            value={manualTime}
            onChange={handleManualTimeChange}
            min="0"
            placeholder="0"
            style={{
              padding: '5px 10px',
              fontSize: '14px',
              width: '150px',
              borderRadius: '4px',
              border: '1px solid #ddd'
            }}
          />
          <span style={{ marginLeft: '10px', color: '#666' }}>
            Current: {Math.floor(manualTime / 60)}:{(manualTime % 60).toString().padStart(2, '0')}
          </span>
        </div>
      </div>
    </div>
  )
}

// Helper function to convert Loom share URL to embed URL
function getLoomEmbedUrl(shareUrl) {
  if (!shareUrl || typeof shareUrl !== 'string') return null
  
  // If it's already an embed URL, return as-is
  if (shareUrl.includes('loom.com/embed/')) {
    return shareUrl
  }
  
  // Extract video ID from Loom share URL
  // Format: https://www.loom.com/share/{video-id} or http://www.loom.com/share/{video-id}
  // Video IDs can contain alphanumeric characters
  const match = shareUrl.match(/loom\.com\/share\/([a-zA-Z0-9]+)/)
  if (match && match[1]) {
    return `https://www.loom.com/embed/${match[1]}`
  }
  
  // If no match, return null instead of the original URL to avoid X-Frame-Options errors
  console.warn('Failed to convert Loom share URL to embed URL:', shareUrl)
  return null
}

// Main Video Player Component. Pass sessionId + apiBaseUrl for Zoom so video plays in-app via backend proxy.
export function VideoPlayer({ 
  video, 
  onEvent,
  onTimeUpdate,
  currentTime: externalCurrentTime,
  playing: externalPlaying,
  sessionId = null,
  apiBaseUrl = '',
  creatorIdentity = ''
}) {
  if (!video) return null

  const playbackMode = video.playback_mode || 'embed'
  
  // For embed mode, convert Loom share URLs to embed URLs
  let embedUrl = video.embed_url || (video.video_url && playbackMode === 'embed' ? video.video_url : null)
  
  // Always convert Loom share URLs to embed URLs (even if embed_url is already set)
  if (embedUrl && video.provider === 'loom' && playbackMode === 'embed') {
    const convertedUrl = getLoomEmbedUrl(embedUrl)
    // If conversion fails, set to null to prevent invalid URLs from being used
    embedUrl = convertedUrl || null
  }
  
  // Zoom in session: use backend proxy so video plays in-app (users stay in TalkBack)
  // Pass creator_identity for legacy sessions where session.CreatedBy is nil
  const streamBase = (video.provider === 'zoom' && sessionId && apiBaseUrl && video.id)
    ? `${apiBaseUrl.replace(/\/$/, '')}/sessions/${sessionId}/video-sources/${video.id}/stream`
    : null
  const zoomStreamUrl = streamBase
    ? (creatorIdentity ? `${streamBase}?creator_identity=${encodeURIComponent(creatorIdentity)}` : streamBase)
    : null
  const mediaUrl = video.media_url ||
    zoomStreamUrl ||
    (video.video_url && playbackMode === 'direct' ? video.video_url : null)

  const openUrl = video.video_url || embedUrl

  if ((playbackMode === 'direct' || zoomStreamUrl) && mediaUrl) {
    return (
      <Html5VideoPlayer
        mediaUrl={mediaUrl}
        posterUrl={video.poster_url}
        onEvent={onEvent}
        onTimeUpdate={onTimeUpdate}
        currentTime={externalCurrentTime}
        playing={externalPlaying}
        openUrl={video.provider === 'zoom' ? openUrl : (openUrl || null)}
        openUrlLabel={video.provider === 'zoom' ? 'Open in Zoom' : 'Open in new tab'}
      />
    )
  } else if (video.provider === 'zoom' && !zoomStreamUrl && openUrl) {
    return (
      <div style={{
        padding: '24px',
        backgroundColor: '#f5f5f5',
        borderRadius: '8px',
        textAlign: 'center',
        border: '2px solid #ddd'
      }}>
        <p style={{ margin: '0 0 16px', color: '#666', fontSize: '15px' }}>
          Zoom recordings cannot be embedded in this page. Open the recording in Zoom to watch.
        </p>
        <a
          href={openUrl}
          target="_blank"
          rel="noopener noreferrer"
          style={{
            display: 'inline-block',
            padding: '12px 24px',
            backgroundColor: '#2D8CFF',
            color: '#fff',
            borderRadius: '6px',
            textDecoration: 'none',
            fontWeight: '600',
            fontSize: '14px'
          }}
        >
          Open in Zoom →
        </a>
        <div style={{
          marginTop: '15px',
          padding: '10px',
          backgroundColor: '#fff3cd',
          borderRadius: '4px',
          fontSize: '13px',
          color: '#856404',
          textAlign: 'left'
        }}>
          <strong>⚠ Embed Mode (Limited Control):</strong> Use the manual timestamp below to sync with the transcript.
          <div style={{ marginTop: '10px' }}>
            <label style={{ display: 'block', marginBottom: '5px', fontWeight: 'bold' }}>
              Manually enter timestamp (seconds):
            </label>
            <input
              type="number"
              value={externalCurrentTime ?? 0}
              onChange={(e) => onTimeUpdate?.(parseInt(e.target.value) || 0)}
              min="0"
              placeholder="0"
              style={{
                padding: '5px 10px',
                fontSize: '14px',
                width: '150px',
                borderRadius: '4px',
                border: '1px solid #ddd'
              }}
            />
            <span style={{ marginLeft: '10px', color: '#666' }}>
              Current: {Math.floor((externalCurrentTime ?? 0) / 60)}:{((externalCurrentTime ?? 0) % 60).toString().padStart(2, '0')}
            </span>
          </div>
        </div>
      </div>
    )
  } else if (playbackMode === 'embed' && embedUrl) {
    return (
      <EmbedPlayer
        embedUrl={embedUrl}
        onEvent={onEvent}
        onManualTimeUpdate={onTimeUpdate}
      />
    )
  } else {
    return (
      <div style={{ 
        padding: '20px', 
        backgroundColor: '#f5f5f5', 
        borderRadius: '8px',
        textAlign: 'center',
        color: '#666'
      }}>
        <p>Video player not available. Missing embed_url or media_url.</p>
        {video.video_url && (
          <a 
            href={video.video_url} 
            target="_blank" 
            rel="noopener noreferrer"
            style={{ color: '#2196F3', textDecoration: 'underline' }}
          >
            Open video in new tab
          </a>
        )}
      </div>
    )
  }
}
