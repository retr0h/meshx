// Copyright (c) 2026 John Dewey

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to
// deal in the Software without restriction, including without limitation the
// rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
// sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
// DEALINGS IN THE SOFTWARE.

package tui

// geo.go — geo + time formatting helpers.
//
// Pure-function helpers used by the radio reports (/rs, /cqr,
// /ping) and the map-style overlays (/nearby, /radar). No model
// state, no side effects; kept separate from app.go so the
// Bubble Tea scaffolding there stays focused on event-loop wiring.

import (
	"fmt"
	"math"
	"time"
)

// haversineKm returns the great-circle distance in kilometers between
// two lat/lon points (degrees). Used by /ping and /whois to surface
// peer-to-self distance when we have a GPS fix on both ends. Returns
// 0 if either coordinate is the (0, 0) origin (Meshtastic's "no fix"
// sentinel).
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	if (lat1 == 0 && lon1 == 0) || (lat2 == 0 && lon2 == 0) {
		return 0
	}
	const r = 6371.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	sinLat := math.Sin(dLat / 2)
	sinLon := math.Sin(dLon / 2)
	a := sinLat*sinLat + math.Cos(phi1)*math.Cos(phi2)*sinLon*sinLon
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return r * c
}

// bearingDeg returns the initial compass bearing from point 1 → 2
// in degrees (0 = north, 90 = east, 180 = south, 270 = west). Used
// by the /nearby and /radar overlays to place each peer at the
// right azimuth on their display. Returns 0 for zero-coordinate
// inputs — the same "no fix" sentinel haversineKm honors.
func bearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	if (lat1 == 0 && lon1 == 0) || (lat2 == 0 && lon2 == 0) {
		return 0
	}
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	phi1 := toRad(lat1)
	phi2 := toRad(lat2)
	dLon := toRad(lon2 - lon1)
	y := math.Sin(dLon) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(dLon)
	brg := math.Atan2(y, x) * 180 / math.Pi
	if brg < 0 {
		brg += 360
	}
	return brg
}

// compassAbbr maps a 0-360° bearing to one of the 8 cardinal
// abbreviations (N, NE, E, SE, S, SW, W, NW). Each slice = 45° wide,
// centered on its cardinal. N is special-cased for the 337.5-22.5
// wrap-around window.
func compassAbbr(deg float64) string {
	// Normalize negative / >360.
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	dirs := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	idx := int(math.Mod((deg+22.5)/45, 8))
	return dirs[idx]
}

// humanDuration formats a time.Duration as a compact label like "2m",
// "1h", "3d" — the style used in the nodes grid's last-heard column.
func humanDuration(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}
