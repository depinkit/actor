// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

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
