package main

import (
	"context"
	"errors"

	"github.com/yourfriendaaron/meat-mode/internal/hub"
)

// runHub runs the hub with settings from MEAT_MODE_* environment variables.
func runHub(args []string) error {
	if len(args) > 0 {
		return errors.New("hub takes no arguments; it reads MEAT_MODE_* environment variables")
	}
	return hub.Run(context.Background(), hub.ConfigFromEnv(), hub.Options{})
}
