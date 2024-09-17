package actor

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	backgroundtasks "gitlab.com/nunet/device-management-service/internal/background_tasks"

	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/did"
	"gitlab.com/nunet/device-management-service/lib/ucan"
	"gitlab.com/nunet/device-management-service/network"
	"gitlab.com/nunet/device-management-service/network/libp2p"
	"gitlab.com/nunet/device-management-service/types"
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
	addrs1, priv1, peer1 := newLibp2pNetwork(t, []multiaddr.Multiaddr{})
	_, priv2, peer2 := newLibp2pNetwork(t, addrs1)

	res, err := peer2.Ping(context.Background(), peer1.Host.ID().String(), time.Second)
	assert.NoError(t, err)
	assert.True(t, res.Success)

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 2, peer2.Host.Peerstore().Peers().Len())
	assert.Equal(t, 2, peer1.Host.Peerstore().Peers().Len())

	// create trust and capability contexts that allow the two separately rooted
	// actors to interact
	root1DID, root1Trust := makeRootTrustContext(t)
	root2DID, root2Trust := makeRootTrustContext(t)
	actor1DID, actor1Trust := makeTrustContext(t, priv1)
	actor2DID, actor2Trust := makeTrustContext(t, priv2)
	actor1Cap := makeCapabilityContext(t, actor1DID, root1DID, actor1Trust, root1Trust)
	actor2Cap := makeCapabilityContext(t, actor2DID, root2DID, actor2Trust, root2Trust)
	allowReciprocal(t, actor1Cap, root1Trust, root1DID, root2DID, "/test")
	allowReciprocal(t, actor2Cap, root2Trust, root2DID, root1DID, "/test")

	// create actors
	actor1 := createActor(t, peer1, actor1Cap)
	err = actor1.Start()
	assert.NoError(t, err)
	actor2 := createActor(t, peer2, actor2Cap)
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

	addrs1, priv1, peer1 := newLibp2pNetwork(t, []multiaddr.Multiaddr{})
	_, priv2, peer2 := newLibp2pNetwork(t, addrs1)

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
	root1DID, root1Trust := makeRootTrustContext(t)
	root2DID, root2Trust := makeRootTrustContext(t)
	actor1DID, actor1Trust := makeTrustContext(t, priv1)
	actor2DID, actor2Trust := makeTrustContext(t, priv2)
	actor1Cap := makeCapabilityContext(t, actor1DID, root1DID, actor1Trust, root1Trust)
	actor2Cap := makeCapabilityContext(t, actor2DID, root2DID, actor2Trust, root2Trust)
	allowBroadcast(t, actor1Cap, actor2Cap, root1Trust, root2Trust, root1DID, root2DID, topic, Capability(behavior))

	// create actors
	actor1 := createActor(t, peer1, actor1Cap)
	err = actor1.Start()
	assert.NoError(t, err)
	actor2 := createActor(t, peer2, actor2Cap)
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

func makeRootTrustContext(t *testing.T) (did.DID, did.TrustContext) {
	privk, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)
	return makeTrustContext(t, privk)
}

func makeTrustContext(t *testing.T, privk crypto.PrivKey) (did.DID, did.TrustContext) {
	provider, err := did.ProviderFromPrivateKey(privk)
	require.NoError(t, err, "provider from public key")

	ctx := did.NewTrustContext()
	ctx.AddProvider(provider)

	return provider.DID(), ctx
}

func makeCapabilityContext(t *testing.T, actorDID, rootDID did.DID, trust, root did.TrustContext) ucan.CapabilityContext {
	actorCap, err := ucan.NewCapabilityContext(trust, actorDID, nil, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	rootCap, err := ucan.NewCapabilityContext(root, rootDID, nil, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	tokens, err := rootCap.Grant(
		ucan.Delegate,
		actorDID,
		did.DID{},
		nil,
		makeExpiry(time.Hour),
		0,
		[]ucan.Capability{ucan.Root},
	)
	require.NoError(t, err)

	err = actorCap.AddRoots([]did.DID{rootDID}, ucan.TokenList{}, tokens)
	require.NoError(t, err)

	return actorCap
}

func makeExpiry(d time.Duration) uint64 {
	return uint64(time.Now().Add(d).UnixNano())
}

func allowReciprocal(t *testing.T, actorCap ucan.CapabilityContext, rootTrust did.TrustContext, rootDID, otherRootDID did.DID, cap string) {
	rootCap, err := ucan.NewCapabilityContext(rootTrust, rootDID, nil, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	tokens, err := rootCap.Grant(
		ucan.Delegate,
		otherRootDID,
		did.DID{},
		nil,
		makeExpiry(time.Hour),
		0,
		[]ucan.Capability{ucan.Capability(cap)},
	)
	require.NoError(t, err)

	err = actorCap.AddRoots(nil, tokens, ucan.TokenList{})
	require.NoError(t, err)
}

func allowBroadcast(t *testing.T, actor1, actor2 ucan.CapabilityContext, root1, root2 did.TrustContext, root1DID, root2DID did.DID, topic string, actorCap ...Capability) {
	root1Cap, err := ucan.NewCapabilityContext(root1, root1DID, nil, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)
	root2Cap, err := ucan.NewCapabilityContext(root2, root2DID, nil, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	tokens, err := root1Cap.Grant(
		ucan.Delegate,
		actor1.DID(),
		did.DID{},
		[]string{topic},
		makeExpiry(120*time.Second),
		0,
		actorCap,
	)
	require.NoError(t, err, "granting broadcast capability")

	err = actor1.AddRoots(nil, ucan.TokenList{}, tokens)
	require.NoError(t, err, "add roots")

	tokens, err = root2Cap.Grant(
		ucan.Delegate,
		actor1.DID(),
		did.DID{},
		[]string{topic},
		makeExpiry(120*time.Second),
		0,
		actorCap,
	)
	require.NoError(t, err, "grant broadcast capability")

	err = actor2.AddRoots(nil, tokens, ucan.TokenList{})
	require.NoError(t, err, "add roots")
}

func createActor(t *testing.T, peer *libp2p.Libp2p, cap ucan.CapabilityContext) *BasicActor {
	privk, pubk, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)

	sctx, err := NewBasicSecurityContext(pubk, privk, cap)
	assert.NoError(t, err)

	params := BasicActorParams{}

	uuid, err := uuid.NewUUID()
	assert.NoError(t, err)

	handle := Handle{
		ID:  sctx.id,
		DID: cap.DID(),
		Address: Address{
			HostID:       peer.Host.ID().String(),
			InboxAddress: uuid.String(),
		},
	}
	actor, err := New(backgroundtasks.NewScheduler(1), peer, sctx, NewRateLimiter(DefaultRateLimiterConfig()), params, handle)
	assert.NoError(t, err)
	assert.NotNil(t, actor)

	return actor
}

func newLibp2pNetwork(t *testing.T, bootstrap []multiaddr.Multiaddr) ([]multiaddr.Multiaddr, crypto.PrivKey, *libp2p.Libp2p) {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	assert.NoError(t, err)
	net, err := network.NewNetwork(&types.NetworkConfig{
		Type: types.Libp2pNetwork,
		Libp2pConfig: types.Libp2pConfig{
			PrivateKey:              priv,
			BootstrapPeers:          bootstrap,
			Rendezvous:              "nunet-randevouz",
			Server:                  false,
			Scheduler:               backgroundtasks.NewScheduler(1),
			CustomNamespace:         "/nunet-dht-1/",
			ListenAddress:           []string{"/ip4/127.0.0.1/tcp/0"},
			PeerCountDiscoveryLimit: 40,
			GossipMaxMessageSize:    2 << 16,
		},
	}, afero.NewMemMapFs())
	assert.NoError(t, err)
	err = net.Init(context.Background())
	assert.NoError(t, err)

	err = net.Start(context.Background())
	assert.NoError(t, err)

	libp2pInstance, _ := net.(*libp2p.Libp2p)

	multi, err := libp2pInstance.GetMultiaddr()
	assert.NoError(t, err)
	return multi, priv, libp2pInstance
}
