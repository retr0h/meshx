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

// Command emojigen parses Unicode's emoji-data.txt and emits
// widths.gen.go in the sibling emoji package — a Go source file
// listing every code-point range we treat as wide-2 in modern
// terminals, plus a binary-search lookup.
//
// Registered as a `tool` in go.mod (same pattern as gofumpt /
// golines / oapi-codegen) so jennifer/jen lands in the require
// block naturally — `go mod tidy` keeps it because this real
// subpackage imports it. Invoked via `go tool` from the parent
// emoji/doc.go's go:generate directive.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/dave/jennifer/jen"
)

func main() {
	in := flag.String("in", "emoji-data.txt", "path to Unicode emoji-data.txt")
	out := flag.String("out", "widths.gen.go", "output Go file (relative to emoji/)")
	flag.Parse()

	ranges, version, err := parse(*in)
	if err != nil {
		log.Fatalf("parse %s: %v", *in, err)
	}
	if err := emit(*out, ranges, version); err != nil {
		log.Fatalf("emit %s: %v", *out, err)
	}
	log.Printf(
		"wrote %s — %d Emoji_Presentation ranges from Unicode %s",
		*out,
		len(ranges),
		version,
	)
}

// parse reads emoji-data.txt and returns the merged set of runes we
// want to treat as "wide-2 in modern terminals," plus the file's
// "Used with Emoji Version <X>" header line so the generated output
// can self-document its source.
//
// We accept a rune if EITHER:
//
//  1. Its property is Emoji_Presentation (Unicode says "render wide-2
//     by default") — covers 😀 👻 🚀 and the rest of the canonical
//     emoji set.
//  2. Its property is Emoji AND its code point is ≥ U+10000 — the
//     "text-by-default" supplementary-plane emoji like 🖐 (U+1F590).
//     Per Unicode, these need VS16 to render emoji-style; per modern
//     terminal reality, they render as wide-2 emoji glyphs anyway,
//     and our renderer needs to plan accordingly. Restricting to SMP
//     avoids ASCII keycap bases (#, *, 0-9) which terminals correctly
//     render at their natural 1-cell width.
func parse(path string) (ranges [][2]rune, version string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		raw := scanner.Text()
		if version == "" {
			if i := strings.Index(raw, "Used with Emoji Version"); i >= 0 {
				version = strings.TrimPrefix(strings.TrimSpace(raw[i:]), "Used with Emoji Version ")
				if j := strings.Index(version, " "); j > 0 {
					version = version[:j]
				}
			}
		}
		line, _, _ := strings.Cut(raw, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ";")
		if len(parts) != 2 {
			continue
		}
		prop := strings.TrimSpace(parts[1])
		isPresentation := prop == "Emoji_Presentation"
		isPlainEmoji := prop == "Emoji"
		if !isPresentation && !isPlainEmoji {
			continue
		}
		cp := strings.TrimSpace(parts[0])
		var lo, hi rune
		if loStr, hiStr, ok := strings.Cut(cp, ".."); ok {
			l, e1 := strconv.ParseUint(loStr, 16, 32)
			h, e2 := strconv.ParseUint(hiStr, 16, 32)
			if e1 != nil || e2 != nil {
				return nil, "", fmt.Errorf("bad range %q", cp)
			}
			lo, hi = rune(l), rune(h)
		} else {
			v, e := strconv.ParseUint(cp, 16, 32)
			if e != nil {
				return nil, "", fmt.Errorf("bad codepoint %q", cp)
			}
			lo, hi = rune(v), rune(v)
		}
		// Plain Emoji (text-by-default) only counts when the whole
		// range sits in the supplementary plane — keeps ASCII keycap
		// bases out.
		if isPlainEmoji && lo < 0x10000 {
			continue
		}
		ranges = append(ranges, [2]rune{lo, hi})
	}
	if err := scanner.Err(); err != nil {
		return nil, "", err
	}
	if version == "" {
		version = "(unknown)"
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	merged := ranges[:0]
	for _, r := range ranges {
		if n := len(merged); n > 0 && r[0] <= merged[n-1][1]+1 {
			if r[1] > merged[n-1][1] {
				merged[n-1][1] = r[1]
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged, version, nil
}

// emit composes the generated source via dave/jennifer and writes it
// to disk. jen handles canonical gofmt-correct output (no trailing-
// whitespace footguns) and gives us typed AST construction so a
// rename of the generated table or lookup func is a one-line change
// at the call sites instead of search/replace through string blobs.
func emit(path string, ranges [][2]rune, version string) error {
	f := jen.NewFile("emoji")
	f.HeaderComment("Code generated by main.go; DO NOT EDIT.")
	f.HeaderComment(fmt.Sprintf("Source: emoji-data.txt — Unicode Emoji Version %s", version))
	f.HeaderComment("Refresh via: just go::generate")
	f.Line()

	f.Comment("emojiPresentationRanges enumerates every code-point range we treat")
	f.Comment("as wide-2 in modern terminals: Unicode Emoji_Presentation = Yes,")
	f.Comment("plus supplementary-plane Emoji = Yes (text-by-default emoji like")
	f.Comment("🖐 U+1F590 that modern terminals render emoji-style anyway).")
	f.Comment("IsEmojiPresentation does the binary-search lookup; ansiCells in")
	f.Comment("internal/tui/components_box.go consults it to promote bare-emoji")
	f.Comment("clusters whose East-Asian-Width is 'Neutral' and therefore measure")
	f.Comment("as 1 cell via ansi.StringWidth.")

	values := make([]jen.Code, 0, len(ranges))
	for _, r := range ranges {
		values = append(values, jen.Values(
			jen.Op(fmt.Sprintf("0x%05X", r[0])),
			jen.Op(fmt.Sprintf("0x%05X", r[1])),
		))
	}
	f.Var().Id("emojiPresentationRanges").Op("=").
		Index(jen.Op("...")).Index(jen.Lit(2)).Rune().
		Custom(jen.Options{
			Open:      "{",
			Close:     "}",
			Separator: ",",
			Multi:     true,
		}, values...)

	f.Line()
	f.Comment("IsEmojiPresentation reports whether r is in the wide-2 emoji set")
	f.Comment("(see emojiPresentationRanges for the source criteria).")
	f.Func().Id("IsEmojiPresentation").Params(jen.Id("r").Rune()).Bool().Block(
		jen.List(jen.Id("lo"), jen.Id("hi")).Op(":=").
			List(jen.Lit(0), jen.Len(jen.Id("emojiPresentationRanges")).Op("-").Lit(1)),
		jen.For(jen.Id("lo").Op("<=").Id("hi")).Block(
			jen.Id("mid").Op(":=").Parens(jen.Id("lo").Op("+").Id("hi")).Op("/").Lit(2),
			jen.Id("rg").Op(":=").Id("emojiPresentationRanges").Index(jen.Id("mid")),
			jen.Switch().Block(
				jen.Case(jen.Id("r").Op("<").Id("rg").Index(jen.Lit(0))).Block(
					jen.Id("hi").Op("=").Id("mid").Op("-").Lit(1),
				),
				jen.Case(jen.Id("r").Op(">").Id("rg").Index(jen.Lit(1))).Block(
					jen.Id("lo").Op("=").Id("mid").Op("+").Lit(1),
				),
				jen.Default().Block(jen.Return(jen.True())),
			),
		),
		jen.Return(jen.False()),
	)
	return f.Save(path)
}
