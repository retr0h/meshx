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
	"context"

	"github.com/retr0h/meshx/internal/transports"
)

// HTTP handlers for /transports/ble/* — every method is a thin
// adapter over *transports.Manager. The business logic (validation,
// store I/O, scanner/pairer dispatch) lives in internal/transports
// and is shared with the CLI. The handler's only job is to map
// Huma's input/output structs to and from the manager's plain
// types.

type listBLEDevicesInput struct{}

type listBLEDevicesOutput struct {
	Body struct {
		Devices []transports.BLEDeviceView `json:"devices"`
	}
}

func (s *Server) handleListBLEDevices(
	ctx context.Context,
	_ *listBLEDevicesInput,
) (*listBLEDevicesOutput, error) {
	devs, err := s.transports.ListBLEDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := &listBLEDevicesOutput{}
	out.Body.Devices = devs
	return out, nil
}

type pairBLEInput struct {
	Body struct {
		UUID string `json:"uuid" minLength:"1" doc:"peripheral identifier from a /transports/ble/scan result"`
	}
}

type pairBLEOutput struct {
	Body transports.BLEDeviceView
}

func (s *Server) handlePairBLE(ctx context.Context, in *pairBLEInput) (*pairBLEOutput, error) {
	view, err := s.transports.PairBLE(ctx, in.Body.UUID)
	if err != nil {
		return nil, err
	}
	return &pairBLEOutput{Body: view}, nil
}

type forgetBLEInput struct {
	UUID string `path:"uuid" doc:"peripheral identifier or saved long/short name"`
}

type forgetBLEOutput struct{}

func (s *Server) handleForgetBLE(
	ctx context.Context,
	in *forgetBLEInput,
) (*forgetBLEOutput, error) {
	if err := s.transports.ForgetBLE(ctx, in.UUID); err != nil {
		return nil, err
	}
	return &forgetBLEOutput{}, nil
}

type setBLEFavoriteInput struct {
	UUID string `path:"uuid" doc:"peripheral identifier or saved long/short name"`
}

type setBLEFavoriteOutput struct {
	Body transports.BLEDeviceView
}

func (s *Server) handleSetBLEFavorite(
	ctx context.Context,
	in *setBLEFavoriteInput,
) (*setBLEFavoriteOutput, error) {
	view, err := s.transports.SetBLEFavorite(ctx, in.UUID)
	if err != nil {
		return nil, err
	}
	return &setBLEFavoriteOutput{Body: view}, nil
}

type clearBLEFavoriteInput struct{}

type clearBLEFavoriteOutput struct{}

func (s *Server) handleClearBLEFavorite(
	ctx context.Context,
	_ *clearBLEFavoriteInput,
) (*clearBLEFavoriteOutput, error) {
	if err := s.transports.ClearBLEFavorite(ctx); err != nil {
		return nil, err
	}
	return &clearBLEFavoriteOutput{}, nil
}

type scanBLEInput struct {
	Body struct {
		TimeoutMS int `json:"timeout_ms,omitempty" doc:"scan duration in milliseconds; default 10000"`
	}
}

type scanBLEOutput struct {
	Body struct {
		Devices []transports.BLESighting `json:"devices"`
	}
}

func (s *Server) handleScanBLE(ctx context.Context, in *scanBLEInput) (*scanBLEOutput, error) {
	hits, err := s.transports.ScanBLE(ctx, in.Body.TimeoutMS)
	if err != nil {
		return nil, err
	}
	out := &scanBLEOutput{}
	out.Body.Devices = hits
	return out, nil
}
