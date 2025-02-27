// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"context"
	"time"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/did"
	"gitlab.com/nunet/device-management-service/lib/ucan"
)

type (
	ID         = crypto.ID
	DID        = did.DID
	Capability = ucan.Capability
)

// ActorHandle is a handle for naming an actor reachable in the network
type Handle struct {
	ID      ID      `json:"id"`
	DID     DID     `json:"did"`
	Address Address `json:"addr"`
}

// ActorAddress is a raw actor address representation
type Address struct {
	HostID       string `json:"host,omitempty"`
	InboxAddress string `json:"inbox,omitempty"`
}

// Envelope is the envelope for messages in the actor system
type Envelope struct {
	To         Handle          `json:"to"`
	Behavior   string          `json:"be"`
	From       Handle          `json:"from"`
	Nonce      uint64          `json:"nonce"`
	Options    EnvelopeOptions `json:"opt"`
	Message    []byte          `json:"msg"`
	Capability []byte          `json:"cap,omitempty"`
	Signature  []byte          `json:"sig,omitempty"`

	Discard func() `json:"-"`
}

// EnvelopeOptions are sender specified options for processing an envelope
type EnvelopeOptions struct {
	Expire  uint64 `json:"exp"`
	ReplyTo string `json:"cont,omitempty"`
	Topic   string `json:"topic,omitempty"`
}

// Actor is the local interface to the actor system
type Actor interface {
	Context() context.Context
	Handle() Handle
	Security() SecurityContext

	AddBehavior(behavior string, continuation Behavior, opt ...BehaviorOption) error
	RemoveBehavior(behavior string)

	Receive(msg Envelope) error
	Send(msg Envelope) error
	Invoke(msg Envelope) (<-chan Envelope, error)

	Publish(msg Envelope) error
	Subscribe(topic string, setup ...BroadcastSetup) error

	Start() error
	Stop() error

	// TODO: add child termination strategies
	// e.g.: childSelfRelease which relies on a func `f` to self release and terminate
	// the child actor
	CreateChild(id string, super Handle, opts ...CreateChildOption) (Actor, error)

	Parent() Handle
	Children() map[did.DID]Handle

	Limiter() RateLimiter
}

// ActorSecurityContext provides a context for which to perform cryptographic operations
// for an actor.
// This includes:
// - signing messages
// - verifying message signatures
// - requiring capabilities
// - granting capabilities
type SecurityContext interface {
	ID() ID
	DID() DID
	Nonce() uint64
	PrivKey() crypto.PrivKey

	// Require checks the capability token(s).
	// It succeeds if and only if
	// - the signature is valid
	// - the capability token(s) in the envelope grants the origin actor ID/DID
	//   any of the specified capabilities.
	Require(msg Envelope, invoke []Capability) error

	// Provide populates the envelope with necessary capability tokens and signs it.
	// the envelope is modified in place
	Provide(msg *Envelope, invoke []Capability, delegate []Capability) error

	// Require verifies the envelope and checks the capability tokens
	// for a broadcast topic
	RequireBroadcast(msg Envelope, topic string, broadcast []Capability) error

	// ProvideBroadcast populates the envelope with the necessary capability tokens
	// for broadcast in the topic and signs it
	ProvideBroadcast(msg *Envelope, topic string, broadcast []Capability) error

	// Verify verifies the message signature in an envelope
	Verify(msg Envelope) error
	// Sign signs an envelope; the envelope is modified in place.
	Sign(msg *Envelope) error

	// Grant grants the specified capabilities to the specified audience.
	//
	// Useful for granting capabilities between actors without sending
	// tokens to each other.
	Grant(sub, aud did.DID, caps []ucan.Capability, expiry time.Duration) error

	// Discard discards unwanted tokens from a consumed envelope
	Discard(msg Envelope)

	// Return the capability context
	Capability() ucan.CapabilityContext
}

// RateLimiter implements a stateful resource access limiter
// This is necessary to combat spam attacks and ensure that our system does not
// become overloaded with too many goroutines.
type RateLimiter interface {
	Allow(msg Envelope) bool
	Acquire(msg Envelope) error
	Release(msg Envelope)

	Config() RateLimiterConfig
	SetConfig(cfg RateLimiterConfig)
}

type RateLimiterConfig struct {
	PublicLimitAllow      int
	PublicLimitAcquire    int
	BroadcastLimitAllow   int
	BroadcastLimitAcquire int
	TopicDefaultLimit     int
	TopicLimit            map[string]int
}

type (
	Behavior      func(msg Envelope)
	MessageOption func(msg *Envelope) error
)

type BehaviorOption func(opt *BehaviorOptions) error

type BehaviorOptions struct {
	Capability []Capability
	Expire     uint64
	OneShot    bool
	Topic      string
}

type BroadcastSetup func(topic string) error

type CreateChildOption func(*CreateChildOptions)

type CreateChildOptions struct {
	PrivKey crypto.PrivKey
}

// WithPrivKey sets a specific private key for the child actor
func WithPrivKey(privKey crypto.PrivKey) CreateChildOption {
	return func(o *CreateChildOptions) {
		o.PrivKey = privKey
	}
}
