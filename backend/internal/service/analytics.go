package service

import (
	"context"
	"time"

	"github.com/AnnaBorzen/avito-ai-buddy/backend/internal/domain"
)

// record writes an analytics fact. Analytics is not part of the user scenario,
// so a failure is logged and never rolls the scenario back.
func (s *Service) record(ctx context.Context, event domain.AnalyticsEvent) {
	event.EventID = domain.EventID(s.newID("evt"))
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if err := s.store.InsertEvent(ctx, event); err != nil {
		s.log.ErrorContext(ctx, "write analytics event",
			"event", event.Event, "task_instance_id", event.TaskInstanceID, "error", err)
	}
}
