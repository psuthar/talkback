import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { MaterialsTreePanel } from '../components/MaterialsTreePanel'

vi.mock('../api/materials', () => ({
	getMaterialSlides: vi.fn().mockResolvedValue({ slides: [] }),
}))
// Stub the API module so SetPrimaryButton's import is harmless if it tries to
// call out (no click in these tests).
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

describe('MaterialsTreePanel SCRUM-276 primary badging on link rows', () => {
	const verifiedLink = { id: 'l1', url: 'https://example.com', title: 'Doc', status: 'verified' }

	it('renders Make-primary button on a link row when canManage + onPrimaryChanged', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		// Two SetPrimaryButton instances may exist (documents + links); the link
		// row's button is the one we care about — at least one is present.
		const buttons = screen.getAllByTestId('set-primary-btn')
		expect(buttons.length).toBeGreaterThan(0)
	})

	it('renders Primary badge when currentPrimary matches the link row', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([verifiedLink])}
				currentPrimary={{ kind: 'link', id: 'l1' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
	})

	it('does not render the button on a processing link (linkSelectable false)', () => {
		const processingLink = { ...verifiedLink, status: 'processing' }
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithLinks([processingLink])}
			/>,
		)
		// The link row exists but no Make-primary button below it.
		expect(screen.getByTestId('link-item')).toBeInTheDocument()
		expect(screen.queryByTestId('set-primary-btn')).toBeNull()
	})

	it('does not render the button when canManage is false', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		expect(screen.queryByTestId('set-primary-btn')).toBeNull()
	})

	it('does not render the button when onPrimaryChanged is not provided', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				onPrimaryChanged={undefined}
				session={sessionWithLinks([verifiedLink])}
			/>,
		)
		expect(screen.queryByTestId('set-primary-btn')).toBeNull()
	})
})

// SCRUM-286: video rows in the presentation slot use SetPrimaryButton's
// badge form instead of a plain "Primary" meta string, so styling and the
// Clear affordance match document/link rows.
describe('MaterialsTreePanel SCRUM-286 primary badge on presentation video row', () => {
	const presVideo = {
		id: 'vs-1',
		display_title: 'Lecture',
		transcript_status: 'ready',
	}

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

	it('renders Primary badge on the presentation video when currentPrimary.kind=video', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
		// The legacy static "Primary" meta string is no longer the source of
		// truth; the badge styling now matches doc/link rows.
		expect(screen.getByTestId('primary-video-item').textContent).toContain('Lecture')
	})

	it('exposes the Clear control via right-click on the badge when onPrimaryChanged is set (SCRUM-290)', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
		// Menu starts closed (Clear lives inside it now, not inline).
		expect(screen.queryByTestId('clear-primary-btn')).toBeNull()
		// Right-click on the badge opens the context menu.
		fireEvent.contextMenu(screen.getByTestId('primary-badge'))
		expect(screen.getByTestId('clear-primary-btn')).toBeInTheDocument()
	})

	it('does not render a primary badge when the session primary is a non-video kind (e.g. document)', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'document', id: 'mat-1' }}
			/>,
		)
		// The primary video row shows no badge here — the document row would.
		// (No matching document fixture in this test, so simply assert no badge
		// from the video row exists.)
		expect(screen.queryByTestId('primary-badge')).toBeNull()
	})

})

// SCRUM-293: participants (canManage=false) should still see the Primary
// badge as a read-only status indicator on whichever row is the session
// primary. They must NOT see the Make-primary button or the right-click
// Clear menu — the badge is purely informational for them.
describe('MaterialsTreePanel SCRUM-293 participant-mode primary badge visibility', () => {
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

	it('participant sees Primary badge on a document row when it is the session primary', () => {
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
		// No Make-primary button, no right-click menu interactivity.
		expect(screen.queryByTestId('set-primary-btn')).toBeNull()
	})

	it('participant sees Primary badge on the video row when a video is the session primary', () => {
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

	it('participant primary badge is non-interactive (no tabIndex, right-click is a no-op)', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				onPrimaryChanged={undefined}
				session={sessionWithDoc()}
				currentPrimary={{ kind: 'document', id: 'mat-1' }}
			/>,
		)
		const badge = screen.getByTestId('primary-badge')
		expect(badge.getAttribute('tabindex')).toBeNull()
		fireEvent.contextMenu(badge)
		expect(screen.queryByTestId('primary-context-menu')).toBeNull()
	})

	it('participant sees no badge on a row that is NOT the session primary', () => {
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

// SCRUM-289: image-kind materials are eligible for primary too. The Images
// section now renders SetPrimaryButton + the badge identically to the
// Documents section, since the backend's primary_content_kind='document'
// is the entity-pointer (any material row), not a content-type label.
describe('MaterialsTreePanel SCRUM-289 primary affordance on image rows', () => {
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

	it('renders Make-primary button on an image row when canManage + onPrimaryChanged', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithImage()}
			/>,
		)
		expect(screen.getByTestId('images-item')).toBeInTheDocument()
		const buttons = screen.getAllByTestId('set-primary-btn')
		expect(buttons.length).toBeGreaterThan(0)
	})

	it('renders Primary badge on an image row when currentPrimary points at it', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithImage()}
				currentPrimary={{ kind: 'document', id: 'img-1' }}
			/>,
		)
		expect(screen.getByTestId('primary-badge')).toBeInTheDocument()
	})

	it('does not render the button on an image row when canManage is false', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				session={sessionWithImage()}
			/>,
		)
		// Image row still appears, but no SetPrimaryButton.
		expect(screen.getByTestId('images-item')).toBeInTheDocument()
		expect(screen.queryByTestId('set-primary-btn')).toBeNull()
	})
})
