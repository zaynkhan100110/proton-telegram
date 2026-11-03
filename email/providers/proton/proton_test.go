package proton

import (
	"strings"
	"testing"
)

func TestParseMessageID(t *testing.T) {
	folder, uid, err := parseMessageID("INBOX|42")
	if err != nil {
		t.Fatal(err)
	}
	if folder != "INBOX" || uid != 42 {
		t.Fatalf("got %q / %d", folder, uid)
	}
	if _, _, err := parseMessageID("not-a-reference"); err == nil {
		t.Fatal("expected invalid reference error")
	}
}

func TestParseSearchUIDs(t *testing.T) {
	got := parseSearchUIDs("* SEARCH 4 9 21\nP0001 OK SEARCH completed\n")
	if len(got) != 3 || got[0] != 4 || got[2] != 21 {
		t.Fatalf("unexpected UIDs: %#v", got)
	}
}

func TestParseRFC822(t *testing.T) {
	raw := "From: Alice Example <alice@example.com>\r\nTo: Bob <bob@example.com>\r\nSubject: =?UTF-8?Q?Hello_=E2=9C=93?=\r\nDate: Mon, 02 Nov 2026 10:00:00 +0000\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nA test message.\r\n"
	m, err := parseRFC822(17, []byte(raw), "* 1 FETCH (UID 17 FLAGS (\\Seen))")
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Hello ✓" {
		t.Fatalf("unexpected subject: %q", m.Subject)
	}
	if m.From != "Alice Example <alice@example.com>" {
		t.Fatalf("unexpected from: %q", m.From)
	}
	if !m.Read {
		t.Fatal("expected message to be marked read")
	}
	if !strings.Contains(m.BodyText, "A test message") {
		t.Fatalf("unexpected body: %q", m.BodyText)
	}
}

func TestParseRFC822Multipart(t *testing.T) {
	raw := "From: sender@example.com\r\nSubject: multipart\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=demo\r\n\r\n--demo\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\nHello=20from=20Proton\r\n--demo\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<b>Hello</b>\r\n--demo--\r\n"
	m, err := parseRFC822(18, []byte(raw), "* 1 FETCH (UID 18 FLAGS ())")
	if err != nil {
		t.Fatal(err)
	}
	if m.BodyText != "Hello from Proton" {
		t.Fatalf("unexpected plain body: %q", m.BodyText)
	}
	if m.BodyHTML != "<b>Hello</b>" {
		t.Fatalf("unexpected HTML body: %q", m.BodyHTML)
	}
}
