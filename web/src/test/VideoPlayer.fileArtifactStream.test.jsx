// SCRUM-472: when a non-primary, non-Zoom recording (e.g. Teams or Meet) is
// selected, the VideoPlayer must route playback through the per-recording
// /video-sources/:id/stream endpoint instead of falling through to the
// generic embed iframe (which fails to render because Graph/Drive playback
// URLs are auth-walled / X-Frame-Options-protected).
//
// Before SCRUM-472, streamUrlForUpload was gated on `source_type === 'upload'`
// only, so Teams/Meet recordings (source_type='embed_url' with a Ready
// file_artifact) skipped the direct-stream path. The fix relaxes that gate
// to also fire when video.file_artifact_id is present.
import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { VideoPlayer } from '../VideoPlayer'

describe('VideoPlayer file_artifact stream URL (SCRUM-472)', () => {
  it('renders Html5VideoPlayer with the per-recording stream URL when a non-primary Teams recording has a file_artifact_id', () => {
    const video = {
      id: 'vs-teams-1',
      provider: 'teams',
      playback_mode: 'embed',
      source_type: 'embed_url',
      video_url: 'https://graph.microsoft.com/v1.0/teams/.../playbackUrl',
      file_artifact_id: 'fa-teams-1',
    }
    render(
      <VideoPlayer
        video={video}
        sessionId="s1"
        apiBaseUrl="http://api.test/api"
      />,
    )
    const el = screen.getByTestId('html5-video-player')
    expect(el).toBeTruthy()
    expect(el.getAttribute('src')).toBe('http://api.test/sessions/s1/video-sources/vs-teams-1/stream')
  })

  it('renders Html5VideoPlayer with the per-recording stream URL when a non-primary Google Meet recording has a file_artifact_id', () => {
    const video = {
      id: 'vs-meet-1',
      provider: 'google_meet',
      playback_mode: 'embed',
      source_type: 'embed_url',
      video_url: 'https://drive.google.com/file/d/abc/view',
      file_artifact_id: 'fa-meet-1',
    }
    render(
      <VideoPlayer
        video={video}
        sessionId="s1"
        apiBaseUrl="http://api.test/api"
      />,
    )
    const el = screen.getByTestId('html5-video-player')
    expect(el).toBeTruthy()
    expect(el.getAttribute('src')).toBe('http://api.test/sessions/s1/video-sources/vs-meet-1/stream')
  })

  it('still routes to Html5VideoPlayer for the existing source_type=upload path (regression guard)', () => {
    const video = {
      id: 'vs-upload-1',
      provider: 'other',
      playback_mode: 'embed',
      source_type: 'upload',
      video_url: '',
      file_artifact_id: null,
    }
    render(
      <VideoPlayer
        video={video}
        sessionId="s1"
        apiBaseUrl="http://api.test/api"
      />,
    )
    const el = screen.getByTestId('html5-video-player')
    expect(el.getAttribute('src')).toBe('http://api.test/sessions/s1/video-sources/vs-upload-1/stream')
  })

  it('falls through to EmbedPlayer when neither source_type=upload nor file_artifact_id is present (no regression for legacy embed-only sources)', () => {
    const video = {
      id: 'vs-legacy-1',
      provider: 'other',
      playback_mode: 'embed',
      source_type: 'embed_url',
      video_url: 'https://example.com/external/embed',
      file_artifact_id: null,
    }
    render(
      <VideoPlayer
        video={video}
        sessionId="s1"
        apiBaseUrl="http://api.test/api"
      />,
    )
    // No Html5VideoPlayer — no media URL was resolved.
    expect(screen.queryByTestId('html5-video-player')).toBeNull()
    // EmbedPlayer renders an iframe with the embed URL.
    const iframe = document.querySelector('iframe[src="https://example.com/external/embed"]')
    expect(iframe).not.toBeNull()
  })

  it('does not build a stream URL for the synthetic primary placeholder (id === "primary")', () => {
    // CreatorMode synthesizes a video with id='primary' for the session primary;
    // it uses primaryVideoAccessUrl for playback rather than the per-recording
    // stream endpoint. The fix must not build /video-sources/primary/stream
    // (which would 400 at the backend because 'primary' is not a UUID).
    const video = {
      id: 'primary',
      provider: 'r2',
      playback_mode: 'direct',
      source_type: 'upload',
      media_url: '',
      file_artifact_id: null,
    }
    render(
      <VideoPlayer
        video={video}
        sessionId="s1"
        apiBaseUrl="http://api.test/api"
        primaryVideoAccessUrl="http://api.test/api/sessions/s1/primary-video"
        primaryVideoArtifactId="fa-primary"
      />,
    )
    const el = screen.getByTestId('html5-video-player')
    // Uses primaryVideoAccessUrl, not /video-sources/primary/stream.
    expect(el.getAttribute('src')).toBe('http://api.test/api/sessions/s1/primary-video')
  })
})
