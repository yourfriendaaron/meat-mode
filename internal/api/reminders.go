package api

// ReminderTest is the reply to POST /api/reminders/test: how the test
// reminder went on each way the hub sends reminders.
type ReminderTest struct {
	// Push is "sent" (to this device), "none" (this device has no
	// subscription) or "failed".
	Push string `json:"push"`
	// Ntfy is "sent", "failed", or "off" when ntfy is not set up.
	Ntfy string `json:"ntfy"`
}
