// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Core CRUD behaviour
func TestRegistryCore(t *testing.T) {
	t.Parallel()

	reg := newRegistry()

	alice := Handle{Address: Address{InboxAddress: "alice"}}
	bob := Handle{Address: Address{InboxAddress: "bob"}}
	parentCarol := Handle{Address: Address{InboxAddress: "carol"}}
	newParentDave := Handle{Address: Address{InboxAddress: "dave"}}

	// add + get
	require.NoError(t, reg.Add(alice, parentCarol, nil))
	info, ok := reg.Get(alice)
	require.True(t, ok)
	require.Equal(t, parentCarol, info.Parent)
	require.NotNil(t, info.Children)
	require.Empty(t, info.Children, "children slice should start empty")

	// duplicate add (should pass with warnning)
	err := reg.Add(alice, parentCarol, nil)
	require.NoError(t, err)

	// set-parent success
	require.NoError(t, reg.SetParent(alice, newParentDave))
	got, _ := reg.GetParent(alice)
	require.Equal(t, newParentDave, got)

	// set-parent on unknown actor
	err = reg.SetParent(bob, parentCarol)
	require.ErrorContains(t, err, "actor not found")

	// Actors() returns a copy
	snap := reg.Actors()
	origLen := len(reg.actors)
	delete(snap, alice.Address.InboxAddress) // mutate copy
	require.Equal(t, origLen, len(reg.actors))
}

// Missing-lookup cases
func TestRegistryMissingLookups(t *testing.T) {
	t.Parallel()

	reg := newRegistry()
	eve := Handle{Address: Address{InboxAddress: "eve"}}

	_, ok := reg.Get(eve)
	require.False(t, ok)

	_, ok = reg.GetParent(eve)
	require.False(t, ok)
}

// Add with explicit children slice
func TestRegistryAddWithChildren(t *testing.T) {
	t.Parallel()

	reg := newRegistry()
	parentCarol := Handle{Address: Address{InboxAddress: "carol"}}
	grandChildBob := Handle{Address: Address{InboxAddress: "gBob"}}
	grandChildAlice := Handle{Address: Address{InboxAddress: "gAlice"}}

	require.NoError(t,
		reg.Add(parentCarol,
			Handle{}, // no parent
			[]Handle{grandChildBob, grandChildAlice},
		),
	)

	info, _ := reg.Get(parentCarol)
	require.Len(t, info.Children, 2)
}

// Concurrency smoke-test (run with –race)
func TestRegistryConcurrency(t *testing.T) {
	t.Parallel()

	reg := newRegistry()
	wg := sync.WaitGroup{}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h := Handle{Address: Address{InboxAddress: fmt.Sprintf("peer%d", i)}}
			require.NoError(t, reg.Add(h, Handle{}, nil))
			_, _ = reg.Get(h)
		}(i)
	}
	wg.Wait()
	require.Equal(t, 100, len(reg.Actors()))
}
