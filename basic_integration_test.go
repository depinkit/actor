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
	"testing"
	"time"

	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/depinkit/did"
	"github.com/depinkit/ucan"
	"github.com/depinkit/network"
	"github.com/depinkit/network/libp2p"
)

func TestNew(t *testing.T) {
	// Set observability to no-op mode for this test
	observability.SetNoOpMode(true)

	t.Parallel()

	cases := map[string]struct {
		net        network.Network
		security   *BasicSecurityContext
		supervisor Handle
		params     BasicActorParams
		self       Handle
		expErr     string
	}{
		"nil network": {
			expErr: "network is nil",
		},
		"nil security": {
			net:    &libp2p.Libp2p{},
			expErr: "security is nil",
		},
		"success": {
			net:        &libp2p.Libp2p{},
			security:   &BasicSecurityContext{},
			supervisor: Handle{},
			params:     BasicActorParams{},
			self:       Handle{},
		},
	}

	for name, tt := range cases {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			act, err := New(tt.supervisor, tt.net, tt.security, NoRateLimiter{}, tt.params, tt.self)
			if tt.expErr != "" {
				assert.Nil(t, act)
				assert.EqualError(t, err, tt.expErr)
			} else {
				assert.NotNil(t, act)
			}
		})
	}
}

func TestActorMessaging(t *testing.T) {
	addrs1, priv1, peer1 := NewLibp2pNetwork(t, []multiaddr.Multiaddr{})
	_, priv2, peer2 := NewLibp2pNetwork(t, addrs1)

	res, err := peer2.Ping(context.Background(), peer1.Host.ID().String(), time.Second)
	assert.NoError(t, err)
	assert.True(t, res.Success)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 2, peer2.Host.Peerstore().Peers().Len())
	assert.Equal(t, 2, peer1.Host.Peerstore().Peers().Len())

	// create trust and capability contexts that allow the two separately rooted
	// actors to interact
	root1DID, root1Trust := MakeRootTrustContext(t)
	root2DID, root2Trust := MakeRootTrustContext(t)
	actor1DID, actor1Trust := MakeTrustContext(t, priv1)
	actor2DID, actor2Trust := MakeTrustContext(t, priv2)
	actor1Cap := MakeCapabilityContext(t, actor1DID, root1DID, actor1Trust, root1Trust)
	actor2Cap := MakeCapabilityContext(t, actor2DID, root2DID, actor2Trust, root2Trust)
	AllowReciprocal(t, actor1Cap, root1Trust, root1DID, root2DID, "/test")
	AllowReciprocal(t, actor2Cap, root2Trust, root2DID, root1DID, "/test")

	// create actors
	actor1 := CreateActor(t, peer1, actor1Cap)
	err = actor1.Start()
	assert.NoError(t, err)
	actor2 := CreateActor(t, peer2, actor2Cap)
	err = actor2.Start()
	assert.NoError(t, err)

	const (
		testBehavior1 = "/test/someBehavior1"
		testBehavior2 = "/test/someBehavior2"
	)

	type payload struct{ Name, Type string }

	envChan := make(chan Envelope)

	err = actor1.AddBehavior(testBehavior1, func(msg Envelope) {
		defer msg.Discard()
		envChan <- msg
	})
	assert.NoError(t, err)

	err = actor1.AddBehavior(testBehavior2, func(msg Envelope) {
		defer msg.Discard()
		reply, err := ReplyTo(msg, payload{Name: "random name", Type: "x"})
		assert.NoError(t, err)
		err = actor1.Send(reply)
		assert.NoError(t, err)
	})
	require.NoError(t, err)

	msg, err := Message(
		actor2.self,
		actor1.self,
		testBehavior1,
		payload{Name: "random name", Type: "x"},
	)
	require.NoError(t, err)

	err = actor2.Send(msg)
	require.NoError(t, err)

	received := <-envChan
	require.Equal(t, string(received.Message), "{\"Name\":\"random name\",\"Type\":\"x\"}")

	// invoke
	msg, err = Message(
		actor2.self,
		actor1.self,
		testBehavior2,
		payload{Name: "random name", Type: "x"},
	)
	require.NoError(t, err)
	replyChan, err := actor2.Invoke(msg)
	assert.NoError(t, err)
	reply := <-replyChan
	require.Equal(t, msg.Message, reply.Message)
}

func TestActorBroadcast(t *testing.T) {
	topic := "test"
	behavior := "/broadcast/test"

	addrs1, priv1, peer1 := NewLibp2pNetwork(t, []multiaddr.Multiaddr{})
	_, priv2, peer2 := NewLibp2pNetwork(t, addrs1)

	res, err := peer2.Ping(context.Background(), peer1.Host.ID().String(), time.Second)
	if err != nil {
		for i := 1; i <= 3; i++ {
			time.Sleep(time.Duration(i) * time.Second)
			res, err = peer2.Ping(context.Background(), peer1.Host.ID().String(), time.Second)
			if err == nil {
				break
			}
		}
		require.NoError(t, err)
	}
	assert.True(t, res.Success)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 2, peer2.Host.Peerstore().Peers().Len())
	assert.Equal(t, 2, peer1.Host.Peerstore().Peers().Len())

	// create trust and capability contexts that allow the two separately rooted
	// actors to interact
	root1DID, root1Trust := MakeRootTrustContext(t)
	root2DID, root2Trust := MakeRootTrustContext(t)
	actor1DID, actor1Trust := MakeTrustContext(t, priv1)
	actor2DID, actor2Trust := MakeTrustContext(t, priv2)
	actor1Cap := MakeCapabilityContext(t, actor1DID, root1DID, actor1Trust, root1Trust)
	actor2Cap := MakeCapabilityContext(t, actor2DID, root2DID, actor2Trust, root2Trust)
	AllowBroadcast(t, actor1Cap, actor2Cap, root1Trust, root2Trust, root1DID, root2DID, topic, Capability(behavior))

	// create actors
	actor1 := CreateActor(t, peer1, actor1Cap)
	err = actor1.Start()
	assert.NoError(t, err)
	actor2 := CreateActor(t, peer2, actor2Cap)
	err = actor2.Start()
	assert.NoError(t, err)

	type payload struct{ Name, Type string }

	mch := make(chan Envelope)
	err = actor2.AddBehavior(behavior, func(msg Envelope) {
		defer msg.Discard()
		mch <- msg
	},
		WithBehaviorTopic(topic),
	)
	require.NoError(t, err)

	err = actor2.Subscribe(topic)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	msg, err := Message(
		actor1.self,
		Handle{},
		behavior,
		payload{Name: "random name", Type: "x"},
		WithMessageTopic(topic),
	)
	require.NoError(t, err)
	require.Equal(t, true, msg.IsBroadcast())

	err = actor1.Publish(msg)
	require.NoError(t, err)

	result := <-mch
	assert.Equal(t, string(result.Message), "{\"Name\":\"random name\",\"Type\":\"x\"}")
}

func TestActorHealthcheck(t *testing.T) {
	addrs1, priv1, peer1 := NewLibp2pNetwork(t, []multiaddr.Multiaddr{})
	_, priv2, peer2 := NewLibp2pNetwork(t, addrs1)

	res, err := peer2.Ping(context.Background(), peer1.Host.ID().String(), time.Second)
	assert.NoError(t, err)
	assert.True(t, res.Success)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 2, peer2.Host.Peerstore().Peers().Len())
	assert.Equal(t, 2, peer1.Host.Peerstore().Peers().Len())

	// create trust and capability contexts that allow the two separately rooted
	// actors to interact
	root1DID, root1Trust := MakeRootTrustContext(t)
	root2DID, root2Trust := MakeRootTrustContext(t)
	actor1DID, actor1Trust := MakeTrustContext(t, priv1)
	actor2DID, actor2Trust := MakeTrustContext(t, priv2)
	actor1Cap := MakeCapabilityContext(t, actor1DID, root1DID, actor1Trust, root1Trust)
	actor2Cap := MakeCapabilityContext(t, actor2DID, root2DID, actor2Trust, root2Trust)
	AllowReciprocal(t, actor1Cap, root1Trust, root1DID, root2DID, "/dms")

	// create actors
	actor1 := CreateActor(t, peer1, actor1Cap)
	err = actor1.Start()
	assert.NoError(t, err)
	actor2 := CreateActor(t, peer2, actor2Cap)
	err = actor2.Start()
	assert.NoError(t, err)

	msg, err := Message(
		actor2.self,
		actor1.self,
		HealthCheckBehavior,
		nil,
	)
	require.NoError(t, err)

	err = actor1.AddBehavior(HealthCheckBehavior, func(msg Envelope) {
		defer msg.Discard()
		reply, err := ReplyTo(msg, nil)
		assert.NoError(t, err)
		err = actor1.Send(reply)
		assert.NoError(t, err)
	})
	require.NoError(t, err)

	replyCh, err := actor2.Invoke(msg)
	require.NoError(t, err)

	select {
	case <-replyCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}
}

func TestParentChildRelationship(t *testing.T) {
	// Setup network and actors
	_, priv1, peer1 := NewLibp2pNetwork(t, []multiaddr.Multiaddr{})

	// Create trust context for parent
	rootDID, rootTrust := MakeRootTrustContext(t)
	actorDID, actorTrust := MakeTrustContext(t, priv1)
	actorCap := MakeCapabilityContext(t, actorDID, rootDID, actorTrust, rootTrust)

	// Create parent actor
	parent := CreateActor(t, peer1, actorCap)
	err := parent.Start()
	require.NoError(t, err)

	// Create child actor
	childID := "child1"
	child, err := parent.CreateChild(childID, parent.Handle())
	require.NoError(t, err)

	err = child.Start()
	require.NoError(t, err)

	// Test parent-child relationship
	require.Equal(t, parent.Handle(), child.Parent())

	children := parent.Children()
	require.Len(t, children, 1)
	require.Contains(t, children, child.Handle().DID)
	require.Equal(t, child.Handle(), children[child.Handle().DID])

	// Test that parent can invoke any behavior on child
	testMessage := "Hello from parent"
	testBehavior := "/test/arbitrary/behavior"
	testPayload := struct {
		Message string
	}{
		Message: testMessage,
	}

	// Add behavior to child
	receivedChan := make(chan Envelope)
	err = child.AddBehavior(testBehavior, func(msg Envelope) {
		defer msg.Discard()
		receivedChan <- msg
	})
	require.NoError(t, err)

	// Parent sends message to child
	msg, err := Message(
		parent.Handle(),
		child.Handle(),
		testBehavior,
		testPayload,
	)
	require.NoError(t, err)

	err = parent.Send(msg)
	require.NoError(t, err)

	// Verify message was received by child
	select {
	case received := <-receivedChan:
		var payload struct {
			Message string
		}
		err := json.Unmarshal(received.Message, &payload)
		require.NoError(t, err)
		require.Equal(t, testMessage, payload.Message)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for child to receive message")
	}
}

// TestChildToChildBehaviorInvocation tests a similar workflow of
// behavior invoking that we have between the orchestrator and allocations
func TestChildToChildBehaviorInvocation(t *testing.T) {
	// Setup networks for both parent actors
	addrs1, priv1, peer1 := NewLibp2pNetwork(t, []multiaddr.Multiaddr{})
	_, priv2, peer2 := NewLibp2pNetwork(t, addrs1)

	// Ensure network connectivity
	res, err := peer2.Ping(context.Background(), peer1.Host.ID().String(), time.Second)
	require.NoError(t, err)
	require.True(t, res.Success)

	time.Sleep(500 * time.Millisecond)
	require.Equal(t, 2, peer2.Host.Peerstore().Peers().Len())
	require.Equal(t, 2, peer1.Host.Peerstore().Peers().Len())

	// Create trust contexts for both parent actors
	root1DID, root1Trust := MakeRootTrustContext(t)
	root2DID, root2Trust := MakeRootTrustContext(t)
	actor1DID, actor1Trust := MakeTrustContext(t, priv1)
	actor2DID, actor2Trust := MakeTrustContext(t, priv2)
	actor1Cap := MakeCapabilityContext(t, actor1DID, root1DID, actor1Trust, root1Trust)
	actor2Cap := MakeCapabilityContext(t, actor2DID, root2DID, actor2Trust, root2Trust)

	// Create parent actors
	parentA := CreateActor(t, peer1, actor1Cap)
	err = parentA.Start()
	require.NoError(t, err)

	parentB := CreateActor(t, peer2, actor2Cap)
	err = parentB.Start()
	require.NoError(t, err)

	// Create child actors
	orchestrator, err := parentA.CreateChild("orchestrator", parentA.Handle())
	require.NoError(t, err)
	err = orchestrator.Start()
	require.NoError(t, err)

	allocation, err := parentB.CreateChild("allocation", parentB.Handle())
	require.NoError(t, err)
	err = allocation.Start()
	require.NoError(t, err)

	// Define test behavior and message
	const testBehavior = "/test/behavior"
	testMessage := "Hello from Child A"
	testPayload := struct {
		Message string
	}{
		Message: testMessage,
	}

	// Add behavior to Child B
	receivedChan := make(chan Envelope)
	err = allocation.AddBehavior(testBehavior, func(msg Envelope) {
		defer msg.Discard()
		receivedChan <- msg
	})
	require.NoError(t, err)

	// TODO: all of them should work

	// does NOT work
	// err = parentB.Security().Grant(
	// 	orchestrator.Handle().DID, allocation.Handle().DID,
	// 	[]ucan.Capability{testBehavior}, time.Hour,
	// )

	// does NOT work
	// err = parentB.Security().Grant(
	// 	did.FromID(orchestrator.Handle().ID), allocation.Handle().DID,
	// 	[]ucan.Capability{testBehavior}, time.Hour,
	// )

	// does NOT work
	// err = parentB.Security().Grant(
	// 	did.FromID(orchestrator.Handle().ID), did.FromID(allocation.Handle().ID),
	// 	[]ucan.Capability{testBehavior}, time.Hour,
	// )

	// also:
	// id, err := did.FromID(orchestrator.Handle().ID)
	// assert.Equal(t, orchestrator.Handle().DID, id)

	allocDID, err := did.FromID(allocation.Handle().ID)
	require.NoError(t, err)

	// Grant Child A permission to invoke the behavior on Child B
	err = allocation.Security().Grant(
		orchestrator.Handle().DID, allocDID,
		[]ucan.Capability{testBehavior}, time.Hour,
	)
	require.NoError(t, err)

	// Child A sends message to Child B
	msg, err := Message(
		orchestrator.Handle(),
		allocation.Handle(),
		testBehavior,
		testPayload,
	)
	require.NoError(t, err)

	err = orchestrator.Send(msg)
	require.NoError(t, err)

	// Verify message was received by Child B
	select {
	case received := <-receivedChan:
		var payload struct {
			Message string
		}
		err := json.Unmarshal(received.Message, &payload)
		require.NoError(t, err)
		require.Equal(t, testMessage, payload.Message)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for Child B to receive message")
	}
}
