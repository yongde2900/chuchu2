package line

import "testing"

func TestEvent_Status(t *testing.T) {
	cases := []struct {
		eventType EventType
		want      Status
	}{
		{EventTypeFollow, StatusFollowing},
		{EventTypeUnfollow, StatusBlocked},
	}

	for _, c := range cases {
		e := Event{UserID: "U1", Type: c.eventType, OccurredAtMillis: 1000}
		if got := e.Status(); got != c.want {
			t.Errorf("Event{Type: %q}.Status() = %v, want %v", c.eventType, got, c.want)
		}
	}
}
