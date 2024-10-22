// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/aojea/kubernetes-controllers-workshop/apis/v1alpha1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/listers"
	"k8s.io/client-go/tools/cache"
)

// PulpoLister helps list Pulpos.
// All objects returned here must be treated as read-only.
type PulpoLister interface {
	// List lists all Pulpos in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.Pulpo, err error)
	// Pulpos returns an object that can list and get Pulpos.
	Pulpos(namespace string) PulpoNamespaceLister
	PulpoListerExpansion
}

// pulpoLister implements the PulpoLister interface.
type pulpoLister struct {
	listers.ResourceIndexer[*v1alpha1.Pulpo]
}

// NewPulpoLister returns a new PulpoLister.
func NewPulpoLister(indexer cache.Indexer) PulpoLister {
	return &pulpoLister{listers.New[*v1alpha1.Pulpo](indexer, v1alpha1.Resource("pulpo"))}
}

// Pulpos returns an object that can list and get Pulpos.
func (s *pulpoLister) Pulpos(namespace string) PulpoNamespaceLister {
	return pulpoNamespaceLister{listers.NewNamespaced[*v1alpha1.Pulpo](s.ResourceIndexer, namespace)}
}

// PulpoNamespaceLister helps list and get Pulpos.
// All objects returned here must be treated as read-only.
type PulpoNamespaceLister interface {
	// List lists all Pulpos in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.Pulpo, err error)
	// Get retrieves the Pulpo from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.Pulpo, error)
	PulpoNamespaceListerExpansion
}

// pulpoNamespaceLister implements the PulpoNamespaceLister
// interface.
type pulpoNamespaceLister struct {
	listers.ResourceIndexer[*v1alpha1.Pulpo]
}
