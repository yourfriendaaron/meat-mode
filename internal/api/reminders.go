package api

// ReminderTestRequest is the body of POST /api/reminders/test.
type ReminderTestRequest struct {
	// Endpoint is the push subscription of the device asking, "" when it
	// has none.
	Endpoint string `json:"endpoint"`
}

// ReminderTest is the reply to POST /api/reminders/test: how the test
// reminder went on each way the hub sends reminders.
type ReminderTest struct {
	// Push is "sent" (to this device), "none" (this device has no
	// subscription), "expired" (the push service no longer knows this
	// device's subscription, so the hub removed it) or "failed".
	Push string `json:"push"`
	// Ntfy is "sent", "failed", or "off" when ntfy is not set up.
	Ntfy string `json:"ntfy"`
}
