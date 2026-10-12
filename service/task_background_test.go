package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackgroundTaskAdmission(t *testing.T) {
	previous := backgroundTaskSlots
	backgroundTaskSlots = make(chan struct{}, 1)
	t.Cleanup(func() { backgroundTaskSlots = previous })
	release, ok := AcquireBackgroundTask()
	require.True(t, ok)
	rejected, ok := AcquireBackgroundTask()
	assert.False(t, ok)
	assert.Nil(t, rejected)
	release()
	release()
	release, ok = AcquireBackgroundTask()
	require.True(t, ok)
	release()
	t.Setenv("TASK_BACKGROUND_TIMEOUT_SECONDS", "45")
	assert.Equal(t, 45*time.Second, BackgroundTaskTimeout())
}
