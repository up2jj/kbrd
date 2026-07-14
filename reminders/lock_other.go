//go:build !darwin

package reminders

func acquireFileLock(string) (func(), error) {
	return func() {}, nil
}
