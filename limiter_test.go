// Copyright 2024 Nunet
// Licensed under the Apache License, Version 2.0 (the “License”);
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an “AS IS” BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package actor

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsPublicBehavior(t *testing.T) {
	t.Parallel()

	require.True(t, isPublicBehavior(Envelope{Behavior: "/public/x"}))
	require.False(t, isPublicBehavior(Envelope{Behavior: "/anything/x"}))
}

func TestLimiterHelpersDefaults(t *testing.T) {
	t.Parallel()

	def := DefaultRateLimiterConfig()
	require.True(t, def.Valid(), "default config should be valid")
}

func TestNoRateLimiter(t *testing.T) {
	t.Parallel()

	nrl := NoRateLimiter{}
	env := Envelope{Behavior: "/public/anything"}

	require.True(t, nrl.Allow(env))
	require.NoError(t, nrl.Acquire(env))
	nrl.Release(env)
}

func TestDynamicConfigUpdate(t *testing.T) {
	t.Parallel()

	cfg := DefaultRateLimiterConfig()
	rl := NewRateLimiter(cfg).(*BasicRateLimiter)

	// tighten public limits to zero → Allow should reject
	cfg.PublicLimitAllow = 0
	cfg.PublicLimitAcquire = 0
	rl.SetConfig(cfg)

	got := rl.Config()
	require.Zero(t, got.PublicLimitAllow)
	require.False(t, rl.Allow(Envelope{Behavior: "/public/x"}))
}

func TestBroadcastDefaultVsCustomLimit(t *testing.T) {
	t.Parallel()

	cfg := RateLimiterConfig{
		BroadcastLimitAllow:   2,
		BroadcastLimitAcquire: 2,
		TopicDefaultLimit:     1,
		TopicLimit: map[string]int{
			"custom": 2,
		},
		PublicLimitAllow:   1,
		PublicLimitAcquire: 1,
	}
	rl := NewRateLimiter(cfg)

	defMsg := Envelope{Behavior: "/broadcast", Options: EnvelopeOptions{Topic: "default"}}
	customMsg := Envelope{Behavior: "/broadcast", Options: EnvelopeOptions{Topic: "custom"}}

	require.NoError(t, rl.Acquire(defMsg))
	require.ErrorIs(t, rl.Acquire(defMsg), ErrRateLimitExceeded)

	require.NoError(t, rl.Acquire(customMsg))
	require.ErrorIs(t, rl.Acquire(customMsg), ErrRateLimitExceeded)
}

func TestReleaseIgnoresUnknownTopic(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(DefaultRateLimiterConfig()).(*BasicRateLimiter)
	start := rl.activeBroadcast

	rl.Release(Envelope{Behavior: "/broadcast", Options: EnvelopeOptions{Topic: "absent"}})
	require.Equal(t, start, rl.activeBroadcast)
}

func TestRateLimiterConfigValidFalseCases(t *testing.T) {
	t.Parallel()

	bad := []RateLimiterConfig{
		{PublicLimitAllow: 0, PublicLimitAcquire: 1, BroadcastLimitAllow: 1, BroadcastLimitAcquire: 1, TopicDefaultLimit: 1},
		{PublicLimitAllow: 2, PublicLimitAcquire: 1, BroadcastLimitAllow: 1, BroadcastLimitAcquire: 1, TopicDefaultLimit: 1},
		{PublicLimitAllow: 1, PublicLimitAcquire: 1, BroadcastLimitAllow: 0, BroadcastLimitAcquire: 1, TopicDefaultLimit: 1},
		{PublicLimitAllow: 1, PublicLimitAcquire: 1, BroadcastLimitAllow: 1, BroadcastLimitAcquire: 0, TopicDefaultLimit: 1},
		{PublicLimitAllow: 1, PublicLimitAcquire: 1, BroadcastLimitAllow: 1, BroadcastLimitAcquire: 1, TopicDefaultLimit: 0},
	}
	for _, cfg := range bad {
		require.False(t, cfg.Valid(), "config %+v should be invalid", cfg)
	}
}

func TestConcurrencySafety(t *testing.T) {
	t.Parallel()

	rl := NewRateLimiter(DefaultRateLimiterConfig())
	msg := Envelope{Behavior: "/public/foo"}

	wg := sync.WaitGroup{}
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rl.Allow(msg) {
				if rl.Acquire(msg) == nil {
					rl.Release(msg)
				}
			}
		}()
	}
	wg.Wait()
}

func TestBasicLimiter(t *testing.T) {
	t.Parallel()
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
