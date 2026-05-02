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
