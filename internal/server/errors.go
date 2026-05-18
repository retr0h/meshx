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

package server

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/retr0h/meshx/internal/radio"
)

// toHumaError translates a radio.OpError into the matching
// huma.Error* so HTTP handlers return the correct status code.
// Non-OpError errors pass through unchanged (Huma defaults them
// to 500).
func toHumaError(err error) error {
	var oe *radio.OpError
	if !errors.As(err, &oe) {
		return err
	}
	switch oe.Code {
	case 400:
		return huma.Error400BadRequest(oe.Message)
	case 404:
		return huma.Error404NotFound(oe.Message)
	case 409:
		return huma.Error409Conflict(oe.Message)
	case 500:
		return huma.Error500InternalServerError(oe.Message)
	case 503:
		return huma.Error503ServiceUnavailable(oe.Message)
	default:
		return huma.Error500InternalServerError(oe.Message)
	}
}
