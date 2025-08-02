package actor

import (
	"context"
	"time"

	"github.com/depinkit/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	pubsub "github.com/libp2p/go-libp2p/pubsub"
)

// Network represents a network interface for actor communication
type Network interface {
	// GetHostID returns the host ID of this network node
	GetHostID() crypto.ID

	// HandleMessage registers a message handler for a specific address
	HandleMessage(address string, handler func(data []byte, srcPeerID peer.ID)) error

	// SendMessage sends a message to a specific peer
	SendMessage(ctx context.Context, peerID string, msg MessageEnvelope, expiry time.Time) error

	// Publish publishes a message to a topic
	Publish(ctx context.Context, topic string, data []byte) error

	// Subscribe subscribes to a topic with validation
	Subscribe(topic string, validator func(data []byte, validatorData interface{}) (ValidationResult, interface{})) (uint64, error)

	// Unsubscribe unsubscribes from a topic
	Unsubscribe(topic string, subID uint64) error

	// UnregisterMessageHandler unregisters a message handler
	UnregisterMessageHandler(address string)
}

// ValidationResult represents the result of message validation
type ValidationResult int

const (
	ValidationAccept = pubsub.ValidationAccept
	ValidationReject = pubsub.ValidationReject
	ValidationIgnore = pubsub.ValidationIgnore
)

// Libp2p represents a libp2p network implementation
type Libp2p struct {
	hostID crypto.ID
}
