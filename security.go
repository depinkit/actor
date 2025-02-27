// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"fmt"
	"sync"
	"time"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/did"
	"gitlab.com/nunet/device-management-service/lib/ucan"
)

type BasicSecurityContext struct {
	id    ID
	privk crypto.PrivKey

	cap ucan.CapabilityContext

	mx    sync.Mutex
	nonce uint64
}

var _ SecurityContext = (*BasicSecurityContext)(nil)

func NewBasicSecurityContext(pubk crypto.PubKey, privk crypto.PrivKey, capCxt ucan.CapabilityContext) (*BasicSecurityContext, error) {
	sctx := &BasicSecurityContext{
		privk: privk,
		cap:   capCxt,
		nonce: uint64(time.Now().UnixNano()),
	}

	var err error
	sctx.id, err = crypto.IDFromPublicKey(pubk)
	if err != nil {
		return nil, fmt.Errorf("creating security context: %w", err)
	}

	return sctx, nil
}

func (s *BasicSecurityContext) ID() ID {
	return s.id
}

func (s *BasicSecurityContext) DID() DID {
	return s.cap.DID()
}

func (s *BasicSecurityContext) Nonce() uint64 {
	s.mx.Lock()
	defer s.mx.Unlock()

	nonce := s.nonce
	s.nonce++

	return nonce
}

func (s *BasicSecurityContext) PrivKey() crypto.PrivKey {
	return s.privk
}

func (s *BasicSecurityContext) Require(msg Envelope, invoke []Capability) error {
	// if we are sending to self, nothing to do, signature is alredady verified
	if s.id.Equal(msg.From.ID) && s.id.Equal(msg.To.ID) {
		return nil
	}

	// first consume the capability tokens in the envelope
	if err := s.cap.Consume(msg.From.DID, msg.Capability); err != nil {
		return fmt.Errorf("consuming capabilities: %w", err)
	}

	// check if any of the requested invocation capabilities are delegated
	if err := s.cap.Require(s.DID(), msg.From.ID, s.id, invoke); err != nil {
		s.cap.Discard(msg.Capability)
		return fmt.Errorf("requiring capabilities: %w", err)
	}

	return nil
}

func (s *BasicSecurityContext) Provide(msg *Envelope, invoke []Capability, delegate []Capability) error {
	// if we are sending to self, nothing to do, just Sign
	if s.id.Equal(msg.From.ID) && s.id.Equal(msg.To.ID) {
		return s.Sign(msg)
	}

	tokens, err := s.cap.Provide(msg.To.DID, s.id, msg.To.ID, msg.Options.Expire, invoke, delegate)
	if err != nil {
		return fmt.Errorf("providing capabilities: %w", err)
	}

	msg.Capability = tokens
	return s.Sign(msg)
}

func (s *BasicSecurityContext) RequireBroadcast(msg Envelope, topic string, broadcast []Capability) error {
	if !msg.IsBroadcast() {
		return fmt.Errorf("not a broadcast message: %w", ErrInvalidMessage)
	}

	if topic != msg.Options.Topic {
		return fmt.Errorf("broadcast topic mismatch: %w", ErrInvalidMessage)
	}

	// first consume the capability tokens in the envelope
	if err := s.cap.Consume(msg.From.DID, msg.Capability); err != nil {
		return fmt.Errorf("consuming capabilities: %w", err)
	}

	// check if any of the requested invocation capabilities are delegated
	if err := s.cap.RequireBroadcast(s.DID(), msg.From.ID, topic, broadcast); err != nil {
		s.cap.Discard(msg.Capability)
		return fmt.Errorf("requiring capabilities: %w", err)
	}

	return nil
}

func (s *BasicSecurityContext) ProvideBroadcast(msg *Envelope, topic string, broadcast []Capability) error {
	if !msg.IsBroadcast() {
		return fmt.Errorf("not a broadcast message: %w", ErrInvalidMessage)
	}

	if topic != msg.Options.Topic {
		return fmt.Errorf("broadcast topic mismatch: %w", ErrInvalidMessage)
	}

	tokens, err := s.cap.ProvideBroadcast(msg.From.ID, topic, msg.Options.Expire, broadcast)
	if err != nil {
		return fmt.Errorf("providing capabilities: %w", err)
	}

	msg.Capability = tokens
	return s.Sign(msg)
}

func (s *BasicSecurityContext) Verify(msg Envelope) error {
	if msg.Expired() {
		return ErrMessageExpired
	}

	pubk, err := crypto.PublicKeyFromID(msg.From.ID)
	if err != nil {
		return fmt.Errorf("public key from id: %w", err)
	}

	data, err := msg.SignatureData()
	if err != nil {
		return fmt.Errorf("signature data: %w", err)
	}

	ok, err := pubk.Verify(data, msg.Signature)
	if err != nil {
		return fmt.Errorf("verify message signature: %w", err)
	}
	if !ok {
		return ErrSignatureVerification
	}

	return nil
}

func (s *BasicSecurityContext) Sign(msg *Envelope) error {
	if !s.id.Equal(msg.From.ID) {
		return ErrBadSender
	}

	data, err := msg.SignatureData()
	if err != nil {
		return fmt.Errorf("signature data: %w", err)
	}

	sig, err := s.privk.Sign(data)
	if err != nil {
		return fmt.Errorf("signing message: %w", err)
	}

	msg.Signature = sig
	return nil
}

func (s *BasicSecurityContext) Grant(
	sub, aud did.DID, caps []ucan.Capability, expiry time.Duration,
) error {
	tokens, err := s.cap.Grant(
		ucan.Delegate,
		sub,
		aud,
		[]string{},
		MakeExpiry(expiry),
		1,
		caps,
	)
	if err != nil {
		return fmt.Errorf("create granting token for audience %s caps: %w", aud, err)
	}

	err = s.cap.AddRoots([]did.DID{}, tokens, ucan.TokenList{}, ucan.TokenList{})
	if err != nil {
		return fmt.Errorf("add roots for audience %s: %w", aud, err)
	}

	return nil
}

func (s *BasicSecurityContext) Discard(msg Envelope) {
	s.cap.Discard(msg.Capability)
}

func (s *BasicSecurityContext) Capability() ucan.CapabilityContext {
	return s.cap
}
