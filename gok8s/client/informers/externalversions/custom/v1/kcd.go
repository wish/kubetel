/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by informer-gen. DO NOT EDIT.

package v1

import (
	time "time"

	custom_v1 "github.com/wish/kubetel/gok8s/apis/custom/v1"
	versioned "github.com/wish/kubetel/gok8s/client/clientset/versioned"
	internalinterfaces "github.com/wish/kubetel/gok8s/client/informers/externalversions/internalinterfaces"
	v1 "github.com/wish/kubetel/gok8s/client/listers/custom/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// KCDInformer provides access to a shared informer and lister for
// KCDs.
type KCDInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1.KCDLister
}

type kCDInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewKCDInformer constructs a new informer for KCD type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewKCDInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredKCDInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredKCDInformer constructs a new informer for KCD type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredKCDInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options meta_v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CustomV1().KCDs(namespace).List(options)
			},
			WatchFunc: func(options meta_v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CustomV1().KCDs(namespace).Watch(options)
			},
		},
		&custom_v1.KCD{},
		resyncPeriod,
		indexers,
	)
}

func (f *kCDInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredKCDInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *kCDInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&custom_v1.KCD{}, f.defaultInformer)
}

func (f *kCDInformer) Lister() v1.KCDLister {
	return v1.NewKCDLister(f.Informer().GetIndexer())
}
