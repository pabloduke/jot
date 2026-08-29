package jot

import "fmt"

// Exit codes let a harness branch on outcome without parsing error prose.
const (
	ExitOK         = 0
	ExitError      = 1 // generic failure
	ExitLintIssues = 2 // the vault is readable but has lint findings
	ExitNotInited  = 3 // no vault configured
	ExitConflict   = 4 // stale base revision, or an unresolved Git rebase
	ExitUsage      = 64
)

// CodedError carries an exit code alongside the underlying failure.
type CodedError struct {
	Code int
	Err  error
}

func (e *CodedError) Error() string { return e.Err.Error() }
func (e *CodedError) Unwrap() error { return e.Err }

func coded(code int, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Err: err}
}

func codedf(code int, format string, args ...any) error {
	return &CodedError{Code: code, Err: fmt.Errorf(format, args...)}
}

// ExitCode reports the process exit status for an error returned by Run.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ce *CodedError
	if as(err, &ce) {
		return ce.Code
	}
	return ExitError
}
