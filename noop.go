package actor

import (
	"context"
	"sync"

	"github.com/depinkit/did"
)

type NoopActor struct {
	mx       sync.Mutex
	ctx      context.Context
	security SecurityContext

	handle     Handle
	supervisor Handle
	parent     Handle
	children   map[did.DID]Handle

	// Testing helpers
	sentMessages     []Envelope
	receivedMessages []Envelope
	behaviors        map[string]Behavior
	invokeResponses  map[string]Envelope
}

var _ Actor = (*NoopActor)(nil)

func NewNoopActor() *NoopActor {
	return &NoopActor{
		mx:               sync.Mutex{},
		ctx:              context.Background(),
		security:         &BasicSecurityContext{},
		handle:           Handle{},
		supervisor:       Handle{},
		parent:           Handle{},
		children:         make(map[did.DID]Handle),
		sentMessages:     []Envelope{},
		receivedMessages: []Envelope{},
		behaviors:        make(map[string]Behavior),
		invokeResponses:  make(map[string]Envelope),
	}
}

func (c *NoopActor) Context() context.Context  { return c.ctx }
func (c *NoopActor) Handle() Handle            { return c.handle }
func (c *NoopActor) Security() SecurityContext { return c.security }
func (c *NoopActor) Supervisor() Handle        { return c.supervisor }

func (c *NoopActor) AddBehavior(name string, behavior Behavior, _ ...BehaviorOption) error {
	c.mx.Lock()
	defer c.mx.Unlock()
	c.behaviors[name] = behavior
	return nil
}

func (c *NoopActor) RemoveBehavior(name string) {
	c.mx.Lock()
	defer c.mx.Unlock()
	delete(c.behaviors, name)
}

func (c *NoopActor) Receive(msg Envelope) error {
	c.mx.Lock()
	c.receivedMessages = append(c.receivedMessages, msg)
	behavior, exists := c.behaviors[msg.Behavior]
	c.mx.Unlock()

	if exists && behavior != nil {
		behavior(msg)
	}
	return nil
}

func (c *NoopActor) Send(msg Envelope) error {
	c.mx.Lock()
	defer c.mx.Unlock()
	c.sentMessages = append(c.sentMessages, msg)
	return nil
}

func (c *NoopActor) Invoke(msg Envelope) (<-chan Envelope, error) {
	c.mx.Lock()
	defer c.mx.Unlock()

	ch := make(chan Envelope, 1)
	if response, exists := c.invokeResponses[msg.Behavior]; exists {
		ch <- response
	}
	close(ch)
	return ch, nil
}

func (c *NoopActor) Publish(msg Envelope) error {
	c.mx.Lock()
	defer c.mx.Unlock()
	c.sentMessages = append(c.sentMessages, msg)
	return nil
}

func (c *NoopActor) Subscribe(_ string, _ ...BroadcastSetup) error { return nil }

func (c *NoopActor) Start() error { return nil }
func (c *NoopActor) Stop() error  { return nil }

func (c *NoopActor) CreateChild(_ string, _ Handle, _ ...CreateChildOption) (Actor, error) {
	return NewNoopActor(), nil
}

func (c *NoopActor) Parent() Handle               { return c.parent }
func (c *NoopActor) Children() map[did.DID]Handle { return c.children }

func (c *NoopActor) Limiter() RateLimiter { return NoRateLimiter{} }

// Testing helper methods

func (c *NoopActor) GetSentMessages() []Envelope {
	c.mx.Lock()
	defer c.mx.Unlock()
	messages := make([]Envelope, len(c.sentMessages))
	copy(messages, c.sentMessages)
	return messages
}

func (c *NoopActor) SetHandle(handle Handle) {
	c.mx.Lock()
	defer c.mx.Unlock()
	c.handle = handle
}
