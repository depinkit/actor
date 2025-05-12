// security_unit_test.go — focused tests for BasicSecurityContext.
//
// We rely on the real capability‑context helpers that exist in the repo
// (MakeRootTrustContext, MakeCapabilityContext, …) to avoid hand‑rolled stubs
// that might drift from the interface definition.

package actor

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/nunet/device-management-service/lib/crypto"
	"gitlab.com/nunet/device-management-service/lib/ucan"
)

// newCtxPair creates two independent BasicSecurityContexts rooted in their own
// trust trees; they can be used as source/destination handles in the tests.
func newCtxPair(t *testing.T) (*BasicSecurityContext, *BasicSecurityContext) {
	t.Helper()

	// root A
	rootADID, rootATrust := MakeRootTrustContext(t)
	// root B
	rootBDID, rootBTrust := MakeRootTrustContext(t)

	// actor alice
	privA, pubA, _ := crypto.GenerateKeyPair(crypto.Ed25519)
	aliceDID, aliceTrust := MakeTrustContext(t, privA)
	capA := MakeCapabilityContext(t, aliceDID, rootADID, aliceTrust, rootATrust)
	aliceCtx, _ := NewBasicSecurityContext(pubA, privA, capA)

	// actor bob
	privB, pubB, _ := crypto.GenerateKeyPair(crypto.Ed25519)
	bobDID, bobTrust := MakeTrustContext(t, privB)
	capB := MakeCapabilityContext(t, bobDID, rootBDID, bobTrust, rootBTrust)
	bobCtx, _ := NewBasicSecurityContext(pubB, privB, capB)

	return aliceCtx, bobCtx
}

// makeHandle wraps a security‑context into a Handle (same pattern as other tests).
func makeHandle(t *testing.T, s *BasicSecurityContext, host, box string) Handle {
	t.Helper()
	return Handle{
		ID:  s.ID(),
		DID: s.DID(),
		Address: Address{
			HostID:       host,
			InboxAddress: box,
		},
	}
}

func TestSecurityProvideRequireHappyAndBadSender(t *testing.T) {
	t.Parallel()

	aliceCtx, bobCtx := newCtxPair(t)

	alice := makeHandle(t, aliceCtx, "hostAlice", "inAlice")
	bob := makeHandle(t, bobCtx, "hostBob", "inBob")

	// Happy path: sender and receiver are the same handle
	selfMsg, err := Message(alice, alice, "/self", nil)
	require.NoError(t, err)

	require.NoError(t, aliceCtx.Provide(&selfMsg, nil, nil))
	require.NoError(t, aliceCtx.Require(selfMsg, nil))

	// Error path: missing capabilities when talking to another actor
	msg, _ := Message(alice, bob, "/be", nil)
	err = aliceCtx.Provide(&msg, nil, nil)
	require.Error(t, err)

	// Sign() bad‑sender branch
	bad := selfMsg
	bad.From = bob
	require.ErrorIs(t, aliceCtx.Sign(&bad), ErrBadSender)
}

func TestSecurityBroadcastTopicMismatch(t *testing.T) {
	t.Parallel()

	aliceCtx, _ := newCtxPair(t)
	alice := makeHandle(t, aliceCtx, "hostAlice", "inAlice")

	bcast, _ := Message(alice, Handle{}, "/b", nil, WithMessageTopic("topic‑X"))
	require.True(t, bcast.IsBroadcast())

	require.NoError(t, aliceCtx.ProvideBroadcast(&bcast, "topic‑X", []Capability{"/b"}))
	require.Error(t, aliceCtx.RequireBroadcast(bcast, "otherTopic", []Capability{"/b"}))
}

func TestSecurityVerifyExpiresAndTamper(t *testing.T) {
	t.Parallel()

	aliceCtx, _ := newCtxPair(t)
	alice := makeHandle(t, aliceCtx, "h", "i")

	// expired
	expired, _ := Message(alice, alice, "/x", nil,
		WithMessageExpiry(uint64(time.Now().Add(-time.Second).UnixNano())))
	require.ErrorIs(t, aliceCtx.Verify(expired), ErrMessageExpired)

	// tamper signature
	msg, _ := Message(alice, alice, "/x", nil)
	require.NoError(t, aliceCtx.Sign(&msg))
	msg.Message = []byte("evil") // tamper after signing
	require.ErrorIs(t, aliceCtx.Verify(msg), ErrSignatureVerification)
}

func TestSecurityNonceConcurrency(t *testing.T) {
	t.Parallel()

	aliceCtx, _ := newCtxPair(t)

	const threads = 64
	var wg sync.WaitGroup
	seen := make(map[uint64]struct{})
	var mx sync.Mutex

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n := aliceCtx.Nonce()
			mx.Lock()
			seen[n] = struct{}{}
			mx.Unlock()
		}()
	}
	wg.Wait()
	require.Len(t, seen, threads)
}

func TestSecurityGrantAddRootsError(t *testing.T) {
	t.Parallel()

	rootDID, rootTrust := MakeRootTrustContext(t)
	priv, pub, _ := crypto.GenerateKeyPair(crypto.Ed25519)
	aliceDID, aliceTrust := MakeTrustContext(t, priv)
	capCtx := MakeCapabilityContext(t, aliceDID, rootDID, aliceTrust, rootTrust)

	aliceCtx, _ := NewBasicSecurityContext(pub, priv, capCtx)

	// Success path
	require.NoError(t, aliceCtx.Grant(aliceCtx.DID(), rootDID,
		[]ucan.Capability{"/do/something"}, time.Minute))

	// force an error with a negative expiry
	err := aliceCtx.Grant(aliceCtx.DID(), rootDID, nil, -24*time.Hour)
	require.Error(t, err)
}
