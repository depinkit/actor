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
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	bt "gitlab.com/nunet/device-management-service/internal/background_tasks"
	"gitlab.com/nunet/device-management-service/network"
	"gitlab.com/nunet/device-management-service/types"
)

type BasicActor struct {
	dispatch  *Dispatch
	scheduler *bt.Scheduler
	registry  *registry
	network   network.Network
	security  SecurityContext
	limiter   RateLimiter

	params BasicActorParams
	self   Handle

	mx            sync.Mutex
	subscriptions map[string]uint64
}

type BasicActorParams struct{}

var _ Actor = (*BasicActor)(nil)

// New creates a new basic actor.
func New(scheduler *bt.Scheduler, net network.Network, security *BasicSecurityContext, limiter RateLimiter, params BasicActorParams, self Handle, opt ...DispatchOption) (*BasicActor, error) {
	if scheduler == nil {
		return nil, errors.New("scheduler is nil")
	}

	if net == nil {
		return nil, errors.New("network is nil")
	}

	if security == nil {
		return nil, errors.New("security is nil")
	}

	dispatchOptions := []DispatchOption{WithRateLimiter(limiter)}
	dispatchOptions = append(dispatchOptions, opt...)
	dispatch := NewDispatch(security, dispatchOptions...)

	actor := &BasicActor{
		dispatch:      dispatch,
		scheduler:     scheduler,
		registry:      newRegistry(),
		network:       net,
		security:      security,
		limiter:       limiter,
		params:        params,
		self:          self,
		subscriptions: make(map[string]uint64),
	}

	return actor, nil
}

func (a *BasicActor) Start() error {
	// Network messages
	if err := a.network.HandleMessage(
		fmt.Sprintf("actor/%s/messages/0.0.1", a.self.Address.InboxAddress),
		a.handleMessage,
	); err != nil {
		return fmt.Errorf("starting actor: %s: %w", a.self.ID, err)
	}

	// and start the internal goroutines
	a.dispatch.Start()
	a.scheduler.Start()
	return nil
}

func (a *BasicActor) handleMessage(data []byte) {
	var msg Envelope
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Debugf("error unmarshaling message: %s", err)
		return
	}

	if !a.self.ID.Equal(msg.To.ID) {
		log.Warnf("message is not for ourselves: %s %s", a.self.ID, msg.To.ID)
		return
	}

	if !a.limiter.Allow(msg) {
		log.Warnf("incoming message invoking %s not allowed by limiter", msg.Behavior)
		return
	}

	_ = a.Receive(msg)
}

func (a *BasicActor) Context() context.Context {
	return a.dispatch.Context()
}

func (a *BasicActor) Handle() Handle {
	return a.self
}

func (a *BasicActor) Security() SecurityContext {
	return a.security
}

func (a *BasicActor) AddBehavior(behavior string, continuation Behavior, opt ...BehaviorOption) error {
	return a.dispatch.AddBehavior(behavior, continuation, opt...)
}

func (a *BasicActor) RemoveBehavior(behavior string) {
	a.dispatch.RemoveBehavior(behavior)
}

func (a *BasicActor) Receive(msg Envelope) error {
	if a.self.ID.Equal(msg.To.ID) {
		return a.dispatch.Receive(msg)
	}

	if msg.IsBroadcast() {
		return a.dispatch.Receive(msg)
	}

	return fmt.Errorf("bad receiver: %w", ErrInvalidMessage)
}

func (a *BasicActor) Send(msg Envelope) error {
	if msg.To.ID.Equal(a.self.ID) {
		return a.Receive(msg)
	}

	if msg.Signature == nil {
		if msg.Nonce == 0 {
			msg.Nonce = a.security.Nonce()
		}

		invoke := []Capability{Capability(msg.Behavior)}
		var delegate []Capability
		if msg.Options.ReplyTo != "" {
			delegate = append(delegate, Capability(msg.Options.ReplyTo))
		}
		if err := a.security.Provide(&msg, invoke, delegate); err != nil {
			return fmt.Errorf("providing behavior capability for %s: %w", msg.Behavior, err)
		}
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	err = a.network.SendMessage(
		a.Context(),
		msg.To.Address.HostID,
		types.MessageEnvelope{
			Type: types.MessageType(
				fmt.Sprintf("actor/%s/messages/0.0.1", msg.To.Address.InboxAddress),
			),
			Data: data,
		},
		msg.Expiry(),
	)
	if err != nil {
		return fmt.Errorf("sending message to %s: %w", msg.To.ID, err)
	}

	return nil
}

func (a *BasicActor) Invoke(msg Envelope) (<-chan Envelope, error) {
	if msg.Options.ReplyTo == "" {
		msg.Options.ReplyTo = fmt.Sprintf("/dms/actor/replyto/%d", a.security.Nonce())
	}

	result := make(chan Envelope, 1)

	if err := a.dispatch.AddBehavior(
		msg.Options.ReplyTo,
		func(reply Envelope) {
			result <- reply
			close(result)
		},
		WithBehaviorExpiry(msg.Options.Expire),
		WithBehaviorOneShot(true),
	); err != nil {
		return nil, fmt.Errorf("adding reply behavior: %w", err)
	}

	if err := a.Send(msg); err != nil {
		a.dispatch.RemoveBehavior(msg.Options.ReplyTo)
		return nil, fmt.Errorf("sending message: %w", err)
	}

	return result, nil
}

func (a *BasicActor) Publish(msg Envelope) error {
	if !msg.IsBroadcast() {
		return ErrInvalidMessage
	}

	if msg.Signature == nil {
		if msg.Nonce == 0 {
			msg.Nonce = a.security.Nonce()
		}

		broadcast := []Capability{Capability(msg.Behavior)}
		if err := a.security.ProvideBroadcast(&msg, msg.Options.Topic, broadcast); err != nil {
			return fmt.Errorf("providing behavior capability for %s: %w", msg.Behavior, err)
		}
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	if err := a.network.Publish(a.Context(), msg.Options.Topic, data); err != nil {
		return fmt.Errorf("publishing message: %w", err)
	}

	return nil
}

func (a *BasicActor) Subscribe(topic string, setup ...BroadcastSetup) error {
	a.mx.Lock()
	defer a.mx.Unlock()

	_, ok := a.subscriptions[topic]
	if ok {
		return nil
	}

	subID, err := a.network.Subscribe(
		a.Context(),
		topic,
		a.handleBroadcast,
		func(data []byte, validatorData interface{}) (network.ValidationResult, interface{}) {
			return a.validateBroadcast(topic, data, validatorData)
		},
	)
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	for _, f := range setup {
		if err := f(topic); err != nil {
			_ = a.network.Unsubscribe(topic, subID)
			return fmt.Errorf("setup broadcast topic: %w", err)
		}
	}

	a.subscriptions[topic] = subID
	return nil
}

func (a *BasicActor) validateBroadcast(topic string, data []byte, validatorData interface{}) (network.ValidationResult, interface{}) {
	var msg Envelope
	if validatorData != nil {
		if _, ok := validatorData.(Envelope); !ok {
			log.Warnf("bogus pubsub validation data: %v", validatorData)
			return network.ValidationReject, nil
		}
		// we have already validated the message, just short-circuit
		return network.ValidationAccept, validatorData
	} else if err := json.Unmarshal(data, &msg); err != nil {
		return network.ValidationReject, nil
	}

	if !msg.IsBroadcast() {
		return network.ValidationReject, nil
	}

	if msg.Options.Topic != topic {
		return network.ValidationReject, nil
	}

	if msg.Expired() {
		return network.ValidationIgnore, nil
	}

	if err := a.security.Verify(msg); err != nil {
		return network.ValidationReject, nil
	}

	if !a.limiter.Allow(msg) {
		log.Warnf("incoming broadcast message in %s not allowed by limiter", topic)
		return network.ValidationIgnore, nil
	}

	return network.ValidationAccept, msg
}

func (a *BasicActor) handleBroadcast(data []byte) {
	var msg Envelope
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Debugf("error unmarshaling broadcast message: %s", err)
		return
	}

	// don't receive message from self
	if msg.From.Equal(a.Handle()) {
		return
	}

	if err := a.Receive(msg); err != nil {
		log.Warnf("error receiving broadcast message: %s", err)
	}
}

func (a *BasicActor) Stop() error {
	a.dispatch.close()
	for topic, subID := range a.subscriptions {
		err := a.network.Unsubscribe(topic, subID)
		if err != nil {
			log.Debugf("error unsubscribing from %s: %s", topic, err)
		}
	}
	return nil
}

func (a *BasicActor) Limiter() RateLimiter {
	return a.limiter
}
