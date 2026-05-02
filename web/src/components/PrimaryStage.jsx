import React from 'react'
import { VideoPlayer } from '../VideoPlayer'
import { DocumentViewer } from './DocumentViewer'

/**
 * PrimaryStage — center-column primary content renderer (SCRUM-273).
 *
 * Encapsulates the document-vs-video conditional that previously lived inline
 * in CreatorMode.jsx so the same view can be reused by ParticipantMode in
 * SCRUM-274. This refactor is intentionally behavior-neutral: the JSX
 * branches, prop wiring, inline styles, and data-testid hooks match the
 * pre-refactor CreatorMode block exactly.
 *
 * The ingest-pending / failed copy and Retry-ingest button are still
 * controlled by CreatorMode-level state (retryProcessing, processingRetrying);
 * pass them through unchanged to preserve existing behavior. ParticipantMode
 * does not yet wire those — SCRUM-274 will decide whether to surface a
 * recoverable Retry there or render a participant-friendly variant.
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
	if (selectedDocument) {
		return (
			<div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden', padding: '12px' }}>
				<div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
					<DocumentViewer
						doc={selectedDocument}
						apiBaseUrl={apiBaseUrl}
						sessionId={sessionId}
						slidesRefreshTrigger={sessionUpdatedVersion}
					/>
				</div>
			</div>
		)
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
