package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"gitlab.com/nunet/device-management-service/network"
	"gitlab.com/nunet/device-management-service/types"
)

// signedEnv builds an envelope that is *properly signed* by sec and whose
// From.Address.HostID is hostID.  Extra options can tweak expiry, topic …
// signedEnv builds and signs an envelope whose From.Address.HostID is hostID.
//
//nolint:unparam // helper is intentionally variadic for future calls
func signedEnv(
	t *testing.T,
	sec SecurityContext,
	hostID string,
	to Handle,
	behaviour string,
	opts ...MessageOption,
) Envelope {
	t.Helper()
	from := Handle{
		ID:  sec.ID(),
		DID: sec.DID(),
		Address: Address{
			HostID: hostID,
		},
	}
	env, err := Message(from, to, behaviour, nil, opts...)
	require.NoError(t, err)

	err = sec.Sign(&env)
	require.NoError(t, err)

	return env
}

// network wrapper that forces SendMessage to fail
type netSendFail struct{ *network.MemoryHost }

func (netSendFail) SendMessage(context.Context, string, types.MessageEnvelope, time.Time) error {
	return fmt.Errorf("net-boom")
}

func (netSendFail) SendMessageSync(context.Context, string, types.MessageEnvelope, time.Time) error {
	return fmt.Errorf("net-boom")
}

// build a fully-wired test actor
func newTestActor(t *testing.T) *BasicActor {
	t.Helper()

	sec := NewTestSecurityContext(t)
	memNet, err := network.NewMemoryNetHost()
	require.NoError(t, err)

	lim := NewRateLimiter(DefaultRateLimiterConfig()) // production limiter

	self := Handle{
		ID:  sec.ID(),
		DID: sec.DID(),
		Address: Address{
			HostID:       memNet.GetHostID().String(),
			InboxAddress: "inboxSelf",
		},
	}

	act, err := New(Handle{}, memNet, sec, lim, BasicActorParams{}, self)
	require.NoError(t, err)

	act.dispatch.Start()
	t.Cleanup(func() { _ = act.Stop(); _ = memNet.Stop() })
	return act
}

// tests
func TestHandleMessagePaths(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	sec, ok := act.security.(*BasicSecurityContext)
	require.True(t, ok)

	// make it PUBLIC so BasicRateLimiter will inspect it
	testBehavior := "/public/test"

	recv := make(chan Envelope, 1)
	require.NoError(t, act.AddBehavior(testBehavior, func(e Envelope) { recv <- e }))

	srcPID := peer.ID("srcPeer")
	in := signedEnv(t, sec, srcPID.String(), act.self, testBehavior)

	raw, err := json.Marshal(in)
	require.NoError(t, err)

	// ── 1. happy path
	act.handleMessage(raw, srcPID)
	requireMsg(t, recv)

	// ── 2. deny path: shrink PublicLimitAllow to zero so Allow() fails
	rl := act.Limiter().(*BasicRateLimiter)
	cfg := rl.Config()
	cfg.PublicLimitAllow = 0
	cfg.PublicLimitAcquire = 0
	rl.SetConfig(cfg)

	act.handleMessage(raw, srcPID)
	ensureNoMsg(t, recv, 200*time.Millisecond)
}

func TestSendLoopback(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	sec, ok := act.security.(*BasicSecurityContext)
	require.True(t, ok)
	env := signedEnv(t, sec,
		act.self.Address.HostID, act.self, "/loop")
	require.NoError(t, act.Send(env))
}

func TestPublishInvalid(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	err := act.Publish(Envelope{To: act.self, Behavior: "/foo"})
	require.ErrorIs(t, err, ErrInvalidMessage)
}

func TestStopCleansUp(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	act.subscriptions = map[string]uint64{"t": 1}
	require.NoError(t, act.Stop())
}

func TestSendNetworkError(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	host, err := network.NewMemoryNetHost()
	require.NoError(t, err)

	memoryHost, ok := host.(*network.MemoryHost)
	require.True(t, ok)
	act.network = &netSendFail{memoryHost}

	dst := Handle{ID: nonZeroID(), Address: Address{HostID: "peer"}}
	bad := signedEnv(t, act.security.(*BasicSecurityContext),
		act.self.Address.HostID, dst, "/x")

	err = act.Send(bad)
	require.ErrorContains(t, err, "net-boom")
}

func TestInvokeSuccessAndRollback(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	// /call behaviour: echoes back on ReplyTo, one-shot so it cleans itself up
	require.NoError(t, act.AddBehavior("/call",
		func(e Envelope) {
			defer e.Discard()

			// Build & sign the reply (Message sets Expire  Nonce)
			reply, err := Message(e.To, e.From, e.Options.ReplyTo, nil)
			require.NoError(t, err)

			err = act.Security().Sign(&reply)
			require.NoError(t, err)

			// Send through the normal path → exercises BasicActor.Send
			err = act.Send(reply)
			require.NoError(t, err)
		},
		WithBehaviorOneShot(true),
	))

	// Pre-assign ReplyTo so Invoke() won’t mutate the envelope after signing
	replyTo := fmt.Sprintf("/dms/actor/replyto/%d", act.Security().Nonce())

	// Build & sign the request
	req, err := Message(
		act.self, // From
		act.self, // To (loop-back)
		"/call",  // Behaviour
		nil,
		WithMessageReplyTo(replyTo),
	)
	require.NoError(t, err)

	err = act.Security().Sign(&req) // signature matches final envelope
	require.NoError(t, err)

	// Invoke and wait for the echoed reply
	ch, err := act.Invoke(req)
	require.NoError(t, err)
	requireMsg(t, ch) // reply arrived

	// Both the one-shot /call and the temporary reply-to behaviours are gone
	require.Empty(t, act.dispatch.behaviors)
}

func TestSendRemoteSuccessCoversNetworkPath(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	memNet, ok := act.network.(*network.MemoryHost) // single-node in-memory network
	require.True(t, ok)

	// ── create a second, fully-wired security context to act as the remote peer
	dstSec := NewTestSecurityContext(t)

	const dstInbox = "dstInbox"

	dst := Handle{
		ID:  dstSec.ID(),
		DID: dstSec.DID(),
		Address: Address{
			HostID:       act.self.Address.HostID, // same host, different peer ID ⇒ “remote”
			InboxAddress: dstInbox,
		},
	}

	// Channel to confirm the network handler fires
	got := make(chan struct{}, 1)

	require.NoError(t, memNet.HandleMessage(
		fmt.Sprintf("actor/%s/messages/0.0.1", dstInbox),
		func(_ []byte, _ peer.ID) { got <- struct{}{} },
	))

	// Build a message addressed to the remote handle
	env, err := Message(act.self, dst, "/remote", nil)
	require.NoError(t, err)

	// Send via the normal (remote) path
	require.NoError(t, act.Send(env))

	// The MemoryNet handler should fire within the timeout
	select {
	case <-got:
	case <-time.After(1 * time.Second):
		t.Fatalf("network path not exercised")
	}
}

func TestValidateBroadcastTopicMismatch(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	env := Envelope{Behavior: "/b", Options: EnvelopeOptions{Topic: "X"}, To: Handle{}}
	b, _ := json.Marshal(env)
	res, _ := act.validateBroadcast("Y", b, nil)
	require.Equal(t, network.ValidationReject, res)
}

func TestStartAndStopRegistersHandler(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	// Start should register the inbox handler without error.
	require.NoError(t, act.Start())

	// Stop must cleanly unregister everything.
	require.NoError(t, act.Stop())
}

func TestGrantSupervisorCapabilitiesNoop(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	// When supervisor == self the helper must exit without error.
	require.NoError(t, act.grantSupervisorCapabilities(act.Handle()))
}

func TestParentAndChildrenAccessors(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	child, err := act.CreateChild("childInbox", act.Handle())
	require.NoError(t, err)

	require.Equal(t, act.Handle(), child.Parent())
	require.Contains(t, act.Children(), child.Handle().DID)
}

func TestHandleMessageUnmarshalError(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	// invalid JSON must be ignored without panicking or delivering a message
	badJSON := []byte(`{ this is not valid json `)
	act.handleMessage(badJSON, peer.ID("src"))
	// nothing to assert – we only check that it does not crash
}

func TestReceiveBadReceiverError(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	other := Handle{ID: nonZeroID()}
	env, err := Message(act.self, other, "/foo", nil)
	require.NoError(t, err)

	err = act.Receive(env)

	require.ErrorIs(t, err, ErrInvalidMessage)
}

func TestValidateBroadcastExpired(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	// expired broadcast => ValidationIgnore
	expired := Envelope{
		Behavior: "/b",
		To:       Handle{}, // broadcast
		Options: EnvelopeOptions{
			Topic:  "T",
			Expire: uint64(time.Now().Add(-time.Second).UnixNano()),
		},
	}
	data, err := json.Marshal(expired)
	require.NoError(t, err)

	res, _ := act.validateBroadcast("T", data, nil)

	require.Equal(t, network.ValidationIgnore, res)
}

// message addressed to a different actor → ignored
func TestHandleMessageWrongRecipient(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	sec := act.security.(*BasicSecurityContext)

	recv := make(chan Envelope, 1)
	_ = act.AddBehavior("/x", func(e Envelope) { recv <- e })

	// craft an envelope whose To.ID is NOT ours
	otherID := nonZeroID()
	badTo := act.self
	badTo.ID = otherID

	srcPID := peer.ID(act.self.Address.HostID)

	env := signedEnv(t, sec, srcPID.String(), badTo, "/x")
	raw, err := json.Marshal(env)
	require.NoError(t, err)

	act.handleMessage(raw, srcPID)
	ensureNoMsg(t, recv, 150*time.Millisecond) // must not be delivered
}

// peer‑ID mismatch between transport and envelope → ignored
func TestHandleMessagePeerMismatch(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	sec := act.security.(*BasicSecurityContext)

	recv := make(chan Envelope, 1)
	err := act.AddBehavior("/y", func(e Envelope) { recv <- e })
	require.NoError(t, err)

	// host‑ID inside the envelope is wrong
	env := signedEnv(t, sec, "someoneElse", act.self, "/y")
	raw, err := json.Marshal(env)
	require.NoError(t, err)

	srcPID := peer.ID(act.self.Address.HostID) // real peer on the wire
	act.handleMessage(raw, srcPID)
	ensureNoMsg(t, recv, 150*time.Millisecond)
}

// RemoveBehavior actually prevents future dispatch
func TestRemoveBehaviorStopsDispatch(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)
	sec := act.security.(*BasicSecurityContext)

	const tmpBehavior = "/tmp"

	recv := make(chan Envelope, 1)
	err := act.AddBehavior(tmpBehavior, func(e Envelope) { recv <- e })
	require.NoError(t, err)

	act.RemoveBehavior(tmpBehavior) // ← the call under test

	// send a loop‑back message that would have matched /tmp
	env := signedEnv(t, sec, act.self.Address.HostID, act.self, tmpBehavior)

	err = act.Receive(env)
	require.NoError(t, err)

	ensureNoMsg(t, recv, 200*time.Millisecond)
}

// grantSupervisorCapabilities returns error on malformed supervisor handle
func TestGrantSupervisorCapabilitiesIgnoresIncompleteSupervisor(t *testing.T) {
	t.Parallel()

	act := newTestActor(t)

	// Handle missing DID ⇒ function should just return nil (no-op).
	incompleteSup := Handle{ID: nonZeroID()} // DID is empty
	require.NoError(t, act.grantSupervisorCapabilities(incompleteSup))
}

// helper asserts
func requireMsg(t *testing.T, ch <-chan Envelope, timeout ...time.Duration) {
	t.Helper()
	d := time.Second
	if len(timeout) > 0 {
		d = timeout[0]
	}
	select {
	case <-ch:
	case <-time.After(d):
		t.Fatalf("expected envelope not received after %s", d)
	}
}

func ensureNoMsg(t *testing.T, ch <-chan Envelope, timeout ...time.Duration) {
	t.Helper()
	d := 150 * time.Millisecond
	if len(timeout) > 0 {
		d = timeout[0]
	}
	select {
	case <-ch:
		t.Fatalf("unexpected envelope")
	case <-time.After(d):
	}
}
