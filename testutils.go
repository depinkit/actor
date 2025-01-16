// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multiformats/go-multiaddr"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	backgroundtasks "gitlab.com/nunet/device-management-service/internal/background_tasks"
	"gitlab.com/nunet/device-management-service/internal/config"
	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/did"
	"gitlab.com/nunet/device-management-service/lib/ucan"
	"gitlab.com/nunet/device-management-service/network"
	"gitlab.com/nunet/device-management-service/network/libp2p"
	"gitlab.com/nunet/device-management-service/types"
)

func MakeRootTrustContext(t *testing.T) (did.DID, did.TrustContext) {
	privk, _, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)
	return MakeTrustContext(t, privk)
}

func MakeTrustContext(t *testing.T, privk crypto.PrivKey) (did.DID, did.TrustContext) {
	provider, err := did.ProviderFromPrivateKey(privk)
	require.NoError(t, err, "provider from public key")

	ctx := did.NewTrustContext()
	ctx.AddProvider(provider)

	return provider.DID(), ctx
}

func MakeCapabilityContext(t *testing.T, actorDID, rootDID did.DID, trust, root did.TrustContext) ucan.CapabilityContext {
	actorCap, err := ucan.NewCapabilityContext(trust, actorDID, nil, ucan.TokenList{}, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	rootCap, err := ucan.NewCapabilityContext(root, rootDID, nil, ucan.TokenList{}, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	tokens, err := rootCap.Grant(
		ucan.Delegate,
		actorDID,
		did.DID{},
		nil,
		MakeExpiry(time.Hour),
		0,
		[]ucan.Capability{ucan.Root},
	)
	require.NoError(t, err)

	err = actorCap.AddRoots([]did.DID{rootDID}, ucan.TokenList{}, tokens, ucan.TokenList{})
	require.NoError(t, err)

	return actorCap
}

func MakeExpiry(d time.Duration) uint64 {
	return uint64(time.Now().Add(d).UnixNano())
}

func AllowReciprocal(t *testing.T, actorCap ucan.CapabilityContext, rootTrust did.TrustContext, rootDID, otherRootDID did.DID, capability string) {
	rootCap, err := ucan.NewCapabilityContext(rootTrust, rootDID, nil, ucan.TokenList{}, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	tokens, err := rootCap.Grant(
		ucan.Delegate,
		otherRootDID,
		did.DID{},
		nil,
		MakeExpiry(time.Hour),
		0,
		[]ucan.Capability{ucan.Capability(capability)},
	)
	require.NoError(t, err)

	err = actorCap.AddRoots(nil, tokens, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)
}

func AllowBroadcast(t *testing.T, actor1, actor2 ucan.CapabilityContext, root1, root2 did.TrustContext, root1DID, root2DID did.DID, topic string, actorCap ...Capability) {
	root1Cap, err := ucan.NewCapabilityContext(root1, root1DID, nil, ucan.TokenList{}, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)
	root2Cap, err := ucan.NewCapabilityContext(root2, root2DID, nil, ucan.TokenList{}, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err)

	tokens, err := root1Cap.Grant(
		ucan.Delegate,
		actor1.DID(),
		did.DID{},
		[]string{topic},
		MakeExpiry(120*time.Second),
		0,
		actorCap,
	)
	require.NoError(t, err, "granting broadcast capability")

	err = actor1.AddRoots(nil, ucan.TokenList{}, tokens, ucan.TokenList{})
	require.NoError(t, err, "add roots")

	tokens, err = root2Cap.Grant(
		ucan.Delegate,
		actor1.DID(),
		did.DID{},
		[]string{topic},
		MakeExpiry(120*time.Second),
		0,
		actorCap,
	)
	require.NoError(t, err, "grant broadcast capability")

	err = actor2.AddRoots(nil, tokens, ucan.TokenList{}, ucan.TokenList{})
	require.NoError(t, err, "add roots")
}

func CreateActor(t *testing.T, peer network.Network, capCxt ucan.CapabilityContext) *BasicActor {
	privk, pubk, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)

	sctx, err := NewBasicSecurityContext(pubk, privk, capCxt)
	assert.NoError(t, err)

	params := BasicActorParams{}

	uuid, err := uuid.NewUUID()
	assert.NoError(t, err)

	handle := Handle{
		ID:  sctx.id,
		DID: capCxt.DID(),
		Address: Address{
			HostID:       peer.GetHostID().String(),
			InboxAddress: uuid.String(),
		},
	}
	actor, err := New(Handle{}, peer, sctx, NewRateLimiter(DefaultRateLimiterConfig()), params, handle)
	assert.NoError(t, err)
	assert.NotNil(t, actor)

	return actor
}

func NewLibp2pNetwork(t *testing.T, bootstrap []multiaddr.Multiaddr) ([]multiaddr.Multiaddr, crypto.PrivKey, *libp2p.Libp2p) {
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
	err = net.Init(&config.Config{})
	assert.NoError(t, err)

	err = net.Start()
	assert.NoError(t, err)

	libp2pInstance, _ := net.(*libp2p.Libp2p)

	multi, err := libp2pInstance.GetMultiaddr()
	assert.NoError(t, err)
	return multi, priv, libp2pInstance
}
