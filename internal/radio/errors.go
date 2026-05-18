// Copyright (c) 2026 John Dewey
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package radio

import "fmt"

// OpError is the domain error type returned by ops_*.go methods.
// Carries an HTTP-like status code so the HTTP layer (internal/server)
// can translate it into the appropriate huma.Error* without the radio
// package importing huma. The TUI and MCP layers only need .Error().
type OpError struct {
	Code    int
	Message string
}

func (e *OpError) Error() string { return e.Message }

// ErrBadRequest returns a 400 validation error.
func ErrBadRequest(msg string) error { return &OpError{Code: 400, Message: msg} }

// ErrNotFound returns a 404 lookup miss.
func ErrNotFound(msg string) error { return &OpError{Code: 404, Message: msg} }

// ErrConflict returns a 409 conflict (duplicate name, full slot table).
func ErrConflict(msg string) error { return &OpError{Code: 409, Message: msg} }

// ErrInternal returns a 500 server-side failure.
func ErrInternal(msg string) error { return &OpError{Code: 500, Message: msg} }

// ErrUnavailable returns a 503 (pump down, no radio attached).
func ErrUnavailable(msg string) error { return &OpError{Code: 503, Message: msg} }

// ErrBadRequestf is the fmt.Sprintf variant of ErrBadRequest.
func ErrBadRequestf(f string, a ...any) error { return ErrBadRequest(fmt.Sprintf(f, a...)) }

// ErrNotFoundf is the fmt.Sprintf variant of ErrNotFound.
func ErrNotFoundf(f string, a ...any) error { return ErrNotFound(fmt.Sprintf(f, a...)) }

// ErrConflictf is the fmt.Sprintf variant of ErrConflict.
func ErrConflictf(f string, a ...any) error { return ErrConflict(fmt.Sprintf(f, a...)) }

// ErrInternalf is the fmt.Sprintf variant of ErrInternal.
func ErrInternalf(f string, a ...any) error { return ErrInternal(fmt.Sprintf(f, a...)) }

// ErrUnavailablef is the fmt.Sprintf variant of ErrUnavailable.
func ErrUnavailablef(f string, a ...any) error { return ErrUnavailable(fmt.Sprintf(f, a...)) }
