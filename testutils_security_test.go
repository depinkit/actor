package actor

import (
	"testing"

	"github.com/depinkit/crypto"
)

// NewTestSecurityContext returns a fully-wired BasicSecurityContext that’s ready
// for unit tests (key-pair, trust context, capability root).
func NewTestSecurityContext(t *testing.T) *BasicSecurityContext {
	t.Helper()

	// Root of trust
	rootDID, rootTrust := MakeRootTrustContext(t)

	// Actor key-pair
	priv, pub, err := crypto.GenerateKeyPair(crypto.Ed25519)
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}

	actorDID, actorTrust := MakeTrustContext(t, priv)
	capCtx := MakeCapabilityContext(t, actorDID, rootDID, actorTrust, rootTrust)

	sctx, err := NewBasicSecurityContext(pub, priv, capCtx)
	if err != nil {
		t.Fatalf("security context: %v", err)
	}
	return sctx
}

// Spy helpers
// SpySecurity counts RequireBroadcast invocations.
type SpySecurity struct {
	*BasicSecurityContext
	Calls int
}

// NewSpySecurity constructs a fresh SpySecurity helper.
func NewSpySecurity(t *testing.T) *SpySecurity {
	return &SpySecurity{BasicSecurityContext: NewTestSecurityContext(t)}
}

func (s *SpySecurity) RequireBroadcast(e Envelope, topic string, caps []Capability) error {
	s.Calls++
	return s.BasicSecurityContext.RequireBroadcast(e, topic, caps)
}

// SpySec counts Provide / ProvideBroadcast calls and can inject errors.
type SpySec struct {
	*BasicSecurityContext
	ProvideCalled, BroadcastCalled bool
	ProvideErr, BroadcastErr       error
}

// NewSpySec constructs a fresh SpySec helper.
func NewSpySec(t *testing.T) *SpySec {
	return &SpySec{BasicSecurityContext: NewTestSecurityContext(t)}
}

func (s *SpySec) Provide(_ *Envelope, _ []Capability, _ []Capability) error {
	s.ProvideCalled = true
	return s.ProvideErr
}

func (s *SpySec) ProvideBroadcast(_ *Envelope, _ string, _ []Capability) error {
	s.BroadcastCalled = true
	return s.BroadcastErr
}
