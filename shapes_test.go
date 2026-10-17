package email

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/go-richdoc/richdoc"
)

// ⛔ The shapes a real message takes that a hand-written fixture does not
// think of. Each of these reaches a branch nothing else does, and the coverage
// profile is what said which ones were missing.
func TestTheShapesRealMailTakes(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("\x89PNG\r\n\x1a\nx"))

	t.Run("an alternative whose richest part is a nested multipart", func(t *testing.T) {
		// This is what every mail client sends: alternative{plain, related{html, images}}.
		doc := parse(t, `
Subject: The usual shape
Content-Type: multipart/alternative; boundary="a"

--a
Content-Type: text/plain

the plain one
--a
Content-Type: multipart/related; boundary="r"

--r
Content-Type: text/html

<html><body><p>the <b>rich</b> one <img src="cid:i@x" alt="pic"></p></body></html>
--r
Content-Type: image/png
Content-Id: <i@x>
Content-Transfer-Encoding: base64

`+png+`
--r--
--a--
`)
		all := richdoc.PlainText(doc)
		if !strings.Contains(all, "rich") {
			t.Errorf("the nested related part was not chosen as the richest:\n%s", all)
		}
		if strings.Contains(all, "the plain one") {
			t.Errorf("both alternatives were read:\n%s", all)
		}
		var inlined int
		richdoc.Walk(doc, visitor(func(n any) {
			if img, ok := n.(richdoc.Image); ok && strings.HasPrefix(img.URL, "data:image/png") {
				inlined++
			}
		}))
		if inlined != 1 {
			t.Errorf("inlined = %d, want the cid: picture resolved inside the nested part", inlined)
		}
	})

	t.Run("an alternative part with no Content-Type at all", func(t *testing.T) {
		doc := parse(t, `
Content-Type: multipart/alternative; boundary="a"

--a

a part with no type, which is text by default
--a
Content-Type: application/x-binary

bytes
--a--
`)
		if all := richdoc.PlainText(doc); !strings.Contains(all, "no type") {
			t.Errorf("the untyped part was not chosen:\n%s", all)
		}
	})

	t.Run("a related part with no Content-Id", func(t *testing.T) {
		doc := parse(t, `
Content-Type: multipart/related; boundary="r"

--r
Content-Type: text/html

<html><body><p><img src="cid:i@x" alt="pic"></p></body></html>
--r
Content-Type: image/png
Content-Transfer-Encoding: base64

`+png+`
--r--
`)
		// Nothing answers the cid:, so the href stays — and the picture part
		// itself is described rather than lost.
		got := shape(doc.Blocks, "")
		if !strings.Contains(got, "cid:i@x") {
			t.Errorf("the unresolved reference was not kept:\n%s", got)
		}
		if !strings.Contains(got, "image/png") {
			t.Errorf("the picture part was not described:\n%s", got)
		}
	})

	t.Run("an attachment with no Content-Type", func(t *testing.T) {
		doc := parse(t, `
Content-Type: multipart/mixed; boundary="b"

--b
Content-Disposition: attachment; filename="mystery.bin"

bytes
--b--
`)
		if got := shape(doc.Blocks, ""); !strings.Contains(got, "an unnamed type") {
			t.Errorf("shape = %q, want the type said to be missing", got)
		}
	})

	t.Run("a multipart that never closes", func(t *testing.T) {
		// ⛔ No "--b--" at the end: a truncated message. What was read is
		// kept, because half a letter is more than none.
		doc := parse(t, `
Content-Type: multipart/mixed; boundary="b"

--b
Content-Type: text/plain

the only part, and the message stops here
`)
		if all := richdoc.PlainText(doc); !strings.Contains(all, "stops here") {
			t.Errorf("a truncated multipart lost its part:\n%s", all)
		}
	})

	t.Run("an encoded word in a charset net/mail does not know itself", func(t *testing.T) {
		// ⛔ mime.WordDecoder handles UTF-8 and ISO-8859-1 on its own; KOI8-R
		// reaches the CharsetReader this package gives it, which is the line
		// no other fixture covers. "Привет" in KOI8-R:
		doc := parse(t, "Subject: =?KOI8-R?B?8NLJ18XU?=\nContent-Type: text/plain\n\nx\n")
		if got := doc.Meta["title"]; got != "Привет" {
			t.Errorf("Subject = %q, want it decoded from KOI8-R", got)
		}
	})

	t.Run("several blank lines between paragraphs", func(t *testing.T) {
		// ⛔ Mail does this constantly — a reply with three blank lines before
		// the signature — and the empty stretches must not become empty
		// paragraphs a writer then renders as blank pages.
		doc := parse(t, "Content-Type: text/plain\n\nfirst\n\n\n\nsecond\n\n\n\n\n\nthird\n")
		if got := shape(doc.Blocks, ""); got != "p first\np second\np third\n" {
			t.Errorf("shape = %q, want three paragraphs and nothing between them", got)
		}
	})

	t.Run("a forwarded message whose own part is refused", func(t *testing.T) {
		deep := strings.Repeat("<div>", 40) + "x" + strings.Repeat("</div>", 40)
		src := "Content-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: message/rfc822\n\n" +
			"From: a@example.org\nContent-Type: text/html\n\n" + deep + "\n--b--\n"
		_, err := Parse(crlf(src), Limits{MaxDepth: 6})
		if err == nil {
			t.Fatal("a refusal inside a forwarded message was swallowed")
		}
		if !strings.Contains(err.Error(), "an HTML part") {
			t.Errorf("error = %v, want the HTML part named", err)
		}
	})

	t.Run("an inner part that is refused refuses the message", func(t *testing.T) {
		// A mixed message whose HTML part is past the HTML reader's depth: the
		// error has to travel out of the loop rather than be swallowed.
		deep := strings.Repeat("<div>", 40) + "x" + strings.Repeat("</div>", 40)
		_, err := Parse(crlf("Content-Type: multipart/mixed; boundary=\"b\"\n\n--b\nContent-Type: text/html\n\n"+deep+"\n--b--\n"), Limits{MaxDepth: 6})
		if err == nil {
			t.Fatal("the refusal was swallowed inside the loop")
		}
		if !strings.Contains(err.Error(), "an HTML part") {
			t.Errorf("error = %v, want the HTML part named", err)
		}
	})
}
