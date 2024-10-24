// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewBasicSecurityContext(t *testing.T) {
	sctx := generateSecurityContext(t)
	assert.NotNil(t, sctx)
	assert.NotEmpty(t, sctx.id.PublicKey)
}

func TestBasicSecurityContextNonce(t *testing.T) {
	sctx := generateSecurityContext(t)

	nonce1 := sctx.Nonce()
	nonce2 := sctx.Nonce()

	assert.Equal(t, nonce2, nonce1+1)
}

func TestBasicSecurityContext_SignAndVerify(t *testing.T) {
	sctx := generateSecurityContext(t)

	me := Handle{
		ID:  sctx.ID(),
		DID: sctx.DID(),
		Address: Address{
			HostID:       "123",
			InboxAddress: "111",
		},
	}

	msg, err := Message(me, me, "test", nil, WithMessageSignature(sctx, nil, nil))
	assert.NoError(t, err)

	err = sctx.Sign(&msg)
	assert.NoError(t, err)

	err = sctx.Verify(msg)
	assert.NoError(t, err)
}

func TestBasicSecurityContextNonceConcurrency(t *testing.T) {
	sctx := generateSecurityContext(t)

	const goroutines = 100
	var wg sync.WaitGroup
	nonceMap := make(map[uint64]struct{})
	var mx sync.Mutex

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nonce := sctx.Nonce()
			mx.Lock()
			nonceMap[nonce] = struct{}{}
			mx.Unlock()
		}()
	}

	wg.Wait()

	assert.Equal(t, goroutines, len(nonceMap))
}
