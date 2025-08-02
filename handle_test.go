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
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/depinkit/crypto"
	"github.com/depinkit/did"
)

// genHandle returns a fully-initialised Handle plus its peer.ID.
func genHandle(t *testing.T) (Handle, peer.ID) {
	t.Helper()

	_, pub, err := crypto.GenerateKeyPair(crypto.Ed25519)
	require.NoError(t, err)

	id, err := crypto.IDFromPublicKey(pub)
	require.NoError(t, err)

	d := did.FromPublicKey(pub)

	peerID, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)

	return Handle{
		ID:  id,
		DID: d,
		Address: Address{
			HostID:       peerID.String(),
			InboxAddress: "root",
		},
	}, peerID
}

func TestHandleEmptyAndAddressEmpty(t *testing.T) {
	t.Parallel()

	var h Handle
	require.True(t, h.Empty())

	h.ID = crypto.ID{PublicKey: []byte{1}}
	require.False(t, h.Empty())

	var a Address
	require.True(t, a.Empty())

	a.HostID = "x"
	require.False(t, a.Empty())
}

func TestHandleString(t *testing.T) {
	t.Parallel()

	h, peerID := genHandle(t)

	// Expected format: "<id-did>[<did>]@<host>:<inbox>"
	idDID, _ := did.FromID(h.ID)
	want := fmt.Sprintf("%s[%s]@%s", idDID, h.DID, h.Address)

	require.Equal(t, want, h.String())
	require.Contains(t, h.String(), peerID.String())
}

func TestHandleEqual(t *testing.T) {
	t.Parallel()

	h1, _ := genHandle(t)
	h2 := h1
	require.True(t, h1.Equal(h2))

	h2.Address.InboxAddress = "other"
	require.False(t, h1.Equal(h2))
}

func TestHandleFromPeerID(t *testing.T) {
	t.Parallel()

	h, peerID := genHandle(t)

	got, err := HandleFromPeerID(peerID.String())
	require.NoError(t, err)
	require.True(t, h.Equal(got))

	_, err = HandleFromPeerID("not-a-peerid")
	require.Error(t, err)
}

func TestHandleFromDID(t *testing.T) {
	t.Parallel()

	h, _ := genHandle(t)

	got, err := HandleFromDID(h.DID.String())
	require.NoError(t, err)
	require.True(t, h.Equal(got))

	_, err = HandleFromDID("foo")
	require.Error(t, err)
}

func TestHandleStringEdges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr   Address
		expect string
	}{
		{Address{HostID: "host", InboxAddress: ""}, "host:"},
		{Address{HostID: "", InboxAddress: "box"}, ":box"},
	}

	for _, tt := range tests {
		require.Equal(t, tt.expect, tt.addr.String())
	}
}

func TestHandleParsersTODO(t *testing.T) {
	t.Parallel()

	_, err := HandleFromString("whatever")
	require.ErrorIs(t, err, ErrTODO)

	_, err = AddressFromString("whatever")
	require.ErrorIs(t, err, ErrTODO)
}

func TestHandleParsersReturnTODO(t *testing.T) {
	t.Parallel()

	_, err := HandleFromString("bogus=foo,host=bar:box")
	require.ErrorIs(t, err, ErrTODO)

	_, err = AddressFromString("bogus=foo")
	require.ErrorIs(t, err, ErrTODO)
}
