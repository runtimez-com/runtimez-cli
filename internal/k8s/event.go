package k8s

// EventInfo is a Kubernetes event as the backend actually returns it.
//
// The row carries uid/name/createdAt and everything worth reading — type, reason, message,
// count, the object it is about — lives inside the `spec` JSON string, the same shape every
// other resource uses. Decoding those as top-level JSON keys yields a struct that is empty
// except for the name, which renders as a table of blanks rather than as an error.
type EventInfo struct {
	Type          string
	Reason        string
	Message       string
	Count         int
	LastTimestamp string
	InvolvedKind  string
	InvolvedName  string
}

// ParseEvent reads one event's spec blob. createdAt is the fallback timestamp for an event
// whose spec carries no lastTimestamp.
func ParseEvent(spec, createdAt string) EventInfo {
	s := parse(spec)
	involved := s.child("involvedObject")

	count, _ := s.num("count")
	ts := s.str("lastTimestamp")
	if ts == "" {
		// Some agent versions have written the alternates; fall through to the row's own
		// creation time rather than rendering an event with no time at all.
		for _, k := range []string{"eventTime", "firstTimestamp"} {
			if v := s.str(k); v != "" {
				ts = v
				break
			}
		}
	}
	if ts == "" {
		ts = createdAt
	}

	return EventInfo{
		Type:          s.str("type"),
		Reason:        s.str("reason"),
		Message:       s.str("message"),
		Count:         count,
		LastTimestamp: ts,
		InvolvedKind:  involved.str("kind"),
		InvolvedName:  involved.str("name"),
	}
}
