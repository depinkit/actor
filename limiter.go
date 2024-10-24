// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"strings"
	"sync"
)

// NoRateLimiter is the null limiter, that does not rate limit
type NoRateLimiter struct{}

var _ RateLimiter = NoRateLimiter{}

type BasicRateLimiter struct {
	cfg RateLimiterConfig

	mx              sync.Mutex
	activeBroadcast int
	activeTopics    map[string]int
	activePublic    int
}

var _ RateLimiter = (*BasicRateLimiter)(nil)

// implementation
func (l NoRateLimiter) Allow(_ Envelope) bool         { return true }
func (l NoRateLimiter) Acquire(_ Envelope) error      { return nil }
func (l NoRateLimiter) Release(_ Envelope)            {}
func (l NoRateLimiter) Config() RateLimiterConfig     { return RateLimiterConfig{} }
func (l NoRateLimiter) SetConfig(_ RateLimiterConfig) {}

func DefaultRateLimiterConfig() RateLimiterConfig {
	return RateLimiterConfig{
		PublicLimitAllow:      4096,
		PublicLimitAcquire:    4112,
		BroadcastLimitAllow:   1024,
		BroadcastLimitAcquire: 1040,
		TopicDefaultLimit:     128,
	}
}

func (cfg *RateLimiterConfig) Valid() bool {
	return cfg.PublicLimitAllow > 0 &&
		cfg.PublicLimitAcquire >= cfg.PublicLimitAllow &&
		cfg.BroadcastLimitAllow > 0 &&
		cfg.BroadcastLimitAcquire >= cfg.BroadcastLimitAllow &&
		cfg.TopicDefaultLimit > 0
}

func NewRateLimiter(cfg RateLimiterConfig) RateLimiter {
	return &BasicRateLimiter{
		cfg:          cfg,
		activeTopics: make(map[string]int),
	}
}

func (l *BasicRateLimiter) Allow(msg Envelope) bool {
	if msg.IsBroadcast() {
		return l.allowBroadcast(msg)
	}

	if isPublicBehavior(msg) {
		return l.allowPublic()
	}

	return true
}

func (l *BasicRateLimiter) allowPublic() bool {
	l.mx.Lock()
	defer l.mx.Unlock()

	return l.activePublic < l.cfg.PublicLimitAllow
}

func (l *BasicRateLimiter) allowBroadcast(msg Envelope) bool {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.activeBroadcast >= l.cfg.BroadcastLimitAllow {
		return false
	}

	topic := msg.Options.Topic
	active := l.activeTopics[topic]
	topicLimit, ok := l.cfg.TopicLimit[topic]
	if !ok {
		return active < l.cfg.TopicDefaultLimit
	}

	return active < topicLimit
}

func (l *BasicRateLimiter) Acquire(msg Envelope) error {
	if msg.IsBroadcast() {
		return l.acquireBroadcast(msg)
	}

	if isPublicBehavior(msg) {
		return l.acquirePublic()
	}

	return nil
}

func (l *BasicRateLimiter) acquirePublic() error {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.activePublic >= l.cfg.PublicLimitAcquire {
		return ErrRateLimitExceeded
	}

	l.activePublic++
	return nil
}

func (l *BasicRateLimiter) acquireBroadcast(msg Envelope) error {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.activeBroadcast >= l.cfg.BroadcastLimitAcquire {
		return ErrRateLimitExceeded
	}

	topic := msg.Options.Topic
	active := l.activeTopics[topic]
	topicLimit, ok := l.cfg.TopicLimit[topic]
	if ok {
		if active >= topicLimit {
			return ErrRateLimitExceeded
		}
	} else if active >= l.cfg.TopicDefaultLimit {
		return ErrRateLimitExceeded
	}

	active++
	l.activeTopics[topic] = active
	l.activeBroadcast++

	return nil
}

func (l *BasicRateLimiter) Release(msg Envelope) {
	if msg.IsBroadcast() {
		l.releaseBroadcast(msg)
	} else if isPublicBehavior(msg) {
		l.releasePublic()
	}
}

func (l *BasicRateLimiter) releasePublic() {
	l.mx.Lock()
	defer l.mx.Unlock()

	l.activePublic--
}

func (l *BasicRateLimiter) releaseBroadcast(msg Envelope) {
	l.mx.Lock()
	defer l.mx.Unlock()

	topic := msg.Options.Topic
	active, ok := l.activeTopics[topic]

	if !ok {
		return
	}

	active--
	if active > 0 {
		l.activeTopics[topic] = active
	} else {
		delete(l.activeTopics, topic)
	}

	l.activeBroadcast--
}

func (l *BasicRateLimiter) Config() RateLimiterConfig {
	l.mx.Lock()
	defer l.mx.Unlock()

	return l.cfg
}

func (l *BasicRateLimiter) SetConfig(cfg RateLimiterConfig) {
	l.mx.Lock()
	defer l.mx.Unlock()

	l.cfg = cfg
}

func isPublicBehavior(msg Envelope) bool {
	return strings.HasPrefix(msg.Behavior, "/public/")
}
