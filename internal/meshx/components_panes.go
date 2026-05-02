// Copyright (c) 2026 John Dewey
//
// Pane Components — every overlay (channels, nodes, nearby, radar,
// help) and the messages pane is a typed Component with a
// Render(Box) method. The pane structs hold a model snapshot;
// Render dispatches to the existing renderer (which itself is
// composing the inner cells via the components_overlays.go +
// components_message.go primitives). This is the React-style
// "everything is a component, props in, output out" architecture
// the tree was building toward.
//
// The wrapper methods (m.renderXxxPane) stay as thin shims for
// now so existing callers (renderIrssiBody, keycap_test.go) keep
// working; once the View tree consumes Components directly, the
// wrappers will go away.

package meshx

// channelsPane is the /channels overlay — a flex VStack of channel
// rows under a CHANNELS header.
type channelsPane struct{ m model }

func (p channelsPane) Render(box Box) string {
	return p.m.renderChannelsPane(box.Width, box.Height)
}

// nodesPane is the BitchX-style users grid — bracketed cells laid
// out in a fixed-width grid under a NODES header + count + legend.
type nodesPane struct{ m model }

func (p nodesPane) Render(box Box) string {
	return p.m.renderNodesPane(box.Width, box.Height)
}

// nearbyPane is the distance-sorted peer roster — peer rows under
// a NEARBY header, with a no-fix / no-peers placeholder when GPS
// data isn't available.
type nearbyPane struct{ m model }

func (p nearbyPane) Render(box Box) string {
	return p.m.renderNearbyPane(box.Width, box.Height)
}

// radarPane is the polar scope — a 2D rune canvas with peers
// plotted by (bearing, distance) under a RADAR header.
type radarPane struct{ m model }

func (p radarPane) Render(box Box) string {
	return p.m.renderRadarPane(box.Width, box.Height)
}

// messagesPane is the chat log — zebra-striped rows scrolling
// under the active channel name + msg count.
type messagesPane struct{ m model }

func (p messagesPane) Render(box Box) string {
	return p.m.renderMessagesPane(box.Width, box.Height)
}

// helpPane is the full-screen /help overlay — section dividers +
// kv rows + scroll indicator under a SQUELCH · HELP banner.
type helpPane struct{ m model }

func (p helpPane) Render(box Box) string {
	return p.m.renderHelpView(box.Width, box.Height)
}
