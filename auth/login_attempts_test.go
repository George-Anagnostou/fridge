package auth

import (
	"testing"
	"time"
)

func TestAllowLoginAttemptLimitAndPrune(t *testing.T) {
	db := setupDB(t)
	now := time.Date(2026, 11, 12, 16, 0, 0, 0, time.UTC)

	for i := 0; i < LoginAttemptLimit; i++ {
		ok, err := AllowLoginAttempt(db, "1.2.3.4", now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		if !ok {
			t.Fatalf("attempt %d unexpectedly blocked", i+1)
		}
	}

	ok, err := AllowLoginAttempt(db, "1.2.3.4", now.Add(time.Duration(LoginAttemptLimit)*time.Second))
	if err != nil {
		t.Fatalf("limit attempt: %v", err)
	}
	if ok {
		t.Fatal("expected 11th attempt in the window to be blocked")
	}

	ok, err = AllowLoginAttempt(db, "9.9.9.9", now.Add(time.Minute))
	if err != nil || !ok {
		t.Fatalf("other IP AllowLoginAttempt = %v, %v, want true", ok, err)
	}

	later := now.Add(LoginAttemptWindow + time.Minute)
	ok, err = AllowLoginAttempt(db, "1.2.3.4", later)
	if err != nil || !ok {
		t.Fatalf("after window AllowLoginAttempt = %v, %v, want true", ok, err)
	}

	cutoff := later.Add(-LoginAttemptWindow).Format(time.RFC3339)
	var old int
	if err := db.QueryRow(`SELECT COUNT(*) FROM login_attempts WHERE at <= ?`, cutoff).Scan(&old); err != nil {
		t.Fatalf("count old: %v", err)
	}
	if old != 0 {
		t.Fatalf("old rows after prune = %d, want 0", old)
	}
}
