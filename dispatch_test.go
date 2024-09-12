package actor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/ucan"
)

func TestNewDispatch(t *testing.T) {
	sc := generateSecurityContext(t)
	d := NewDispatch(sc, WithDispatchWorkers(5), WithDispatchGCInterval(60*time.Second))
	require.Equal(t, 5, d.options.Workers)
	require.Equal(t, 60*time.Second, d.options.GCInterval)
}

func TestDispatchStart(t *testing.T) {
	sc := generateSecurityContext(t)
	d := NewDispatch(sc, WithDispatchWorkers(3))
	d.Start()
	assert.True(t, d.started)
}

func TestDispatchAddBehavior(t *testing.T) {
	sc := generateSecurityContext(t)
	d := NewDispatch(sc)
	d.Start()

	behavior := func(_ Envelope) {}

	err := d.AddBehavior("test", behavior)
	assert.NoError(t, err)
	assert.Len(t, d.behaviors, 1)

	d.RemoveBehavior("test")
	assert.Len(t, d.behaviors, 0)
}

func TestDispatchReceive(t *testing.T) {
	sc := generateSecurityContext(t)
	d := NewDispatch(sc)
	d.Start()

	behaviorExecuted := make(chan bool)

	behavior := func(_ Envelope) {
		behaviorExecuted <- true
	}

	err := d.AddBehavior("/test/1", behavior)
	assert.NoError(t, err)

	me := Handle{
		ID:  sc.ID(),
		DID: sc.DID(),
		Address: Address{
			HostID:       "123",
			InboxAddress: "111",
		},
	}

	msg, err := Message(me, me, "/test/1", nil, WithMessageSignature(sc, []ucan.Capability{ucan.Capability("/test/1")}, nil))
	assert.NoError(t, err)

	err = d.Receive(msg)
	assert.NoError(t, err)

	select {
	case <-behaviorExecuted:
	case <-time.After(2 * time.Second):
		t.Fatal("behavior was not executed")
	}
}

func TestDispatchGC(t *testing.T) {
	sc := generateSecurityContext(t)
	d := NewDispatch(sc, WithDispatchGCInterval(10*time.Millisecond))
	d.Start()

	behavior := func(_ Envelope) {}
	expireTime := uint64(time.Now().Add(10 * time.Millisecond).UnixNano())
	err := d.AddBehavior("test", behavior, WithBehaviorExpiry(expireTime))
	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	assert.Len(t, d.behaviors, 0)
}

func generateSecurityContext(t *testing.T) *BasicSecurityContext {
	priv, pub, err := crypto.GenerateKeyPair(crypto.Ed25519)
	assert.NoError(t, err)

	rootDID, rootTrust := makeRootTrustContext(t)
	actorDID, actorTrust := makeRootTrustContext(t)
	actorCap := makeCapabilityContext(t, actorDID, rootDID, actorTrust, rootTrust)

	sc, err := NewBasicSecurityContext(pub, priv, actorCap)
	assert.NoError(t, err)
	return sc
}
