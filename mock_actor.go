package actor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/depinkit/crypto"
	"github.com/depinkit/did"
	"github.com/depinkit/network"
)

// MockActor is a simplified actor implementation that uses virtualNet for communications
// but without the dispatch and registry complexity of BasicActor
type MockActor struct {
	network    network.Network
	security   SecurityContext
	supervisor Handle
	limiter    RateLimiter

	parent   Handle
	children map[did.DID]Handle

	self Handle

	mx            sync.Mutex
	behaviors     map[string]Behavior
	subscriptions map[string]uint64
}

// NewMockActor creates a new mock actor
func NewMockActor(
	supervisor Handle,
	net network.Network,
	security SecurityContext,
	limiter RateLimiter,
	self Handle,
) (*MockActor, error) {
	if net == nil {
		return nil, errors.New("network is nil")
	}

	if security == nil {
		return nil, errors.New("security is nil")
	}

	actor := &MockActor{
		network:       net,
		security:      security,
		limiter:       limiter,
		supervisor:    supervisor,
		self:          self,
		behaviors:     make(map[string]Behavior),
		subscriptions: make(map[string]uint64),
		children:      make(map[did.DID]Handle),
	}

	return actor, nil
}

func (a *MockActor) Context() context.Context {
	return context.Background()
}

func (a *MockActor) Handle() Handle {
	return a.self
}

func (a *MockActor) Supervisor() Handle {
	return a.supervisor
}

func (a *MockActor) Security() SecurityContext {
	return a.security
}

func (a *MockActor) AddBehavior(behavior string, continuation Behavior, opt ...BehaviorOption) error {
	st := &BehaviorState{
		cont: continuation,
		opt: BehaviorOptions{
			Capability: []Capability{Capability(behavior)},
		},
	}

	for _, f := range opt {
		if err := f(&st.opt); err != nil {
			return fmt.Errorf("adding behavior: %w", err)
		}
	}
	a.mx.Lock()
	defer a.mx.Unlock()
	a.behaviors[behavior] = continuation
	return nil
}

func (a *MockActor) RemoveBehavior(behavior string) {
	a.mx.Lock()
	defer a.mx.Unlock()
	delete(a.behaviors, behavior)
}

func (a *MockActor) Receive(msg Envelope) error {
	a.mx.Lock()
	behavior, ok := a.behaviors[msg.Behavior]
	a.mx.Unlock()

	if !ok {
		return fmt.Errorf("no behavior registered for %s", msg.Behavior)
	}

	msg = Envelope{
		From:       msg.From,
		To:         msg.To,
		Message:    msg.Message,
		Behavior:   msg.Behavior,
		Options:    msg.Options,
		Signature:  msg.Signature,
		Capability: msg.Capability,
		Nonce:      msg.Nonce,
		Discard:    func() {},
	}
	behavior(msg)
	return nil
}

func (a *MockActor) Send(msg Envelope) error {
	if msg.To.ID.Equal(a.self.ID) {
		return a.Receive(msg)
	}

	// Sign the message
	if err := a.security.Sign(&msg); err != nil {
		return fmt.Errorf("signing message: %w", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	protocol := fmt.Sprintf("actor/%s/messages/0.0.1", msg.To.Address.InboxAddress)

	err = a.network.SendMessage(
		a.Context(),
		msg.To.Address.HostID,
		MessageEnvelope{
			Type: MessageType(protocol),
			Data: data,
		},
		msg.Expiry(),
	)
	if err != nil {
		return fmt.Errorf("sending message to %s: %w", msg.To.ID, err)
	}

	return nil
}

func (a *MockActor) Invoke(msg Envelope) (<-chan Envelope, error) {
	if msg.Options.ReplyTo == "" {
		msg.Options.ReplyTo = fmt.Sprintf("/dms/actor/replyto/%d", a.security.Nonce())
	}

	result := make(chan Envelope, 1)

	if err := a.AddBehavior(
		msg.Options.ReplyTo,
		func(reply Envelope) {
			result <- reply
			close(result)
		},
	); err != nil {
		return nil, fmt.Errorf("adding reply behavior: %w", err)
	}

	if err := a.Send(msg); err != nil {
		a.RemoveBehavior(msg.Options.ReplyTo)
		return nil, fmt.Errorf("sending message: %w", err)
	}

	return result, nil
}

func (a *MockActor) CreateChild(
	id string,
	super Handle,
	opts ...CreateChildOption,
) (Actor, error) {
	// Create default options
	privk, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	if err != nil {
		return nil, fmt.Errorf("failed to create a child actor: %w", err)
	}

	options := &CreateChildOptions{
		PrivKey: privk,
	}

	// apply caller's options
	for _, opt := range opts {
		opt(options)
	}

	sctx, err := NewBasicSecurityContext(options.PrivKey.GetPublic(), options.PrivKey, a.security.Capability())
	if err != nil {
		return nil, fmt.Errorf("failed to create a child actor: %w", err)
	}

	child, err := NewMockActor(
		super,
		a.network,
		sctx,
		a.limiter,
		Handle{
			ID:  sctx.ID(),
			DID: sctx.DID(),
			Address: Address{
				HostID:       a.self.Address.HostID,
				InboxAddress: id,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create a child actor: %w", err)
	}

	a.mx.Lock()
	child.parent = a.Handle()
	a.children[child.Handle().DID] = child.Handle()
	a.mx.Unlock()

	return child, nil
}

func (a *MockActor) Parent() Handle {
	return a.parent
}

func (a *MockActor) Children() map[did.DID]Handle {
	a.mx.Lock()
	defer a.mx.Unlock()

	c := make(map[did.DID]Handle)
	for did, handle := range a.children {
		c[did] = handle
	}

	return c
}

func (a *MockActor) Publish(msg Envelope) error {
	if !msg.IsBroadcast() {
		return ErrInvalidMessage
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

func (a *MockActor) Subscribe(topic string, setup ...BroadcastSetup) error {
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

func (a *MockActor) validateBroadcast(topic string, data []byte, validatorData interface{}) (network.ValidationResult, interface{}) {
	var msg Envelope
	if validatorData != nil {
		if _, ok := validatorData.(Envelope); !ok {
			return network.ValidationReject, nil
		}
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
		return network.ValidationIgnore, nil
	}

	return network.ValidationAccept, msg
}

func (a *MockActor) handleBroadcast(data []byte) {
	var msg Envelope
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	// don't receive message from self
	if msg.From.Equal(a.Handle()) {
		return
	}

	if err := a.Receive(msg); err != nil {
		return
	}
}

func (a *MockActor) Start() error {
	// Network messages
	if err := a.network.HandleMessage(
		fmt.Sprintf("actor/%s/messages/0.0.1", a.self.Address.InboxAddress),
		a.handleMessage,
	); err != nil {
		return fmt.Errorf("starting actor: %s: %w", a.self.ID, err)
	}

	return nil
}

func (a *MockActor) handleMessage(data []byte, peerID peer.ID) {
	var msg Envelope
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	if !a.self.ID.Equal(msg.To.ID) {
		return
	}

	if msg.From.Address.HostID != peerID.String() {
		return
	}

	_ = a.Receive(msg)
}

func (a *MockActor) Stop() error {
	for topic, subID := range a.subscriptions {
		err := a.network.Unsubscribe(topic, subID)
		if err != nil {
			return err
		}
	}

	a.network.UnregisterMessageHandler(fmt.Sprintf("actor/%s/messages/0.0.1", a.self.Address.InboxAddress))
	return nil
}

func (a *MockActor) Limiter() RateLimiter {
	return a.limiter
}

func NewMockActorForTest(t *testing.T, supervisor Handle, substrate *network.Substrate) (Actor, network.Network, Handle, crypto.PrivKey, crypto.PubKey) {
	t.Helper()
	priv, pub, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)

	rootDID, root1Trust := MakeRootTrustContext(t)
	mockActorDID, actor1Trust := MakeTrustContext(t, priv)
	actorCap := MakeCapabilityContext(t, mockActorDID, rootDID, actor1Trust, root1Trust)

	securityCtx, err := NewBasicSecurityContext(pub, priv, actorCap)
	require.NoError(t, err)

	id := securityCtx.ID()
	peerID, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)
	peer := substrate.AddWiredPeer(peerID)
	require.NoError(t, peer.Start())

	handle := Handle{
		ID:  id,
		DID: mockActorDID,
		Address: Address{
			HostID:       peerID.String(),
			InboxAddress: "root",
		},
	}

	mockActor, err := NewMockActor(
		supervisor,
		peer,
		securityCtx,
		nil,
		handle,
	)
	require.NoError(t, err)
	require.NoError(t, mockActor.Start())

	return mockActor, peer, handle, priv, pub
}
