// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

// Does a ceiling stop the WORK, or only the ANSWER?
//
// ⛔ THE CONTROL IS THE WHOLE TEST, ported from go-richdoc/data — where it found
// a JSON ceiling checked after encoding/json had built everything — and from
// go-richdoc/html, where it found a refusal that cost 96 MB and now costs
// 7 096 bytes. Both times the code READ as if the guard were in the right
// place. So it is run here rather than argued about, and it stays committed so
// that a future change to how parts are decoded has to face it.
//
// ⛔ Every figure is a count of BYTES from runtime.MemStats: exact, and
// independent of what else the machine is doing (taken at a load average above
// 50). There is no timing claim here.
package email

import (
	"runtime"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

func allocatedBy(t *testing.T, f func() (*richdoc.Document, error)) (total uint64, retained int64, err error) {
	t.Helper()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	doc, err := f()

	runtime.ReadMemStats(&after)
	total = after.TotalAlloc - before.TotalAlloc
	runtime.GC()
	var held runtime.MemStats
	runtime.ReadMemStats(&held)
	// ⛔ SIGNED: a refusal frees what it decoded, so the heap can end lower than
	// it started, and as a uint64 subtraction that reads as eighteen quintillion.
	retained = int64(held.HeapAlloc) - int64(before.HeapAlloc)
	runtime.KeepAlive(doc)
	return total, retained, err
}

// plainOf is an ordinary message whose body is n paragraphs of HTML.
//
// ⛔ text/html, because that is the part handed to the delegated reader — the
// place where go-richdoc/html had its ceiling in the wrong spot. A text/plain
// body would exercise this package's own cheapest path and miss the question.
//
// ⛔ IT RETURNS BYTES, AND THE CONVERSION HAPPENS OUTSIDE THE MEASUREMENT. The
// first draft returned a string and the call read Parse([]byte(src), ...)
// INSIDE the measured closure, so every run also measured a full copy of the
// message. That reported a ratio of 7.3 and a defect in this package that does
// not exist: 223 528 bytes for a 200 124-byte message and 1 624 360 for a
// 1 600 124-byte one is the copy, almost exactly, and nothing else. An
// instrument has to be read before its reading is.
func plainOf(n int) []byte { return []byte(plainString(n)) }

func plainString(n int) string {
	return "From: a@example.com\r\n" +
		"To: b@example.com\r\n" +
		"Subject: one\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<html><body>" + strings.Repeat("<p>one</p>", n) + "</body></html>\r\n"
}

// TestTheTotalByteCeilingStopsTheWork.
//
// ⛔ Two messages refused by the SAME ceiling, the second eight times the body
// of the first. The reader was supposed to stop at the same byte in both, so
// the costs must agree. If allocation tracks the message instead, the ceiling
// is checked after the work it exists to prevent.
func TestTheTotalByteCeilingStopsTheWork(t *testing.T) {
	const (
		ceiling = 4 << 10
		small   = 20_000  // ~200 KB of body
		large   = 160_000 // eight times as much
	)

	cost := func(n int) uint64 {
		src := plainOf(n)
		total, _, err := allocatedBy(t, func() (*richdoc.Document, error) {
			return Parse(src, Limits{MaxTotalBytes: ceiling})
		})
		if err == nil {
			t.Fatalf("a body of %d paragraphs against a %d byte ceiling was accepted", n, ceiling)
		}
		t.Logf("%d paragraphs, message %d bytes, refused: %d allocated", n, len(src), total)
		return total
	}

	s, l := cost(small), cost(large)
	ratio := float64(l) / float64(s)
	t.Logf("same ceiling, 8x the body: %d -> %d bytes allocated, ratio %.1f", s, l, ratio)
	if ratio > 2 {
		t.Errorf("the total-byte ceiling does not bound the work: 8x the body costs %.1fx "+
			"the allocation", ratio)
	}
}

// TestWhatOneByteOfMessageBuys, for the messages that are accepted.
func TestWhatOneByteOfMessageBuys(t *testing.T) {
	const (
		paragraphs = 20_000
		ceiling    = 400 // bytes allocated per byte of message
	)
	src := plainOf(paragraphs)
	total, retained, err := allocatedBy(t, func() (*richdoc.Document, error) {
		return Parse(src, Limits{})
	})
	if err != nil {
		t.Fatalf("%d bytes: %v", len(src), err)
	}
	perByte := float64(total) / float64(len(src))
	t.Logf("%d bytes of message -> %d allocated (%.1f per byte), %d retained",
		len(src), total, perByte, retained)
	if perByte > ceiling {
		t.Errorf("allocates %.1f bytes per byte of message, past the ceiling of %d",
			perByte, ceiling)
	}
}
