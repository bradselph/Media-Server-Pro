package config

import (
	"fmt"
	"reflect"
	"strings"
)

// GetValue gets a configuration value by dot-notation path.
// Uses the same normalizeFieldName logic as SetValue (lowercase, strip underscores)
// so paths like "hls.cdn_base_url" work consistently for both get and set.
func (m *Manager) GetValue(path string) (interface{}, error) {
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

func setReflectField(field reflect.Value, value interface{}, path string) error {
	if !field.CanSet() {
		return fmt.Errorf("cannot set config value: %s", path)
	}
	newVal := reflect.ValueOf(value)
	if !newVal.Type().ConvertibleTo(field.Type()) {
		return fmt.Errorf("type mismatch for config value: %s", path)
	}
	field.Set(newVal.Convert(field.Type()))
	return nil
}

// SetValue sets a configuration value by dot-notation path. It persists to disk
// on every call. For multiple updates, prefer batching or Update() to avoid
// repeated I/O and partial state on failure. SetValue does not call
// syncFeatureToggles() or resolveAbsolutePaths(); use Load() after overrides for that.
func (m *Manager) SetValue(path string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	parts := strings.Split(path, ".")
	field, err := navigateToField(reflect.ValueOf(m.config).Elem(), parts, path)
	if err != nil {
		return err
	}
	if err := setReflectField(field, value, path); err != nil {
		return err
	}
	return m.save()
}
