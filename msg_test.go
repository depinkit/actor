// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/depinkit/crypto"
)

// Message construction helpers
func TestMsgConstructionAndOptions(t *testing.T) {
	t.Parallel()

	src := Handle{ID: nonZeroID()}
	dst := Handle{ID: nonZeroID()}

	// basic construction
	msg, err := Message(src, dst, "/be", map[string]string{"k": "v"})
	require.NoError(t, err)
	require.NotEmpty(t, msg.Options.Expire)
	require.Less(t, time.Now(), msg.Expiry())
	require.False(t, msg.IsBroadcast())

	// timeout helper
	msg, _ = Message(src, dst, "/be", nil, WithMessageTimeout(time.Second))
	require.InDelta(t,
		time.Now().Add(time.Second).UnixNano(),
		int64(msg.Options.Expire),
		5e7,
	)

	// explicit expiry helper
	ts := time.Now().Add(2 * time.Second)
	msg, err = Message(src, dst, "/be", nil, WithMessageExpiryTime(ts))
	require.NoError(t, err)
	require.Equal(t, uint64(ts.UnixNano()), msg.Options.Expire)

	// topic + replyTo + source helpers
	msg, err = Message(src, Handle{}, "/br", nil,
		WithMessageTopic("T"),
		WithMessageReplyTo("/rep"),
		WithMessageSource(dst),
	)
	require.NoError(t, err)
	require.True(t, msg.IsBroadcast())
	require.Equal(t, "/rep", msg.Options.ReplyTo)
	require.Equal(t, dst, msg.From)
	require.Equal(t, "T", msg.Options.Topic)
}

// WithMessageSignature: provide vs provide-broadcast vs bad context
func TestWithMessageSignaturePaths(t *testing.T) {
	t.Parallel()

	// point-to-point path (Provide)
	sec := NewSpySec(t)
	src := Handle{ID: sec.ID()}
	dst := Handle{}

	msg, _ := Message(src, dst, "/be", nil,
		WithMessageSignature(sec, nil, nil),
	)
	require.True(t, sec.ProvideCalled)
	require.False(t, sec.BroadcastCalled)
	require.NotZero(t, msg.Nonce)

	// broadcast path (ProvideBroadcast)
	secB := NewSpySec(t)
	srcB := Handle{ID: secB.ID(), DID: secB.DID()} // valid "From"

	msgB, _ := Message(
		srcB,
		Handle{}, // completely empty "To" → broadcast
		"/br",
		nil,
		WithMessageTopic("T"),
		WithMessageSignature(secB, nil, nil),
	)
	require.True(t, msgB.IsBroadcast())
	require.True(t, secB.BroadcastCalled)
	require.False(t, secB.ProvideCalled)

	// invalid security context → error
	badSec := NewSpySec(t)
	badEnv := Envelope{From: Handle{ID: crypto.ID{}}, To: dst}
	err := WithMessageSignature(badSec, nil, nil)(&badEnv)
	require.ErrorIs(t, err, ErrInvalidSecurityContext)
}

// ReplyTo helper
func TestMessageReplyTo(t *testing.T) {
	t.Parallel()

	src := Handle{ID: nonZeroID()}
	dst := Handle{ID: nonZeroID()}

	orig, _ := Message(src, dst, "/call", nil, WithMessageReplyTo("/r"))
	reply, err := ReplyTo(orig, map[string]int{"x": 1})
	require.NoError(t, err)
	require.Equal(t, orig.To, reply.From)
	require.Equal(t, orig.From, reply.To)
	require.Equal(t, "/r", reply.Behavior)

	// error path when ReplyTo is empty
	orig.Options.ReplyTo = ""
	_, err = ReplyTo(orig, nil)
	require.Error(t, err)
}

// Error paths in Message
func TestMessageErrorPaths(t *testing.T) {
	t.Parallel()

	// JSON marshal failure
	_, err := Message(Handle{}, Handle{}, "/be", make(chan int))
	require.Error(t, err)

	// option returning error
	badOpt := func(_ *Envelope) error { return ErrInvalidMessage }
	_, err = Message(Handle{}, Handle{}, "/x", nil, badOpt)
	require.Error(t, err)
}

// Misc envelope helpers
func TestEnvelopeHelpers(t *testing.T) {
	t.Parallel()

	msg := Envelope{Behavior: "/be", From: Handle{ID: nonZeroID()}}
	msg.Options.Expire = uint64(time.Now().Add(-time.Second).UnixNano())
	require.True(t, msg.Expired())

	// SignatureData should start with the canonical prefix
	out, err := msg.SignatureData()
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(out, signaturePrefix))
}
