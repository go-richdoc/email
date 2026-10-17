package email

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"strings"

	"github.com/go-richdoc/html"
	"github.com/go-richdoc/richdoc"
	"golang.org/x/text/encoding/htmlindex"
)

// part reads one MIME part — which may be the message itself — into blocks.
func (r *reader) part(header map[string][]string, body []byte, depth int) ([]richdoc.Block, error) {
	if err := r.spend(depth, int64(len(body))); err != nil {
		return nil, err
	}
	mediaType, params := contentType(header)
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		return r.multipart(mediaType, params["boundary"], body, depth)
	case mediaType == "message/rfc822":
		// A forwarded message: headers and parts again, one level down.
		return r.forwarded(body, depth)
	}

	decoded, err := decode(header, body)
	if err != nil {
		// ⛔ A part whose encoding does not decode is DESCRIBED, not refused:
		// one broken attachment in a message must not lose the letter it came
		// with.
		return []richdoc.Block{describe(mediaType, header, int64(len(body)), err)}, nil
	}
	// ⛔ THE DISPOSITION WINS, and it is checked before the type rather than
	// after. A part the sender marked as an attachment with a filename is a
	// FILE — including a .txt one — and a reader that inlined it because its
	// type happened to be text would paste a log file into the middle of a
	// letter. A part with no Content-Type at all is text/plain by RFC 2045,
	// which is why the order mattered: "mystery.bin" with no type arrived as
	// the letter's own words until this check moved above the switch.
	if isAttachment(header) {
		return []richdoc.Block{describe(mediaType, header, int64(len(decoded)), nil)}, nil
	}
	switch mediaType {
	case "text/html":
		doc, err := html.Parse(decoded, html.Limits{MaxDepth: r.lim.MaxDepth, MaxNodes: r.lim.MaxNodes})
		if err != nil {
			return nil, fmt.Errorf("email: an HTML part: %w", err)
		}
		return doc.Blocks, nil
	case "text/plain", "":
		return plain(charsetOf(params, decoded)), nil
	}
	if !strings.HasPrefix(mediaType, "text/") {
		return []richdoc.Block{describe(mediaType, header, int64(len(decoded)), nil)}, nil
	}
	// Another flavour of text — text/markdown, text/csv, text/calendar — is
	// still words, and words are better shown than named.
	return plain(charsetOf(params, decoded)), nil
}

// multipart walks a multipart body.
//
// ⛔ The three kinds mean three different things, and treating them alike is
// the commonest defect in a mail converter: alternative is the same message
// twice and must be read ONCE, related is one message plus its pictures, mixed
// is a sequence. A reader that concatenated an alternative's parts would print
// every HTML email twice, once as markup-free text and once as itself.
func (r *reader) multipart(mediaType, boundary string, body []byte, depth int) ([]richdoc.Block, error) {
	if boundary == "" {
		// ⛔ A multipart with no boundary cannot be split at all. Its raw body
		// is still text a person can read, and showing it beats refusing the
		// message: this is what a truncated or hand-written message looks
		// like.
		return plain(string(body)), nil
	}
	type piece struct {
		header map[string][]string
		body   []byte
	}
	var pieces []piece
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextRawPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A malformed boundary: what was read so far is kept, because half
			// a message is more than none.
			break
		}
		// ⛔ Bounded while reading, and NOT refused here. A part bigger than
		// MaxPartBytes is refused by spend when the part is read — and it
		// cannot get this far without the ENCLOSING body being bigger still,
		// which spend already refused one level up. A second refusal here
		// would be a line no input can reach. The LimitReader stays, because
		// what it bounds is the allocation.
		b, _ := io.ReadAll(io.LimitReader(p, r.lim.MaxPartBytes+1))
		pieces = append(pieces, piece{header: p.Header, body: b})
	}

	switch mediaType {
	case "multipart/alternative":
		// The richest part this package can read, which is the LAST one it
		// understands: RFC 2046 puts the poorest first.
		best := -1
		for i, p := range pieces {
			if rank(contentTypeOf(p.header)) > 0 && (best < 0 ||
				rank(contentTypeOf(p.header)) >= rank(contentTypeOf(pieces[best].header))) {
				best = i
			}
		}
		if best < 0 {
			// Nothing readable: describe the first part rather than nothing.
			if len(pieces) == 0 {
				return nil, nil
			}
			best = 0
		}
		return r.part(pieces[best].header, pieces[best].body, depth+1)

	case "multipart/signed":
		// The message is the first part; the rest is the signature.
		if len(pieces) == 0 {
			return nil, nil
		}
		return r.part(pieces[0].header, pieces[0].body, depth+1)
	}

	// related and mixed, and anything else: a sequence. For related, the
	// pictures are gathered first so the HTML part can resolve its cid:
	// references against them.
	byCID := map[string][]byte{}
	if mediaType == "multipart/related" {
		for _, p := range pieces {
			id := strings.Trim(textproto.MIMEHeader(p.header).Get("Content-Id"), "<>")
			if id == "" {
				continue
			}
			if b, err := decode(p.header, p.body); err == nil {
				byCID[id] = b
			}
		}
	}
	var out []richdoc.Block
	for _, p := range pieces {
		blocks, err := r.part(p.header, p.body, depth+1)
		if err != nil {
			return nil, err
		}
		if len(byCID) > 0 {
			blocks = inlineCIDs(blocks, p.header, byCID)
		}
		out = append(out, blocks...)
	}
	return out, nil
}

// forwarded reads a message/rfc822 part: a whole message inside a part.
func (r *reader) forwarded(body []byte, depth int) ([]richdoc.Block, error) {
	inner, err := parseHeaders(body)
	if err != nil {
		// Not a message after all: show the bytes as text rather than nothing.
		return plain(string(body)), nil
	}
	out := headerBlock(inner.header)
	blocks, err := r.part(inner.header, inner.body, depth+1)
	if err != nil {
		return nil, err
	}
	return append(out, blocks...), nil
}

// rank says how much this package can do with a media type, for choosing
// between the alternatives of one message. Zero means "cannot read it".
func rank(mediaType string) int {
	switch mediaType {
	case "text/html":
		return 3
	case "text/plain":
		return 2
	case "":
		return 1
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		// A nested multipart inside an alternative is usually a related part
		// carrying the HTML and its pictures, which is the richest of all.
		return 4
	}
	return 0
}

// describe names a part this reader will not render.
//
// ⛔ Named rather than dropped, with its size: "there is a 2.3 MB
// application/pdf here called invoice.pdf" is information, and a converted
// message that silently loses its attachment reads as a message that never had
// one.
func describe(mediaType string, header map[string][]string, size int64, err error) richdoc.Block {
	name := filename(header)
	what := mediaType
	if what == "" {
		what = "an unnamed type"
	}
	text := fmt.Sprintf("[%s, %s", what, humanSize(size))
	if name != "" {
		text += ", " + name
	}
	text += "]"
	if err != nil {
		text += " — which did not decode: " + err.Error()
	}
	return richdoc.Paragraph{Inlines: []richdoc.Inline{
		richdoc.Emph{Inlines: []richdoc.Inline{richdoc.Text{Value: text}}},
	}}
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// plain turns the text of a text/plain part into paragraphs.
//
// ⛔ A blank line is a paragraph break and a single newline is NOT. Mail is
// hard-wrapped at seventy-odd columns, and a reader that made a paragraph of
// every line would turn every message into a column of fragments. A line that
// is only a quotation marker keeps its ">" — the quoting a reader expects to
// see — and a run of quoted lines becomes a block quotation.
func plain(s string) []richdoc.Block {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []richdoc.Block
	for _, block := range strings.Split(s, "\n\n") {
		lines := strings.Split(strings.Trim(block, "\n"), "\n")
		if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {
			continue
		}
		quoted := true
		for _, l := range lines {
			if !strings.HasPrefix(strings.TrimSpace(l), ">") {
				quoted = false
				break
			}
		}
		var in []richdoc.Inline
		for i, l := range lines {
			if quoted {
				l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), ">"))
			}
			if i > 0 {
				in = append(in, richdoc.LineBreak{})
			}
			in = append(in, richdoc.Text{Value: l})
		}
		p := richdoc.Paragraph{Inlines: in}
		if quoted {
			out = append(out, richdoc.BlockQuote{Blocks: []richdoc.Block{p}})
			continue
		}
		out = append(out, p)
	}
	return out
}

// decode undoes the part's transfer encoding.
func decode(header map[string][]string, body []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(textproto.MIMEHeader(header).Get("Content-Transfer-Encoding"))) {
	case "base64":
		// ⛔ Whitespace and line breaks are stripped first: base64 in mail is
		// wrapped at 76 columns, and the strict decoder refuses a newline.
		clean := strings.Join(strings.Fields(string(body)), "")
		return base64.StdEncoding.WithPadding(base64.StdPadding).DecodeString(clean)
	case "quoted-printable":
		return io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
	}
	// 7bit, 8bit, binary, and anything a message invented: the bytes as they
	// stand.
	return body, nil
}

// charsetOf turns a part's bytes into a Go string, in whatever charset the part
// declared.
//
// ⛔ A charset nothing recognises is read as UTF-8 rather than refused. Mail
// carries charset="unknown-8bit", charset="ansi" and charset="" every day, and
// the alternative to guessing is losing the message.
func charsetOf(params map[string]string, b []byte) string {
	name := params["charset"]
	if name == "" || strings.EqualFold(name, "utf-8") || strings.EqualFold(name, "us-ascii") {
		return string(b)
	}
	enc, err := htmlindex.Get(name)
	if err != nil {
		return string(b)
	}
	// ⛔ The decode itself does not fail: x/text's decoders substitute U+FFFD
	// for a byte sequence the charset does not allow rather than refusing it,
	// which is what a reader of real mail wants — a message with one bad byte
	// is still a message.
	out, _ := enc.NewDecoder().Bytes(b)
	return string(out)
}

// charsetReader is what mime.WordDecoder uses for an encoded word in something
// other than UTF-8.
func charsetReader(name string, r io.Reader) (io.Reader, error) {
	enc, err := htmlindex.Get(name)
	if err != nil {
		return nil, err
	}
	return enc.NewDecoder().Reader(r), nil
}

// contentType is a part's media type and its parameters, lowercased.
func contentType(header map[string][]string) (string, map[string]string) {
	v := textproto.MIMEHeader(header).Get("Content-Type")
	if v == "" {
		return "", map[string]string{}
	}
	mediaType, params, err := mime.ParseMediaType(v)
	if err != nil {
		// ⛔ A Content-Type that does not parse still says something: the type
		// before the first semicolon is usually right, and a message with a
		// broken parameter list is a message, not an error.
		if i := strings.Index(v, ";"); i > 0 {
			return strings.ToLower(strings.TrimSpace(v[:i])), map[string]string{}
		}
		return strings.ToLower(strings.TrimSpace(v)), map[string]string{}
	}
	// mime.ParseMediaType returns a non-nil map on success, so there is no
	// "it gave me nothing" case to guard.
	return mediaType, params
}

func contentTypeOf(header map[string][]string) string {
	t, _ := contentType(header)
	return t
}

// isAttachment reports whether the part says it is one.
func isAttachment(header map[string][]string) bool {
	d := textproto.MIMEHeader(header).Get("Content-Disposition")
	if d == "" {
		return false
	}
	disp, _, err := mime.ParseMediaType(d)
	if err != nil {
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(d)), "attachment")
	}
	return strings.EqualFold(disp, "attachment")
}

// filename is what the part calls itself, from either of the two headers that
// carry it.
func filename(header map[string][]string) string {
	h := textproto.MIMEHeader(header)
	for _, key := range []string{"Content-Disposition", "Content-Type"} {
		v := h.Get(key)
		if v == "" {
			continue
		}
		if _, params, err := mime.ParseMediaType(v); err == nil {
			for _, p := range []string{"filename", "name"} {
				if s := strings.TrimSpace(params[p]); s != "" {
					return decodeHeader(s)
				}
			}
		}
	}
	return ""
}

// inlineCIDs replaces cid: references with the bytes of the part they name.
//
// ⛔ Only cid:, and only from parts that travelled INSIDE this message. An
// http(s) reference is left exactly as it is: fetching one would tell the
// sender that a human opened the mail, which is what a tracking pixel is for.
func inlineCIDs(blocks []richdoc.Block, header map[string][]string, byCID map[string][]byte) []richdoc.Block {
	return mapImages(blocks, func(img richdoc.Image) richdoc.Image {
		id, ok := strings.CutPrefix(img.URL, "cid:")
		if !ok {
			return img
		}
		b, ok := byCID[id]
		if !ok {
			return img
		}
		img.URL = "data:" + sniff(b) + ";base64," + base64.StdEncoding.EncodeToString(b)
		return img
	})
}

// sniff is the media type of some bytes, from their first few.
//
// ⛔ Read from the BYTES and not from the part's Content-Type, because the
// part this resolves to was found by its Content-Id and a sender that mislabels
// a PNG as a JPEG is common. http.DetectContentType is not used: it would pull
// net/http into a package that must have no network code in it at all.
func sniff(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "image/jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif"
	case len(b) > 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(b, []byte("<?xml")), bytes.HasPrefix(b, []byte("<svg")):
		return "image/svg+xml"
	}
	return "application/octet-stream"
}

// mapImages is richdoc's own tree rewrite, named here so the one call site
// reads as what it does.
//
// ⛔ It used to be thirty lines of type switch in this package AND thirty in
// go-richdoc/epub, each with the same hazard: a container the switch forgets
// keeps the un-rewritten node, silently. Two callers is what moved it into
// richdoc v0.7.0, where the node set lives -- so a container the model gains
// cannot be forgotten by one copy and not the other.
func mapImages(blocks []richdoc.Block, f func(richdoc.Image) richdoc.Image) []richdoc.Block {
	return richdoc.MapInlines(blocks, func(in richdoc.Inline) richdoc.Inline {
		if img, ok := in.(richdoc.Image); ok {
			return f(img)
		}
		return in
	})
}
