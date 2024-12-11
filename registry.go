// Copyright 2024, Nunet
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and limitations under the License.

package actor

import (
	"errors"
	"sync"
)

type Info struct {
	Addr     Handle
	Parent   Handle
	Children []Handle
}

type Registry interface {
	Actors() map[string]Info
	Add(a Handle, parent Handle, children []Handle) error
	Get(a Handle) (Info, bool)
	SetParent(a Handle, parent Handle) error
	GetParent(a Handle) (Handle, bool)
}

type registry struct {
	mx     sync.Mutex
	actors map[string]Info
}

func newRegistry() *registry {
	return &registry{
		actors: make(map[string]Info),
	}
}

func (r *registry) Actors() map[string]Info {
	r.mx.Lock()
	defer r.mx.Unlock()

	actors := make(map[string]Info, len(r.actors))
	for k, v := range r.actors {
		actors[k] = v
	}
	return actors
}

func (r *registry) Add(a Handle, parent Handle, children []Handle) error {
	r.mx.Lock()
	defer r.mx.Unlock()

	if _, ok := r.actors[a.Address.InboxAddress]; ok {
		return errors.New("actor already exists")
	}

	if children == nil {
		children = []Handle{}
	}

	r.actors[a.Address.InboxAddress] = Info{
		Addr:     a,
		Parent:   parent,
		Children: children,
	}

	return nil
}

func (r *registry) Get(a Handle) (Info, bool) {
	r.mx.Lock()
	defer r.mx.Unlock()

	info, ok := r.actors[a.Address.InboxAddress]
	return info, ok
}

func (r *registry) SetParent(a Handle, parent Handle) error {
	r.mx.Lock()
	defer r.mx.Unlock()

	info, ok := r.actors[a.Address.InboxAddress]
	if !ok {
		return errors.New("actor not found")
	}

	info.Parent = parent
	r.actors[a.Address.InboxAddress] = info
	return nil
}

func (r *registry) GetParent(a Handle) (Handle, bool) {
	r.mx.Lock()
	defer r.mx.Unlock()

	info, ok := r.actors[a.Address.InboxAddress]
	if !ok {
		return Handle{}, false
	}

	return info.Parent, true
}
