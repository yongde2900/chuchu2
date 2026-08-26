package line

import (
	"context"
	"errors"
	"testing"
)

// fakeRepository 記錄每次 Upsert 收到的 *User，可設定回傳的錯誤來模擬失敗。
type fakeRepository struct {
	upserted []*User
	err      error
}

func (f *fakeRepository) Upsert(_ context.Context, u *User) error {
	if f.err != nil {
		return f.err
	}
	f.upserted = append(f.upserted, u)
	return nil
}

func TestService_Handle_EmptyInput_NoRepoCalls(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo)

	if err := svc.Handle(context.Background(), nil); err != nil {
		t.Fatalf("Handle(nil) returned error: %v", err)
	}
	if err := svc.Handle(context.Background(), []Event{}); err != nil {
		t.Fatalf("Handle([]Event{}) returned error: %v", err)
	}

	if len(repo.upserted) != 0 {
		t.Fatalf("expected zero Upsert calls, got %d", len(repo.upserted))
	}
}

func TestService_Handle_CallsUpsertInOrder(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo)

	events := []Event{
		{UserID: "U1", Type: EventTypeFollow, OccurredAtMillis: 100},
		{UserID: "U2", Type: EventTypeUnfollow, OccurredAtMillis: 200},
		{UserID: "U1", Type: EventTypeUnfollow, OccurredAtMillis: 300},
	}

	if err := svc.Handle(context.Background(), events); err != nil {
		t.Fatalf("Handle returned unexpected error: %v", err)
	}

	if len(repo.upserted) != len(events) {
		t.Fatalf("expected %d Upsert calls, got %d", len(events), len(repo.upserted))
	}

	wantUsers := []struct {
		userID            string
		status            Status
		lastEventAtMillis int64
	}{
		{"U1", StatusFollowing, 100},
		{"U2", StatusBlocked, 200},
		{"U1", StatusBlocked, 300},
	}

	for i, want := range wantUsers {
		got := repo.upserted[i]
		if got.UserID != want.userID {
			t.Errorf("upserted[%d].UserID = %q, want %q", i, got.UserID, want.userID)
		}
		if got.Status != want.status {
			t.Errorf("upserted[%d].Status = %v, want %v", i, got.Status, want.status)
		}
		if got.LastEventAtMillis != want.lastEventAtMillis {
			t.Errorf("upserted[%d].LastEventAtMillis = %d, want %d", i, got.LastEventAtMillis, want.lastEventAtMillis)
		}
		if got.CreatedAt.IsZero() {
			t.Errorf("upserted[%d].CreatedAt is zero", i)
		}
		if got.UpdatedAt.IsZero() {
			t.Errorf("upserted[%d].UpdatedAt is zero", i)
		}
	}
}

func TestService_Handle_FirstErrorAborts(t *testing.T) {
	sentinel := errors.New("upsert boom")
	repo := &fakeRepository{err: sentinel}
	svc := NewService(repo)

	events := []Event{
		{UserID: "U1", Type: EventTypeFollow, OccurredAtMillis: 100},
		{UserID: "U2", Type: EventTypeFollow, OccurredAtMillis: 200},
	}

	err := svc.Handle(context.Background(), events)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected error to wrap sentinel via errors.Is, got: %v", err)
	}
	if len(repo.upserted) != 0 {
		t.Fatalf("expected zero successful Upsert records (repo always errors), got %d", len(repo.upserted))
	}
}
