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

	"github.com/libp2p/go-libp2p/core/peer"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/did"
)

func (h *Handle) Empty() bool {
	return h.ID.Empty() &&
		h.DID.Empty() &&
		h.Address.Empty()
}

func (h *Handle) String() string {
	var idStr string
	idDID, err := did.FromID(h.ID)
	if err == nil {
		idStr = idDID.String()
	}
	return fmt.Sprintf("%s[%s]@%s", idStr, h.DID, h.Address)
}

func HandleFromString(_ string) (Handle, error) {
	// TODO
	return Handle{}, ErrTODO
}

func (a *Address) Empty() bool {
	return a.HostID == "" && a.InboxAddress == ""
}

func (a *Address) String() string {
	return a.HostID + ":" + a.InboxAddress
}

func AddressFromString(_ string) (Address, error) {
	// TODO
	return Address{}, ErrTODO
}

func HandleFromPeerID(dest string) (Handle, error) {
	peerID, err := peer.Decode(dest)
	if err != nil {
		return Handle{}, err
	}

	pubk, err := peerID.ExtractPublicKey()
	if err != nil {
		return Handle{}, err
	}

	if !crypto.AllowedKey(int(pubk.Type())) {
		return Handle{}, fmt.Errorf("unexpected key type: %d", pubk.Type())
	}

	actorID, err := crypto.IDFromPublicKey(pubk)
	if err != nil {
		return Handle{}, err
	}

	actorDID := did.FromPublicKey(pubk)
	handle := Handle{
		ID:  actorID,
		DID: actorDID,
		Address: Address{
			HostID:       peerID.String(),
			InboxAddress: "root",
		},
	}

	return handle, nil
}

func HandleFromDID(dest string) (Handle, error) {
	actorDID, err := did.FromString(dest)
	if err != nil {
		return Handle{}, err
	}

	pubk, err := did.PublicKeyFromDID(actorDID)
	if err != nil {
		return Handle{}, err
	}

	actorID, err := crypto.IDFromPublicKey(pubk)
	if err != nil {
		return Handle{}, err
	}

	peerID, err := peer.IDFromPublicKey(pubk)
	if err != nil {
		return Handle{}, err
	}

	handle := Handle{
		ID:  actorID,
		DID: actorDID,
		Address: Address{
			HostID:       peerID.String(),
			InboxAddress: "root",
		},
	}

	return handle, nil
}
