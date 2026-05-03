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

// Package pump bridges a Transport (any wire impl returned from
// transport.Dial) and the Bubble Tea runtime.
// One goroutine reads FromRadio frames and publishes typed event
// values via program.Send(); another drains outbound ToRadio envelopes
// from the consumer and writes them to the device.
//
// Pump owns the reconnect policy. When client.Run returns an error
// the pump closes the dead client, sleeps with exponential backoff,
// re-dials dest, and resumes pumping — all transparent to the
// consumer. If a session manages to receive at least one frame the
// attempt counter resets, so a long-stable connection that hiccups
// gets a fresh budget.
//
// Concrete *Pump is exported so the construction site can take the
// address (`var p Pump = pump.New(...)`); consumers should bind the
// pointer to their own narrow interface (see internal/meshx/pump.go
// for the canonical TUI consumer interface). Per the osapi-io
// pattern: each consumer declares only the methods it actually
// calls, so a future daemon can declare a different (likely larger)
// interface without bloating the TUI's view of the bridge.
package pump

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	pb "github.com/lmatte7/gomesh/github.com/meshtastic/gomeshproto"

	"github.com/retr0h/meshx/internal/meshx/model"
	"github.com/retr0h/meshx/internal/meshx/transport"
)

// Reconnect policy: truncated exponential backoff with no jitter,
// retried indefinitely. Schedule is 1s,2s,4s,8s,16s,30s,30s,30s,…
// (every step beyond 30s stays at 30s). The pump only stops on ctx
// cancellation (Ctrl+X / process exit) or a clean transport-side
// disconnect. Real-world BLE re-pair (radio in a drawer, walked out
// of range, OS Bluetooth hiccup) routinely takes more than two
// minutes, and the user told us they'd rather see "retry 47/∞ in
// 30s" forever than have meshx silently give up. Jitter is a no-op
// here — single client, single radio, no thundering-herd risk.
const (
	minReconnectBackoff = 1 * time.Second
	maxReconnectBackoff = 30 * time.Second
)

// Transport is the wire-level bridge the pump consumes — a
// bidirectional stream of Meshtastic protobuf envelopes. Declared
// here at the consumer seam (osapi-io pattern), implemented
// structurally by transport.Serial / transport.TCP / transport.BLE
// returned from transport.Dial. Twins meshx.Store / meshx.Pump:
// each consumer narrows the producer's surface to just the methods
// it actually calls.
type Transport interface {
	// Run pumps FromRadio frames to `out` and reads ToRadio envelopes
	// from `in`. Blocks until ctx is cancelled or the connection
	// fails. Returns the first error encountered.
	Run(ctx context.Context, out chan<- *pb.FromRadio, in <-chan *pb.ToRadio) error
	// Close shuts down the underlying connection. Always called from
	// runSession's defer; safe to call on a half-open client.
	Close() error
}

// reconnectBackoff returns the delay before the Nth retry. attempt
// is 1-indexed (the first retry uses attempt=1 → 1s).
func reconnectBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// Cap the shift so 1<<(attempt-1) can't overflow on absurd inputs.
	if attempt > 30 {
		attempt = 30
	}
	d := minReconnectBackoff * time.Duration(1<<(attempt-1))
	if d > maxReconnectBackoff || d <= 0 {
		d = maxReconnectBackoff
	}
	return d
}

// Pump is the running transport ↔ tea bridge.
type Pump struct {
	client  Transport
	program *tea.Program

	// Destination string from the original Dial — re-used by the
	// reconnect loop. Stash it so the pump doesn't need to plumb the
	// dest back through the consumer.
	dest string

	// Our own node num, learned from MyNodeInfo. Used to filter
	// outbound-echo MeshPackets.
	myNum uint32

	// Outbound ToRadio envelopes — consumer code enqueues to send.
	// Survives across reconnects: the channel itself is stable while
	// the underlying Transport gets swapped out.
	outbound chan *pb.ToRadio

	// Cancellation for the running goroutines.
	cancel context.CancelFunc
}

// New spins up a pump goroutine and returns the handle immediately.
// Dialing happens inside the goroutine — that way an 8-second BLE
// scan at startup doesn't block the tea Update loop, and a doomed
// dest (radio off, bad UUID) flows through the same indefinite-retry
// path as a mid-session drop. Call p.Stop() to tear down.
func New(dest string, program *tea.Program) *Pump {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Pump{
		// client is intentionally nil; the run loop's "if p.client
		// == nil" branch performs the first dial. That keeps initial
		// connect and reconnect on the same code path.
		client:   nil,
		program:  program,
		dest:     dest,
		outbound: make(chan *pb.ToRadio, 16),
		cancel:   cancel,
	}

	go p.run(ctx)

	return p
}

// Stop cancels the run loop and closes any live client. Safe to call
// repeatedly; safe to call before the first dial completes.
func (p *Pump) Stop() {
	p.cancel()
	// p.client is nil between New and the first successful dial, so a
	// Ctrl+X during the initial BLE scan would panic here without the
	// guard. The run loop owns subsequent client mutations and will
	// observe ctx cancellation on its next iteration to clean up.
	if p.client != nil {
		_ = p.client.Close()
	}
}

// Enqueue is how consumer code sends a ToRadio from the Update
// goroutine. Non-blocking — drops the message (flashing a hint is
// the caller's responsibility) if the outbound buffer is full,
// which should never happen in practice.
func (p *Pump) Enqueue(msg *pb.ToRadio) bool {
	select {
	case p.outbound <- msg:
		return true
	default:
		return false
	}
}

// run is the main pump loop. Spawns the transport Run goroutine,
// pumps outbound messages, translates inbound FromRadio frames into
// event values and ships them via program.Send().
//
// When $MESHX_DEBUG is set (to a file path, or to "1" for a default
// path), every pump event is appended to that file — the TUI's
// alt-screen swallows stderr so this is the only way to see what's
// flowing when a BLE / serial session appears to hang. Pipe-friendly
// single-line records so `tail -f` reads cleanly.
func (p *Pump) run(ctx context.Context) {
	dbg := openPumpDebugLog()
	defer func() {
		if dbg != nil {
			_ = dbg.Close()
		}
	}()
	dbgf := func(format string, args ...any) {
		if dbg == nil {
			return
		}
		line := fmt.Sprintf(format, args...)
		_, _ = fmt.Fprintf(dbg, "[%s] %s\n", time.Now().Format("15:04:05.000"), line)
	}

	dbgf("pump.run start dest=%s", p.dest)

	attempt := 0
	for {
		if ctx.Err() != nil {
			dbgf("ctx done before next session — exiting")
			return
		}

		// Re-dial if we're between sessions. First time through, the
		// client is nil (set by New) so we always dial here.
		var sessErr error
		if p.client == nil {
			dbgf("re-dial attempt %d", attempt+1)
			client, derr := transport.Dial(p.dest)
			if derr != nil {
				dbgf("re-dial failed: %v", derr)
				sessErr = derr
			} else {
				p.client = client
			}
		}

		if sessErr == nil && p.client != nil {
			established, err := p.runSession(ctx, dbgf)
			if ctx.Err() != nil {
				dbgf("ctx done after session — exiting")
				return
			}
			// A session that produced any inbound frames counts as a
			// successful connect — reset the budget so a long-running
			// link that hiccups gets a fresh 8 attempts.
			if established {
				attempt = 0
			}
			_ = p.client.Close()
			p.client = nil

			if err == nil {
				// Clean disconnect — radio rebooted or got unplugged.
				// Don't auto-redial; the user probably did this on
				// purpose and the pump goroutine should exit.
				dbgf("clean disconnect — not retrying")
				p.program.Send(model.Disconnected{})
				return
			}
			sessErr = err
		}

		// Either dial or session failed. Bump attempt, sleep, and
		// loop. We never give up — the only exits from this loop are
		// ctx cancel (user quit) or clean disconnect (radio rebooted).
		attempt++
		backoff := reconnectBackoff(attempt)
		dbgf("retry %d in %s after: %v", attempt, backoff, sessErr)
		p.program.Send(model.Reconnecting{
			Attempt: attempt,
			After:   backoff,
			Err:     sessErr,
		})
		select {
		case <-ctx.Done():
			dbgf("ctx done during backoff — exiting")
			return
		case <-time.After(backoff):
		}
	}
}

// runSession drives one connection's lifetime: kick off the transport
// reader, request the config dump, and forward translated frames to
// Bubble Tea until the transport drops or ctx cancels. Returns
// established=true when at least one inbound frame arrived (so the
// caller can reset its retry budget) and the underlying error from
// transport.Run if any.
func (p *Pump) runSession(
	ctx context.Context,
	dbgf func(string, ...any),
) (bool, error) {
	inbound := make(chan *pb.FromRadio, 64)

	// Each session runs under a child ctx so cancelling it (e.g. when
	// runSession returns early on ctx.Done) yanks transport.Run out
	// of any blocking read without affecting the parent.
	sessCtx, sessCancel := context.WithCancel(ctx)
	defer sessCancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- p.client.Run(sessCtx, inbound, p.outbound)
	}()
	dbgf("transport.Run goroutine started")

	// Fire the config handshake — prompts the radio to dump its
	// NodeDB, channels, configs, and ConfigComplete. We do this on
	// every (re)connect; the consumer's dedup logic absorbs the
	// replay.
	nonce := transport.SendWantConfig(p.outbound)
	dbgf("SendWantConfig nonce=0x%08x", nonce)

	totalIn := 0
	for {
		select {
		case <-ctx.Done():
			dbgf("ctx.Done in session")
			return totalIn > 0, nil
		case err := <-runErr:
			if err != nil {
				dbgf("transport.Run returned error: %v", err)
				return totalIn > 0, err
			}
			dbgf("transport.Run returned cleanly (radio disconnect?)")
			return totalIn > 0, nil
		case msg := <-inbound:
			if msg == nil {
				dbgf("inbound nil — skipping")
				continue
			}
			totalIn++
			tms := p.translate(msg)
			if len(tms) == 0 {
				dbgf("[%d] inbound translated to nil (housekeeping)", totalIn)
				continue
			}
			for _, tm := range tms {
				dbgf("[%d] sending %T to tea", totalIn, tm)
				p.program.Send(tm)
			}
		}
	}
}

// openPumpDebugLog opens the pump debug log file when $MESHX_DEBUG
// is set. Value is interpreted as a file path; special value "1"
// expands to /tmp/meshx-pump.log for convenience. Returns nil (no
// error) when the env var is unset — pump.run no-ops its logging
// in that case. Safe to call at process startup; the file is opened
// in append mode so repeated sessions accumulate.
func openPumpDebugLog() *os.File {
	path := os.Getenv("MESHX_DEBUG")
	if path == "" {
		return nil
	}
	if path == "1" {
		path = "/tmp/meshx-pump.log"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		// Silently fall back to no logging. Anything else (stderr
		// write) would get eaten by the TUI's alt-screen anyway.
		return nil
	}
	return f
}
