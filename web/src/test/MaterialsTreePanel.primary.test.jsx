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
