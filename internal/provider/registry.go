package provider

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var providerTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Registry struct {
	mu        sync.RWMutex
	factories map[ProviderType]Factory
}

func NewRegistry(factories ...Factory) (*Registry, error) {
	registry := &Registry{factories: make(map[ProviderType]Factory, len(factories))}
	for _, factory := range factories {
		if err := registry.Register(factory); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(factory Factory) error {
	if factory == nil {
		return errors.New("provider factory is required")
	}
	providerType := factory.Type()
	metadata := factory.Metadata()
	if !providerTypePattern.MatchString(string(providerType)) || metadata.Type != providerType || strings.TrimSpace(metadata.DisplayName) == "" {
		return errors.New("provider factory metadata is invalid")
	}
	if metadata.DocumentationURL != "" {
		documentationURL, err := url.Parse(metadata.DocumentationURL)
		if err != nil || documentationURL.Scheme != "https" || documentationURL.Host == "" || documentationURL.User != nil || documentationURL.RawQuery != "" || documentationURL.Fragment != "" {
			return errors.New("provider documentation URL must be a trusted HTTPS URL")
		}
	}
	if err := factory.CredentialDescriptor().Validate(); err != nil {
		return fmt.Errorf("provider %s credential descriptor: %w", providerType, err)
	}
	if err := factory.AccountOptionsDescriptor().Validate(); err != nil {
		return fmt.Errorf("provider %s account options descriptor: %w", providerType, err)
	}
	capabilities := factory.Capabilities()
	if err := capabilities.Validate(); err != nil {
		return fmt.Errorf("provider %s capabilities: %w", providerType, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[providerType]; exists {
		return fmt.Errorf("provider factory %q is already registered", providerType)
	}
	r.factories[providerType] = factory
	return nil
}

func (r *Registry) Factory(providerType ProviderType) (Factory, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	factory, exists := r.factories[providerType]
	return factory, exists
}

func (r *Registry) Definitions() []ProviderDefinition {
	if r == nil {
		return []ProviderDefinition{}
	}
	r.mu.RLock()
	definitions := make([]ProviderDefinition, 0, len(r.factories))
	for _, factory := range r.factories {
		capabilities := factory.Capabilities()
		capabilities.SupportedRecordTypes = sortedRecordTypes(capabilities.SupportedRecordTypes)
		definitions = append(definitions, ProviderDefinition{
			Metadata:       factory.Metadata(),
			Credentials:    factory.CredentialDescriptor(),
			AccountOptions: factory.AccountOptionsDescriptor(),
			Capabilities:   capabilities,
		})
	}
	r.mu.RUnlock()
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Metadata.Type < definitions[j].Metadata.Type
	})
	return definitions
}
