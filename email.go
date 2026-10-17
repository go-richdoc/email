package email

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"

	"github.com/go-richdoc/richdoc"
)

// Limits bound what a message may ask for. A zero field means the default, so
// Limits{} is the default everywhere.
type Limits struct {
	// MaxParts is how many MIME parts the reader will look at. A message is a
	// tree and a hostile one is a very bushy tree.
	MaxParts int

	// MaxPartBytes is how large one decoded part may be, and MaxTotalBytes how
	// much this reader will decode in all. Base64 is four characters for every
	// three bytes, so a part's decoded size is not its size in the file.
	MaxPartBytes  int64
	MaxTotalBytes int64

	// MaxDepth is how deeply multipart bodies may nest. A message/rfc822
	// forwarded a hundred times is the shape this bounds.
	MaxDepth int

	// MaxNodes is handed to the HTML reader for an HTML part, along with
	// MaxDepth.
	MaxNodes int
}

const (
	defaultMaxParts     = 1024
	defaultMaxPartBytes = 32 << 20
	defaultMaxTotal     = 128 << 20
	defaultMaxDepth     = 32
)

func (l Limits) orDefaults() Limits {
	if l.MaxParts <= 0 {
		l.MaxParts = defaultMaxParts
	}
	if l.MaxPartBytes <= 0 {
		l.MaxPartBytes = defaultMaxPartBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = defaultMaxTotal
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = defaultMaxDepth
	}
	return l
}

// Parse reads a message into a document: a block of headers, then the body.
func Parse(src []byte, lim Limits) (*richdoc.Document, error) {
	lim = lim.orDefaults()
	msg, err := mail.ReadMessage(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("email: %w", err)
	}
	r := &reader{lim: lim}
	// ⛔ The read cannot fail: net/mail's body reader sits on the bytes.Reader
	// above it, and a bytes.Reader does not fail. A branch no input can reach
	// is a branch no test can defend, and this package's CI refuses an
	// uncovered one.
	body, _ := io.ReadAll(io.LimitReader(msg.Body, lim.MaxTotalBytes+1))
	if int64(len(body)) > lim.MaxTotalBytes {
		return nil, fmt.Errorf("email: the body is larger than the %d byte limit", lim.MaxTotalBytes)
	}

	out := &richdoc.Document{Meta: metaOf(msg.Header)}
	out.Blocks = append(out.Blocks, headerBlock(msg.Header)...)

	blocks, err := r.part(asHeader(msg.Header), body, 1)
	if err != nil {
		return nil, err
	}
	out.Blocks = append(out.Blocks, blocks...)
	return out, nil
}

// a reader carries the limits and what is left of them.
type reader struct {
	lim   Limits
	parts int
	spent int64
}

// spend accounts for one part.
func (r *reader) spend(depth int, size int64) error {
	if depth > r.lim.MaxDepth {
		return fmt.Errorf("email: parts nested deeper than %d", r.lim.MaxDepth)
	}
	r.parts++
	if r.parts > r.lim.MaxParts {
		return fmt.Errorf("email: more than %d parts", r.lim.MaxParts)
	}
	if size > r.lim.MaxPartBytes {
		return fmt.Errorf("email: a part of %d bytes, over the %d limit", size, r.lim.MaxPartBytes)
	}
	r.spent += size
	if r.spent > r.lim.MaxTotalBytes {
		return fmt.Errorf("email: %d bytes decoded, over the %d limit", r.spent, r.lim.MaxTotalBytes)
	}
	return nil
}

// headerBlock is the short block of headers a reader of a printed email looks
// at first.
//
// ⛔ Five headers and not the lot. A message carries thirty — Received,
// DKIM-Signature, X-Spam-Status — and printing them all buries the message
// under its own envelope. The five are the ones every mail client shows.
func headerBlock(h mail.Header) []richdoc.Block {
	var out []richdoc.Block
	for _, name := range []string{"From", "To", "Cc", "Subject", "Date"} {
		v := decodeHeader(h.Get(name))
		if strings.TrimSpace(v) == "" {
			continue
		}
		out = append(out, richdoc.Paragraph{Inlines: []richdoc.Inline{
			richdoc.Strong{Inlines: []richdoc.Inline{richdoc.Text{Value: name + ": "}}},
			richdoc.Text{Value: v},
		}})
	}
	if len(out) == 0 {
		return nil
	}
	return append(out, richdoc.ThematicBreak{})
}

// metaOf is what the message says about itself, in the keys richdoc's other
// readers use.
func metaOf(h mail.Header) map[string]string {
	out := map[string]string{}
	for key, name := range map[string]string{
		"title":  "Subject",
		"author": "From",
		"date":   "Date",
	} {
		if v := strings.TrimSpace(decodeHeader(h.Get(name))); v != "" {
			out[key] = v
		}
	}
	for _, name := range []string{"To", "Cc", "Message-Id"} {
		if v := strings.TrimSpace(decodeHeader(h.Get(name))); v != "" {
			out[strings.ToLower(name)] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// decodeHeader reads RFC 2047 encoded words, in whatever charset they name.
//
// ⛔ A word this cannot decode is kept AS IT STANDS rather than dropped:
// "=?x-unknown?B?aGk=?=" on screen is ugly and tells a reader what happened,
// while an empty subject line looks like a message that had none.
func decodeHeader(s string) string {
	dec := &mime.WordDecoder{CharsetReader: charsetReader}
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

// asHeader is the header map a part carries, as net/mail gives it for the
// message itself. The two are the same shape, which is what lets the message
// and its parts go through one function.
func asHeader(h mail.Header) map[string][]string { return h }
