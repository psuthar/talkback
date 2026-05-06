package googlemeet

// SetAPIBasesForTest swaps the Meet API and Drive API base URLs so callers
// outside this package (e.g. internal/handlers smoke tests) can drive the
// client against an httptest server. Returns a restore function that resets
// the bases back to their original values; intended use:
//
//	restore := googlemeet.SetAPIBasesForTest(srv.URL+"/v2", srv.URL+"/drive/v3")
//	t.Cleanup(restore)
//
// This file exists only for tests. It is not part of the production API
// surface.
func SetAPIBasesForTest(meet, drive string) func() {
	prevMeet := meetAPIBase
	prevDrive := driveAPIBase
	if meet != "" {
		meetAPIBase = meet
	}
	if drive != "" {
		driveAPIBase = drive
	}
	return func() {
		meetAPIBase = prevMeet
		driveAPIBase = prevDrive
	}
}
