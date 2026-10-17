package email

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/mail"
)

// a message is headers and a body, which is what a .eml file is and what a
// message/rfc822 part holds.
type message struct {
	header mail.Header
	body   []byte
}

// parseHeaders splits a message into its headers and its body.
//
// ⛔ net/mail is what reads the headers, folding, comments and all. A reader
// that split on the first blank line itself would get every message with a
// folded Subject wrong, and mail folds a long Subject as a matter of course.
//
// The body is read whole and unbounded ON PURPOSE: this is only ever called on
// the bytes of a part the caller has already measured against MaxPartBytes, so
// a second limit here would be a second number for the same thing. The read
// cannot fail either, for the reason Parse's cannot: underneath it is a
// bytes.Reader.
func parseHeaders(b []byte) (message, error) {
	m, err := mail.ReadMessage(bufio.NewReader(bytes.NewReader(b)))
	if err != nil {
		return message{}, fmt.Errorf("email: %w", err)
	}
	body, _ := io.ReadAll(m.Body)
	return message{header: m.Header, body: body}, nil
}
