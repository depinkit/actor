package actor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var (
	DefaultDispatchGCInterval = 120 * time.Second
	DefaultDispatchWorkers    = 1
)

// Dispatch provides a reaction kernel with multithreaded dispatch and oneshot
// continuations.
type Dispatch struct {
	ctx   context.Context
	close func()

	sctx SecurityContext

	mx        sync.Mutex
	q         chan Envelope // incoming message queue
	vq        chan Envelope // verified message queue
	behaviors map[string]*BehaviorState
	started   bool

	options DispatchOptions
}

type DispatchOptions struct {
	Limiter    RateLimiter
	GCInterval time.Duration
	Workers    int
}

type BehaviorState struct {
	cont Behavior
	opt  BehaviorOptions
}

type DispatchOption func(o *DispatchOptions)

func WithDispatchWorkers(count int) DispatchOption {
	return func(o *DispatchOptions) {
		o.Workers = count
	}
}

func WithDispatchGCInterval(dt time.Duration) DispatchOption {
	return func(o *DispatchOptions) {
		o.GCInterval = dt
	}
}

func WithRateLimiter(limiter RateLimiter) DispatchOption {
	return func(o *DispatchOptions) {
		o.Limiter = limiter
	}
}

func NewDispatch(sctx SecurityContext, opt ...DispatchOption) *Dispatch {
	ctx, cancel := context.WithCancel(context.Background())
	k := &Dispatch{
		sctx:      sctx,
		ctx:       ctx,
		close:     cancel,
		q:         make(chan Envelope),
		vq:        make(chan Envelope),
		behaviors: make(map[string]*BehaviorState),
		options: DispatchOptions{
			GCInterval: DefaultDispatchGCInterval,
			Workers:    DefaultDispatchWorkers,
			Limiter:    NoRateLimiter{},
		},
	}

	for _, f := range opt {
		f(&k.options)
	}

	return k
}

func (k *Dispatch) Start() {
	k.mx.Lock()
	defer k.mx.Unlock()

	if !k.started {
		for i := 0; i < k.options.Workers; i++ {
			go k.recv()
		}
		go k.dispatch()
		go k.gc()
		k.started = true
	}
}

func (k *Dispatch) AddBehavior(behavior string, continuation Behavior, opt ...BehaviorOption) error {
	st := &BehaviorState{
		cont: continuation,
		opt: BehaviorOptions{
			Capability: []Capability{Capability(behavior)},
		},
	}

	for _, f := range opt {
		if err := f(&st.opt); err != nil {
			return fmt.Errorf("adding behavior: %w", err)
		}
	}

	k.mx.Lock()
	defer k.mx.Unlock()
	k.behaviors[behavior] = st

	return nil
}

func (k *Dispatch) RemoveBehavior(behavior string) {
	k.mx.Lock()
	defer k.mx.Unlock()

	delete(k.behaviors, behavior)
}

func (k *Dispatch) Receive(msg Envelope) error {
	select {
	case k.q <- msg:
		return nil
	case <-k.ctx.Done():
		return k.ctx.Err()
	}
}

func (k *Dispatch) Context() context.Context {
	return k.ctx
}

func (k *Dispatch) recv() {
	for {
		select {
		case msg := <-k.q:
			if err := k.sctx.Verify(msg); err != nil {
				log.Debugf("failed to verify message from %s: %s", msg.From, err)
				return
			}

			k.vq <- msg
		case <-k.ctx.Done():
			return
		}
	}
}

func (k *Dispatch) dispatch() {
	for {
		select {
		case msg := <-k.vq:
			k.mx.Lock()
			b, ok := k.behaviors[msg.Behavior]

			if !ok {
				k.mx.Unlock()
				log.Debugf("unknown behavior %s", msg.Behavior)
				continue
			}

			if b.Expired(time.Now()) {
				delete(k.behaviors, msg.Behavior)
				k.mx.Unlock()
				log.Debugf("expired behavior %s", msg.Behavior)
				continue
			}

			if msg.IsBroadcast() {
				if err := k.sctx.RequireBroadcast(msg, b.opt.Topic, b.opt.Capability); err != nil {
					k.mx.Unlock()
					log.Warnf("broadcast message from %s does not have the required capability %s: %s", msg.From, b.opt.Capability, err)
					continue
				}
			} else if err := k.sctx.Require(msg, b.opt.Capability); err != nil {
				k.mx.Unlock()
				log.Warnf("message from %s does not have the required capability %s: %s", msg.From, b.opt.Capability, err)
				continue
			}

			if b.opt.OneShot {
				delete(k.behaviors, msg.Behavior)
			}

			k.mx.Unlock()

			if err := k.options.Limiter.Acquire(msg); err != nil {
				k.sctx.Discard(msg)
				log.Debugf("limiter rejected message from %s: %s", msg.From, err)
				continue
			}

			msg.Discard = func() {
				k.sctx.Discard(msg)
			}

			go func() {
				defer k.options.Limiter.Release(msg)
				b.cont(msg)
			}()

		case <-k.ctx.Done():
			return
		}
	}
}

func (k *Dispatch) gc() {
	ticker := time.NewTicker(k.options.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			k.mx.Lock()
			now := time.Now()
			for x, b := range k.behaviors {
				if b.Expired(now) {
					delete(k.behaviors, x)
				}
			}
			k.mx.Unlock()
		case <-k.ctx.Done():
			return
		}
	}
}

func (b *BehaviorState) Expired(now time.Time) bool {
	if b.opt.Expire > 0 {
		return uint64(now.UnixNano()) > b.opt.Expire
	}
	return false
}

func WithBehaviorExpiry(expire uint64) BehaviorOption {
	return func(opt *BehaviorOptions) error {
		opt.Expire = expire
		return nil
	}
}

func WithBehaviorCapability(require ...Capability) BehaviorOption {
	return func(opt *BehaviorOptions) error {
		opt.Capability = require
		return nil
	}
}

func WithBehaviorOneShot(oneShot bool) BehaviorOption {
	return func(opt *BehaviorOptions) error {
		opt.OneShot = oneShot
		return nil
	}
}

func WithBehaviorTopic(topic string) BehaviorOption {
	return func(opt *BehaviorOptions) error {
		opt.Topic = topic
		return nil
	}
}
