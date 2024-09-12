package actor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBasicLimiter(t *testing.T) {
	cfg := RateLimiterConfig{
		PublicLimitAllow:      1,
		PublicLimitAcquire:    2,
		BroadcastLimitAllow:   2,
		BroadcastLimitAcquire: 3,
		TopicDefaultLimit:     1,
		TopicLimit: map[string]int{
			"/broadcast/1": 2,
		},
	}

	limiter := NewRateLimiter(cfg)

	msg1, err := Message(Handle{}, Handle{}, "/public/1", nil)
	require.NoError(t, err)

	require.Equal(t, true, limiter.Allow(msg1))
	require.NoError(t, limiter.Acquire(msg1))
	require.Equal(t, 1, limiter.(*BasicRateLimiter).activePublic)
	require.Equal(t, false, limiter.Allow(msg1))
	require.NoError(t, limiter.Acquire(msg1))
	require.Equal(t, 2, limiter.(*BasicRateLimiter).activePublic)
	require.Error(t, limiter.Acquire(msg1))
	limiter.Release(msg1)
	limiter.Release(msg1)
	require.Equal(t, 0, limiter.(*BasicRateLimiter).activePublic)
	require.Equal(t, true, limiter.Allow(msg1))

	msg2, err := Message(Handle{}, Handle{}, "/broadcast", nil, WithMessageTopic("/broadcast/1"))
	require.NoError(t, err)

	msg3, err := Message(Handle{}, Handle{}, "/broadcast", nil, WithMessageTopic("/broadcast/2"))
	require.NoError(t, err)

	require.Equal(t, true, limiter.Allow(msg2))
	require.Equal(t, true, limiter.Allow(msg3))
	require.NoError(t, limiter.Acquire(msg2))
	require.Equal(t, 1, limiter.(*BasicRateLimiter).activeBroadcast)
	require.Equal(t, 1, limiter.(*BasicRateLimiter).activeTopics[msg2.Options.Topic])
	require.Equal(t, true, limiter.Allow(msg2))
	require.Equal(t, true, limiter.Allow(msg3))
	require.NoError(t, limiter.Acquire(msg2))
	require.Equal(t, 2, limiter.(*BasicRateLimiter).activeBroadcast)
	require.Equal(t, 2, limiter.(*BasicRateLimiter).activeTopics[msg2.Options.Topic])
	require.Error(t, limiter.Acquire(msg2))
	require.Equal(t, false, limiter.Allow(msg2))
	require.Equal(t, false, limiter.Allow(msg3))
	require.NoError(t, limiter.Acquire(msg3))
	require.Equal(t, 3, limiter.(*BasicRateLimiter).activeBroadcast)
	require.Equal(t, 1, limiter.(*BasicRateLimiter).activeTopics[msg3.Options.Topic])
	require.Error(t, limiter.Acquire(msg3))
	limiter.Release(msg3)
	require.Equal(t, 2, limiter.(*BasicRateLimiter).activeBroadcast)
	require.Equal(t, 2, limiter.(*BasicRateLimiter).activeTopics[msg2.Options.Topic])
	require.Equal(t, 0, limiter.(*BasicRateLimiter).activeTopics[msg3.Options.Topic])
	limiter.Release(msg2)
	require.Equal(t, 1, limiter.(*BasicRateLimiter).activeBroadcast)
	require.Equal(t, 1, limiter.(*BasicRateLimiter).activeTopics[msg2.Options.Topic])
	require.Equal(t, true, limiter.Allow(msg2))
	require.Equal(t, true, limiter.Allow(msg3))
	limiter.Release(msg2)
	require.Equal(t, 0, limiter.(*BasicRateLimiter).activeBroadcast)
	require.Equal(t, 0, limiter.(*BasicRateLimiter).activeTopics[msg2.Options.Topic])
	require.Equal(t, 0, limiter.(*BasicRateLimiter).activeTopics[msg3.Options.Topic])
}
