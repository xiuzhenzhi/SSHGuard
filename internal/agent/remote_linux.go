package agent

import (
	"context"
	"time"
)

func collectRemotePlatform(_ context.Context, _ *spool) (time.Time, error) { return time.Time{}, nil }
func platformJournalAvailable() bool                                       { return journalAvailable() }
