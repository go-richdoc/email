// Copyright (c) the go-richdoc authors.
// SPDX-License-Identifier: BSD-3-Clause

package email

import "github.com/go-richdoc/html"

// htmlLimits is what this reader's ceilings mean to the HTML reader underneath.
//
// ⛔ MaxPartBytes IS HANDED ON AS MaxInputBytes. A caller who raises
// MaxPartBytes to 128 MiB and is then refused at the HTML reader's own 64 MiB —
// a ceiling they never set and cannot see from here — has been told about a
// limit that is not in this package's API. Threading it is what makes the two
// agree by construction instead of by both happening to hold a number that
// looks compatible today.
//
// ⛔ AND MaxPartBytes IS THE WHOLE OF WHAT IS HANDED OVER — checked, not
// assumed. The pictures of a related message become data URLs in inlineCIDS,
// which rewrites the richdoc tree AFTER the parse, so the bytes the HTML reader
// sees are the decoded part and nothing more. The sibling MOBI reader
// substitutes its pictures INTO the HTML before parsing, so its ceiling has to
// be the text plus the image budget; threading MaxTextBytes alone there would
// have refused a legitimate illustrated book. Two readers, two sums, because
// the substitution happens on opposite sides of the parse.
//
// ⛔ CLAMPED, because MaxPartBytes is an int64 and MaxInputBytes is an int: on
// a 32-bit target or js/wasm a caller's 8 GiB becomes a small and welcoming
// number if it is merely converted. No addition here, so nothing can wrap
// before the clamp — which it did in MOBI's version, where the sum overflowed
// into a negative and every orDefaults in this family reads a negative as
// "use the default".
func htmlLimits(lim Limits) html.Limits {
	return html.Limits{
		MaxDepth:      lim.MaxDepth,
		MaxNodes:      lim.MaxNodes,
		MaxInputBytes: clampToInt(lim.MaxPartBytes),
	}
}

// maxInt is the largest int on this platform, which on js/wasm and 32-bit
// targets is not the largest int64.
const maxInt = int(^uint(0) >> 1)

// clampToInt is n as an int, or the largest int if it does not fit.
//
// ⛔ min AND NOT AN if. Written as a branch, the "does not fit" arm is
// unreachable wherever int is 64 bits — dead code on this machine, uncovered by
// any test that can run here, and this repository's CI refuses an uncovered
// block. One expression is covered everywhere and behaves the same on a 32-bit
// target, where that arm is the one that matters.
func clampToInt(n int64) int { return int(min(n, int64(maxInt))) }
