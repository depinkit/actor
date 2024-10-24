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
	"testing"
	"time"

	backgroundtasks "gitlab.com/nunet/device-management-service/internal/background_tasks"

	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/nunet/device-management-service/network"
	"gitlab.com/nunet/device-management-service/network/libp2p"
)

func TestNew(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		scheduler *backgroundtasks.Scheduler
		net       network.Network
		security  *BasicSecurityContext
		params    BasicActorParams
		self      Handle
		expErr    string
	}{
		"nil scheduler": {
			expErr: "scheduler is nil",
		},
		"nil network": {
			scheduler: &backgroundtasks.Scheduler{},
			expErr:    "network is nil",
		},
		"nil security": {
			scheduler: &backgroundtasks.Scheduler{},
			net:       &libp2p.Libp2p{},
			expErr:    "security is nil",
		},
		"success": {
			scheduler: &backgroundtasks.Scheduler{},
			net:       &libp2p.Libp2p{},
			security:  &BasicSecurityContext{},
			params:    BasicActorParams{},
			self:      Handle{},
		},
	}

	for name, tt := range cases {
		tt := tt
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			act, err := New(tt.scheduler, tt.net, tt.security, NoRateLimiter{}, tt.params, tt.self)
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
