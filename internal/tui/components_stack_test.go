// Copyright (c) 2026 John Dewey
//
// Boundary tests for the composition primitives — VStack / HStack /
// Bordered. Shared fixtures (rangeOfWidths, assertExactBox) live in
// components_box_test.go alongside the leaf-primitive tests.
//
// Same contract as the leaf primitives: Render(box) returns exactly
// box.Height lines of exactly box.Width cells. The composition
// primitives subtract border/padding/sibling cells from their inner
// budget before delegating; a regression in that math surfaces here
// as a width or line-count mismatch, not as visible drift in the live
// UI two re-renders later.

package tui

import (
	"strings"
	"testing"
)

// TestVStack_Render — VStack.Render(box) distributes Box.Height
// across sized + flex children. Single mechanic (sweep box dims,
// assert exact-fit) so the matrix is one tight loop.
func TestVStack_Render(t *testing.T) {
	stack := VStack{Children: []SizedChild{
		{Comp: Text{Content: "top"}, Size: 1},
		{Comp: Text{Content: "middle"}, Size: -1},
		{Comp: Text{Content: "bottom"}, Size: 1},
	}}
	for _, w := range rangeOfWidths {
		for _, h := range []int{3, 5, 10, 50} {
			box := Box{Width: w, Height: h}
			out := stack.Render(box)
			assertExactBox(t,
				"VStack w="+itoa(w)+" h="+itoa(h), out, box)
		}
	}
}

// TestHStack_Render — HStack.Render(box) distributes Box.Width
// across sized + flex children. Same mechanic as VStack on the
// orthogonal axis.
func TestHStack_Render(t *testing.T) {
	stack := HStack{Children: []SizedChild{
		{Comp: Text{Content: "L"}, Size: 10},
		{Comp: Text{Content: "M"}, Size: -1},
		{Comp: Text{Content: "R"}, Size: 10},
	}}
	for _, w := range []int{30, 80, 200} {
		for _, h := range []int{1, 5, 20} {
			box := Box{Width: w, Height: h}
			out := stack.Render(box)
			assertExactBox(t, "HStack", out, box)
		}
	}
}

// TestBordered_Render — Bordered.Render(box) draws a frame around an
// inner Component, subtracting border + padding from the inner budget
// before delegating. Both scenarios exercise the same Render method
// (inner-budget subtraction vs overflow-absorption) so they share one
// parent function — sub-tests because the inner fixtures diverge.
func TestBordered_Render(t *testing.T) {
	t.Run("inner-budget-subtracted-from-box", func(t *testing.T) {
		inner := Text{Content: strings.Repeat("X\n", 50)}
		for _, w := range []int{10, 80, 200, 206} {
			for _, h := range []int{5, 20, 50} {
				box := Box{Width: w, Height: h}
				out := Bordered{
					Inner: inner,
					Chars: DoubleBorder,
				}.Render(box)
				assertExactBox(t, "Bordered", out, box)
			}
		}
	})
	//
	t.Run("long-inner-line-absorbed-without-overflow", func(t *testing.T) {
		// Inner that emits a 5000-char line: Bordered must absorb it
		// via padCells truncation; outer rows still match Box.Width.
		inner := Text{Content: strings.Repeat("Z", 5000)}
		box := Box{Width: 80, Height: 10}
		out := Bordered{Inner: inner, Chars: NormalBorder}.Render(box)
		assertExactBox(t, "Bordered+long-inner", out, box)
	})
}
