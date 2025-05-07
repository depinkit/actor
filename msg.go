// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

const (
	defaultMessageTimeout = 30 * time.Second
)

var signaturePrefix = []byte("dms:msg:")

type HealthCheckResponse struct {
	OK    bool
	Error string
}

// Message constructs a new message envelope and applies the options
func Message(src Handle, dest Handle, behavior string, payload interface{}, opt ...MessageOption) (Envelope, error) {
	var data []byte
	if payload == nil || (reflect.ValueOf(payload).Kind() == reflect.Ptr && reflect.ValueOf(payload).IsNil()) {
		data = []byte{}
	} else {
		var err error
		data, err = json.Marshal(payload)
		if err != nil {
			return Envelope{}, fmt.Errorf("marshaling payload: %w", err)
		}
	}

	msg := Envelope{
		To:       dest,
		Behavior: behavior,
		From:     src,
		Message:  data,
		Options: EnvelopeOptions{
			Expire: uint64(time.Now().Add(defaultMessageTimeout).UnixNano()),
		},
		Discard: func() {},
	}

	for _, f := range opt {
		if err := f(&msg); err != nil {
			return Envelope{}, fmt.Errorf("setting message option: %w", err)
		}
	}

	return msg, nil
}

func ReplyTo(msg Envelope, payload interface{}, opt ...MessageOption) (Envelope, error) {
	if msg.Options.ReplyTo == "" {
		return Envelope{}, fmt.Errorf("no behavior to reply to: %w", ErrInvalidMessage)
	}

	msgOptions := []MessageOption{WithMessageExpiry(msg.Options.Expire)}
	msgOptions = append(msgOptions, opt...)
	return Message(msg.To, msg.From, msg.Options.ReplyTo, payload, msgOptions...)
}

// WithMessageContext provides the necessary envelope and signs it.
//
// NOTE: If this option must be passed last, otherwise the signature will be invalidated by further modifications.
//
// NOTE: Signing is implicit in Send.
func WithMessageSignature(sctx SecurityContext, invoke []Capability, delegate []Capability) MessageOption {
	return func(msg *Envelope) error {
		if !msg.From.ID.Equal(sctx.ID()) {
			return ErrInvalidSecurityContext
		}

		msg.Nonce = sctx.Nonce()
		if msg.IsBroadcast() {
			return sctx.ProvideBroadcast(msg, msg.Options.Topic, invoke)
		}

		return sctx.Provide(msg, invoke, delegate)
	}
}

// WithMessageTimeout sets the message expiration from a relative timeout
//
// NOTE: messages created with Message have an implicit timeout of DefaultMessageTimeout
func WithMessageTimeout(timeo time.Duration) MessageOption {
	return func(msg *Envelope) error {
		msg.Options.Expire = uint64(time.Now().Add(timeo).UnixNano())
		return nil
	}
}

// WithMessageExpiry sets the message expiry
//
// NOTE: created with Message message have an implicit timeout of DefaultMessageTimeout
func WithMessageExpiry(expiry uint64) MessageOption {
	return func(msg *Envelope) error {
		msg.Options.Expire = expiry
		return nil
	}
}

// WithMessageExpiry TODO
func WithMessageExpiryTime(t time.Time) MessageOption {
	return func(msg *Envelope) error {
		msg.Options.Expire = uint64(t.UnixNano())
		return nil
	}
}

// WithMessageReplyTo sets the message replyto behavior
//
// NOTE: ReplyTo is set implicitly in Invoke and the appropriate capability
//
//	tokens are delegated by Provide.
func WithMessageReplyTo(replyto string) MessageOption {
	return func(msg *Envelope) error {
		msg.Options.ReplyTo = replyto
		return nil
	}
}

// WithMessageTopic sets the broadcast topic
func WithMessageTopic(topic string) MessageOption {
	return func(msg *Envelope) error {
		msg.Options.Topic = topic
		return nil
	}
}

// WithMessageSource sets the message source
func WithMessageSource(source Handle) MessageOption {
	return func(msg *Envelope) error {
		msg.From = source
		return nil
	}
}

func (msg *Envelope) SignatureData() ([]byte, error) {
	msgCopy := *msg
	msgCopy.Signature = nil
	data, err := json.Marshal(&msgCopy)
	if err != nil {
		return nil, fmt.Errorf("signature data: %w", err)
	}

	result := make([]byte, len(signaturePrefix)+len(data))
	copy(result, signaturePrefix)
	copy(result[len(signaturePrefix):], data)

	return result, nil
}

func (msg *Envelope) Expired() bool {
	return uint64(time.Now().UnixNano()) > msg.Options.Expire
}

// convert the expiration to a time.Time object
func (msg *Envelope) Expiry() time.Time {
	sec := msg.Options.Expire / uint64(time.Second)
	nsec := msg.Options.Expire % uint64(time.Second)
	return time.Unix(int64(sec), int64(nsec))
}

func (msg *Envelope) IsBroadcast() bool {
	return msg.To.Empty() && msg.Options.Topic != ""
}
