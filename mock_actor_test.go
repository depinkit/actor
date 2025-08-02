package actor

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/depinkit/crypto"
	"github.com/depinkit/did"
	"github.com/depinkit/ucan"
	"github.com/depinkit/network"
)

// setupTestSecurityContext creates a new security context for testing
func setupTestSecurityContext(t *testing.T, privKey crypto.PrivKey) *BasicSecurityContext {
	t.Helper()

	// Create trust context
	trustCtx, err := did.NewTrustContextWithPrivateKey(privKey)
	require.NoError(t, err)

	// Get the provider from the trust context
	provider, err := trustCtx.GetProvider(did.FromPublicKey(privKey.GetPublic()))
	require.NoError(t, err)

	// Create capability context
	capCtx, err := ucan.NewCapabilityContext(trustCtx, provider.DID(), nil, ucan.TokenList{}, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	// Create security context
	secCtx, err := NewBasicSecurityContext(privKey.GetPublic(), privKey, capCtx)
	require.NoError(t, err)

	return secCtx
}

func TestMockActorBehaviorCommunication(t *testing.T) {
	// Create a substrate for testing
	substrate := network.NewSubstrate()
	require.NotNil(t, substrate)

	privKey1, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)
	privKey2, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)

	peerID1, err := peer.IDFromPublicKey(privKey1.GetPublic())
	require.NoError(t, err)
	peerID2, err := peer.IDFromPublicKey(privKey2.GetPublic())
	require.NoError(t, err)

	// Create networks for both actors
	vnet1 := substrate.AddWiredPeer(peerID1)
	vnet2 := substrate.AddWiredPeer(peerID2)

	// Start the network(s)
	require.NoError(t, vnet1.Start())
	require.NoError(t, vnet2.Start())

	// Create a rate limiter
	limiter := NewRateLimiter(DefaultRateLimiterConfig())

	// Create security contexts
	secCtx1 := setupTestSecurityContext(t, privKey1)
	secCtx2 := setupTestSecurityContext(t, privKey2)

	// Create actor handles
	handle1 := Handle{
		ID:  secCtx1.ID(),
		DID: secCtx1.DID(),
		Address: Address{
			HostID:       vnet1.GetHostID().String(),
			InboxAddress: "inbox1",
		},
	}
	handle2 := Handle{
		ID:  secCtx2.ID(),
		DID: secCtx2.DID(),
		Address: Address{
			HostID:       vnet2.GetHostID().String(),
			InboxAddress: "inbox2",
		},
	}

	// Create the actors
	actor1, err := NewMockActor(handle1, vnet1, secCtx1, limiter, handle1)
	require.NoError(t, err)
	actor2, err := NewMockActor(handle2, vnet2, secCtx2, limiter, handle2)
	require.NoError(t, err)

	// Start both actors
	err = actor1.Start()
	require.NoError(t, err)
	err = actor2.Start()
	require.NoError(t, err)

	// Create a channel to receive the test message
	received := make(chan bool, 1)

	// Register a test behavior on actor1
	testBehavior := "test-behavior"
	err = actor1.AddBehavior(testBehavior, func(msg Envelope) {
		var data string
		err := json.Unmarshal(msg.Message, &data)
		require.NoError(t, err)
		assert.Equal(t, "hello", data)
		received <- true
	})
	require.NoError(t, err)

	// Create and send a message from actor2 to actor1
	msg, err := Message(
		handle2,
		handle1,
		testBehavior,
		"hello",
	)
	require.NoError(t, err)

	err = actor2.Send(msg)
	require.NoError(t, err)

	// Wait for the message to be received
	select {
	case <-received:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message")
	}

	// Clean up
	err = actor1.Stop()
	require.NoError(t, err)
	err = actor2.Stop()
	require.NoError(t, err)
}

func TestMockActorBehaviorInvoke(t *testing.T) {
	// Create a substrate for testing
	substrate := network.NewSubstrate()
	require.NotNil(t, substrate)

	// Create private keys for both actors
	privKey1, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)
	privKey2, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)

	peerID1, err := peer.IDFromPublicKey(privKey1.GetPublic())
	require.NoError(t, err)
	peerID2, err := peer.IDFromPublicKey(privKey2.GetPublic())
	require.NoError(t, err)

	// Create networks for both actors
	vnet1 := substrate.AddWiredPeer(peerID1)
	vnet2 := substrate.AddWiredPeer(peerID2)

	// Create a rate limiter
	limiter := NewRateLimiter(DefaultRateLimiterConfig())

	// Create security contexts
	secCtx1 := setupTestSecurityContext(t, privKey1)
	secCtx2 := setupTestSecurityContext(t, privKey2)

	// Create actor handles
	handle1 := Handle{
		ID:  secCtx1.ID(),
		DID: secCtx1.DID(),
		Address: Address{
			HostID:       vnet1.GetHostID().String(),
			InboxAddress: "inbox1",
		},
	}
	handle2 := Handle{
		ID:  secCtx2.ID(),
		DID: secCtx2.DID(),
		Address: Address{
			HostID:       vnet2.GetHostID().String(),
			InboxAddress: "inbox2",
		},
	}

	// Create the actors
	actor1, err := NewMockActor(handle1, vnet1, secCtx1, limiter, handle1)
	require.NoError(t, err)
	actor2, err := NewMockActor(handle2, vnet2, secCtx2, limiter, handle2)
	require.NoError(t, err)

	// Start both actors
	err = actor1.Start()
	require.NoError(t, err)
	err = actor2.Start()
	require.NoError(t, err)

	// Register a test behavior on actor1 that sends a reply
	testBehavior := "test-behavior"
	err = actor1.AddBehavior(testBehavior, func(msg Envelope) {
		reply, err := Message(
			actor1.Handle(),
			msg.From,
			msg.Options.ReplyTo,
			"reply",
		)
		require.NoError(t, err)
		err = actor1.Send(reply)
		require.NoError(t, err)
	})
	require.NoError(t, err)

	// Create and invoke a message from actor2 to actor1
	msg, err := Message(
		actor2.Handle(),
		actor1.Handle(),
		testBehavior,
		"hello",
	)
	require.NoError(t, err)

	replyCh, err := actor2.Invoke(msg)
	require.NoError(t, err)

	// Wait for the reply
	select {
	case reply := <-replyCh:
		defer reply.Discard()
		var data string
		err := json.Unmarshal(reply.Message, &data)
		require.NoError(t, err)
		assert.Equal(t, "reply", data)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for reply")
	}

	// Clean up
	err = actor1.Stop()
	require.NoError(t, err)
	err = actor2.Stop()
	require.NoError(t, err)
}
