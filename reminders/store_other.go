//go:build !darwin

package reminders

import (
	"context"
	"errors"

	"kbrd/config"
)

type unsupportedStore struct{}

func newPlatformStore() Store { return unsupportedStore{} }

func (unsupportedStore) Fetch(context.Context, config.RemindersConfig, bool) ([]Reminder, error) {
	return nil, errors.New("Apple Reminders sync is only available on macOS")
}

func (unsupportedStore) Apply(context.Context, config.RemindersConfig, []RemoteOperation) ([]Reminder, error) {
	return nil, errors.New("Apple Reminders sync is only available on macOS")
}
