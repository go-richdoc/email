// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package email

import (
	"fmt"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// FuzzParse points this reader at bytes nobody wrote, which for a mail reader
// is the ordinary case and not the exception: a message arrives from whoever
// sent it.
//
// It completes a sweep. Six of this family's seven readers had no fuzz target
// at all, and this is the last of the ones that read a format directly
// (go-richdoc/markdown delegates its tokenising to goldmark, which is fuzzed
// upstream).
//
// Four properties:
//
//   - Parse does not panic. A MIME tree is walked recursively and its headers
//     are decoded word by word, which is where an off-by-one lives.
//   - its two results are exclusive: a document OR an error, never neither.
//   - Parse is DETERMINISTIC. Headers arrive as a map and Meta is built from
//     one, and Go randomises map iteration on purpose: a single range reaching
//     the output would make a message's metadata depend on nothing the message
//     said.
//   - the limits hold, measured on the DOCUMENT. This reader has the most
//     complete Limits in the family -- parts, per-part bytes, total bytes,
//     nesting depth -- and spend() checks all of them in one place. What the
//     assertion adds is the other direction: that what came OUT respects what
//     was declared, since a limit counted in the wrong place passes its own
//     unit test and still lets the document through.
func FuzzParse(f *testing.F) {
	plain := "From: a@b\r\nTo: c@d\r\nSubject: s\r\n\r\nhello\r\n"
	seeds := []string{
		plain,
		// A multipart with both alternatives, which is the common shape.
		"From: a@b\r\nSubject: s\r\nContent-Type: multipart/alternative; boundary=X\r\n\r\n" +
			"--X\r\nContent-Type: text/plain\r\n\r\nplain\r\n" +
			"--X\r\nContent-Type: text/html\r\n\r\n<p>rich</p>\r\n--X--\r\n",
		// Nesting, which is what MaxDepth is for: a forwarded message.
		"From: a@b\r\nContent-Type: multipart/mixed; boundary=A\r\n\r\n" +
			"--A\r\nContent-Type: message/rfc822\r\n\r\n" +
			"From: e@f\r\nContent-Type: multipart/mixed; boundary=B\r\n\r\n" +
			"--B\r\nContent-Type: text/plain\r\n\r\ninner\r\n--B--\r\n--A--\r\n",
		// Encodings: base64, quoted-printable, and an encoded-word header.
		"From: a@b\r\nSubject: =?utf-8?B?aGVsbG8=?=\r\nContent-Transfer-Encoding: base64\r\n" +
			"Content-Type: text/plain\r\n\r\naGVsbG8gdGhlcmU=\r\n",
		"From: a@b\r\nContent-Transfer-Encoding: quoted-printable\r\nContent-Type: text/plain\r\n\r\n" +
			"a=3Db c=\r\nd\r\n",
		// An attachment, and a part with a type nothing knows.
		"From: a@b\r\nContent-Type: multipart/mixed; boundary=X\r\n\r\n" +
			"--X\r\nContent-Type: text/plain\r\n\r\nbody\r\n" +
			"--X\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=f.bin\r\n\r\n" +
			"\x00\x01\x02\r\n--X--\r\n",
		// Malformed, which must be an error rather than a panic: no blank line,
		// a boundary that never closes, a header with no colon.
		"From: a@b\r\nSubject: s",
		"From: a@b\r\nContent-Type: multipart/mixed; boundary=X\r\n\r\n--X\r\nContent-Type: text/plain\r\n\r\nunclosed\r\n",
		"not a header at all\r\n\r\nbody\r\n",
		"",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, src []byte) {
		// Small limits on purpose: a bound is only tested near its edge.
		lim := Limits{
			MaxParts:      32,
			MaxPartBytes:  1 << 16,
			MaxTotalBytes: 1 << 16,
			MaxDepth:      8,
			MaxNodes:      2048,
		}
		doc, err := Parse(src, lim)
		if err != nil {
			if doc != nil {
				t.Fatalf("both a document and an error: %v", err)
			}
			return
		}
		if doc == nil {
			t.Fatal("neither a document nor an error")
		}

		again, err2 := Parse(src, lim)
		if err2 != nil || again == nil {
			t.Fatalf("the second Parse disagreed with the first: doc=%v err=%v", again != nil, err2)
		}
		if first, second := fmt.Sprintf("%#v", doc), fmt.Sprintf("%#v", again); first != second {
			t.Fatalf("two parses of the same bytes differ\nfirst:\n%.400s\nsecond:\n%.400s", first, second)
		}

		// What came out respects what was declared. The slack covers what the
		// reader legitimately adds around a part's own bytes: an attachment's
		// name and type, a heading per part, and the markup of an HTML part
		// rendered to text.
		if n, ceiling := fuzzCarried(doc), 4*lim.MaxTotalBytes+64*1024; n > ceiling {
			t.Errorf("the document carries %d bytes from %d of source, over the %d the limits allow",
				n, len(src), ceiling)
		}
	})
}

func fuzzCarried(d *richdoc.Document) int64 {
	n := int64(len(richdoc.PlainText(d)))
	for k, v := range d.Meta {
		n += int64(len(k) + len(v))
	}
	var blocks func([]richdoc.Block)
	blocks = func(bs []richdoc.Block) {
		for _, b := range bs {
			switch v := b.(type) {
			case richdoc.Paragraph:
				n += fuzzInlineBytes(v.Inlines)
			case richdoc.Heading:
				n += fuzzInlineBytes(v.Inlines)
			case richdoc.CodeBlock:
				n += int64(len(v.Text))
			case richdoc.RawBlock:
				n += int64(len(v.Text))
			case richdoc.BlockQuote:
				blocks(v.Blocks)
			case richdoc.List:
				for _, it := range v.Items {
					blocks(it.Blocks)
				}
			case richdoc.Table:
				for _, row := range append([][]richdoc.Cell{v.Header}, v.Rows...) {
					for _, c := range row {
						n += fuzzInlineBytes(c.Inlines)
						blocks(c.Blocks)
					}
				}
			}
		}
	}
	blocks(d.Blocks)
	return n
}

func fuzzInlineBytes(xs []richdoc.Inline) int64 {
	var n int64
	for _, x := range xs {
		switch v := x.(type) {
		case richdoc.Image:
			n += int64(len(v.URL) + len(v.Alt))
		case richdoc.Link:
			n += int64(len(v.URL)) + fuzzInlineBytes(v.Inlines)
		case richdoc.Emph:
			n += fuzzInlineBytes(v.Inlines)
		case richdoc.Strong:
			n += fuzzInlineBytes(v.Inlines)
		case richdoc.Code:
			n += int64(len(v.Value))
		case richdoc.Text:
			n += int64(len(v.Value))
		case richdoc.RawInline:
			n += int64(len(v.Text))
		}
	}
	return n
}
