import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

vi.mock('../api/materials', () => ({
	getMaterialSlides: vi.fn().mockResolvedValue({ slides: [] }),
}))
// Stub the API module so SessionPrimaryRow's import is harmless if it tries
// to call out (no menu interaction in most tests).
vi.mock('../api/sessionPrimary', async () => {
	const actual = await vi.importActual('../api/sessionPrimary')
	return { ...actual, patchSessionPrimary: vi.fn() }
})

const noop = vi.fn()
const baseProps = {
	apiBaseUrl: '',
	selectedVideo: null,
	setSelectedVideo: noop,
	setVideoId: noop,
	setVideoPlayerKey: noop,
	onSelectDocument: noop,
	onSelectVideo: noop,
	onSelectLink: noop,
	selectedDocumentId: null,
	collapsed: false,
	onCollapsedChange: noop,
	hideHeader: true,
	canManage: true,
	onPrimaryChanged: noop,
}

function sessionWithLinks(links, currentPrimary = null) {
	return {
		session: { id: 'sess-1' },
		video_sources: [],
		materials: [],
		links,
		unread_material_ids: [],
		primary_video: null,
		additional_videos: [],
		material_slides_ready: {},
		material_slides_status: {},
		currentPrimary,
	}
}

describe('MaterialsTreePanel SCRUM-294 link-row primary affordance (right-click menu)', () => {
	const verifiedLink = { id: 'l1', url: 'https://example.com', title: 'Doc', status: 'verified' }

	it('does NOT render an inline "Make primary" text link below an eligible link row', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		// SCRUM-294: the previous "Make primary" text link is gone — no inline
		// affordance renders below the row anymore.
		expect(screen.queryByText(/^Make primary$/i)).toBeNull()
	})

	it('right-click on the link row opens the Make-primary context menu', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		const row = screen.getByTestId('link-item').parentElement
		expect(row).not.toBeNull()
		fireEvent.contextMenu(row)
		expect(screen.getByTestId('primary-context-menu')).toBeInTheDocument()
		expect(screen.getByTestId('make-primary-btn')).toBeInTheDocument()
	})

	it('renders the inline Primary badge on a link row when currentPrimary matches', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([verifiedLink])}
				currentPrimary={{ kind: 'link', id: 'l1' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
	})

	it('does not open a Make-primary menu on a processing link (linkSelectable false)', () => {
		const processingLink = { ...verifiedLink, status: 'processing' }
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([processingLink])}
			/>,
		)
		const row = screen.getByTestId('link-item').parentElement
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('does not open a menu on link rows when canManage is false', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		const row = screen.getByTestId('link-item').parentElement
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('does not open a menu when onPrimaryChanged is not provided', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				onPrimaryChanged={undefined}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		const row = screen.getByTestId('link-item').parentElement
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})
})

describe('MaterialsTreePanel SCRUM-294 video-row primary badge + right-click clear', () => {
	const presVideo = { id: 'vs-1', display_title: 'Lecture', transcript_status: 'ready' }

	function sessionWithPresentationVideo(currentPrimary = null) {
		return {
			session: { id: 'sess-1' },
			video_sources: [presVideo],
			materials: [],
			links: [],
			unread_material_ids: [],
			primary_video: presVideo,
			additional_videos: [],
			material_slides_ready: {},
			material_slides_status: {},
			currentPrimary,
		}
	}

	it('renders Primary badge inline on the presentation video when currentPrimary.kind=video', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
		expect(screen.getByTestId('primary-video-item').textContent).toContain('Lecture')
	})

	it('right-click on the primary video row opens the Clear-primary menu', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
		const row = screen.getByTestId('primary-video-item').closest('div')
		expect(row).not.toBeNull()
		fireEvent.contextMenu(row)
		expect(screen.getByTestId('clear-primary-btn')).toBeInTheDocument()
	})

	it('does not render a primary badge when the session primary is a non-video kind', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'document', id: 'mat-1' }}
			/>,
		)
		expect(screen.queryByTestId('primary-badge')).toBeNull()
	})
})

describe('MaterialsTreePanel SCRUM-295 video-row Make-primary affordance', () => {
	// SCRUM-295: video rows now expose Make-primary via right-click, just like
	// document/image/link rows. The PATCH id is the video's file_artifact_id
	// (serialized on VideoSource as `file_artifact_id` after migration 038).
	// Eligibility requires creator + onPrimaryChanged + file_artifact_id is
	// present + the video is not already the current primary. Transcript
	// status is intentionally NOT a gate: file_artifact is born ready by the
	// upload handler, so set-primary works before transcription finishes.
	const presVideoWithFileArtifact = {
		id: 'vs-1',
		display_title: 'Lecture',
		transcript_status: 'ready',
		file_artifact_id: 'fa-vs-1',
	}

	function sessionWithVideos(videos, currentPrimary = null) {
		return {
			session: { id: 'sess-1' },
			video_sources: videos,
			materials: [],
			links: [],
			unread_material_ids: [],
			primary_video: videos[0] ?? null,
			additional_videos: [],
			material_slides_ready: {},
			material_slides_status: {},
			currentPrimary,
		}
	}

	it('right-click on a freshly-uploaded video (no currentPrimary) opens Make-primary menu', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithVideos([presVideoWithFileArtifact])}
				currentPrimary={null}
			/>,
		)
		const row = screen.getByTestId('primary-video-item').closest('div')
		fireEvent.contextMenu(row)
		expect(screen.getByTestId('make-primary-btn')).toBeInTheDocument()
	})

	it('does not open a menu when video has no file_artifact_id (legacy / pre-migration row)', () => {
		const noFA = { ...presVideoWithFileArtifact, file_artifact_id: undefined }
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithVideos([noFA])}
				currentPrimary={null}
			/>,
		)
		const row = screen.getByTestId('primary-video-item').closest('div')
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('opens Make-primary menu even while transcript is processing (file_artifact gate is server-side)', () => {
		// Transcript readiness is decoupled from file_artifact readiness: a
		// just-uploaded MP4 has transcript_status=pending but its file_artifact
		// is already status=ready, so the PATCH primary will succeed.
		const transcriptProcessing = { ...presVideoWithFileArtifact, transcript_status: 'processing' }
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithVideos([transcriptProcessing])}
				currentPrimary={null}
			/>,
		)
		const row = screen.getByTestId('primary-video-item').closest('div')
		fireEvent.contextMenu(row)
		expect(screen.getByTestId('make-primary-btn')).toBeInTheDocument()
	})

	it('does not open a menu when canManage is false (participant)', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWithVideos([presVideoWithFileArtifact])}
				currentPrimary={null}
			/>,
		)
		const row = screen.getByTestId('primary-video-item').closest('div')
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('right-click on a non-primary additional video row also opens Make-primary menu', () => {
		const additional = { id: 'vs-2', display_title: 'Q&A clip', transcript_status: 'ready', file_artifact_id: 'fa-vs-2' }
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithVideos([presVideoWithFileArtifact, additional])}
				currentPrimary={null}
			/>,
		)
		// The additional video row uses testid="video-item" (not primary-video-item).
		const row = screen.getByTestId('video-item').closest('div')
		fireEvent.contextMenu(row)
		expect(screen.getByTestId('make-primary-btn')).toBeInTheDocument()
	})
})

describe('MaterialsTreePanel SCRUM-294 participant-mode primary badge visibility', () => {
	const docMaterial = {
		id: 'mat-1',
		kind: 'document',
		filename: 'spec.pdf',
		content_type: 'application/pdf',
		text_status: 'ready',
	}
	const presVideo = { id: 'vs-1', display_title: 'Lecture', transcript_status: 'ready' }

	function sessionWithDoc(currentPrimary = null) {
		return {
			session: { id: 'sess-1' },
			video_sources: [],
			materials: [docMaterial],
			links: [],
			unread_material_ids: [],
			primary_video: null,
			additional_videos: [],
			material_slides_ready: {},
			material_slides_status: {},
			currentPrimary,
		}
	}

	function sessionWithVideo(currentPrimary = null) {
		return {
			session: { id: 'sess-1' },
			video_sources: [presVideo],
			materials: [],
			links: [],
			unread_material_ids: [],
			primary_video: presVideo,
			additional_videos: [],
			material_slides_ready: {},
			material_slides_status: {},
			currentPrimary,
		}
	}

	it('participant sees Primary badge inline on a primary document row', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWithDoc()}
				currentPrimary={{ kind: 'document', id: 'mat-1' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
		// No make-primary text affordance, no menu on right-click.
		expect(screen.queryByText(/^Make primary$/i)).toBeNull()
		const row = screen.getByTestId('material-item').parentElement
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('participant sees Primary badge inline on the video row when video is primary', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWithVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
	})

	it('participant sees no badge on a row that is NOT primary', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWithDoc()}
				currentPrimary={{ kind: 'document', id: 'different-id' }}
			/>,
		)
		expect(screen.queryByTestId('primary-badge')).toBeNull()
	})
})

describe('MaterialsTreePanel SCRUM-294 image-row primary affordance (right-click menu)', () => {
	const imageMaterial = {
		id: 'img-1',
		kind: 'image',
		filename: 'diagram.png',
		content_type: 'image/png',
		text_status: 'ready',
	}

	function sessionWithImage(currentPrimary = null) {
		return {
			session: { id: 'sess-1' },
			video_sources: [],
			materials: [imageMaterial],
			links: [],
			unread_material_ids: [],
			primary_video: null,
			additional_videos: [],
			material_slides_ready: {},
			material_slides_status: {},
			currentPrimary,
		}
	}

	it('right-click on an image row opens the Make-primary context menu (creator)', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithImage()}
			/>,
		)
		expect(screen.getByTestId('images-item')).toBeInTheDocument()
		// No inline "Make primary" text — only the right-click affordance.
		expect(screen.queryByText(/^Make primary$/i)).toBeNull()
		const row = screen.getByTestId('images-item').parentElement
		fireEvent.contextMenu(row)
		expect(screen.getByTestId('make-primary-btn')).toBeInTheDocument()
	})

	it('renders inline Primary badge on an image row when currentPrimary points at it', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithImage()}
				currentPrimary={{ kind: 'document', id: 'img-1' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
	})

	it('does not open a menu on an image row when canManage is false', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				session={sessionWithImage()}
			/>,
		)
		const row = screen.getByTestId('images-item').parentElement
		fireEvent.contextMenu(row)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})
})

describe('MaterialsTreePanel SCRUM-484 startHereChip placement', () => {
	const docMaterial = {
		id: 'mat-1',
		kind: 'document',
		filename: 'spec.pdf',
		content_type: 'application/pdf',
		text_status: 'ready',
	}
	const otherDoc = {
		id: 'mat-2',
		kind: 'document',
		filename: 'other.pdf',
		content_type: 'application/pdf',
		text_status: 'ready',
	}
	const presVideo = { id: 'vs-1', display_title: 'Lecture', transcript_status: 'ready' }
	const link = { id: 'l1', url: 'https://example.com', title: 'Doc', status: 'verified' }

	function sessionWith({ video_sources = [], materials = [], links = [], currentPrimary = null } = {}) {
		return {
			session: { id: 'sess-1' },
			video_sources,
			materials,
			links,
			unread_material_ids: [],
			primary_video: video_sources[0] ?? null,
			additional_videos: [],
			material_slides_ready: {},
			material_slides_status: {},
			currentPrimary,
		}
	}

	const chipNode = <span data-testid="start-here-chip-stub">CHIP</span>

	it('renders the chip on the primary document row only', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWith({ materials: [docMaterial, otherDoc] })}
				currentPrimary={{ kind: 'document', id: 'mat-1' }}
				startHereChip={chipNode}
			/>,
		)
		// One chip total, attached to the primary row.
		expect(screen.getAllByTestId('start-here-chip-stub')).toHaveLength(1)
		const primaryRow = screen.getByText('spec.pdf').closest('div')
		expect(primaryRow.textContent).toContain('CHIP')
	})

	it('renders the chip on a primary link row only', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWith({ links: [link] })}
				currentPrimary={{ kind: 'link', id: 'l1' }}
				startHereChip={chipNode}
			/>,
		)
		expect(screen.getAllByTestId('start-here-chip-stub')).toHaveLength(1)
	})

	it('renders the chip on the primary video row only', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWith({ video_sources: [presVideo] })}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
				startHereChip={chipNode}
			/>,
		)
		expect(screen.getAllByTestId('start-here-chip-stub')).toHaveLength(1)
	})

	it('renders no chip when no primary is set, even with materials present', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWith({ materials: [docMaterial] })}
				currentPrimary={null}
				startHereChip={chipNode}
			/>,
		)
		expect(screen.queryByTestId('start-here-chip-stub')).toBeNull()
	})

	it('renders no chip when the session is empty', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWith({})}
				currentPrimary={null}
				startHereChip={chipNode}
			/>,
		)
		expect(screen.queryByTestId('start-here-chip-stub')).toBeNull()
	})
})
