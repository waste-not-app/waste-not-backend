package auth

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/config"
)

func TestSMTPMailerRefusesPlaintextBeforeAuthentication(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	commands := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			commands <- ""
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = fmt.Fprint(conn, "220 local SMTP\r\n")
		reader := bufio.NewReader(conn)
		var seen strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				commands <- seen.String()
				return
			}
			seen.WriteString(line)
			if strings.HasPrefix(line, "EHLO") {
				_, _ = fmt.Fprint(conn, "250-local\r\n250 AUTH PLAIN\r\n")
			} else {
				_, _ = fmt.Fprint(conn, "250 OK\r\n")
			}
		}
	}()
	host, portString, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	mailer, err := NewSMTPMailer(config.Config{SMTPHost: host, SMTPPort: port, SMTPUsername: "test-user", SMTPPassword: "test-secret", SMTPFrom: "support@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mailer.Check(context.Background()); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("accepted SMTP downgrade: %v", err)
	}
	select {
	case sent := <-commands:
		if strings.Contains(sent, "AUTH ") || strings.Contains(sent, "test-secret") || strings.Contains(sent, "MAIL ") {
			t.Fatal("credentials or recipient sent before verified TLS")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("SMTP probe did not close connection")
	}
}

func TestSMTPMailerMissingConfigurationAndHeaderValidation(t *testing.T) {
	if mailer, err := NewSMTPMailer(config.Config{}); mailer != nil || err != nil {
		t.Fatal("missing configuration must leave reset unavailable")
	}
	_, err := NewSMTPMailer(config.Config{SMTPHost: "smtp.example.com", SMTPUsername: "user", SMTPPassword: "secret", SMTPFrom: "sender@example.com\r\nBcc: attacker@example.com"})
	if err == nil {
		t.Fatal("accepted SMTP header injection")
	}
}

func TestSMTPMailerRejectsUntrustedTLSCertificate(t *testing.T) {
	certificateSource := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(certificateSource.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = fmt.Fprint(conn, "220 local SMTP\r\n")
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadString('\n'); err != nil {
			return
		}
		_, _ = fmt.Fprint(conn, "250-local\r\n250 STARTTLS\r\n")
		line, err := reader.ReadString('\n')
		if err != nil || !strings.HasPrefix(line, "STARTTLS") {
			return
		}
		_, _ = fmt.Fprint(conn, "220 begin TLS\r\n")
		_ = tls.Server(conn, &tls.Config{Certificates: certificateSource.TLS.Certificates, MinVersion: tls.VersionTLS12}).Handshake()
	}()
	host, portString, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portString)
	mailer, err := NewSMTPMailer(config.Config{SMTPHost: host, SMTPPort: port, SMTPUsername: "test-user", SMTPPassword: "test-secret", SMTPFrom: "support@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if err := mailer.Check(context.Background()); !errors.Is(err, ErrMailUnavailable) {
		t.Fatalf("untrusted SMTP certificate accepted: %v", err)
	}
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("TLS probe did not terminate")
	}
}
