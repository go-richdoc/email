package email

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// crlf turns a test's readable message into the line endings mail actually
// uses. A reader tested only on "\n" meets "\r\n" the first time it is used
// in anger.
func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(strings.TrimLeft(s, "\n"), "\n", "\r\n"))
}

func parse(t *testing.T, src string) *richdoc.Document {
	t.Helper()
	doc, err := Parse(crlf(src), Limits{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc
}

// shape renders a document one line per block, with markings visible.
func shape(blocks []richdoc.Block, indent string) string {
	var b strings.Builder
	for _, blk := range blocks {
		switch v := blk.(type) {
		case richdoc.Paragraph:
			fmt.Fprintf(&b, "%sp %s\n", indent, text(v.Inlines))
		case richdoc.Heading:
			fmt.Fprintf(&b, "%sh%d %s\n", indent, v.Level, text(v.Inlines))
		case richdoc.BlockQuote:
			fmt.Fprintf(&b, "%squote\n", indent)
			b.WriteString(shape(v.Blocks, indent+"  "))
		case richdoc.ThematicBreak:
			fmt.Fprintf(&b, "%shr\n", indent)
		case richdoc.CodeBlock:
			fmt.Fprintf(&b, "%scode %q\n", indent, v.Text)
		case richdoc.List:
			fmt.Fprintf(&b, "%slist\n", indent)
			for _, it := range v.Items {
				b.WriteString(shape(it.Blocks, indent+"  "))
			}
		default:
			fmt.Fprintf(&b, "%s?%T\n", indent, blk)
		}
	}
	return b.String()
}

func text(in []richdoc.Inline) string {
	var b strings.Builder
	for _, i := range in {
		switch v := i.(type) {
		case richdoc.Text:
			b.WriteString(v.Value)
		case richdoc.Strong:
			fmt.Fprintf(&b, "<strong>%s</strong>", text(v.Inlines))
		case richdoc.Emph:
			fmt.Fprintf(&b, "<em>%s</em>", text(v.Inlines))
		case richdoc.Link:
			fmt.Fprintf(&b, "[%s](%s)", text(v.Inlines), v.URL)
		case richdoc.Image:
			fmt.Fprintf(&b, "![%s](%.40s)", v.Alt, v.URL)
		case richdoc.LineBreak:
			b.WriteString("\\n")
		case richdoc.Code:
			fmt.Fprintf(&b, "`%s`", v.Value)
		default:
			fmt.Fprintf(&b, "?%T", i)
		}
	}
	return b.String()
}

func TestAPlainMessage(t *testing.T) {
	doc := parse(t, `
From: Ada <ada@example.org>
To: Charles <charles@example.org>
Subject: On the Analytical Engine
Date: Mon, 17 Oct 2026 21:00:00 +0200
Message-Id: <1@example.org>
X-Spam-Status: No
Received: from nowhere

The first paragraph, which is
hard-wrapped the way mail is.

The second one.

> you wrote something
> on two lines

and a reply.
`)
	want := "" +
		"p <strong>From: </strong>Ada <ada@example.org>\n" +
		"p <strong>To: </strong>Charles <charles@example.org>\n" +
		"p <strong>Subject: </strong>On the Analytical Engine\n" +
		"p <strong>Date: </strong>Mon, 17 Oct 2026 21:00:00 +0200\n" +
		"hr\n" +
		"p The first paragraph, which is\\nhard-wrapped the way mail is.\n" +
		"p The second one.\n" +
		"quote\n" +
		"  p you wrote something\\non two lines\n" +
		"p and a reply.\n"
	if got := shape(doc.Blocks, ""); got != want {
		t.Errorf("shape =\n%s\nwant\n%s", got, want)
	}

	// ⛔ The envelope is NOT in the document: a message carries thirty headers
	// and printing them all buries the letter.
	all := richdoc.PlainText(doc)
	for _, forbidden := range []string{"X-Spam", "Received", "nowhere"} {
		if strings.Contains(all, forbidden) {
			t.Errorf("the document carries %q, which is envelope and not content", forbidden)
		}
	}

	for k, v := range map[string]string{
		"title":      "On the Analytical Engine",
		"author":     "Ada <ada@example.org>",
		"to":         "Charles <charles@example.org>",
		"message-id": "<1@example.org>",
	} {
		if doc.Meta[k] != v {
			t.Errorf("Meta[%q] = %q, want %q", k, doc.Meta[k], v)
		}
	}
}

// ⛔ THE DECISION THAT DEFINES THIS PACKAGE: an alternative is the same message
// twice, and a reader that concatenated its parts would print every HTML email
// once as markup-free text and once as itself.
func TestAnAlternativeIsReadOnce(t *testing.T) {
	doc := parse(t, `
From: a@example.org
Subject: Two renderings
Content-Type: multipart/alternative; boundary="b"

--b
Content-Type: text/plain; charset=utf-8

The plain rendering, which a reader should not see twice.
--b
Content-Type: text/html; charset=utf-8

<html><body><p>The <b>rich</b> rendering.</p></body></html>
--b--
`)
	all := richdoc.PlainText(doc)
	if !strings.Contains(all, "rich") {
		t.Errorf("the HTML part was not chosen:\n%s", all)
	}
	if strings.Contains(all, "should not see twice") {
		t.Errorf("both parts of the alternative were read:\n%s", all)
	}
}

func TestAnAlternativeWithNothingReadable(t *testing.T) {
	// Two parts this package cannot render: the first is described rather than
	// the message coming back empty.
	doc := parse(t, `
Content-Type: multipart/alternative; boundary="b"

--b
Content-Type: application/x-strange

aaa
--b
Content-Type: application/x-stranger

bbb
--b--
`)
	if got := shape(doc.Blocks, ""); !strings.Contains(got, "application/x-strange") {
		t.Errorf("shape = %q, want the first part described", got)
	}
}

// ⛔ cid: is resolved from the parts that travelled WITH the message; an
// http(s) reference is left exactly as it is, because fetching one tells the
// sender a human opened the mail.
func TestRelatedPicturesAreInlinedAndRemoteOnesAreNot(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nhello"))
	doc := parse(t, `
Subject: With a picture
Content-Type: multipart/related; boundary="b"

--b
Content-Type: text/html

<html><body>
<p><img src="cid:logo@example" alt="the logo"></p>
<p><img src="https://tracker.example/pixel.gif" alt="a pixel"></p>
<p><img src="cid:absent@example" alt="missing"></p>
</body></html>
--b
Content-Type: image/png
Content-Id: <logo@example>
Content-Transfer-Encoding: base64

`+png+`
--b--
`)
	var inlined, remote, kept int
	richdoc.Walk(doc, visitor(func(n any) {
		img, ok := n.(richdoc.Image)
		if !ok {
			return
		}
		switch {
		case strings.HasPrefix(img.URL, "data:image/png;base64,"):
			inlined++
		case strings.HasPrefix(img.URL, "https://"):
			remote++
		default:
			kept++
		}
	}))
	if inlined != 1 {
		t.Errorf("inlined = %d, want the one cid: picture", inlined)
	}
	if remote != 1 {
		t.Errorf("remote = %d, want the tracking pixel's URL untouched", remote)
	}
	if kept != 1 {
		t.Errorf("kept = %d, want the cid: nothing answers left as it is", kept)
	}
}

func TestMixedListsWhatItWillNotRender(t *testing.T) {
	doc := parse(t, `
Subject: With an attachment
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: text/plain

Here is the invoice.
--b
Content-Type: application/pdf; name="invoice.pdf"
Content-Disposition: attachment; filename="invoice.pdf"
Content-Transfer-Encoding: base64

`+base64.StdEncoding.EncodeToString([]byte(strings.Repeat("%PDF-1.7 ", 200)))+`
--b--
`)
	got := shape(doc.Blocks, "")
	if !strings.Contains(got, "Here is the invoice.") {
		t.Errorf("the text part is missing:\n%s", got)
	}
	if !strings.Contains(got, "invoice.pdf") || !strings.Contains(got, "application/pdf") {
		t.Errorf("the attachment is not named:\n%s", got)
	}
	if !strings.Contains(got, "1.8 kB") {
		t.Errorf("the attachment's size is not given:\n%s", got)
	}
}

func TestAForwardedMessage(t *testing.T) {
	doc := parse(t, `
From: b@example.org
Subject: Fwd: the original
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: text/plain

See below.
--b
Content-Type: message/rfc822

From: a@example.org
Subject: the original
Date: Sun, 16 Oct 2026 10:00:00 +0200

The original words.
--b--
`)
	got := shape(doc.Blocks, "")
	for _, want := range []string{"Subject: </strong>Fwd: the original", "See below.",
		"Subject: </strong>the original", "The original words."} {
		if !strings.Contains(got, want) {
			t.Errorf("the document does not say %q:\n%s", want, got)
		}
	}
}

func TestAMessageRfc822PartThatIsNotAMessage(t *testing.T) {
	doc := parse(t, `
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: message/rfc822

!!! not headers at all
--b--
`)
	if got := shape(doc.Blocks, ""); !strings.Contains(got, "not headers at all") {
		t.Errorf("shape = %q, want the bytes shown as text", got)
	}
}

func TestASignedMessageIsItsFirstPart(t *testing.T) {
	doc := parse(t, `
Subject: Signed
Content-Type: multipart/signed; protocol="application/pgp-signature"; boundary="b"

--b
Content-Type: text/plain

The signed words.
--b
Content-Type: application/pgp-signature

-----BEGIN PGP SIGNATURE-----
not really
-----END PGP SIGNATURE-----
--b--
`)
	all := richdoc.PlainText(doc)
	if !strings.Contains(all, "The signed words.") {
		t.Errorf("the message is missing:\n%s", all)
	}
	if strings.Contains(all, "PGP SIGNATURE") {
		t.Errorf("the signature was read as content:\n%s", all)
	}
}

func TestAnEmptySignedOrAlternative(t *testing.T) {
	for _, mediaType := range []string{"multipart/signed", "multipart/alternative", "multipart/mixed"} {
		doc := parse(t, "Content-Type: "+mediaType+"; boundary=\"b\"\n\n--b--\n")
		if len(doc.Blocks) != 0 {
			t.Errorf("%s with no parts gave %d blocks, want none", mediaType, len(doc.Blocks))
		}
	}
}

func TestEncodedWordsInHeaders(t *testing.T) {
	doc := parse(t, `
From: =?UTF-8?B?QWRhIExvdmVsYWNl?= <ada@example.org>
Subject: =?ISO-8859-1?Q?Caf=E9_et_cro=EFssants?=
Content-Type: text/plain

x
`)
	if got := doc.Meta["title"]; got != "Café et croïssants" {
		t.Errorf("Subject = %q, want it decoded from ISO-8859-1", got)
	}
	if got := doc.Meta["author"]; !strings.Contains(got, "Ada Lovelace") {
		t.Errorf("From = %q, want the base64 word decoded", got)
	}
}

// ⛔ A word in a charset nothing here knows is kept AS IT STANDS. It is ugly
// and it says what happened; an empty subject line looks like a message that
// had none.
func TestAnUndecodableEncodedWordIsKept(t *testing.T) {
	doc := parse(t, `
Subject: =?x-made-up-charset?B?aGVsbG8=?=
Content-Type: text/plain

x
`)
	if got := doc.Meta["title"]; !strings.Contains(got, "x-made-up-charset") {
		t.Errorf("Subject = %q, want the undecoded word kept", got)
	}
}

func TestCharsets(t *testing.T) {
	// "Café" in ISO-8859-1 is 43 61 66 E9.
	doc := parse(t, "Content-Type: text/plain; charset=iso-8859-1\n\nCaf\xe9\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "Café") {
		t.Errorf("text = %q, want it decoded from ISO-8859-1", all)
	}
	// A charset nothing recognises is read as UTF-8 rather than refused.
	doc = parse(t, "Content-Type: text/plain; charset=unknown-8bit\n\nplain words\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "plain words") {
		t.Errorf("text = %q, want the bytes read as UTF-8", all)
	}
	// And bytes that are not valid in the charset they claim are kept rather
	// than lost.
	doc = parse(t, "Content-Type: text/plain; charset=utf-8\n\nbroken \xff\xfe bytes\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "broken") {
		t.Errorf("text = %q, want what could be read", all)
	}
}

func TestTransferEncodings(t *testing.T) {
	for _, tc := range []struct{ name, header, body, want string }{
		{"base64", "base64", base64.StdEncoding.EncodeToString([]byte("decoded words")), "decoded words"},
		{
			// ⛔ Wrapped at 76 columns, which is how mail writes base64 and
			// what the strict decoder refuses.
			name: "base64 wrapped across lines", header: "base64",
			body: wrap(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("long ", 40))), 76),
			want: "long long",
		},
		// ⛔ The soft line break comes straight after the word: writing
		// "Caf=C3=A9 =\n and more" gives "Café  and more" with TWO spaces,
		// which is correct quoted-printable and was my fixture being wrong
		// about the format rather than the reader being wrong about it.
		{"quoted-printable", "quoted-printable", "Caf=C3=A9=\n and more", "Café and more"},
		{"7bit", "7bit", "as it stands", "as it stands"},
		{"an encoding nobody has heard of", "x-uuencode-ish", "as it stands", "as it stands"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := parse(t, "Content-Type: text/plain\nContent-Transfer-Encoding: "+tc.header+"\n\n"+tc.body+"\n")
			if all := richdoc.PlainText(doc); !strings.Contains(all, tc.want) {
				t.Errorf("text = %q, want it to contain %q", all, tc.want)
			}
		})
	}
}

// ⛔ A part whose encoding does not decode is DESCRIBED, not refused: one
// broken attachment must not lose the letter it came with.
func TestAPartThatDoesNotDecodeIsDescribed(t *testing.T) {
	doc := parse(t, `
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: text/plain

The letter.
--b
Content-Type: application/pdf; name="broken.pdf"
Content-Transfer-Encoding: base64

!!!!not base64 at all!!!!
--b--
`)
	got := shape(doc.Blocks, "")
	if !strings.Contains(got, "The letter.") {
		t.Errorf("the letter was lost:\n%s", got)
	}
	if !strings.Contains(got, "did not decode") || !strings.Contains(got, "broken.pdf") {
		t.Errorf("the broken part is not described:\n%s", got)
	}
}

func TestOtherFlavoursOfTextAreShown(t *testing.T) {
	doc := parse(t, "Content-Type: text/calendar\n\nBEGIN:VCALENDAR\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "VCALENDAR") {
		t.Errorf("text = %q, want the words shown rather than named", all)
	}
	// Unless it says it is an attachment, in which case it is named.
	doc = parse(t, "Content-Type: text/csv; name=\"rows.csv\"\nContent-Disposition: attachment\n\na,b\n")
	if got := shape(doc.Blocks, ""); !strings.Contains(got, "rows.csv") {
		t.Errorf("shape = %q, want the attachment named", got)
	}
}

func TestAMultipartWithNoBoundary(t *testing.T) {
	doc := parse(t, "Content-Type: multipart/mixed\n\nThe body, unsplittable.\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "unsplittable") {
		t.Errorf("text = %q, want the raw body shown", all)
	}
}

func TestAContentTypeThatDoesNotParse(t *testing.T) {
	// A broken parameter list: the type before the semicolon is still right.
	doc := parse(t, "Content-Type: text/plain; charset=\"unterminated\n\nthe words\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "the words") {
		t.Errorf("text = %q, want the type read up to the semicolon", all)
	}
	// And one with no semicolon at all that still does not parse.
	doc = parse(t, "Content-Type: text/plain\x00broken\n\nmore words\n")
	if all := richdoc.PlainText(doc); !strings.Contains(all, "more words") {
		t.Errorf("text = %q, want the message read anyway", all)
	}
}

func TestLimits(t *testing.T) {
	many := "Content-Type: multipart/mixed; boundary=\"b\"\n\n"
	for range 30 {
		many += "--b\nContent-Type: text/plain\n\nx\n"
	}
	many += "--b--\n"

	for _, tc := range []struct {
		name string
		lim  Limits
		src  string
		says string
	}{
		{"too many parts", Limits{MaxParts: 5}, many, "more than 5 parts"},
		{
			name: "one part too large", lim: Limits{MaxPartBytes: 50},
			src:  "Content-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/plain\n\n" + strings.Repeat("x", 500) + "\n--b--\n",
			says: "over the 50 limit",
		},
		{
			name: "the whole message too large", lim: Limits{MaxTotalBytes: 100},
			src:  "Content-Type: text/plain\n\n" + strings.Repeat("x", 500) + "\n",
			says: "larger than the 100 byte limit",
		},
		{
			name: "the decoded total too large", lim: Limits{MaxTotalBytes: 400, MaxPartBytes: 1000},
			src:  "Content-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/plain\n\n" + strings.Repeat("x", 150) + "\n--b\nContent-Type: text/plain\n\n" + strings.Repeat("y", 150) + "\n--b--\n",
			says: "decoded, over the 400 limit",
		},
		{
			name: "parts nested too deep", lim: Limits{MaxDepth: 2},
			src:  "Content-Type: multipart/mixed; boundary=\"a\"\n\n--a\nContent-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: multipart/mixed; boundary=\"c\"\n\n--c\nContent-Type: text/plain\n\nx\n--c--\n--b--\n--a--\n",
			says: "nested deeper than 2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(crlf(tc.src), tc.lim)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("error = %v, want it to say %q", err, tc.says)
			}
			if !strings.HasPrefix(err.Error(), "email:") {
				t.Errorf("error = %v, want it to name this package", err)
			}
		})
	}
}

func TestAnHTMLPartPastTheHTMLReadersLimits(t *testing.T) {
	deep := strings.Repeat("<div>", 50) + "x" + strings.Repeat("</div>", 50)
	_, err := Parse(crlf("Content-Type: text/html\n\n"+deep+"\n"), Limits{MaxDepth: 5})
	if err == nil || !strings.Contains(err.Error(), "an HTML part") {
		t.Fatalf("error = %v, want the HTML part named", err)
	}
}

func TestWhatIsNotAMessage(t *testing.T) {
	if _, err := Parse([]byte("\x00\x01\x02 no headers here"), Limits{}); err == nil {
		t.Error("a file of bytes was read as a message")
	}
}

func TestAMessageWithNoHeadersWorthShowing(t *testing.T) {
	// No From, To, Subject or Date: no header block, no metadata, just words.
	doc := parse(t, "X-Only: envelope\nContent-Type: text/plain\n\nthe words\n")
	if doc.Meta != nil {
		t.Errorf("Meta = %v, want nil", doc.Meta)
	}
	if got := shape(doc.Blocks, ""); got != "p the words\n" {
		t.Errorf("shape = %q, want the words alone", got)
	}
}

func TestSniff(t *testing.T) {
	for want, b := range map[string]string{
		"image/png":                "\x89PNG\r\n\x1a\nrest",
		"image/jpeg":               "\xff\xd8\xffrest",
		"image/gif":                "GIF89a...",
		"image/webp":               "RIFF0000WEBPVP8",
		"image/svg+xml":            "<svg xmlns=\"\"/>",
		"application/octet-stream": "who knows",
	} {
		if got := sniff([]byte(b)); got != want {
			t.Errorf("sniff(%.10q) = %q, want %q", b, got, want)
		}
	}
	if got := sniff([]byte("<?xml version=\"1.0\"?><svg/>")); got != "image/svg+xml" {
		t.Errorf("an XML declaration before the svg gave %q", got)
	}
	if got := sniff([]byte("GIF87a")); got != "image/gif" {
		t.Errorf("the older GIF signature gave %q", got)
	}
}

func TestFilenameFromEitherHeader(t *testing.T) {
	for _, tc := range []struct{ name, headers, want string }{
		{"from the disposition", "Content-Disposition: attachment; filename=\"a.pdf\"\nContent-Type: application/pdf", "a.pdf"},
		{"from the type's name", "Content-Type: application/pdf; name=\"b.pdf\"\nContent-Disposition: attachment", "b.pdf"},
		{"encoded, as RFC 2047 allows", "Content-Disposition: attachment; filename=\"=?UTF-8?Q?caf=C3=A9.pdf?=\"\nContent-Type: application/pdf", "café.pdf"},
		{"neither", "Content-Type: application/pdf\nContent-Disposition: attachment", ""},
		{"a disposition that does not parse", "Content-Type: application/pdf\nContent-Disposition: attachment; filename=\"unterminated", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := parse(t, "Content-Type: multipart/mixed; boundary=\"b\"\n\n--b\n"+tc.headers+"\n\nbytes\n--b--\n")
			got := shape(doc.Blocks, "")
			if tc.want == "" {
				// ⛔ Asserted as the WHOLE line: the description always carries
				// a comma — "[application/pdf, 5 bytes]" — so looking for one
				// was a test that could never pass.
				if want := "p <em>[application/pdf, 5 bytes]</em>\n"; got != want {
					t.Errorf("shape = %q, want %q with no filename in it", got, want)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("shape = %q, want the filename %q", got, tc.want)
			}
		})
	}
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		12:            "12 bytes",
		2048:          "2.0 kB",
		3 * 1 << 20:   "3.0 MB",
		1<<20 + 1<<19: "1.5 MB",
	} {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

// wrap breaks a string every n characters, as a mail transfer agent does.
func wrap(s string, n int) string {
	var out []string
	for len(s) > n {
		out = append(out, s[:n])
		s = s[n:]
	}
	return strings.Join(append(out, s), "\n")
}

// visitor adapts a function to richdoc.Visitor.
type visitor func(any)

func (v visitor) Enter(n any) bool { v(n); return true }
func (v visitor) Leave(any)        {}
