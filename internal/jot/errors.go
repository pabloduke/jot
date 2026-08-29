package jot

import "errors"

// as is a thin wrapper so exit.go can stay free of the errors import.
func as(err error, target any) bool { return errors.As(err, target) }
