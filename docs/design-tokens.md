# Design tokens

Every colour in the web app is picked by what it means, not by its name in the palette. Look the meaning up here instead of choosing a colour, so a pink edge means the same thing on every screen.

## How it works

The token block is the three `:root` rules at the top of `web/src/styles.css`. It has two layers:

- **Palette tokens**, `--palette-*`, hold the raw colours: [Dracula Classic](https://draculatheme.com/spec) in the dark theme and Alucard Classic in the light one. Only the token block uses them.
- **Role tokens**, `--role-*`, say what a colour means and point at a palette token. Both themes share one set of roles; each resolves against the palette of the theme in force.

Every other rule, in every `web/src/*.css` file, uses role tokens only. `web/test/unit/tokens.test.ts` fails when a rule outside the token block uses a palette token or a literal colour (a hex value, `rgb()`, `hsl()`, `oklch()` and the like, or a named colour). `transparent`, `currentColor` and `inherit` are fine, and so is `color-mix()` over role tokens.

To change what a meaning looks like, change the role's line in the token block. To colour something new, use the role whose meaning fits. When none fits, add a role to the token block and a row to this page in the same change.

## The colour rules

- Pink is the agent's channel, and nothing else is pink.
- Solid pink plus the sparkle icon means "this wakes an agent".
- A pink edge means "the agent said it".
- Cyan is only the user's own words.
- Purple is general actions, Done, and anything new.
- Green is done or soon, orange is due or waiting, red is overdue, stopped or an error.
- Stop is red, never pink.

## Roles

### Who said it

| Role | Means | Used on | Do | Don't |
|---|---|---|---|---|
| `--role-agent` | The agent said it. | The edge of an agent's note and of the "asks because" box; the check box of an agent's task. | Edge or tint whatever the agent wrote. | Use it for anything the user did or for a general highlight. |
| `--role-agent-action` | Tapping this wakes an agent. A solid fill, always with the sparkle icon. | Buttons that ask an agent. | Fill the button and pair it with `--role-agent-action-fg`. | Fill a button that does not wake an agent, or colour Stop with it. |
| `--role-agent-action-fg` | Text and icon on an `--role-agent-action` fill. | The same buttons. | Use it only on that fill. | Use it as a text colour elsewhere. |
| `--role-you` | The user's own words. | The edge of the user's own notes. | Mark what the user wrote. | Use it for agent replies, links or "new". |

### Actions and what is new

| Role | Means | Used on | Do | Don't |
|---|---|---|---|---|
| `--role-action` | A general action, the current choice, or something new. | Primary buttons, the add button, the current tab and filter, selected rows, check boxes in select mode, the Undo button, the New and New reply chips, the count on the Agents tab, the wizard's progress bar. | Use it for any button or selection that does not wake an agent. | Use it for a warning or an outcome. |
| `--role-new-tint` | A faint ground for something unread. | An unread note. | Tint the whole surface of a new item. | Use it on text. |
| `--role-focus` | The keyboard focus ring. | `:focus-visible`, focused fields. | Keep every focus ring on it. | Change it per component. |
| `--role-danger` | This button removes or stops something. | Delete buttons, and Stop on an agent run. | Outline a destructive button with it. | Fill a whole screen area with it. |
| `--role-on-accent` | Text and icons on a solid fill other than the agent's. | Primary buttons, the add button, checked boxes, swipe backgrounds, badges. | Pair it with a solid role fill. | Use it on the page background. |

### Time and outcome

| Role | Means | Used on | Do | Don't |
|---|---|---|---|---|
| `--role-done` | Done, on, passed or synced. | Checked boxes, the done swipe, switches that are on, the synced dot, passed checks, the day meter under its load. | Show a good outcome with it. | Use it for a date. |
| `--role-soon` | Due today. | The date chip of a task due today. | Mark a date that is today. | Mark a date that has passed. |
| `--role-date` | A date with nothing pressing about it. | Date chips for later days, the due chip in quick add, the parsed time in the snooze picker. | Show a plain upcoming date. | Use it for a past or snoozed date. |
| `--role-due` | Waiting on time, or about to need the user. | The snoozed chip, the snooze swipe and buttons, a day over its load, the sync dot while changes wait or the agent machine is offline. | Show "not now, but soon". | Use it for an error. |
| `--role-overdue` | Past its due time. | The Past due heading, overdue date chips. | Mark what is late. | Use it for a destructive button. |
| `--role-stopped` | An agent run stopped before it finished. | A stopped run's line of text. | Pair it with the stopped run surface. | Use it for an error the user caused. |
| `--role-error` | Something failed or is not valid. | Error messages, failed actions, invalid input. | Explain what went wrong next to it. | Use it for overdue tasks. |
| `--role-priority` | High priority. | The High chip. | Mark a raised priority. | Use it as a general highlight. |
| `--role-urgent` | The top priority. | The Urgent chip. | Mark only the top priority. | Use it for errors. |

### Agent run states

Each state has an edge (also the colour of the state's line of text) and a tint for the surface. The motion on top of them belongs to the component.

| State | Edge | Tint | Means |
|---|---|---|---|
| Working | `--role-run-working-edge` (pink) | `--role-run-working-tint` | An agent is on it now. |
| Waiting | `--role-run-waiting-edge` (pink) | `--role-run-waiting-tint` | The agent replied and waits on the user. |
| Done | `--role-run-done-edge` (green) | `--role-run-done-tint` | The run finished. |
| Stopped | `--role-run-stopped-edge` (red) | `--role-run-stopped-tint` | The run stopped; the line of text says why. |

Do: show a run's state on its surface plus one line of text in the edge colour. Don't: add a coloured pill for the state.

### Quick add and lists

| Role | Means | Used on | Do | Don't |
|---|---|---|---|---|
| `--role-hl-date`, `--role-hl-list`, `--role-hl-repeat` | What quick add read out of the typed text: a date, a list, a repeat. | Highlights behind the typed words. | Highlight parsed words with them. | Use them outside quick add. |
| `--role-repo-0` to `--role-repo-5` | Tells one list from another. | The coloured dot beside a list's name. | Pick the dot by the list's place in order. | Add pink, so no list reads as an agent. |

### Surfaces and text

| Role | Means |
|---|---|
| `--role-bg` | The page background. |
| `--role-sunk` | A surface set below the page: the sidebar, the tab bar, the bulk bar, command boxes. |
| `--role-raise` | A surface raised above the page: buttons, fields. |
| `--role-pane` | Panes and the open row: darker than the page in the dark theme, white in the light one, so coloured chip text keeps its contrast. |
| `--role-hover` | A row or button under the pointer. |
| `--role-line` | Borders and dividers. |
| `--role-fg` | Body text. |
| `--role-soft` | Secondary text. |
| `--role-dim` | The faintest text; also one list dot. |
| `--role-scrim` | The shade behind a sheet or dialog. |
| `--role-shadow` | The shadow under floating surfaces. |

Do: build every surface from these. Don't: use a role from the tables above as a background for a whole pane.

## Outside the stylesheets

The browser's theme colour (`<meta name="theme-color">`) is set from `web/src/theme.ts`, which repeats the two background values. Change it there when `--palette-bg` changes.
