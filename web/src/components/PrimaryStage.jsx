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
	// SCRUM-328: when the user has explicitly clicked a video row, suppress
	// the "fall back to currentSession.primary as a document/link" branch
	// below — otherwise the user's video choice is silently overridden by
	// the session's primary descriptor and the player never renders.
	userSelectedVideo = false,
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
	// SCRUM-288: switches the empty-state copy when nothing resolves. CreatorMode
	// is the only consumer today; add 'participant' if/when ParticipantMode adopts
	// this component.
	mode = 'creator',
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
	//
	// SCRUM-328: skip this fallback entirely when the user has explicitly
	// clicked a video row in the sidebar. Without the guard, a session with
	// primary.kind=document re-renders the document here as soon as the
	// parent clears selectedDocument (handleBackToVideo / handleSelectVideo),
	// so the video player never appears.
	const primaryKind = currentSession?.primary?.kind
	const primaryId = currentSession?.primary?.id
	if (!userSelectedVideo && primaryKind === 'document' && primaryId) {
		const materials = Array.isArray(currentSession?.materials) ? currentSession.materials : []
		const material = materials.find((m) => m && String(m.id) === String(primaryId))
		if (material) {
			return renderDoc(material)
		}
	}
	if (!userSelectedVideo && primaryKind === 'link' && primaryId) {
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
		// SCRUM-288: render a useful empty-state instead of a blank pane when
		// the session has no resolvable primary and no fallback video. Copy
		// differs by mode so participants get a "waiting for the creator"
		// hint while creators see a direct call to action.
		//
		// SCRUM-471 NOTE: the user-facing "post-creation imports should not
		// auto-primary" rule lives entirely in the backend. The UI continues
		// to render the player from whichever video_source is selected; a
		// dedicated "no primary, pick one" chooser is deferred to a follow-up
		// so this story doesn't break the MP4-upload / SCRUM-327 / SCRUM-328
		// e2e flows that rely on video_sources-only rendering.
		const isCreator = mode !== 'participant'
		const heading = isCreator
			? 'No primary content yet'
			: 'Waiting for the creator to choose primary content'
		const body = isCreator
			? 'Pick any material, link, or video from the left to make it primary.'
			: 'The creator hasn’t set a primary yet. The center pane will update when they do.'
		return (
			<div
				data-testid="primary-empty-state"
				role="status"
				style={{
					flex: 1,
					minHeight: 0,
					display: 'flex',
					flexDirection: 'column',
					alignItems: 'center',
					justifyContent: 'center',
					textAlign: 'center',
					padding: '32px',
					color: '#555',
				}}
			>
				<h3 style={{ margin: '0 0 8px', fontSize: 18, fontWeight: 600 }}>{heading}</h3>
				<p style={{ margin: 0, fontSize: 14, maxWidth: 420 }}>{body}</p>
			</div>
		)
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
