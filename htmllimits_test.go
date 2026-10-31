// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package email

import (
	"math"
	"testing"

	"github.com/go-richdoc/html"
)

// TestTheByteCeilingReachesTheHtmlReader.
//
// ⛔ TESTED AS A MAPPING, not through a parse. Observing it through behaviour
// would need a part larger than 64 MiB — the whole point is what happens ABOVE
// the HTML reader's own default — and such a fixture buys a slow test and
// several gigabytes of allocation to assert one assignment. The claim is "these
// ceilings mean those ceilings", which is a claim about a function.
func TestTheByteCeilingReachesTheHtmlReader(t *testing.T) {
	got := htmlLimits(Limits{MaxDepth: 7, MaxNodes: 11, MaxPartBytes: 128 << 20})
	want := html.Limits{MaxDepth: 7, MaxNodes: 11, MaxInputBytes: 128 << 20}
	if got != want {
		t.Errorf("htmlLimits gave %+v, want %+v", got, want)
	}

	// ⛔ The case the threading exists for, asserted on its own.
	if got.MaxInputBytes <= 64<<20 {
		t.Errorf("a part ceiling of 128 MiB reached the HTML reader as %d", got.MaxInputBytes)
	}
}

// TestTheClampIsNotAConversion.
//
// ⛔ The planted positive is the value that becomes a SMALL POSITIVE int on a
// 32-bit target. A plain conversion of 1<<32 is zero — and zero means "use the
// default" to every orDefaults in this family, so the overflow would not look
// like an error, it would look like a caller's enormous ceiling being quietly
// ignored.
func TestTheClampIsNotAConversion(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want int
	}{
		{0, 0},
		{1, 1},
		{32 << 20, 32 << 20},
		{int64(maxInt), maxInt},
		{math.MaxInt64, maxInt},
	} {
		if got := clampToInt(c.in); got != c.want {
			t.Errorf("clampToInt(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
