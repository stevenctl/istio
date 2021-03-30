// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package memory

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
)

type controller struct {
	monitor     Monitor
	configStore model.ConfigStore
}

// NewController return an implementation of model.ConfigStoreCache
// This is a client-side monitor that dispatches events as the changes are being
// made on the client.
func NewController(cs model.ConfigStore) model.ConfigStoreCache {
	out := &controller{
		configStore: cs,
		monitor:     NewMonitor(cs),
	}
	return out
}

// NewSyncController return an implementation of model.ConfigStoreCache which processes events synchronously
func NewSyncController(cs model.ConfigStore) model.ConfigStoreCache {
	out := &controller{
		configStore: cs,
		monitor:     NewSyncMonitor(cs),
	}
	return out
}

func (c *controller) RegisterEventHandler(kind config.GroupVersionKind, f func(config.Config, config.Config, model.Event)) {
	c.monitor.AppendEventHandler(kind, f)
}

// Memory implementation is always synchronized with cache
func (c *controller) HasSynced() bool {
	return true
}

func (c *controller) SyncErr() error {
	return nil
}

func (c *controller) Run(stop <-chan struct{}) {
	c.monitor.Run(stop)
}

func (c *controller) Schemas() collection.Schemas {
	return c.configStore.Schemas()
}

func (c *controller) Get(kind config.GroupVersionKind, key, namespace string) *config.Config {
	return c.configStore.Get(kind, key, namespace)
}

func (c *controller) Create(config config.Config) (revision string, err error) {
	if revision, err = c.configStore.Create(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			config: config,
			event:  model.EventAdd,
		})
	}
	return
}

func (c *controller) Update(config config.Config) (newRevision string, err error) {
	oldconfig := c.configStore.Get(config.GroupVersionKind, config.Name, config.Namespace)
	if newRevision, err = c.configStore.Update(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    *oldconfig,
			config: config,
			event:  model.EventUpdate,
		})
	}
	return
}

func (c *controller) UpdateStatus(config config.Config) (newRevision string, err error) {
	oldconfig := c.configStore.Get(config.GroupVersionKind, config.Name, config.Namespace)
	if newRevision, err = c.configStore.UpdateStatus(config); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    *oldconfig,
			config: config,
			event:  model.EventUpdate,
		})
	}
	return
}

func (c *controller) Patch(orig config.Config, patchFn config.PatchFunc) (newRevision string, err error) {
	cfg, typ := patchFn(orig.DeepCopy())
	switch typ {
	case types.MergePatchType:
	case types.JSONPatchType:
	default:
		return "", fmt.Errorf("unsupported merge type: %s", typ)
	}
	if newRevision, err = c.configStore.Update(cfg); err == nil {
		c.monitor.ScheduleProcessEvent(ConfigEvent{
			old:    orig,
			config: cfg,
			event:  model.EventUpdate,
		})
	}
	return
}

func (c *controller) Delete(kind config.GroupVersionKind, key, namespace string, resourceVersion *string) (err error) {
	if config := c.Get(kind, key, namespace); config != nil {
		if err = c.configStore.Delete(kind, key, namespace, resourceVersion); err == nil {
			c.monitor.ScheduleProcessEvent(ConfigEvent{
				config: *config,
				event:  model.EventDelete,
			})
			return
		}
	}
	return errors.New("Delete failure: config" + key + "does not exist")
}

func (c *controller) List(kind config.GroupVersionKind, namespace string) ([]config.Config, error) {
	return c.configStore.List(kind, namespace)
}
