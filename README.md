# email

**RFC 5322 messages and MIME into [richdoc](https://github.com/go-richdoc/richdoc).**
A `.eml` file, or whatever an IMAP server hands back. Pure Go, CGO-free,
including `GOOS=js`.

```go
doc, err := email.Parse(src, email.Limits{})
```

A message is headers and then parts. The headers are metadata and also the
first thing a reader of a printed email looks at; the parts are a **tree**, and
which branch of it is "the message" is a decision rather than a lookup.

## ⛔ Which part is the body is the whole job

| | |
|---|---|
| `multipart/alternative` | the **same** content twice or more, poorest first. The richest one this package can read wins — the HTML, read by [`go-richdoc/html`](https://github.com/go-richdoc/html) |
| `multipart/related` | an HTML part plus the pictures it refers to by `Content-Id`. The `cid:` references are resolved against those parts and inlined |
| `multipart/mixed` | a sequence: text, then attachments. Every text part is read in order, and the attachments are **listed** |
| `multipart/signed` | the message is the first part; the second is the signature, which is not content |
| `message/rfc822` | a forwarded message: its headers and its parts, one level down |

A reader that concatenated an alternative's parts would print every HTML email
**twice**, once as markup-free text and once as itself. That is the commonest
defect in a mail converter, and it is why these five are five cases and not one
loop.

The shape nearly every mail client actually sends is
`alternative{ plain, related{ html, images } }` — so the richest alternative is
often a *nested multipart*, and that is in the tests.

## ⛔ Nothing is fetched, ever

An HTML email's `<img src="https://…">` is a **tracking pixel** as often as it
is a picture, and fetching one tells the sender that a human opened the
message. Remote references keep their URLs untouched; only `cid:` parts that
travelled inside the message are inlined. There is no network code in this
package and no way to ask it for any — `http.DetectContentType` is not used
either, for exactly that reason, so the few image signatures it needs are read
by hand.

## ⛔ The disposition wins over the type

A part the sender marked as an attachment **with a filename** is a file —
including a `.txt` one — and a reader that inlined it because its type happened
to be text would paste a log file into the middle of a letter. A part with no
`Content-Type` at all is `text/plain` by RFC 2045, which is why the order
mattered: `mystery.bin` with no type arrived as the letter's own words until
that check moved above the type switch.

A part this package will not render is **named, with its type and its size** —
`[application/pdf, 1.8 kB, invoice.pdf]` — rather than dropped. A part whose
encoding does not decode says so in the same breath, and the letter it came
with is kept: one broken attachment must not lose a message.

## What a reader of a printed email sees

Five headers — From, To, Cc, Subject, Date — then a rule, then the body. A
message carries thirty: `Received`, `DKIM-Signature`, `X-Spam-Status`. Printing
them all buries the letter under its own envelope.

RFC 2047 encoded words (`=?UTF-8?B?…?=`) are decoded, including the ones in
charsets `mime.WordDecoder` does not know itself — KOI8-R, windows-1251 — and
a word in a charset *nothing* knows is kept as it stands: ugly, and it says
what happened, where an empty subject line looks like a message that had none.

In a plain-text part, **a blank line is a paragraph break and a single newline
is not**: mail is hard-wrapped at seventy-odd columns, and a reader that made a
paragraph of every line would turn every message into a column of fragments. A
run of quoted lines becomes a block quotation.

## ⛔ The limits are the security surface

```go
email.Limits{MaxParts: 1024, MaxPartBytes: 32 << 20, MaxTotalBytes: 128 << 20, MaxDepth: 32}
```

A message is a tree, and a hostile one is a very bushy tree: parts nested a
hundred deep, a thousand empty parts, a base64 attachment that decodes to a
gigabyte. Base64 is four characters for every three bytes, so a part's decoded
size is not its size in the file — which is why the budget is spent on what was
**decoded**.

## How it is checked

- 100% of statements, **no uncovered block**, race detector on, `gofmt` and
  `go vet` clean, ten build targets including `js/wasm`.
- Fixtures are written with **CRLF**, which is what mail uses: a reader tested
  only on `\n` meets `\r\n` the first time it is used in anger.
- Every limit refuses and **names which limit**. Four branches were removed
  rather than covered, each with the reason beside it — a `bytes.Reader` does
  not fail, `mime.ParseMediaType` returns a non-nil map on success, x/text
  substitutes U+FFFD rather than refusing, and a part over the ceiling is
  refused one level up before it could be refused here.
- Base64 wrapped at 76 columns (which the strict decoder refuses), a
  quoted-printable soft line break, a multipart that never closes, a
  `Content-Type` that does not parse, a `message/rfc822` part that is not a
  message, several blank lines between paragraphs.

## This reader is fuzzed, and it finishes a sweep

`FuzzParse` (v0.1.1) asserts no panic, a document OR an error and never
neither, determinism — headers arrive as a map and `Meta` is built from one,
and Go randomises map iteration on purpose — and **the limits measured on the
output**. The unit cases check that each limit *refuses*; the fuzz target
checks the other direction, that what came out respects what was declared,
because a limit counted in the wrong place passes its own unit test and still
lets the document through.

**5.54M executions over 582 interesting inputs found nothing** — the richest
coverage of the family, because MIME is text with a great deal of structure in
it. For contrast, within the same org and the same budget:

| reader | executions | interesting inputs |
|---|---|---|
| `email` (this) | 5.5M | **582** |
| `fb2` | 14.3M | 431 |
| `html` | 1.5M | 490 |
| `latex` | 1.2M | 324 |
| `epub` (a checksummed ZIP) | 1.8M | **32** |

Those numbers are not comparable as assurance: a container refuses mutations
before any of its own code runs, so its execution count reads better than it
is. Six of this family's seven readers had no fuzz target at all before this
round and the two before it; this is the last of the ones that read a format
directly (`go-richdoc/markdown` delegates its tokenising to goldmark, which is
fuzzed upstream).

## The rest of the family

[**richdoc**](https://github.com/go-richdoc/richdoc) is the model, and its
`MapInlines` — added in v0.7.0 because this package and
[`epub`](https://github.com/go-richdoc/epub) had each written the same tree
rewrite — is what resolves a `cid:` here ·
[**html**](https://github.com/go-richdoc/html) reads the HTML part ·
[**epub**](https://github.com/go-richdoc/epub) ·
[**fb2**](https://github.com/go-richdoc/fb2) ·
[**markdown**](https://github.com/go-richdoc/markdown) ·
[**rst**](https://github.com/go-richdoc/rst) ·
[**latex**](https://github.com/go-richdoc/latex) ·
[**data**](https://github.com/go-richdoc/data)

[**go-pdfkit**](https://github.com/go-pdfkit) composes a richdoc document into
a PDF, which is what makes an email into a PDF a two-step with no
format-specific code in between.

## Licence

BSD-3-Clause.
