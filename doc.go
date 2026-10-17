// Package email reads an RFC 5322 message — a .eml file, or anything an IMAP
// server hands back — into the richdoc document model.
//
// A message is headers and then parts. The headers are metadata and also the
// first thing a reader of a printed email looks at; the parts are a tree, and
// which branch of it is "the message" is a decision rather than a lookup.
//
// # ⛔ Which part is the body is the whole job
//
//	multipart/alternative   the SAME content twice or more, poorest first.
//	                        The richest one this package can read wins, which
//	                        is the HTML — read by github.com/go-richdoc/html.
//	multipart/related       an HTML part plus the pictures it refers to by
//	                        Content-ID. The cid: references are resolved
//	                        against those parts and inlined.
//	multipart/mixed         a sequence: text, then attachments. Every text
//	                        part is read in order, and the attachments are
//	                        LISTED rather than decoded.
//	multipart/signed        the message is the first part; the second is the
//	                        signature, which is not content.
//
// A part this package cannot read is named, with its type and its size, rather
// than dropped: "there is a 2.3 MB application/pdf here called invoice.pdf" is
// information, and silence is not.
//
// # ⛔ Nothing is fetched, ever
//
// An HTML email's <img src="https://..."> is a TRACKING PIXEL as often as it
// is a picture, and fetching one tells the sender that a human opened the
// message. Remote references keep their URLs untouched; only cid: parts that
// travelled inside the message are inlined. There is no network code in this
// package and no way to ask it for any.
//
// # The headers a reader sees
//
// From, To, Cc, Subject and Date come out as a short block at the top of the
// document and as entries in the document's metadata. RFC 2047 encoded words
// (=?UTF-8?B?...?=) are decoded, including the ones in the wrong charset,
// because a subject line that reads as mojibake is a defect a reader blames on
// the converter.
//
// Nothing here is CGO, and it builds for GOOS=js.
package email
