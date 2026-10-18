package api

// Settings is GET /api/settings and the reply to PUT /api/settings: what
// the hub keeps in the app about its user.
type Settings struct {
	// Name is the user's name, or MEAT_MODE_USER while none was chosen. Notes
	// the app writes are signed "<name>-app".
	Name string `json:"name"`
	// TimeZone is an IANA time zone such as Europe/Paris, "" until chosen.
	// The bridge reads local dates and times in it.
	TimeZone string `json:"timeZone"`
	// ReminderLead is how many minutes before an item's due time an early
	// reminder comes, 0 (none) to MaxLeadMinutes.
	ReminderLead int `json:"reminderLead"`
}

// SettingsUpdate is PUT /api/settings. A field left out stays as it is.
type SettingsUpdate struct {
	// Name "" goes back to MEAT_MODE_USER.
	Name         *string `json:"name,omitempty"`
	TimeZone     *string `json:"timeZone,omitempty"`
	ReminderLead *int    `json:"reminderLead,omitempty"`
}

// CalendarStatus is GET /api/calendar.
type CalendarStatus struct {
	On bool `json:"on"`
}

// CalendarLink is POST /api/calendar's reply: the feed's address, shown
// once. A new link replaces the old one.
type CalendarLink struct {
	URL string `json:"url"`
}
