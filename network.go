package actor

import (
	"context"
	"time"

	"github.com/depinkit/network/libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"gitlab.com/nunet/device-management-service/types"
)

// Network represents a network interface for actor communication
type Network interface {
	// GetHostID returns the host ID of this network node
	GetHostID() peer.ID

	// HandleMessage registers a message handler for a specific address
	HandleMessage(address string, handler func(data []byte, srcPeerID peer.ID)) error

	// SendMessage sends a message to a specific peer
	SendMessage(ctx context.Context, peerID string, msg types.MessageEnvelope, expiry time.Time) error

	// Publish publishes a message to a topic
	Publish(ctx context.Context, topic string, data []byte) error

	// Subscribe subscribes to a topic with validation
	Subscribe(ctx context.Context, topic string, handler func(data []byte), validator libp2p.Validator) (uint64, error)

	// Unsubscribe unsubscribes from a topic
	Unsubscribe(topic string, subID uint64) error

	// UnregisterMessageHandler unregisters a message handler
	UnregisterMessageHandler(address string)
}

// ValidationResult represents the result of message validation
type ValidationResult = pubsub.ValidationResult

const (
	ValidationAccept = pubsub.ValidationAccept
	ValidationReject = pubsub.ValidationReject
	ValidationIgnore = pubsub.ValidationIgnore
)

// MessageEnvelope is a wrapper for actor messages sent over the network
type MessageEnvelope struct {
	Type MessageType
	Data []byte
}

type MessageType string
