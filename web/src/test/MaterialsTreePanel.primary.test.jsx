import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
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

	it('renders the Clear control next to the badge when onPrimaryChanged is set', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
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

	it('does not render a primary badge when canManage is false', () => {
		render(
			<MaterialsTreePanel
				{...baseProps}
				canManage={false}
				session={sessionWithPresentationVideo()}
				currentPrimary={{ kind: 'video', id: 'fa-7' }}
			/>,
		)
		expect(screen.queryByTestId('primary-badge')).toBeNull()
	})
})
