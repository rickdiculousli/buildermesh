package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// tomlName is the key a struct field decodes from: its toml tag, else its
// name lowercased (the decoder matches an untagged field case-insensitively).
// "" for a field the file never sets.
func tomlName(f reflect.StructField) string {
	tag, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	switch tag {
	case "-":
		return ""
	case "":
		return strings.ToLower(f.Name)
	}
	return tag
}

// field is the value of section's key in v (a file), or false when the
// layout has no such key.
func field(v reflect.Value, section, key string) (reflect.Value, bool) {
	for i := 0; i < v.NumField(); i++ {
		sf := v.Type().Field(i)
		if tomlName(sf) != section || sf.Name == "Repos" {
			continue
		}
		sec := v.Field(i)
		if sec.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		for j := 0; j < sec.NumField(); j++ {
			if tomlName(sec.Type().Field(j)) == key {
				return sec.Field(j), true
			}
		}
	}
	return reflect.Value{}, false
}

// Known reports whether config.toml has the key `[section] key`: one this
// binary reads (a key it ignores, such as one bmesh dropped, is unknown).
func Known(section, key string) bool {
	_, ok := field(reflect.ValueOf(file{}), section, key)
	return ok
}

// KnownRepoKey reports whether a repos.toml row has key.
func KnownRepoKey(key string) bool {
	t := reflect.TypeOf(Repo{})
	for i := 0; i < t.NumField(); i++ {
		if tomlName(t.Field(i)) == key {
			return true
		}
	}
	return false
}

// Values reads path over the defaults the way Load does, but checks
// nothing, so it reads a file Load refuses (one an older bmesh wrote, or
// one this binary rejects): get is `[section] key`'s value as config.toml
// writes it (a duration as "6h", a list comma-separated), the default when
// the file doesn't set it, and false for a key the layout doesn't have. A
// missing file is the defaults.
func Values(path string) (get func(section, key string) (string, bool), err error) {
	c := defaults()
	f := c.file()
	push := append([]string{}, c.Notify.Push...)
	f.Notify.Push = &push
	if _, err := os.Stat(path); err == nil {
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
	}
	v := reflect.ValueOf(f)
	return func(section, key string) (string, bool) {
		fv, ok := field(v, section, key)
		if !ok {
			return "", false
		}
		return valueText(fv), true
	}, nil
}

// valueText is v as config.toml writes it.
func valueText(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Type() == reflect.TypeOf(duration(0)) {
		return FormatDuration(time.Duration(v.Int()))
	}
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = valueText(v.Index(i))
		}
		return strings.Join(parts, ",")
	}
	return fmt.Sprint(v.Interface())
}

// Literal is value as the TOML literal `[section] key` takes in
// config.toml: bare for a bool or a number (refused when value isn't
// one), a list of strings for a list (value comma-separated), else a
// quoted string.
func Literal(section, key, value string) (string, error) {
	fv, ok := field(reflect.ValueOf(file{}), section, key)
	if !ok {
		return "", fmt.Errorf("[%s] %s is not a key of config.toml", section, key)
	}
	return literal(fv.Type(), value)
}

func literal(t reflect.Type, value string) (string, error) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == reflect.TypeOf(duration(0)):
		if _, err := ParseDuration(value); err != nil {
			return "", err
		}
		return tomlString(value), nil
	case t.Kind() == reflect.Bool:
		if _, err := strconv.ParseBool(value); err != nil {
			return "", fmt.Errorf("%q: want true or false", value)
		}
		return value, nil
	case t.Kind() == reflect.Int || t.Kind() == reflect.Int64:
		if _, err := strconv.Atoi(value); err != nil {
			return "", fmt.Errorf("%q: want a whole number", value)
		}
		return value, nil
	case t.Kind() == reflect.Slice:
		var l []string
		for _, s := range strings.Split(value, ",") {
			if s = strings.TrimSpace(s); s != "" {
				l = append(l, s)
			}
		}
		return tomlList(l), nil
	}
	return tomlString(value), nil
}

// SetRepoKey sets key on r to value (a list comma-separated).
func SetRepoKey(r *Repo, key, value string) error {
	v := reflect.ValueOf(r).Elem()
	for i := 0; i < v.NumField(); i++ {
		if tomlName(v.Type().Field(i)) != key || key == "" {
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString(value)
		case reflect.Slice:
			var l []string
			for _, s := range strings.Split(value, ",") {
				if s = strings.TrimSpace(s); s != "" {
					l = append(l, s)
				}
			}
			f.Set(reflect.ValueOf(l))
		default:
			return fmt.Errorf("repos.toml %s: can't set a %s", key, f.Kind())
		}
		return r.check()
	}
	return fmt.Errorf("%s is not a key of a repos.toml row", key)
}
