package actor

type MessageEnvelope struct {
	Type MessageType
	Data []byte
}

type MessageType string
