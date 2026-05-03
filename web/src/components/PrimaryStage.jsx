import React from 'react'
import { VideoPlayer } from '../VideoPlayer'
import { DocumentViewer } from './DocumentViewer'

/**
 * PrimaryStage — center-column primary content renderer (SCRUM-273, extended
 * for all three kinds in SCRUM-284).
 *
 * Render priority (the first match wins):
 *   1. `selectedDocument` — explicit selection from the parent (set by the
 *      auto-select helper or a manual click on the materials tree). Rendered
 *      via DocumentViewer regardless of whether the underlying entity is a
 *      material or a link, since DocumentViewer handles both.
 *   2. `currentSession.primary.kind === 'document'` — fall back to the
 *      session's resolved primary descriptor and look the material up in
 *      currentSession.materials. This guards against the case where the
 *      parent cleared selection but the session still has a primary set.
 *   3. `currentSession.primary.kind === 'link'` — same idea, looking up
 *      currentSession.links. The synthesised doc shape matches what
 *      ParticipantMode's handleSelectLink produces (type='link', url, title,
 *      id) so DocumentViewer renders the iframe path.
 *   4. Otherwise — the legacy video-stage: render the VideoPlayer with
 *      ingest-pending / failed copy when the primary artifact isn't ready.
 *
 * The ingest-pending / failed copy and Retry-ingest button are creator-level
 * concerns (retryProcessing, processingRetrying); pass them through to
 * preserve existing behavior. ParticipantMode still owns its own
 * transcript pane + VideoStartOverlay around the video case, so it does
 * not consume PrimaryStage end-to-end — but it CAN reuse the doc/link
 * fallback above by passing currentSession through.
 */
export function PrimaryStage({
	selectedDocument,
	apiBaseUrl,
	sessionId,
	sessionUpdatedVersion,
	currentSession,
	primaryVideoAccessUrl,
	hasPrimaryR2Video,
	video,
	transcriptJobs,
	handleVideoPlayerEvent,
	handleVideoTimeUpdate,
	currentVideoTime,
	isVideoPlaying,
	creatorIdentity,
	handlePrimaryVideoMounted,
	retryProcessing,
	processingRetrying,
}) {
	const renderDoc = (doc) => (
		<div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: '12px' }}>
			<div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
				<DocumentViewer
					doc={doc}
					apiBaseUrl={apiBaseUrl}
					sessionId={sessionId}
					slidesRefreshTrigger={sessionUpdatedVersion}
				/>
			</div>
		</div>
	)

	if (selectedDocument) {
		return renderDoc(selectedDocument)
	}

	// SCRUM-284: fall back to the session's resolved primary descriptor when
	// the parent didn't pre-select. Document kind looks up the material;
	// link kind synthesizes the doc shape DocumentViewer expects for an
	// iframe render. Returns null silently when the descriptor points at a
	// row that's been removed (resolver collapse) so the parent's empty-state
	// UX still wins.
	const primaryKind = currentSession?.primary?.kind
	const primaryId = currentSession?.primary?.id
	if (primaryKind === 'document' && primaryId) {
		const materials = Array.isArray(currentSession?.materials) ? currentSession.materials : []
		const material = materials.find((m) => m && String(m.id) === String(primaryId))
		if (material) {
			return renderDoc(material)
		}
	}
	if (primaryKind === 'link' && primaryId) {
		const links = Array.isArray(currentSession?.links)
			? currentSession.links
			: (Array.isArray(currentSession?.session?.links) ? currentSession.session.links : [])
		const link = links.find((l) => l && String(l.id) === String(primaryId))
		if (link?.url) {
			return renderDoc({
				type: 'link',
				id: `link-${link.id}`,
				url: link.url,
				title: link.title || link.url,
			})
		}
	}

	const hasVideoSources = currentSession?.video_sources && currentSession.video_sources.length > 0
	const hasPrimaryArtifact = !!currentSession?.session?.primary_video_artifact_id
	if (!hasVideoSources && !hasPrimaryArtifact) {
		return null
	}

	const primaryArtifactNotReady = hasPrimaryArtifact && !primaryVideoAccessUrl && !!currentSession?.playback_reason_code

	return (
		<div data-testid="video-player-container" className="creator-video-container">
			{primaryArtifactNotReady && (
				<div style={{
					padding: '24px',
					backgroundColor: currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? '#fff8e1' : currentSession.playback_reason_code === 'VIDEO_INGEST_FAILED' ? '#ffebee' : '#f5f5f5',
					textAlign: 'center',
					border: '1px solid #e0e0e0'
				}}>
					<p style={{ margin: '0 0 12px', color: '#333', fontSize: '15px' }}>
						{currentSession.playback_message || (currentSession.playback_reason_code === 'VIDEO_INGEST_PENDING' ? 'Video is still being prepared. Refresh in a moment.' : currentSession.playback_reason_code === 'VIDEO_INGEST_FAILED' ? 'Video ingest failed. Creator can retry import.' : 'Video not available for this session.')}
					</p>
					{currentSession.playback_reason_code === 'VIDEO_INGEST_FAILED' && retryProcessing && (
						<button
							type="button"
							onClick={retryProcessing}
							disabled={processingRetrying}
							style={{
								padding: '8px 16px',
								fontSize: '14px',
								backgroundColor: 'var(--color-primary-mid)',
								color: 'white',
								border: 'none',
								borderRadius: '4px',
								cursor: processingRetrying ? 'not-allowed' : 'pointer',
								margin: 0
							}}
						>
							{processingRetrying ? 'Retrying…' : 'Retry ingest'}
						</button>
					)}
				</div>
			)}
			{hasPrimaryR2Video && (
				<div style={{ padding: '6px 12px', fontSize: '13px', color: '#ccc', backgroundColor: '#1a1a1a' }}>
					<strong style={{ color: '#999' }}>Transcript:</strong>{' '}
					<span style={{
						color: video.transcript_status === 'ready' ? 'var(--color-success-mid)' :
							   video.transcript_status === 'pending' ? '#ff9800' :
							   video.transcript_status === 'failed' ? 'var(--color-danger)' : '#999',
						fontWeight: 'bold'
					}}>
						{video.transcript_status === 'missing' ? 'No transcript' :
						 video.transcript_status === 'pending' ? 'Pending...' :
						 video.transcript_status === 'processing' ? 'Processing...' :
						 video.transcript_status === 'ready' ? 'Ready' :
						 video.transcript_status === 'failed' ? 'Failed' :
						 video.transcript_status || 'Unknown'}
					</span>
					{transcriptJobs && transcriptJobs[video?.id] && (
						<>
							{' | '}
							<strong style={{ color: '#999' }}>Job:</strong>{' '}
							<span style={{
								color: transcriptJobs[video.id].status === 'completed' ? 'var(--color-success-mid)' :
									   transcriptJobs[video.id].status === 'failed' ? 'var(--color-danger)' : '#ff9800',
								fontWeight: 'bold'
							}}>
								{transcriptJobs[video.id].status}
							</span>
						</>
					)}
				</div>
			)}
			{video && !primaryArtifactNotReady && (
				<VideoPlayer
					video={video}
					onEvent={handleVideoPlayerEvent}
					onTimeUpdate={handleVideoTimeUpdate}
					currentTime={currentVideoTime}
					playing={isVideoPlaying}
					sessionId={currentSession?.session?.id || currentSession?.id}
					apiBaseUrl={apiBaseUrl}
					creatorIdentity={creatorIdentity}
					primaryVideoAccessUrl={primaryVideoAccessUrl}
					primaryVideoArtifactId={currentSession?.session?.primary_video_artifact_id ?? null}
					onPrimaryVideoMounted={handlePrimaryVideoMounted}
				/>
			)}
		</div>
	)
}

export default PrimaryStage
