/*
Copyright 2021 The Lynx Authors.

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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/smartxworks/lynx/pkg/apis/agent/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// AgentInfoLister helps list AgentInfos.
type AgentInfoLister interface {
	// List lists all AgentInfos in the indexer.
	List(selector labels.Selector) (ret []*v1alpha1.AgentInfo, err error)
	// Get retrieves the AgentInfo from the index for a given name.
	Get(name string) (*v1alpha1.AgentInfo, error)
	AgentInfoListerExpansion
}

// agentInfoLister implements the AgentInfoLister interface.
type agentInfoLister struct {
	indexer cache.Indexer
}

// NewAgentInfoLister returns a new AgentInfoLister.
func NewAgentInfoLister(indexer cache.Indexer) AgentInfoLister {
	return &agentInfoLister{indexer: indexer}
}

// List lists all AgentInfos in the indexer.
func (s *agentInfoLister) List(selector labels.Selector) (ret []*v1alpha1.AgentInfo, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.AgentInfo))
	})
	return ret, err
}

// Get retrieves the AgentInfo from the index for a given name.
func (s *agentInfoLister) Get(name string) (*v1alpha1.AgentInfo, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("agentinfo"), name)
	}
	return obj.(*v1alpha1.AgentInfo), nil
}
