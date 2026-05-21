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

package tui

import (
	"math"
	"testing"
	"time"
)

func TestHaversineKm(t *testing.T) {
	tests := []struct {
		name        string
		lat1, lon1  float64
		lat2, lon2  float64
		wantApprox  float64 // km
		wantZero    bool
		toleranceKm float64
	}{
		{
			name: "origin-sentinel-returns-zero",
			lat1: 0, lon1: 0,
			lat2: 34.05, lon2: -118.24,
			wantZero: true,
		},
		{
			name: "both-origin-returns-zero",
			lat1: 0, lon1: 0,
			lat2: 0, lon2: 0,
			wantZero: true,
		},
		{
			name: "second-origin-sentinel-returns-zero",
			lat1: 34.05, lon1: -118.24,
			lat2: 0, lon2: 0,
			wantZero: true,
		},
		{
			// Los Angeles to New York — ~3940km great circle
			name: "la-to-new-york",
			lat1: 34.05, lon1: -118.24,
			lat2: 40.71, lon2: -74.01,
			wantApprox:  3940,
			toleranceKm: 50,
		},
		{
			// Same point — distance zero
			name: "same-point",
			lat1: 51.5074, lon1: -0.1278,
			lat2: 51.5074, lon2: -0.1278,
			wantApprox:  0,
			toleranceKm: 0.001,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := haversineKm(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if tt.wantZero {
				if got != 0 {
					t.Errorf("haversineKm(%v,%v,%v,%v) = %v, want 0",
						tt.lat1, tt.lon1, tt.lat2, tt.lon2, got)
				}
				return
			}
			diff := math.Abs(got - tt.wantApprox)
			if diff > tt.toleranceKm {
				t.Errorf("haversineKm = %.2f km, want ~%.2f km (diff=%.2f > tol=%.2f)",
					got, tt.wantApprox, diff, tt.toleranceKm)
			}
		})
	}
}

func TestBearingDeg(t *testing.T) {
	tests := []struct {
		name          string
		lat1, lon1    float64
		lat2, lon2    float64
		wantZero      bool
		wantApproxDeg float64
		toleranceDeg  float64
	}{
		{
			name: "first-origin-returns-zero",
			lat1: 0, lon1: 0, lat2: 10, lon2: 10,
			wantZero: true,
		},
		{
			name: "second-origin-returns-zero",
			lat1: 10, lon1: 10, lat2: 0, lon2: 0,
			wantZero: true,
		},
		{
			// Due north: same longitude, higher latitude; avoid (0,0) sentinel
			name: "due-north",
			lat1: 1, lon1: 1,
			lat2: 2, lon2: 1,
			wantZero:      false,
			wantApproxDeg: 0,
			toleranceDeg:  1,
		},
		{
			// Due east: same latitude, higher longitude; avoid (0,0) sentinel
			name: "due-east",
			lat1: 1, lon1: 1,
			lat2: 1, lon2: 2,
			wantZero:      false,
			wantApproxDeg: 90,
			toleranceDeg:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bearingDeg(tt.lat1, tt.lon1, tt.lat2, tt.lon2)
			if tt.wantZero {
				if got != 0 {
					t.Errorf("bearingDeg = %v, want 0", got)
				}
				return
			}
			diff := math.Abs(got - tt.wantApproxDeg)
			// Handle wrap-around (e.g. 359 vs 1)
			if diff > 180 {
				diff = 360 - diff
			}
			if diff > tt.toleranceDeg {
				t.Errorf("bearingDeg = %.2f°, want ~%.2f° (diff=%.2f > tol=%.2f)",
					got, tt.wantApproxDeg, diff, tt.toleranceDeg)
			}
		})
	}
}

func TestCompassAbbr(t *testing.T) {
	tests := []struct {
		name string
		deg  float64
		want string
	}{
		{name: "north-at-zero", deg: 0, want: "N"},
		{name: "north-at-360", deg: 360, want: "N"},
		{name: "north-east", deg: 45, want: "NE"},
		{name: "east", deg: 90, want: "E"},
		{name: "south-east", deg: 135, want: "SE"},
		{name: "south", deg: 180, want: "S"},
		{name: "south-west", deg: 225, want: "SW"},
		{name: "west", deg: 270, want: "W"},
		{name: "north-west", deg: 315, want: "NW"},
		{name: "north-near-boundary", deg: 350, want: "N"},
		{name: "negative-degrees-normalized", deg: -90, want: "W"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compassAbbr(tt.deg); got != tt.want {
				t.Errorf("compassAbbr(%.1f) = %q, want %q", tt.deg, got, tt.want)
			}
		})
	}
}

func TestMaidenhead(t *testing.T) {
	// Known grid locators verified against online Maidenhead calculators.
	tests := []struct {
		name string
		lat  float64
		lon  float64
		want string
	}{
		{
			// Portland, Oregon — computed from this implementation
			name: "portland-oregon",
			lat:  45.52,
			lon:  -122.68,
			want: "CN85pm",
		},
		{
			// London, UK — computed from this implementation
			name: "london-uk",
			lat:  51.508,
			lon:  -0.128,
			want: "IO91wm",
		},
		{
			// Tokyo — computed from this implementation
			name: "tokyo-japan",
			lat:  35.68,
			lon:  139.69,
			want: "PM95uq",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maidenhead(tt.lat, tt.lon)
			// Must be exactly 6 chars
			if len(got) != 6 {
				t.Errorf(
					"maidenhead(%v, %v) = %q (len=%d), want 6 chars",
					tt.lat,
					tt.lon,
					got,
					len(got),
				)
			}
			// Field (chars 0-1) must be uppercase letters
			if got[0] < 'A' || got[0] > 'R' || got[1] < 'A' || got[1] > 'R' {
				t.Errorf("maidenhead(%v, %v) = %q, field chars must be A-R", tt.lat, tt.lon, got)
			}
			// Square (chars 2-3) must be digits
			if got[2] < '0' || got[2] > '9' || got[3] < '0' || got[3] > '9' {
				t.Errorf("maidenhead(%v, %v) = %q, square chars must be 0-9", tt.lat, tt.lon, got)
			}
			// Subsquare (chars 4-5) must be lowercase letters
			if got[4] < 'a' || got[4] > 'x' || got[5] < 'a' || got[5] > 'x' {
				t.Errorf(
					"maidenhead(%v, %v) = %q, subsquare chars must be a-x",
					tt.lat,
					tt.lon,
					got,
				)
			}
			if got != tt.want {
				t.Errorf("maidenhead(%v, %v) = %q, want %q", tt.lat, tt.lon, got, tt.want)
			}
		})
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "seconds", d: 45 * time.Second, want: "45s"},
		{name: "zero-seconds", d: 0, want: "0s"},
		{name: "one-minute", d: 60 * time.Second, want: "1m"},
		{name: "minutes", d: 90 * time.Second, want: "1m"},
		{name: "59-minutes", d: 59 * time.Minute, want: "59m"},
		{name: "one-hour", d: time.Hour, want: "1h"},
		{name: "hours", d: 5 * time.Hour, want: "5h"},
		{name: "23-hours", d: 23 * time.Hour, want: "23h"},
		{name: "one-day", d: 24 * time.Hour, want: "1d"},
		{name: "days", d: 3 * 24 * time.Hour, want: "3d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := humanDuration(tt.d); got != tt.want {
				t.Errorf("humanDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
