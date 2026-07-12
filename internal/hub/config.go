package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const (
	configDirName  = "agent-deck"
	configFileName = "hub.toml"
	maxTargetBytes = 255
)

var (
	idPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	unitPattern = regexp.MustCompile(`^[A-Za-z0-9_.@:][A-Za-z0-9_.@:-]*\.service$`)
)

// ConfigPath returns the dedicated Hub inventory path using the XDG config
// directory. Hub inventory never falls back to Agent Deck's legacy directory.
func ConfigPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory for hub configuration: %w", err)
		}
		base = filepath.Join(home, ".config")
	} else if !filepath.IsAbs(base) {
		return "", fmt.Errorf("XDG_CONFIG_HOME must be an absolute path")
	}
	return filepath.Join(base, configDirName, configFileName), nil
}

// Load reads and strictly validates the inventory at ConfigPath.
func Load() (*Inventory, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadFile(path)
}

// LoadFile reads and strictly validates an inventory at path. It is exposed so
// callers can validate an explicit inventory without executing any actions.
func LoadFile(path string) (*Inventory, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read hub configuration: %w", err)
	}

	var inventory Inventory
	metadata, err := toml.Decode(string(data), &inventory)
	if err != nil {
		return nil, fmt.Errorf("decode hub configuration: %w", err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		return nil, fmt.Errorf("decode hub configuration: unknown field(s): %s", strings.Join(keys, ", "))
	}
	if err := Validate(&inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}

// Validate rejects an incomplete or unsafe inventory before any transport or
// subprocess layer can consume it.
func Validate(inventory *Inventory) error {
	if inventory == nil {
		return fmt.Errorf("validate hub configuration: inventory is nil")
	}
	if len(inventory.Hosts) == 0 {
		return fmt.Errorf("validate hub configuration: at least one host is required")
	}

	hostIDs := make(map[string]struct{}, len(inventory.Hosts))
	for hostIndex := range inventory.Hosts {
		host := &inventory.Hosts[hostIndex]
		prefix := fmt.Sprintf("host[%d]", hostIndex)
		if !idPattern.MatchString(host.ID) {
			return fmt.Errorf("validate hub configuration: %s.id is invalid", prefix)
		}
		if _, exists := hostIDs[host.ID]; exists {
			return fmt.Errorf("validate hub configuration: %s.id is duplicated", prefix)
		}
		hostIDs[host.ID] = struct{}{}
		if err := validateTarget(host.Target); err != nil {
			return fmt.Errorf("validate hub configuration: %s.target %w", prefix, err)
		}

		serviceIDs := make(map[string]struct{}, len(host.Services))
		for serviceIndex := range host.Services {
			service := &host.Services[serviceIndex]
			servicePrefix := fmt.Sprintf("%s.service[%d]", prefix, serviceIndex)
			if !idPattern.MatchString(service.ID) {
				return fmt.Errorf("validate hub configuration: %s.id is invalid", servicePrefix)
			}
			if _, exists := serviceIDs[service.ID]; exists {
				return fmt.Errorf("validate hub configuration: %s.id is duplicated", servicePrefix)
			}
			serviceIDs[service.ID] = struct{}{}
			if !unitPattern.MatchString(service.Unit) {
				return fmt.Errorf("validate hub configuration: %s.unit is invalid", servicePrefix)
			}
			if service.Scope != ScopeSystem && service.Scope != ScopeUser {
				return fmt.Errorf("validate hub configuration: %s.scope is invalid", servicePrefix)
			}
			if len(service.Actions) == 0 {
				return fmt.Errorf("validate hub configuration: %s.actions must not be empty", servicePrefix)
			}
			seenActions := make(map[Action]struct{}, len(service.Actions))
			for _, action := range service.Actions {
				if action != ActionStatus && action != ActionLogs && action != ActionRestart {
					return fmt.Errorf("validate hub configuration: %s.actions contains an invalid action", servicePrefix)
				}
				if _, exists := seenActions[action]; exists {
					return fmt.Errorf("validate hub configuration: %s.actions contains a duplicate", servicePrefix)
				}
				seenActions[action] = struct{}{}
			}
		}
	}
	return nil
}

func validateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.HasPrefix(target, "-") {
		return fmt.Errorf("must not begin with a hyphen")
	}
	if len(target) > maxTargetBytes {
		return fmt.Errorf("exceeds %d bytes", maxTargetBytes)
	}
	if !utf8.ValidString(target) {
		return fmt.Errorf("must be valid UTF-8")
	}
	for _, r := range target {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("contains a control character")
		}
	}
	return nil
}
