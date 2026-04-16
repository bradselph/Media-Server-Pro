package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// GetValue gets a configuration value by dot-notation path.
// Uses the same normalizeFieldName logic as SetValue (lowercase, strip underscores)
// so paths like "hls.cdn_base_url" work consistently for both get and set.
func (m *Manager) GetValue(path string) (any, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	parts := strings.Split(path, ".")
	field, err := navigateToField(reflect.ValueOf(m.config).Elem(), parts, path)
	if err != nil {
		return nil, err
	}
	return field.Interface(), nil
}

func normalizeFieldName(name string) string {
	return strings.ToLower(strings.ReplaceAll(name, "_", ""))
}

func navigateToField(root reflect.Value, parts []string, path string) (reflect.Value, error) {
	v := root
	for _, part := range parts {
		if v.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf(errConfigPathNotFoundFmt, path)
		}
		normalized := normalizeFieldName(part)
		v = v.FieldByNameFunc(func(name string) bool {
			return normalizeFieldName(name) == normalized
		})
		if !v.IsValid() {
			return reflect.Value{}, fmt.Errorf(errConfigPathNotFoundFmt, path)
		}
	}
	return v, nil
}

func setReflectField(field reflect.Value, value any, path string) error {
	if !field.CanSet() {
		return fmt.Errorf("cannot set config value: %s", path)
	}
	newVal := reflect.ValueOf(value)
	if newVal.Type().ConvertibleTo(field.Type()) {
		field.Set(newVal.Convert(field.Type()))
		return nil
	}
	// For complex types (slices of structs, nested structs, etc.) that arrive
	// from JSON as []any or map[string]any, round-trip through
	// JSON so the standard decoder handles nested field mapping and coercion.
	return setReflectFieldViaJSON(field, value, path)
}

// setReflectFieldViaJSON merges the incoming value into the existing field
// value. For structs this preserves fields absent from the update; for slices
// and primitives it replaces outright (which is the correct behavior).
func setReflectFieldViaJSON(field reflect.Value, value any, path string) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("type mismatch for config value: %s (marshal: %w)", path, err)
	}
	// Start from the current field value so that struct fields not present in
	// the incoming JSON retain their existing values instead of being zeroed.
	target := reflect.New(field.Type())
	existing, marshalErr := json.Marshal(field.Interface())
	if marshalErr == nil {
		_ = json.Unmarshal(existing, target.Interface())
	}
	if err := json.Unmarshal(data, target.Interface()); err != nil {
		return fmt.Errorf("type mismatch for config value: %s (unmarshal: %w)", path, err)
	}
	field.Set(target.Elem())
	return nil
}

// SetValue sets a configuration value by dot-notation path. It persists to disk
// on every call. For multiple updates, use SetValuesBatch to avoid partial writes on failure.
func (m *Manager) SetValue(path string, value any) error {
	return m.SetValuesBatch(map[string]any{path: value})
}

// SetValuesBatch applies multiple configuration updates and persists once atomically.
// On save failure, in-memory changes are rolled back so the config stays consistent with disk.
// After saving, feature toggles are synced so runtime module enable/disable matches config.
// Watchers are called synchronously after the lock is released, matching Update()'s behaviour
// so that module side-effects (rate-limiter reconfiguration, security headers, etc.) are
// applied before the handler returns and cannot accumulate as unbounded goroutines.
func (m *Manager) SetValuesBatch(updates map[string]any) error {
	m.mu.Lock()

	// Snapshot for rollback on save failure
	originalJSON, snapErr := json.Marshal(m.config)
	if snapErr != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to snapshot config: %w", snapErr)
	}

	for path, value := range updates {
		parts := strings.Split(path, ".")
		field, err := navigateToField(reflect.ValueOf(m.config).Elem(), parts, path)
		if err != nil {
			m.mu.Unlock()
			return err
		}
		if err := setReflectField(field, value, path); err != nil {
			m.mu.Unlock()
			return err
		}
	}
	// Sync feature toggles BEFORE validation so module-level Enabled fields
	// reflect the new config. This matches the ordering in Load() and Update().
	m.syncFeatureToggles()
	if err := m.validate(); err != nil {
		m.rollbackFromJSON(originalJSON, err)
		m.mu.Unlock()
		return fmt.Errorf("config validation failed: %w", err)
	}
	if err := m.save(); err != nil {
		m.rollbackFromJSON(originalJSON, err)
		m.mu.Unlock()
		return err
	}
	// Snapshot watchers and config copy under the lock, then release before
	// calling them — consistent with Update() so watchers can safely call
	// m.Get() without deadlocking on m.mu.
	cfg := m.getCopy()
	watchers := make([]func(*Config), len(m.watchers))
	copy(watchers, m.watchers)
	m.mu.Unlock()

	// Notify watchers synchronously so modules (security, streaming, CORS, etc.)
	// pick up changes before the handler returns, not in a background goroutine.
	for _, w := range watchers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					m.log.Error("Config watcher panic recovered: %v", r)
				}
			}()
			w(cfg)
		}()
	}
	return nil
}
