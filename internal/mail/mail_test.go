package mail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"sick-fansubs/internal/logging"
)

// fakeSMTPServer speaks the minimal SMTP command sequence the Relay client
// uses: 220 greeting, EHLO (advertising AUTH PLAIN and optionally STARTTLS),
// AUTH PLAIN, MAIL FROM, RCPT TO, DATA (dot-terminated), QUIT. It records
// the received message and whether auth was attempted.
type fakeSMTPServer struct {
	tlsMode          string // "implicit" or "starttls"
	advertises       bool   // advertise AUTH PLAIN in EHLO
	requireTLS       bool   // reject AUTH before TLS
	withholdSTARTTLS bool   // suppress the STARTTLS advertisement
	authReply        string // non-empty replaces the 235 AUTH response
	malformedReply   string // non-empty answers AUTH with a code-less line
	rcptReply        string // non-empty replaces the 250 RCPT response

	mu      sync.Mutex
	got     []string
	authed  bool
	msgBody string
}

func (s *fakeSMTPServer) handle(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	write := func(line string) bool {
		if _, err := fmt.Fprintf(w, "%s\r\n", line); err != nil {
			return false
		}
		return w.Flush() == nil
	}

	if !write("220 fake.relay.test ESMTP") {
		return
	}

	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.mu.Lock()
		s.got = append(s.got, line)
		s.mu.Unlock()

		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"):
			var ext []string
			if s.advertises {
				ext = append(ext, "AUTH PLAIN")
			}
			if s.tlsMode == "starttls" && !s.withholdSTARTTLS {
				ext = append(ext, "STARTTLS")
			}
			resp := "250-fake.relay.test"
			if len(ext) > 0 {
				resp += "\r\n250-" + strings.Join(ext, "\r\n250-")
			}
			resp += "\r\n250 OK"
			if !write(resp) {
				return
			}
		case strings.HasPrefix(upper, "STARTTLS"):
			write("220 Ready")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{testCert()}})
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			br = bufio.NewReader(conn)
			w = bufio.NewWriter(conn)
		case strings.HasPrefix(upper, "AUTH"):
			if !s.advertises {
				write("535 auth not supported")
				continue
			}
			if s.requireTLS {
				if _, isTLS := conn.(*tls.Conn); !isTLS {
					write("538 Encryption required")
					continue
				}
			}
			s.mu.Lock()
			s.authed = true
			s.mu.Unlock()
			if s.malformedReply != "" {
				write(s.malformedReply)
				continue
			}
			if s.authReply != "" {
				write(s.authReply)
				continue
			}
			write("235 ok")
		case strings.HasPrefix(upper, "MAIL FROM"):
			write("250 ok")
		case strings.HasPrefix(upper, "RCPT TO"):
			if s.rcptReply != "" {
				write(s.rcptReply)
				continue
			}
			write("250 ok")
		case strings.HasPrefix(upper, "DATA"):
			write("354 go ahead")
			body, err := readDotTerminated(br)
			if err != nil {
				return
			}
			s.mu.Lock()
			s.msgBody = body
			s.mu.Unlock()
			write("250 queued")
		case strings.HasPrefix(upper, "QUIT"):
			write("221 bye")
			return
		default:
			write("250 ok")
		}
	}
}

func readDotTerminated(br *bufio.Reader) (string, error) {
	var b strings.Builder
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", err
		}
		if line == ".\r\n" {
			return b.String(), nil
		}
		b.WriteString(line)
	}
}

// testCert is a throwaway self-signed cert for the fake server's TLS. It
// carries 127.0.0.1 as an IP SAN so the Relay's real TLS verification
// succeeds against the loopback test listener (no insecure-skip test hook).
var (
	testCertOnce sync.Once
	testCertPEM  tls.Certificate
)

func testCert() tls.Certificate {
	testCertOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		tmpl := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject:      pkix.Name{CommonName: "fake.relay.test"},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(time.Hour),
			IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
		if err != nil {
			panic(err)
		}
		testCertPEM = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	})
	return testCertPEM
}

// startFakeServer runs the fake SMTP server on a loopback port and returns
// the port. The implicit-TLS mode wraps the listener in TLS.
func startFakeServer(t *testing.T, srv *fakeSMTPServer) int {
	t.Helper()

	var ln net.Listener
	var err error
	if srv.tlsMode == "implicit" {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{testCert()}})
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.handle(conn)
		}
	}()
	return port
}

// testRelay builds a Relay whose TLS mode follows the REAL port constants
// (465 = implicit, 587 = STARTTLS) but whose dial address is the fake
// server's dynamic loopback port. dialAddr is the test-only seam: the
// production path derives the address from Host+Port.
func testRelay(port int, fakePort int) *Relay {
	// Trust the fake server's self-signed cert: it is its own CA.
	cert := testCert()
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		panic(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	return &Relay{
		cfg: Config{
			Host:     "127.0.0.1",
			Port:     port,
			Username: "resend",
			Password: "test-key",
			From:     "beta@sickfansubs.com",
		},
		dialAddr:    net.JoinHostPort("127.0.0.1", strconv.Itoa(fakePort)),
		rootCAs:     pool,
		sendTimeout: defaultSendTimeout,
	}
}

func TestSend_ImplicitTLS(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "implicit", advertises: true}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(465, fakePort) // 465 → implicit TLS dial
	if err := relay.Send(t.Context(), "user@example.com", "Θέμα", "Γεια σου.\nΣύνδεσμος."); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !srv.authed {
		t.Error("expected AUTH to be attempted")
	}
	if !strings.Contains(srv.msgBody, "Σύνδεσμος.") {
		t.Errorf("message body missing Greek copy: %q", srv.msgBody)
	}
}

func TestSend_STARTTLS(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "starttls", advertises: true, requireTLS: true}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(587, fakePort) // 587 → plain dial + STARTTLS upgrade
	if err := relay.Send(t.Context(), "user@example.com", "Θέμα", "Σώμα μηνύματος."); err != nil {
		t.Fatalf("Send over STARTTLS: %v", err)
	}
	if !srv.authed {
		t.Error("expected AUTH to be attempted after STARTTLS")
	}
	if !strings.Contains(srv.msgBody, "Σώμα μηνύματος.") {
		t.Errorf("message body missing copy: %q", srv.msgBody)
	}
}

func TestSend_AuthFailureSurfaces(t *testing.T) {
	// AUTH not advertised: the client's Auth must fail cleanly.
	srv := &fakeSMTPServer{tlsMode: "implicit"} // advertises = false
	fakePort := startFakeServer(t, srv)

	relay := testRelay(465, fakePort)
	if err := relay.Send(t.Context(), "u@example.com", "s", "b"); err == nil {
		t.Fatal("got nil, want auth failure")
	}
}

// TestSend_AuthReplyTextNeverReachesTheError: a rejecting relay's response
// text can echo credential material, and the handler logs the send error —
// only the stage and the reply code may survive.
func TestSend_AuthReplyTextNeverReachesTheError(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "implicit", advertises: true, authReply: "535 5.7.8 bad credentials secret-sentinel"}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(465, fakePort)
	err := relay.Send(t.Context(), "u@example.com", "s", "b")
	if err == nil {
		t.Fatal("got nil, want auth failure")
	}
	if !strings.Contains(err.Error(), "smtp auth") || !strings.Contains(err.Error(), "535") {
		t.Errorf("error = %q, want the stage and the reply code", err)
	}
	if strings.Contains(err.Error(), "secret-sentinel") {
		t.Errorf("relay response text reached the error: %q", err)
	}
}

// TestSend_MalformedReplyTextNeverReachesTheError: a code-less reply line is a
// textproto.ProtocolError whose text embeds the raw server bytes — it must be
// flattened like any other relay-controlled reply.
func TestSend_MalformedReplyTextNeverReachesTheError(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "implicit", advertises: true, malformedReply: "oops secret-sentinel"}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(465, fakePort)
	err := relay.Send(t.Context(), "u@example.com", "s", "b")
	if err == nil {
		t.Fatal("got nil, want a malformed-reply failure")
	}
	if !strings.Contains(err.Error(), "smtp auth") || !strings.Contains(err.Error(), "malformed server reply") {
		t.Errorf("error = %q, want the stage and the malformed-reply class", err)
	}
	if strings.Contains(err.Error(), "secret-sentinel") {
		t.Errorf("relay response text reached the error: %q", err)
	}
}

// TestSend_RcptFailureCarriesNoAddress: the RCPT refusal is category-only —
// neither the recipient nor the relay's response text may reach the error
// the handler logs.
func TestSend_RcptFailureCarriesNoAddress(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "implicit", advertises: true, rcptReply: "550 user@example.com rejected: sentinel-relay-text"}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(465, fakePort)
	err := relay.Send(t.Context(), "user@example.com", "s", "b")
	if err == nil {
		t.Fatal("got nil, want rcpt failure")
	}
	if got := err.Error(); got != "smtp rcpt to rejected" {
		t.Errorf("error = %q, want the category-only refusal", got)
	}
}

// TestSend_ImplicitTLSVerifiesTheCertificate: the dial uses the real trust
// roots, so a self-signed relay fails rather than being trusted.
func TestSend_ImplicitTLSVerifiesTheCertificate(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "implicit", advertises: true}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(465, fakePort)
	relay.rootCAs = nil // system roots: the fake server's self-signed cert must fail
	err := relay.Send(t.Context(), "u@example.com", "s", "b")
	if err == nil {
		t.Fatal("Send trusted a self-signed certificate")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Errorf("error = %q, want a certificate-verification failure", err)
	}
}

// TestSend_RefusesAuthWhenSTARTTLSWithheld: a plain port whose server never
// advertises STARTTLS must not receive credentials — smtp.PlainAuth refuses
// an unencrypted connection to a non-local host, and no AUTH line moves.
func TestSend_RefusesAuthWhenSTARTTLSWithheld(t *testing.T) {
	srv := &fakeSMTPServer{tlsMode: "starttls", advertises: true, withholdSTARTTLS: true}
	fakePort := startFakeServer(t, srv)

	relay := testRelay(587, fakePort)
	// PlainAuth exempts localhost; keep the test-seam dial while the
	// configured host stays a real relay name, the production shape.
	relay.cfg.Host = "fake.relay.test"
	err := relay.Send(t.Context(), "u@example.com", "s", "b")
	if err == nil {
		t.Fatal("Send sent credentials over an unencrypted connection")
	}
	if !strings.Contains(err.Error(), "unencrypted connection") {
		t.Errorf("error = %q, want the stdlib unencrypted-connection refusal", err)
	}
	if srv.authed {
		t.Error("AUTH reached the server without TLS")
	}
}

func TestSend_UnreachableServer(t *testing.T) {
	relay := &Relay{cfg: Config{Host: "127.0.0.1", Port: 587, Username: "r", Password: "k", From: "beta@sickfansubs.com"}, dialAddr: "127.0.0.1:1"}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	err := relay.Send(ctx, "u@example.com", "s", "b")
	if err == nil {
		t.Fatal("got nil, want dial failure")
	}
	if _, ok := errors.AsType[net.Error](err); !ok {
		t.Errorf("error = %v, want the wrapped transport cause", err)
	}
}

func TestSend_StallingServerTimesOut(t *testing.T) {
	// A relay that accepts TCP and then never responds must not hold the
	// request goroutine: the dial-time deadline bounds every post-dial
	// command (stdlib net/smtp has no context support).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Accept and hold — no greeting, ever.
			go func(c net.Conn) { <-time.After(30 * time.Second); c.Close() }(conn)
		}
	}()

	relay := testRelay(587, ln.Addr().(*net.TCPAddr).Port)
	// The deadline IS the thing under test — shrink the budget so the pin
	// waits milliseconds instead of the production 10 s (the seam, not a
	// weakened contract: production keeps defaultSendTimeout).
	relay.sendTimeout = 200 * time.Millisecond
	start := time.Now()
	err = relay.Send(t.Context(), "u@example.com", "s", "b")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("got nil, want timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("Send took %v, want ≤ ~2s (the injected deadline must bound post-dial commands)", elapsed)
	}
}

func TestSend_RejectsCRLFInRecipient(t *testing.T) {
	relay := testRelay(587, 1) // dial never happens — rejected first
	if err := relay.Send(t.Context(), "a@b.c\r\nBcc: evil@x.y", "s", "b"); err == nil {
		t.Fatal("got nil, want invalid-recipient error")
	}
}

func TestNewRelay_RequiresHostAndFrom(t *testing.T) {
	if _, err := NewRelay(Config{From: "a@b.c"}); err == nil {
		t.Error("expected host-required error")
	}
	if _, err := NewRelay(Config{Host: "smtp.example.com"}); err == nil {
		t.Error("expected from-required error")
	}
}

func TestBuildMessage(t *testing.T) {
	msg := string(buildMessage("beta@sickfansubs.com", "user@example.com", "Επαναφορά κωδικού", "Γεια σου."))

	for _, want := range []string{
		"From: beta@sickfansubs.com\r\n",
		"To: user@example.com\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n",
		"\r\n\r\nΓεια σου.",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q in:\n%s", want, msg)
		}
	}
	// No Reply-To header — replies to the no-reply From
	// are meant to bounce.
	if strings.Contains(msg, "Reply-To:") {
		t.Errorf("message carries a Reply-To header:\n%s", msg)
	}
	// The Greek subject must be RFC 2047 encoded (raw non-ASCII in headers
	// is invalid).
	headers, _, _ := strings.Cut(msg, "\r\n\r\n")
	if strings.Contains(headers, "Επαναφορά") {
		t.Errorf("raw Greek in headers — subject must be encoded-word:\n%s", headers)
	}
	if !strings.Contains(headers, "Subject: =?utf-8?") {
		t.Errorf("subject not encoded-word encoded:\n%s", headers)
	}
}

// TestLogLink_LogsBodyNotRecipient pins the audited log-hygiene exemption:
// the dev sender writes the message body (the reset link) but never the
// recipient address.
func TestLogLink_LogsBodyNotRecipient(t *testing.T) {
	var buf bytes.Buffer
	ctx := logging.With(t.Context(), slog.New(slog.NewJSONHandler(&buf, nil)))

	if err := (LogLink{}).Send(ctx, "user@example.com", "Θέμα", "Σύνδεσμος: https://example.test/reset?token=abc"); err != nil {
		t.Fatalf("LogLink.Send: %v", err)
	}
	record := map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &record); err != nil {
		t.Fatalf("decode log record: %v", err)
	}
	if got := record["msg"]; got != "dev mail not sent (no SMTP configured)" {
		t.Errorf("msg = %v, want the dev-mail record", got)
	}
	if got, _ := record["subject"].(string); got != "Θέμα" {
		t.Errorf("subject = %q, want Θέμα", got)
	}
	body, _ := record["body"].(string)
	if !strings.Contains(body, "Σύνδεσμος") {
		t.Errorf("body = %q, want the message link", body)
	}
	for key, value := range record {
		if s, ok := value.(string); ok && strings.Contains(s, "user@example.com") {
			t.Errorf("field %q carries the recipient address: %q", key, s)
		}
	}
}

func TestResetEmail(t *testing.T) {
	subject, body := ResetEmail("https://v2.sickfansubs.com/auth/reset?token=abc")
	if subject == "" || !strings.Contains(body, "https://v2.sickfansubs.com/auth/reset?token=abc") {
		t.Errorf("ResetEmail missing link: subject=%q body=%q", subject, body)
	}
	if !strings.Contains(body, "30 λεπτά") {
		t.Errorf("ResetEmail missing expiry note: %q", body)
	}
}

func TestVerifyEmailMessage(t *testing.T) {
	subject, body := VerifyEmail("https://v2.sickfansubs.com/auth/verify?token=abc")
	if subject == "" || !strings.Contains(body, "https://v2.sickfansubs.com/auth/verify?token=abc") {
		t.Errorf("VerifyEmail missing link: subject=%q body=%q", subject, body)
	}
	if !strings.Contains(body, "7 ημέρες") {
		t.Errorf("VerifyEmail missing expiry note: %q", body)
	}
}

func TestEmailChangedNotice(t *testing.T) {
	subject, body := EmailChangedNotice("new@example.com")
	if subject == "" || !strings.Contains(body, "new@example.com") {
		t.Errorf("EmailChangedNotice missing the new address: subject=%q body=%q", subject, body)
	}
	if !strings.Contains(body, "Αν δεν κάνατε εσείς την αλλαγή") {
		t.Errorf("EmailChangedNotice missing the not-you guidance: %q", body)
	}
}
