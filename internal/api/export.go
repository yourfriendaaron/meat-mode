package api

import "time"

// The export's names. An export is a zip holding ExportJSONName and one
// entry per attachment, ExportAttachmentDir followed by the attachment's id.
const (
	ExportJSONName      = "export.json"
	ExportAttachmentDir = "attachments/"
)

// The export file's identity. ExportVersion goes up by one whenever what an
// export holds changes in any way, an added field included, so a hub never
// restores a file it would read only in part. A hub restores its own version
// and every older one, upgrading an older file step by step, and refuses a
// newer one. docs/api.md, Export and restore, describes the format.
const (
	ExportFormat  = "meat-mode-export"
	ExportVersion = 2
)

// Export is the whole of what a hub keeps that no other copy holds: the
// lists the hub keeps itself (the personal list and the projects), the
// action log, the reminders already sent for those lists and the non-secret
// settings. Items from a bridge's repos are copies of the machine's tracker
// and stay out; a machine paired again sends them anew.
type Export struct {
	Format     string           `json:"format"`  // ExportFormat
	Version    int              `json:"version"` // ExportVersion
	ExportedAt time.Time        `json:"exportedAt"`
	Settings   ExportSettings   `json:"settings"`
	Items      []ExportItem     `json:"items"`
	Actions    []Action         `json:"actions"`
	Reminders  []ExportReminder `json:"reminders"`
	Origins    []ExportOrigin   `json:"origins"`
	// Machines names the machines paired when the export was made, revoked
	// ones left out, so a restore can say which to pair again.
	Machines []string `json:"machines"`
}

// ExportSettings are the settings the app keeps, as GET /api/settings
// shows them, and whether the calendar feed was on. The feed's address is a
// secret and stays out; a restored hub has the feed off.
type ExportSettings struct {
	Name         string `json:"name"` // "" while none was chosen
	TimeZone     string `json:"timeZone"`
	ReminderLead int    `json:"reminderLead"`
	CalendarFeed bool   `json:"calendarFeed"`
}

// ExportItem is one item of a list the hub keeps: the Item with Key, Bridge
// ("hub"), Repo (the list), FirstSeen, SeenAt, Labels and Attachments filled
// in, and no Run, Pending or Error. Each attachment's bytes travel beside the
// JSON in the export's zip, as attachments/<id> (docs/api.md, Export and
// restore).
type ExportItem struct {
	Item
	// Owner is the machine whose agents added the item and may change it
	// (docs/api.md, Agents: adding to the personal list); "" for one added
	// in the app.
	Owner string `json:"owner,omitempty"`
}

// ExportReminder records one reminder already sent for an item of a list
// the hub keeps, so a restored hub does not send it again.
type ExportReminder struct {
	Key  string    `json:"key"`  // the item key
	Kind string    `json:"kind"` // ReminderDue or ReminderEarly
	At   time.Time `json:"at"`   // the moment it fired for; for ReminderEarly, the due time it came before
}

// Kinds of ExportReminder.
const (
	ReminderDue   = "due"   // at the due time or a snooze end
	ReminderEarly = "early" // the lead time before the due time
)

// ExportOrigin records that a machine's personal repo item became a personal
// item on the hub (docs/api.md, Moving a bridge's personal repo to the hub),
// so a machine that still sends that repo does not add it twice.
type ExportOrigin struct {
	Machine string `json:"machine"`
	BeadID  string `json:"beadId"`
	ItemID  string `json:"itemId"`
}

// RestoreResult is what POST /api/restore answers.
type RestoreResult struct {
	Items     int `json:"items"`
	Actions   int `json:"actions"`
	Reminders int `json:"reminders"`
	// Cancelled counts the actions for a machine's repos that were still
	// waiting to be sent; the restore cancels them (see docs/api.md).
	Cancelled int `json:"cancelled"`
	// Machines are the export's Machines, to pair again.
	Machines []string `json:"machines"`
	// CalendarFeed is true when the export had the feed on: turn it on
	// again, and give calendars its new address.
	CalendarFeed bool `json:"calendarFeed"`
	// Attachments counts the files it loaded.
	Attachments int `json:"attachments"`
}
