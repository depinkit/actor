package actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/ucan"
)

// helper constructors & test doubles
// signSelf returns a self-addressed, correctly-signed envelope so Verify /
// Require both succeed with no extra capability tokens.
// TODO: why not use the same helper within basic_unit_test.go
func signSelf(t *testing.T, sec *BasicSecurityContext, beh string, opts ...MessageOption) Envelope {
	t.Helper()
	self := Handle{ID: sec.ID(), DID: sec.DID()}
	e, err := Message(self, self, beh, nil, opts...)
	require.NoError(t, err)

	err = sec.Sign(&e)
	require.NoError(t, err)
	return e
}

// testRateLimiter is a simple toggleable limiter used to hit allow/deny paths.
type testRateLimiter struct {
	allow bool
	busy  int32
}

func (l *testRateLimiter) Allow(Envelope) bool { return l.allow }

func (l *testRateLimiter) Acquire(Envelope) error { l.busy++; return nil }

func (l *testRateLimiter) Release(Envelope) { l.busy-- }

func (l *testRateLimiter) Config() RateLimiterConfig { return RateLimiterConfig{} }

func (l *testRateLimiter) SetConfig(RateLimiterConfig) {}

// rateLimiterAcquireErr always errors on Acquire to test rejection paths.
type rateLimiterAcquireErr struct{ testRateLimiter }

func (rateLimiterAcquireErr) Acquire(Envelope) error { return errors.New("acq-boom") }

// rlPanic records Release even if the continuation panics.
type rlPanic struct {
	testRateLimiter
	released chan struct{}
}

func (r *rlPanic) Release(Envelope) { r.released <- struct{}{} }

const (
	waitVeryShort = 20 * time.Millisecond
	waitShort     = 50 * time.Millisecond
	waitMedium    = 100 * time.Millisecond
)

func TestDispatchExpiredHelper(t *testing.T) {
	t.Parallel()

	b := &BehaviorState{
		opt: BehaviorOptions{Expire: uint64(time.Now().Add(-time.Second).UnixNano())},
	}
	require.True(t, b.Expired(time.Now()))
}

func TestDispatchOneShot(t *testing.T) {
	t.Parallel()

	sec := NewTestSecurityContext(t)
	d := NewDispatch(sec, WithRateLimiter(&testRateLimiter{allow: true}))
	d.Start()
	t.Cleanup(d.Stop)

	const oneshotBehavior = "/oneshot"

	done := make(chan struct{}, 1)
	require.NoError(t, d.AddBehavior(oneshotBehavior,
		func(Envelope) { done <- struct{}{} },
		WithBehaviorOneShot(true)),
	)

	err := d.Receive(signSelf(t, sec, oneshotBehavior))
	require.NoError(t, err)

	<-done
	require.Empty(t, d.behaviors)
}

func TestDispatchLimiterReject(t *testing.T) {
	t.Parallel()

	sec := NewTestSecurityContext(t)
	rl := &rateLimiterAcquireErr{testRateLimiter{allow: true}}
	d := NewDispatch(sec, WithRateLimiter(rl))
	d.Start()
	t.Cleanup(d.Stop)

	const rejectBehavior = "/reject"

	called := false
	err := d.AddBehavior(rejectBehavior, func(Envelope) { called = true })
	require.NoError(t, err)

	err = d.Receive(signSelf(t, sec, rejectBehavior))
	require.NoError(t, err)

	time.Sleep(waitVeryShort)
	require.False(t, called)
}

func TestDispatchBroadcast(t *testing.T) {
	t.Parallel()

	// TODO: is this spy needed here?
	spy := NewSpySecurity(t)

	d := NewDispatch(spy, WithRateLimiter(&testRateLimiter{allow: true}))
	d.Start()
	t.Cleanup(d.Stop)

	const bcastBehavior = "/bcast"

	got := make(chan struct{}, 1)
	require.NoError(t, d.AddBehavior(bcastBehavior,
		func(Envelope) { got <- struct{}{} },
		WithBehaviorTopic("T"),
	))

	// Proper broadcast envelope
	from := Handle{ID: spy.ID(), DID: spy.DID()}
	env, err := Message(from, Handle{}, bcastBehavior, nil, WithMessageTopic("T"))
	require.NoError(t, err)

	require.NoError(t, spy.ProvideBroadcast(&env, "T", []Capability{Capability(bcastBehavior)}))

	err = d.Receive(env)
	require.NoError(t, err)

	select {
	case <-got:
	case <-time.After(waitShort):
		t.Fatalf("broadcast behaviour not invoked")
	}
}

func TestDispatchGCremoves(t *testing.T) {
	t.Parallel()

	sec := NewTestSecurityContext(t)
	d := NewDispatch(sec,
		WithRateLimiter(&testRateLimiter{allow: true}),
		WithDispatchGCInterval(5*time.Millisecond),
	)
	d.Start()
	t.Cleanup(d.Stop)

	exp := uint64(time.Now().Add(2 * time.Millisecond).UnixNano())
	err := d.AddBehavior("/temp", func(Envelope) {}, WithBehaviorExpiry(exp))
	require.NoError(t, err)

	time.Sleep(waitVeryShort + 5*time.Millisecond)
	require.Empty(t, d.behaviors)
}

func TestAddBehaviorOptionError(t *testing.T) {
	t.Parallel()

	d := NewDispatch(NewTestSecurityContext(t))
	expectedErr := errors.New("opt-boom")

	err := d.AddBehavior("/bad", nil, func(*BehaviorOptions) error { return expectedErr })
	require.ErrorIs(t, err, expectedErr)
	require.Empty(t, d.behaviors)
}

func TestDispatchReceiveAfterStop(t *testing.T) {
	t.Parallel()

	d := NewDispatch(NewTestSecurityContext(t))
	d.Start()
	d.Stop()

	err := d.Receive(Envelope{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestDispatchStartIdempotent(t *testing.T) {
	t.Parallel()

	d := NewDispatch(NewTestSecurityContext(t))
	d.Start()
	t.Cleanup(d.Stop)

	firstCtx := d.ctx
	d.Start() // second call is a no-op
	require.Equal(t, firstCtx, d.ctx)
}

func TestWithBehaviorCapability(t *testing.T) {
	t.Parallel()

	var opt BehaviorOptions
	require.NoError(t, WithBehaviorCapability(Capability("/x"), Capability("/y"))(&opt))
	require.ElementsMatch(t, []Capability{"/x", "/y"}, opt.Capability)
}

func TestDispatchReleaseOnPanic(t *testing.T) {
	t.Parallel()

	sec := NewTestSecurityContext(t)
	released := make(chan struct{}, 1)

	rl := &rlPanic{
		testRateLimiter: testRateLimiter{allow: true},
		released:        released,
	}

	d := NewDispatch(sec, WithRateLimiter(rl))
	d.Start()
	t.Cleanup(d.Stop)

	require.NoError(t, d.AddBehavior("/panic", func(Envelope) {
		defer func() { _ = recover() }()
		panic("boom")
	}))

	err := d.Receive(signSelf(t, sec, "/panic"))
	require.NoError(t, err)

	select {
	case <-released:
	case <-time.After(waitMedium):
		t.Fatalf("Release not called after panic")
	}
}

func TestBehaviorOptionsHelpers(t *testing.T) {
	t.Parallel()

	var opt BehaviorOptions
	require.NoError(t, WithBehaviorExpiry(123)(&opt))
	require.EqualValues(t, 123, opt.Expire)

	require.NoError(t, WithBehaviorOneShot(true)(&opt))
	require.True(t, opt.OneShot)
}

func TestNewDispatch(t *testing.T) {
	t.Parallel()
	sc := generateSecurityContext(t)
	d := NewDispatch(sc, WithDispatchWorkers(5), WithDispatchGCInterval(60*time.Second))
	require.Equal(t, 5, d.options.Workers)
	require.Equal(t, 60*time.Second, d.options.GCInterval)
}

func TestDispatchStart(t *testing.T) {
	t.Parallel()
	sc := generateSecurityContext(t)
	d := NewDispatch(sc, WithDispatchWorkers(3))
	d.Start()
	require.True(t, d.running)
}

func TestDispatchAddBehavior(t *testing.T) {
	t.Parallel()
	sc := generateSecurityContext(t)
	d := NewDispatch(sc)
	d.Start()

	behavior := func(_ Envelope) {}

	err := d.AddBehavior("test", behavior)
	assert.NoError(t, err)
	assert.Len(t, d.behaviors, 1)

	d.RemoveBehavior("test")
	assert.Len(t, d.behaviors, 0)
}

func TestDispatchReceive(t *testing.T) {
	t.Parallel()
	sc := generateSecurityContext(t)
	d := NewDispatch(sc)
	d.Start()

	behaviorExecuted := make(chan bool)

	behavior := func(_ Envelope) {
		behaviorExecuted <- true
	}

	err := d.AddBehavior("/test/1", behavior)
	assert.NoError(t, err)

	me := Handle{
		ID:  sc.ID(),
		DID: sc.DID(),
		Address: Address{
			HostID:       "123",
			InboxAddress: "111",
		},
	}

	msg, err := Message(me, me, "/test/1", nil, WithMessageSignature(sc, []ucan.Capability{ucan.Capability("/test/1")}, nil))
	assert.NoError(t, err)

	err = d.Receive(msg)
	assert.NoError(t, err)

	select {
	case <-behaviorExecuted:
	case <-time.After(2 * time.Second):
		t.Fatal("behavior was not executed")
	}
}

func TestDispatchGC(t *testing.T) {
	t.Parallel()
	sc := generateSecurityContext(t)
	d := NewDispatch(sc, WithDispatchGCInterval(10*time.Millisecond))
	d.Start()

	behavior := func(_ Envelope) {}
	expireTime := uint64(time.Now().Add(10 * time.Millisecond).UnixNano())
	err := d.AddBehavior("test", behavior, WithBehaviorExpiry(expireTime))
	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond)
	assert.Len(t, d.behaviors, 0)
}

func generateSecurityContext(t *testing.T) *BasicSecurityContext {
	t.Helper()

	priv, pub, err := crypto.GenerateKeyPair(crypto.Ed25519)
	assert.NoError(t, err)

	rootDID, rootTrust := MakeRootTrustContext(t)
	actorDID, actorTrust := MakeRootTrustContext(t)
	actorCap := MakeCapabilityContext(t, actorDID, rootDID, actorTrust, rootTrust)

	sc, err := NewBasicSecurityContext(pub, priv, actorCap)
	assert.NoError(t, err)
	return sc
}
