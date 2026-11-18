package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/waste-not-app/waste-not-backend/internal/config"
)

type ResetMailer interface {
	Check(ctx context.Context) error
	Send(ctx context.Context, email, token string) error
}

type smtpMailer struct {
	host, username, password, from string
	port                           int
}

func NewSMTPMailer(c config.Config) (ResetMailer, error) {
	if c.SMTPHost == "" || c.SMTPUsername == "" || c.SMTPPassword == "" || c.SMTPFrom == "" {
		return nil, nil
	}
	from, err := mail.ParseAddress(c.SMTPFrom)
	if err != nil || strings.ContainsAny(c.SMTPFrom, "\r\n") || strings.ContainsAny(c.SMTPHost, "\r\n /:") {
		return nil, errors.New("invalid SMTP sender or host")
	}
	port := c.SMTPPort
	if port == 0 {
		port = 587
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("invalid SMTP port")
	}
	return &smtpMailer{c.SMTPHost, c.SMTPUsername, c.SMTPPassword, from.Address, port}, nil
}

func (m *smtpMailer) connect(ctx context.Context) (*smtp.Client, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(m.host, strconv.Itoa(m.port)))
	if err != nil {
		cancel()
		return nil, nil, ErrMailUnavailable
	}
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	rawConn := conn
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close() })
	cleanup := func() { stop(); _ = rawConn.Close(); cancel() }
	tlsConfig := &tls.Config{ServerName: m.host, MinVersion: tls.VersionTLS12}
	if m.port == 465 {
		secured := tls.Client(conn, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			cleanup()
			return nil, nil, ErrMailUnavailable
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, m.host)
	if err != nil {
		cleanup()
		return nil, nil, ErrMailUnavailable
	}
	if m.port != 465 {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			cleanup()
			return nil, nil, ErrMailUnavailable
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			cleanup()
			return nil, nil, ErrMailUnavailable
		}
	}
	if err := client.Auth(smtp.PlainAuth("", m.username, m.password, m.host)); err != nil {
		cleanup()
		return nil, nil, ErrMailUnavailable
	}
	return client, cleanup, nil
}

func (m *smtpMailer) Check(ctx context.Context) error {
	client, cleanup, err := m.connect(ctx)
	if err != nil {
		return ErrMailUnavailable
	}
	defer cleanup()
	if err := client.Noop(); err != nil {
		return ErrMailUnavailable
	}
	return nil
}

func (m *smtpMailer) Send(ctx context.Context, email, token string) error {
	recipient, err := mail.ParseAddress(email)
	if err != nil || recipient.Address != email || strings.ContainsAny(email, "\r\n") || len(token) != 43 {
		return ErrMailUnavailable
	}
	client, cleanup, err := m.connect(ctx)
	if err != nil {
		return ErrMailUnavailable
	}
	defer cleanup()
	if err := client.Mail(m.from); err != nil {
		return ErrMailUnavailable
	}
	if err := client.Rcpt(email); err != nil {
		return ErrMailUnavailable
	}
	writer, err := client.Data()
	if err != nil {
		return ErrMailUnavailable
	}
	message := fmt.Sprintf("Date: %s\r\nFrom: %s\r\nTo: %s\r\nSubject: Atur ulang kata sandi WasteNot\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nTempel kode berikut di aplikasi WasteNot untuk mengganti kata sandi:\r\n\r\n%s\r\n\r\nKode berlaku 15 menit sejak permintaan dan hanya dapat digunakan sekali. Permintaan baru membatalkan kode sebelumnya.\r\nJika kamu tidak meminta perubahan ini, abaikan email ini.\r\n", time.Now().UTC().Format(time.RFC1123Z), m.from, email, token)
	if _, err := io.WriteString(writer, message); err != nil {
		_ = writer.Close()
		return ErrMailUnavailable
	}
	if err := writer.Close(); err != nil {
		return ErrMailUnavailable
	}
	return nil
}
