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

	mdl "github.com/retr0h/meshx/internal/meshx/model"
)

// GET /radios/{radio_id}/nodes — projection of the radio's NodeDB.
// Each NodeItem is decorated with its CurrentState() (online / stale /
// offline based on last-heard age) so clients don't have to recompute
// the heuristic. The list mirrors the order State carries it; no
// server-side sort to preserve "last-heard first" semantics for
// callers that depend on it.

type listNodesInput struct {
	RadioID string `path:"radio_id" doc:"canonical radio identifier — see GET /radios"`
}

type listNodesOutput struct {
	Body struct {
		Nodes []mdl.NodeItem `json:"nodes"`
	}
}

func (s *Server) handleListNodes(_ context.Context, in *listNodesInput) (*listNodesOutput, error) {
	d, err := s.resolveRadio(in.RadioID)
	if err != nil {
		return nil, err
	}
	out := &listNodesOutput{}
	out.Body.Nodes = []mdl.NodeItem{}
	st := d.Snapshot()
	if st == nil {
		return out, nil
	}
	for i := range st.Nodes {
		n := st.Nodes[i]
		n.State = n.CurrentState()
		out.Body.Nodes = append(out.Body.Nodes, n)
	}
	return out, nil
}
