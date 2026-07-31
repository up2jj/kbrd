// Package browseredit provides kbrd's loopback browser editor service.
package browseredit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"kbrd/frontmatter"
)

var ErrMalformedFrontmatter = errors.New("card has an unterminated frontmatter block")

// Document is the browser-safe view of a card. Body never contains a valid or
// apparently malformed leading metadata block.
type Document struct {
	Body               string `json:"body"`
	Revision           string `json:"revision"`
	FrontmatterPresent bool   `json:"frontmatterPresent"`
	WYSIWYGSafe        bool   `json:"wysiwygSafe"`
	Warning            string `json:"warning,omitzero"`
}

// ParseDocument exposes only the Markdown body and hashes the complete raw
// document for optimistic concurrency.
func ParseDocument(raw string) Document {
	_, _, body, fenced, apparent := frontmatter.SplitExact(raw)
	doc := Document{
		Body:               body,
		Revision:           Revision(raw),
		FrontmatterPresent: fenced,
		WYSIWYGSafe:        true,
	}
	if apparent && !fenced {
		doc.Body = ""
		doc.WYSIWYGSafe = false
		doc.Warning = ErrMalformedFrontmatter.Error()
	}
	return doc
}

// MergeBody combines body with the exact current metadata prefix. The current
// prefix always wins, which makes conflict retries preserve external metadata.
func MergeBody(currentRaw, body string) (string, error) {
	prefix, _, _, fenced, apparent := frontmatter.SplitExact(currentRaw)
	if apparent && !fenced {
		return "", ErrMalformedFrontmatter
	}
	if fenced {
		return prefix + body, nil
	}
	return body, nil
}

// Revision returns the lowercase SHA-256 of the complete raw card bytes.
func Revision(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
