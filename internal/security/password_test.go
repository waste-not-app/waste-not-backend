package security

import "testing"

func TestPassword(t *testing.T) {
	a, e := Hash("password-example-123")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := Hash("password-example-123")
	if a == b || !Verify("password-example-123", a) || Verify("password-example-124", a) {
		t.Fatal("salt or verification failed")
	}
}
