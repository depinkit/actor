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
