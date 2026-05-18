package engine

import (
	"fmt"
	"sync"
	"time"

	"github.com/open-policy-agent/opa/rego"
	"kube-policy-engine/api/v1alpha1"
)

type PolicyMode string

const (
	Enforce PolicyMode = "enforce"
	Audit   PolicyMode = "audit"
)

type Policy struct {
	Name      string
	Mode      PolicyMode
	Targets   []v1alpha1.Target
	RegoSrc   string
	Compiled  *rego.PreparedEvalQuery
	Mutations []string
	Message   string
	UpdatedAt time.Time
}

type PolicyRegistry interface {
	Get(name string) (*Policy, bool)
	Set(policy *Policy) error
	Delete(name string)
	List() []*Policy
}

type registry struct {
	mu       sync.RWMutex
	policies map[string]*Policy
	engine   Engine
}

func NewRegistry(engine Engine) PolicyRegistry {
	return &registry{
		policies: make(map[string]*Policy),
		engine:   engine,
	}
}

func (r *registry) Get(name string) (*Policy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.policies[name]
	return p, ok
}

func (r *registry) Set(policy *Policy) error {
	compiled, err := r.engine.Compile(policy.RegoSrc)
	if err != nil {
		return fmt.Errorf("failed to compile rego for policy %s: %w", policy.Name, err)
	}
	policy.Compiled = compiled

	r.mu.Lock()
	defer r.mu.Unlock()
	r.policies[policy.Name] = policy
	return nil
}

func (r *registry) Delete(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.policies, name)
}

func (r *registry) List() []*Policy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Policy, 0, len(r.policies))
	for _, p := range r.policies {
		list = append(list, p)
	}
	return list
}
