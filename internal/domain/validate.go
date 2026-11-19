package domain

import (
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

func Email(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, e := mail.ParseAddress(s)
	return s, e == nil && a.Address == s && len(s) <= MaxEmailLength && strings.Contains(s, "@") && !strings.ContainsAny(s, "\r\n ")
}

func ValidTimezone(s string) bool {
	if s == "" || s == "Local" {
		return false
	}
	_, e := time.LoadLocation(s)
	return e == nil
}

func ValidUUID(s string) bool {
	u, err := uuid.Parse(s)
	return err == nil && u != uuid.Nil && u.String() == s
}
