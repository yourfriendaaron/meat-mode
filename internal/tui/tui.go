// Package tui runs the terminal viewer: a Bubble Tea program that draws
// view.Frame, feeds keys to view.Model.Key and carries out its effects with
// appclient and clipboard. Mouse reporting is never turned on, so the
// terminal's own selection always works.
package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/yourfriendaaron/meat-mode/internal/appclient"
	"github.com/yourfriendaaron/meat-mode/internal/tui/clipboard"
)

// Options is what Run needs.
type Options struct {
	// Client is built with an onSession that saves the session, so an
	// in-place login and the daily renewal both persist.
	Client    *appclient.Client
	Clipboard clipboard.Env
}

// Run shows the viewer until the user quits or ctx ends.
func Run(ctx context.Context, o Options) error { return errNotBuilt }

var errNotBuilt = errors.New("tui: not built yet")

// keep Bubble Tea in the module graph until lane 4 builds the program
var _ = tea.NewProgram
