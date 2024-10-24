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
